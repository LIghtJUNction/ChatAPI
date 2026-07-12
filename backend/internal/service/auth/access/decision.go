package access

import (
	"context"
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/decision"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
)

type PrincipalAdmissionKind string

const (
	PrincipalAdmissionUnknown  PrincipalAdmissionKind = "unknown"
	PrincipalAdmissionUser     PrincipalAdmissionKind = "user"
	PrincipalAdmissionSession  PrincipalAdmissionKind = "session"
	PrincipalAdmissionAppKey   PrincipalAdmissionKind = "app_key"
	PrincipalAdmissionModelKey PrincipalAdmissionKind = "model_key"
)

type PrincipalAdmissionDecision struct {
	Result decision.Result        `json:"result"`
	Kind   PrincipalAdmissionKind `json:"kind"`
	Key    string                 `json:"key,omitempty"`
}

func AllowPrincipalDecision() PrincipalAdmissionDecision {
	return PrincipalAdmissionDecision{
		Result: decision.Result{
			Effect: decision.EffectAllow,
		},
	}
}

func DenyPrincipalRateLimitDecision(kind PrincipalAdmissionKind, key string) PrincipalAdmissionDecision {
	return PrincipalAdmissionDecision{
		Result: decision.Deny(decision.SubjectNone, decision.ReasonRateLimited, 429, "principal rate limited", "principal_rate_limited"),
		Kind:   kind,
		Key:    key,
	}
}

func (s *Service) PrincipalAdmissionDecision(ctx context.Context) PrincipalAdmissionDecision {
	if s == nil || s.principalLimiter == nil {
		return AllowPrincipalDecision()
	}
	settings := s.currentSettings(ctx)
	subjects := principalSubjectsFromContext(ctx)
	if len(subjects) == 0 {
		return AllowPrincipalDecision()
	}
	if denied, ok := s.principalLimiter.FirstDenied(subjects, settings); ok {
		return DenyPrincipalRateLimitDecision(admissionKindFromSubject(denied.kind), denied.key)
	}
	return AllowPrincipalDecision()
}

func (s *Service) SessionCSRFDecision(ctx context.Context, r *http.Request, policyService *policy.Service) decision.Result {
	_, hasSession := sessionPrincipalFromContext(ctx)
	if !s.ShouldCheckSessionCSRF(r, hasSession) {
		return decision.Allow(decision.SubjectCSRF)
	}
	if s.ValidSessionCSRFSameOrigin(r) {
		return decision.Allow(decision.SubjectCSRF)
	}
	if policyService != nil {
		return policyService.SessionCSRFFailedDecision()
	}
	return decision.Deny(decision.SubjectCSRF, decision.ReasonSessionCSRF, 403, "csrf origin check failed", "csrf_origin_check_failed")
}

func sessionPrincipalFromContext(ctx context.Context) (string, bool) {
	principal, ok := session.PrincipalFromContext(ctx)
	if !ok {
		return "", false
	}
	return principal.UserID, true
}

func admissionKindFromSubject(kind string) PrincipalAdmissionKind {
	switch kind {
	case "user":
		return PrincipalAdmissionUser
	case "session":
		return PrincipalAdmissionSession
	case "app_key":
		return PrincipalAdmissionAppKey
	case "model_key":
		return PrincipalAdmissionModelKey
	default:
		return PrincipalAdmissionUnknown
	}
}
