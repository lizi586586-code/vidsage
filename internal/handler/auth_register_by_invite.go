package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler/dto"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// LoginByInvite combines password login and invited registration. Account
// existence is checked server-side; failed authentication never creates a user.
func (h *AuthHandler) LoginByInvite(c *gin.Context) {
	var req struct {
		Token    string `json:"token" binding:"required"`
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("请输入有效邮箱和密码"))
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	req.Email = strings.TrimSpace(req.Email)
	ctx := c.Request.Context()
	if h.invitationSvc == nil {
		c.Error(apperrors.NewInternalServerError("invitation service unavailable"))
		return
	}
	inv, err := h.invitationSvc.LookupByToken(ctx, req.Token)
	if err != nil {
		c.Error(&apperrors.AppError{Code: apperrors.ErrNotFound, Message: "邀请链接已失效，请联系老师重新发送", HTTPCode: http.StatusGone})
		return
	}
	user, err := h.userService.GetUserByEmail(ctx, req.Email)
	// Historical accounts may have been stored with mixed-case addresses,
	// while invited registration canonicalises new addresses to lowercase.
	// Try the canonical form after an exact miss so the same person cannot be
	// mistaken for a new account on a later invite.
	if errors.Is(err, repository.ErrUserNotFound) {
		canonicalEmail := strings.ToLower(req.Email)
		if canonicalEmail != req.Email {
			user, err = h.userService.GetUserByEmail(ctx, canonicalEmail)
		}
	}
	if err != nil && !errors.Is(err, repository.ErrUserNotFound) {
		c.Error(apperrors.NewInternalServerError("暂时无法登录，请稍后重试"))
		return
	}
	if user == nil {
		if service.ValidatePasswordPolicy(req.Password) != nil {
			c.Error(apperrors.NewValidationError("新密码需为 8–32 位，并包含字母和数字"))
			return
		}
		// Reuse invited registration's provisioning and rollback guarantees.
		h.registerInvitedAccount(c, registerByInviteRequest{
			Token: req.Token, Email: req.Email, Password: req.Password,
			Username: "学员_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16],
		})
		return
	}
	if !user.IsActive || h.userService.ValidatePassword(ctx, user.ID, req.Password) != nil {
		c.Error(apperrors.NewUnauthorizedError("邮箱或密码不正确"))
		return
	}
	if _, err := h.invitationSvc.AcceptByToken(ctx, req.Token, user.ID); err != nil {
		c.Error(&apperrors.AppError{Code: apperrors.ErrNotFound, Message: "邀请链接已失效，请联系老师重新发送", HTTPCode: http.StatusGone})
		return
	}
	response, err := h.userService.SwitchTenant(ctx, user, inv.TenantID, "")
	if err != nil {
		c.Error(apperrors.NewInternalServerError("暂时无法进入学习空间，请稍后重试"))
		return
	}
	c.JSON(http.StatusOK, dto.NewAuthLoginResponse(response))
}

// registerByInviteRequest is the body for /auth/register-by-invite.
//
// Email is collected from the invitee themselves: share-link rows have
// no specific invitee, so we cannot pre-bind any address. The token
// IS the authorisation; the email is whatever account the invitee
// wants to register.
type registerByInviteRequest struct {
	Token    string `json:"token"    binding:"required"`
	Email    string `json:"email"    binding:"required,email"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

// invitationLookupResponse is the public projection of a share-link
// row returned by /auth/invitations/lookup. Kept narrow on purpose:
// just enough for the registration page to render context ("X invited
// you to Y") without exposing inviter audit fields.
type invitationLookupResponse struct {
	TenantID          uint64           `json:"tenant_id"`
	TenantName        string           `json:"tenant_name,omitempty"`
	TenantDescription string           `json:"tenant_description,omitempty"`
	OwnerUsername     string           `json:"owner_username,omitempty"`
	Role              types.TenantRole `json:"role"`
	ExpiresAt         string           `json:"expires_at"`
}

// invitationLookupRequest carries the token in the request body
// instead of the URL path: GET /auth/invitations/:token would have
// landed the plaintext token in access logs, browser history, and
// tracing spans. POST + body keeps the token out of those surfaces
// at the cost of "this endpoint reads, why is it POST" — worth it
// because the token is the sole authorisation for registration.
type invitationLookupRequest struct {
	Token string `json:"token" binding:"required"`
}

// LookupInvitationByToken godoc
// @Summary      解析共享邀请链接 token
// @Description  根据邀请链接中的 token 返回邀请上下文（空间名 / 角色 / 过期时间），
// @Description  供注册页展示。无认证；token 无效或被撤销返回 410。
// @Description  使用 POST + body 而非 GET + path，避免 token 落入访问日志 / 浏览器历史 / tracing。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request  body      invitationLookupRequest  true  "邀请 token"
// @Success      200      {object}  invitationLookupResponse
// @Failure      410      {object}  apperrors.AppError  "链接无效或已撤销"
// @Router       /auth/invitations/lookup [post]
func (h *AuthHandler) LookupInvitationByToken(c *gin.Context) {
	ctx := c.Request.Context()

	if h.invitationSvc == nil {
		c.Error(apperrors.NewInternalServerError("invitation service unavailable"))
		return
	}

	var req invitationLookupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("token is required").WithDetails(err.Error()))
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		c.Error(apperrors.NewValidationError("token is required"))
		return
	}

	inv, err := h.invitationSvc.LookupByToken(ctx, token)
	if err != nil {
		// Collapse "unknown / expired / revoked" into 410 to avoid
		// leaking which slot a stolen token used to occupy.
		c.Error(&apperrors.AppError{
			Code:     apperrors.ErrNotFound,
			Message:  "invitation link is invalid or has been revoked",
			HTTPCode: http.StatusGone,
		})
		return
	}

	resp := invitationLookupResponse{
		TenantID:  inv.TenantID,
		Role:      inv.Role,
		ExpiresAt: inv.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	tenant, terr := h.tenantService.GetTenantByID(ctx, inv.TenantID)
	if terr != nil || tenant == nil {
		c.Error(apperrors.NewInternalServerError("暂时无法加载学习空间，请稍后重试"))
		return
	}
	resp.TenantName = tenant.Name
	resp.TenantDescription = tenant.Description
	// Ownership comes from membership, not the user's home tenant. An owner
	// may create several spaces without changing their home tenant.
	if h.userService == nil {
		c.Error(apperrors.NewInternalServerError("暂时无法加载学习空间，请稍后重试"))
		return
	}
	owner, uerr := h.userService.GetTenantOwner(ctx, inv.TenantID)
	if uerr != nil || owner == nil {
		c.Error(apperrors.NewInternalServerError("暂时无法加载学习空间，请稍后重试"))
		return
	}
	resp.OwnerUsername = owner.Username
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// RegisterByInvite godoc
// @Summary      使用共享链接注册
// @Description  通过 Owner 生成的共享邀请链接 token 完成注册，绕过 invite_only 模式拦截。
// @Description  注册者自填邮箱（与 token 不绑定）；注册成功后自动加入对应空间。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request  body      registerByInviteRequest  true  "邀请注册请求"
// @Success      201      {object}  types.LoginResponse
// @Failure      400      {object}  apperrors.AppError  "请求参数错误"
// @Failure      409      {object}  apperrors.AppError  "邮箱已注册"
// @Failure      410      {object}  apperrors.AppError  "链接无效或已撤销"
// @Router       /auth/register-by-invite [post]
//
// RegisterByInvite is intentionally NOT subject to the invite_only gate:
// the gate suppresses public registration, while this endpoint requires
// a valid token issued by an Owner. The token IS the authorisation.
func (h *AuthHandler) RegisterByInvite(c *gin.Context) {
	if h.invitationSvc == nil {
		c.Error(apperrors.NewInternalServerError("invitation service unavailable"))
		return
	}

	var req registerByInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("Invalid registration parameters").WithDetails(err.Error()))
		return
	}
	h.registerInvitedAccount(c, req)
}

func (h *AuthHandler) registerInvitedAccount(c *gin.Context, req registerByInviteRequest) {
	ctx := c.Request.Context()
	req.Token = strings.TrimSpace(req.Token)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Username = strings.TrimSpace(req.Username)
	// Password is intentionally NOT sanitized: SanitizeForLog strips
	// \n, \r, \t and control chars to make a string safe to write into
	// a log line — applying it to a real password would silently
	// rewrite the stored credential and lock the user out. Passwords
	// must never be logged, so they don't need that defence here.
	if req.Token == "" || req.Email == "" || req.Username == "" || req.Password == "" {
		c.Error(apperrors.NewValidationError("token, email, username and password are required"))
		return
	}

	inv, err := h.invitationSvc.LookupByToken(ctx, req.Token)
	if err != nil {
		c.Error(&apperrors.AppError{
			Code:     apperrors.ErrNotFound,
			Message:  "invitation link is invalid or has been revoked",
			HTTPCode: http.StatusGone,
		})
		return
	}

	// If the email already has an account we shouldn't quietly succeed
	// here: register would fail with a duplicate-email error, but the
	// share-link flow is "create new account + join tenant", not "join
	// my existing account". Surface 409 so the SPA can prompt them to
	// log in instead, and the invitation can be applied to their
	// existing account via /me/invitations once we wire that path.
	existing, lookupErr := h.userService.GetUserByEmail(ctx, req.Email)
	if lookupErr != nil && !errors.Is(lookupErr, repository.ErrUserNotFound) {
		c.Error(apperrors.NewInternalServerError("暂时无法创建账号，请稍后重试"))
		return
	}
	if existing != nil {
		c.Error(apperrors.NewConflictError(
			"this email already has an account; please log in to join the workspace"))
		return
	}

	user, err := h.userService.Register(ctx, &types.RegisterRequest{
		Username:           req.Username,
		Email:              req.Email,
		Password:           req.Password,
		TenantProvisioning: types.TenantProvisioningTenantless,
	})
	if err != nil {
		logger.Errorf(ctx, "register-by-invite: user create failed for %s: %v",
			secutils.SanitizeForLog(req.Email), err)
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}

	// The invited tenant becomes the user's initial/default tenant. No
	user.TenantID = inv.TenantID
	if err := h.userService.UpdateUser(ctx, user); err != nil {
		logger.Errorf(ctx, "register-by-invite: failed to set home tenant for user %s: %v", user.ID, err)
		_ = h.userService.DeleteUser(ctx, user.ID)
		c.Error(apperrors.NewInternalServerError("failed to finalise invited account").WithDetails(err.Error()))
		return
	}

	if _, err := h.invitationSvc.AcceptByToken(ctx, req.Token, user.ID); err != nil {
		// Race: link was revoked between Lookup and Accept. Keep the new
		// account, but restore it to tenantless so it does not point at a
		// tenant for which no membership was created. If even that repair
		// fails, remove the half-provisioned identity.
		logger.Errorf(ctx, "register-by-invite: accept failed for user %s: %v", user.ID, err)
		user.TenantID = 0
		if rollbackErr := h.userService.UpdateUser(ctx, user); rollbackErr != nil {
			logger.Errorf(ctx, "register-by-invite: failed to restore tenantless user %s: %v", user.ID, rollbackErr)
			_ = h.userService.DeleteUser(ctx, user.ID)
		}
		c.Error(&apperrors.AppError{
			Code:     apperrors.ErrNotFound,
			Message:  "invitation link is no longer valid; please log in to your new account",
			HTTPCode: http.StatusGone,
		})
		return
	}

	accessToken, refreshToken, err := h.userService.GenerateTokens(ctx, user)
	if err != nil {
		logger.Errorf(ctx, "register-by-invite: token generation failed for user %s: %v", user.ID, err)
		c.Error(apperrors.NewInternalServerError("token generation failed").WithDetails(err.Error()))
		return
	}

	tenant, _ := h.tenantService.GetTenantByID(ctx, inv.TenantID)
	c.JSON(http.StatusCreated, dto.NewAuthLoginResponse(&types.LoginResponse{
		Success:      true,
		Message:      "Registration successful",
		User:         user,
		ActiveTenant: tenant,
		Memberships: []types.Membership{{
			TenantID:   inv.TenantID,
			TenantName: tenantNameOrEmpty(tenant),
			Role:       inv.Role,
		}},
		Token:        accessToken,
		RefreshToken: refreshToken,
	}))
}
