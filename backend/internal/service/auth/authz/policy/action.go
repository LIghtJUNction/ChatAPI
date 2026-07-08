package policy

import (
	app "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	"github.com/zyf/chatapi/internal/service/auth/authz/decision"
	"github.com/zyf/chatapi/internal/service/auth/authz/principal"
)

func (s *Service) CanAccessWeb(pr principal.Principal) bool {
	return s.IsHumanSession(pr)
}

func (s *Service) CanManageUsers(pr principal.Principal) bool {
	return s.IsAdmin(pr)
}

func (s *Service) CanManageRuntime(pr principal.Principal) bool {
	return s.IsAdmin(pr)
}

func (s *Service) CanManageSystemConfig(pr principal.Principal) bool {
	return s.IsAdmin(pr)
}

func (s *Service) CanManageAudit(pr principal.Principal) bool {
	return s.IsAdmin(pr)
}

func (s *Service) CanCreateAppAPIKey(pr principal.Principal) bool {
	return s.IsHumanSession(pr)
}

func (s *Service) CanCreateModelAPIKey(pr principal.Principal) bool {
	return s.IsHumanSession(pr)
}

func (s *Service) CanUseAppAPI(pr principal.Principal) bool {
	return s.IsAppAPIKey(pr)
}

func (s *Service) CanUseVirtualModelAPI(pr principal.Principal) bool {
	return s.IsModelAPIKey(pr)
}

func (s *Service) CanUseAdminAPI(pr principal.Principal) bool {
	return s.IsHumanSession(pr) && s.IsAdmin(pr)
}

func (s *Service) CanUseAppScope(pr app.Principal, scope string) bool {
	if !s.IsAppAPIKey(pr.AuthPrincipal()) {
		return false
	}
	_, ok := pr.Scopes[scope]
	return ok
}

func (s *Service) CanUseAppSourceIP(pr app.Principal, sourceIP string, allow func(app.Principal, string) bool) bool {
	if !s.IsAppAPIKey(pr.AuthPrincipal()) {
		return false
	}
	return allow(pr, sourceIP)
}

func (s *Service) CanUseAppRateLimit(pr app.Principal, allow func(app.Principal) bool) bool {
	if !s.IsAppAPIKey(pr.AuthPrincipal()) {
		return false
	}
	return allow(pr)
}

func (s *Service) UserOrAppDecision(sessionPresent bool, appPresent bool) decision.Result {
	switch {
	case sessionPresent:
		return decision.Allow(decision.SubjectSession)
	case appPresent:
		return decision.Allow(decision.SubjectAppAPIKey)
	default:
		return decision.Deny(decision.SubjectNone, decision.ReasonUnauthorized, 401, "session unauthorized", "session_unauthorized")
	}
}

func (s *Service) AppAPIUnauthorizedDecision() decision.Result {
	return decision.Deny(decision.SubjectAppAPIKey, decision.ReasonUnauthorized, 401, "app api key unauthorized", "unauthorized")
}

func (s *Service) AppAPISourceIPForbiddenDecision() decision.Result {
	return decision.Deny(decision.SubjectAppAPIKey, decision.ReasonSourceIP, 403, "app api key source ip forbidden", "source_ip_forbidden")
}

func (s *Service) AppAPIForbiddenDecision() decision.Result {
	return decision.Deny(decision.SubjectAppAPIKey, decision.ReasonForbidden, 403, "app api key forbidden", "forbidden")
}

func (s *Service) AppAPIRateLimitedDecision() decision.Result {
	return decision.Deny(decision.SubjectAppAPIKey, decision.ReasonRateLimited, 429, "app api key rate limited", "rate_limited")
}

func (s *Service) ModelAPIUnauthorizedDecision() decision.Result {
	return decision.Deny(decision.SubjectModelAPIKey, decision.ReasonUnauthorized, 401, "model api key unauthorized", "unauthorized")
}

func (s *Service) SessionUnauthorizedDecision() decision.Result {
	return decision.Deny(decision.SubjectSession, decision.ReasonUnauthorized, 401, "session unauthorized", "session_unauthorized")
}

func (s *Service) AdminSessionUnauthorizedDecision() decision.Result {
	return decision.Deny(decision.SubjectAdminAPI, decision.ReasonUnauthorized, 401, "admin session unauthorized", "session_unauthorized")
}

func (s *Service) AdminForbiddenDecision() decision.Result {
	return decision.Deny(decision.SubjectAdminAPI, decision.ReasonForbidden, 403, "admin forbidden", "forbidden")
}

func (s *Service) SessionCSRFFailedDecision() decision.Result {
	return decision.Deny(decision.SubjectCSRF, decision.ReasonSessionCSRF, 403, "csrf origin check failed", "csrf_origin_check_failed")
}
