package policy_test

import (
	"testing"

	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
	"github.com/zyf/chatapi/internal/service/auth/authz/principal"
	"github.com/zyf/chatapi/internal/store"
)

func TestSessionPrincipalMarksAdminUser(t *testing.T) {
	svc := policy.NewService()
	pr := svc.SessionPrincipal(store.User{
		ID:         "user_admin",
		Username:   "alice",
		Email:      "alice@example.com",
		Role:       "user",
		LocalAdmin: true,
	}, "sess_123", "password")

	if !svc.IsAuthenticated(pr) || !svc.IsHumanSession(pr) {
		t.Fatalf("expected authenticated human session: %#v", pr)
	}
	if !svc.IsAdmin(pr) || pr.Role != "admin" || !pr.IsAdmin {
		t.Fatalf("expected admin principal: %#v", pr)
	}
}

func TestCanAccessUserAllowsOwnerAndAdmin(t *testing.T) {
	svc := policy.NewService()
	owner := principal.Principal{
		Kind:      principal.KindHumanSession,
		SubjectID: "sess_owner",
		UserID:    "user_1",
		Role:      "user",
	}
	admin := principal.Principal{
		Kind:      principal.KindHumanSession,
		SubjectID: "sess_admin",
		UserID:    "admin_1",
		Role:      "admin",
		IsAdmin:   true,
	}
	other := principal.Principal{
		Kind:      principal.KindHumanSession,
		SubjectID: "sess_other",
		UserID:    "user_2",
		Role:      "user",
	}

	if !svc.CanAccessUser(owner, "user_1") {
		t.Fatal("expected owner access")
	}
	if svc.CanAccessUser(other, "user_1") {
		t.Fatal("did not expect cross-user access")
	}
	if !svc.CanAccessUser(admin, "user_1") {
		t.Fatal("expected admin cross-user access")
	}
}

func TestActionPredicates(t *testing.T) {
	svc := policy.NewService()
	human := principal.Principal{
		Kind:      principal.KindHumanSession,
		SubjectID: "sess_1",
		UserID:    "user_1",
		Role:      "user",
	}
	app := principal.Principal{
		Kind:      principal.KindAppAPIKey,
		SubjectID: "app_1",
		UserID:    "user_1",
		Role:      "app_api",
	}
	model := principal.Principal{
		Kind:      principal.KindModelAPIKey,
		SubjectID: "model_1",
		UserID:    "user_1",
		Role:      "model_api",
	}

	if !svc.CanAccessWeb(human) || !svc.CanCreateAppAPIKey(human) || !svc.CanCreateModelAPIKey(human) {
		t.Fatal("expected human session capabilities")
	}
	if !svc.CanUseAppAPI(app) {
		t.Fatal("expected app api capability")
	}
	if !svc.CanUseVirtualModelAPI(model) {
		t.Fatal("expected model api capability")
	}
	if svc.CanManageUsers(human) {
		t.Fatal("did not expect non-admin user management capability")
	}
}
