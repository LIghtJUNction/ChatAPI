package access

import (
	"net/http"

	"github.com/zyf/chatapi/internal/config"
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
	baseOrigin := normalizedOrigin(s.cfg.BaseURL)
	requestOrigin := requestOrigin(r)
	origin := normalizedOrigin(r.Header.Get("Origin"))
	if origin == "" {
		origin = normalizedOrigin(r.Header.Get("Referer"))
	}
	if origin == "" {
		return false
	}
	return origin == requestOrigin || (baseOrigin != "" && origin == baseOrigin)
}
