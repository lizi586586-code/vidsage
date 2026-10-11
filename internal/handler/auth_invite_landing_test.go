package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type inviteLandingUsers struct {
	invitedRegistrationUserService
	existing    *types.User
	lookupErr   error
	passwordErr error
	registered  bool
	switched    uint64
}

func (s *inviteLandingUsers) GetUserByEmail(context.Context, string) (*types.User, error) {
	return s.existing, s.lookupErr
}

func (s *inviteLandingUsers) GetTenantOwner(context.Context, uint64) (*types.User, error) {
	return &types.User{ID: "owner", Username: "张三", Email: "private@example.com"}, nil
}

func (s *inviteLandingUsers) Register(ctx context.Context, req *types.RegisterRequest) (*types.User, error) {
	s.registered = true
	return s.invitedRegistrationUserService.Register(ctx, req)
}

func (s *inviteLandingUsers) ValidatePassword(context.Context, string, string) error {
	return s.passwordErr
}

func (s *inviteLandingUsers) SwitchTenant(_ context.Context, user *types.User, tenantID uint64, _ string) (*types.LoginResponse, error) {
	s.switched = tenantID
	return &types.LoginResponse{Success: true, User: user, Token: "access", RefreshToken: "refresh", ActiveTenant: &types.Tenant{ID: tenantID}}, nil
}

type inviteLandingInvitations struct {
	interfaces.TenantInvitationService
	lookupErr error
	accepted  bool
}

func (s *inviteLandingInvitations) LookupByToken(context.Context, string) (*types.TenantInvitation, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return &types.TenantInvitation{TenantID: 42, Role: types.TenantRoleViewer}, nil
}

func (s *inviteLandingInvitations) AcceptByToken(context.Context, string, string) (*types.TenantMember, error) {
	s.accepted = true
	return &types.TenantMember{TenantID: 42, Role: types.TenantRoleViewer}, nil
}

type inviteLandingTenant struct{ interfaces.TenantService }

func (*inviteLandingTenant) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 42, Name: "前沿大模型与应用实战空间", Description: "老师的真实空间介绍"}, nil
}

func TestInviteLookupReturnsSpaceAndOwnerWithoutPrivateFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AuthHandler{userService: &inviteLandingUsers{}, tenantService: &inviteLandingTenant{}, invitationSvc: &inviteLandingInvitations{}}
	r := gin.New()
	r.Use(errorCapture())
	r.POST("/lookup", h.LookupInvitationByToken)
	req := httptest.NewRequest(http.MethodPost, "/lookup", strings.NewReader(`{"token":"valid"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var result struct {
		Success bool
		Data    invitationLookupResponse
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !result.Success || result.Data.OwnerUsername != "张三" || result.Data.TenantName != "前沿大模型与应用实战空间" || result.Data.TenantDescription != "老师的真实空间介绍" {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private@example.com") || strings.Contains(w.Body.String(), "owner\"") {
		t.Fatal("lookup exposed private owner fields")
	}
}

func TestInviteLoginRegistersOnlyMissingAccountsAndAuthenticatesExistingUsers(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		existing                          *types.User
		lookupErr, passwordErr, inviteErr error
		password                          string
		status                            int
		register, accept                  bool
	}{
		{name: "new account", lookupErr: repository.ErrUserNotFound, status: http.StatusCreated, register: true, accept: true},
		{name: "existing account", existing: &types.User{ID: "existing", IsActive: true}, status: http.StatusOK, accept: true},
		{name: "legacy password", existing: &types.User{ID: "existing", IsActive: true}, password: "oldpwd", status: http.StatusOK, accept: true},
		{name: "wrong password", existing: &types.User{ID: "existing", IsActive: true}, passwordErr: errors.New("mismatch"), status: http.StatusUnauthorized},
		{name: "disabled account", existing: &types.User{ID: "existing"}, status: http.StatusUnauthorized},
		{name: "database failure", lookupErr: errors.New("offline"), status: http.StatusInternalServerError},
		{name: "revoked invite", inviteErr: errors.New("revoked"), status: http.StatusGone},
		{name: "weak new password", password: "abcdefgh", status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			users := &inviteLandingUsers{existing: tc.existing, lookupErr: tc.lookupErr, passwordErr: tc.passwordErr}
			invites := &inviteLandingInvitations{lookupErr: tc.inviteErr}
			h := &AuthHandler{userService: users, tenantService: &inviteLandingTenant{}, invitationSvc: invites}
			r := gin.New()
			r.Use(errorCapture())
			r.POST("/login", h.LoginByInvite)
			password := tc.password
			if password == "" {
				password = "valid1234"
			}
			body, _ := json.Marshal(map[string]string{"token": "invite", "email": "learner@example.com", "password": password})
			req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status || users.registered != tc.register || invites.accepted != tc.accept {
				t.Fatalf("status=%d body=%s registered=%v accepted=%v", w.Code, w.Body.String(), users.registered, invites.accepted)
			}
			if tc.existing != nil && tc.accept && users.switched != 42 {
				t.Fatal("existing account did not enter invited space")
			}
			if tc.register && users.registeredMode != types.TenantProvisioningTenantless {
				t.Fatal("new account created an unwanted personal space")
			}
		})
	}
}
