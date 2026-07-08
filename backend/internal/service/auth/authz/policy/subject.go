package policy

import (
	"strings"

	"github.com/zyf/chatapi/internal/service/auth/authz/principal"
	"github.com/zyf/chatapi/internal/store"
)

func (s *Service) IsAuthenticated(pr principal.Principal) bool {
	return pr.Valid()
}

func (s *Service) IsHumanSession(pr principal.Principal) bool {
	return pr.Kind == principal.KindHumanSession && pr.Valid()
}

func (s *Service) IsAppAPIKey(pr principal.Principal) bool {
	return pr.Kind == principal.KindAppAPIKey && pr.Valid()
}

func (s *Service) IsModelAPIKey(pr principal.Principal) bool {
	return pr.Kind == principal.KindModelAPIKey && pr.Valid()
}

func (s *Service) IsAdmin(pr principal.Principal) bool {
	if !pr.Valid() {
		return false
	}
	if pr.IsAdmin {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(pr.Role), "admin")
}

func (s *Service) IsAdminUser(user store.User) bool {
	if user.LocalAdmin {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(user.Role), "admin")
}

func (s *Service) EffectiveRole(user store.User) string {
	if s.IsAdminUser(user) {
		return "admin"
	}
	role := strings.TrimSpace(user.Role)
	if role == "" {
		return "user"
	}
	return role
}

func (s *Service) SessionPrincipal(user store.User, sessionID string, authMethod string) principal.Principal {
	return principal.Principal{
		Kind:       principal.KindHumanSession,
		SubjectID:  strings.TrimSpace(sessionID),
		UserID:     strings.TrimSpace(user.ID),
		Username:   firstNonEmpty(strings.TrimSpace(user.Username), strings.TrimSpace(user.Email)),
		Role:       s.EffectiveRole(user),
		IsAdmin:    s.IsAdminUser(user),
		Source:     "session",
		EntryPoint: "web",
		AuthMethod: strings.TrimSpace(authMethod),
	}
}
