package oidc

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func TestRoleForEmailLeavesSuperAdminAsSessionCapability(t *testing.T) {
	svc := &Service{cfg: config.Config{
		SuperAdminEmail: "me@kirari.fun",
		OIDCAdminEmails: []string{"team-admin@kirari.fun"},
	}}
	if got := svc.roleForEmail("me@kirari.fun", true); got != "user" {
		t.Fatalf("superadmin role = %q", got)
	}
	if got := svc.roleForEmail("team-admin@kirari.fun", true); got != "admin" {
		t.Fatalf("admin role = %q", got)
	}
	if got := svc.roleForEmail("me@kirari.fun", false); got != "user" {
		t.Fatalf("unverified superadmin role = %q", got)
	}
}

func TestNextRolePreservesManuallyPromotedOIDCAdmin(t *testing.T) {
	svc := &Service{cfg: config.Config{SuperAdminEmail: "me@kirari.fun"}}
	user := common.User{Role: "admin"}
	if got := svc.nextRole(user, "other@kirari.fun", true); got != "admin" {
		t.Fatalf("manually assigned role was not preserved: %q", got)
	}
	if got := svc.nextRole(user, "me@kirari.fun", true); got != "admin" {
		t.Fatalf("persisted administrator role changed unexpectedly = %q", got)
	}
}
