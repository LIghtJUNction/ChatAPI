package session

import (
	"context"

	"github.com/zyf/chatapi/internal/service/auth/authz/decision"
	"github.com/zyf/chatapi/internal/service/auth/authz/policy"
)

func RequireDecision(ctx context.Context, policyService *policy.Service) decision.Result {
	_, ok := PrincipalFromContext(ctx)
	if !ok {
		if policyService != nil {
			return policyService.SessionUnauthorizedDecision()
		}
		return decision.Deny(decision.SubjectSession, decision.ReasonUnauthorized, 401, "session unauthorized", "session_unauthorized")
	}
	return decision.Allow(decision.SubjectSession)
}

func RequireAdminDecision(ctx context.Context, policyService *policy.Service) decision.Result {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		if policyService != nil {
			return policyService.AdminSessionUnauthorizedDecision()
		}
		return decision.Deny(decision.SubjectAdminAPI, decision.ReasonUnauthorized, 401, "admin session unauthorized", "session_unauthorized")
	}
	if policyService != nil && !policyService.CanUseAdminAPI(principal) {
		return policyService.AdminForbiddenDecision()
	}
	return decision.Allow(decision.SubjectAdminAPI)
}
