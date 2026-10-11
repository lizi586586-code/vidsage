package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type spaceOwnerRepo struct{ interfaces.UserRepository }

func (*spaceOwnerRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	return &types.User{ID: id, Username: "老师", TenantID: 7, IsActive: true}, nil
}

type spaceOwnerMembers struct{ interfaces.TenantMemberService }

func (*spaceOwnerMembers) ListByTenant(_ context.Context, tenantID uint64) ([]*types.TenantMember, error) {
	return []*types.TenantMember{
		nil,
		{TenantID: tenantID, UserID: "former-owner", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusSuspended},
		{TenantID: tenantID, UserID: "current-owner", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
	}, nil
}

func TestGetTenantOwnerUsesActiveOwnershipOfSecondarySpace(t *testing.T) {
	svc := &userService{userRepo: &spaceOwnerRepo{}, memberService: &spaceOwnerMembers{}}
	owner, err := svc.GetTenantOwner(context.Background(), 42)
	if err != nil || owner == nil || owner.ID != "current-owner" || owner.TenantID != 7 {
		t.Fatalf("owner=%+v err=%v; expected active owner whose home space differs", owner, err)
	}
}
