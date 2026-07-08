package access

import (
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/config"
)

func (s *Service) ShouldCheckSessionCSRF(r *http.Request, hasSessionPrincipal bool) bool {
	if s == nil {
		return false
	}
	if s.cfg.Mode == config.ModeLab || !hasSessionPrincipal || !requiresCSRFCheck(r) || isBearerAPIKeyRequest(r) {
		return false
	}
	return true
}

func (s *Service) ValidSessionCSRFSameOrigin(r *http.Request) bool {
	if s == nil {
		return false
	}
	origin := normalizedOrigin(r.Header.Get("Origin"))
	if origin == "" {
		origin = normalizedOrigin(r.Header.Get("Referer"))
	}
	if origin == "" {
		return false
	}
	if origin == normalizedOrigin(requestOrigin(r)) {
		return true
	}
	_, ok := s.trustedOrigins[origin]
	return ok
}
