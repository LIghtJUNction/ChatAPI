package policy

import (
	"strings"

	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/principal"
)

func (s *Service) CanAccessUser(pr principal.Principal, targetUserID string) bool {
	targetUserID = strings.TrimSpace(targetUserID)
	if targetUserID == "" || !s.IsAuthenticated(pr) {
		return false
	}
	if s.IsAdmin(pr) {
		return true
	}
	return strings.TrimSpace(pr.UserID) == targetUserID
}

func (s *Service) CanReadRequest(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}

func (s *Service) CanManageRequest(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}

func (s *Service) CanReadConversation(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}

func (s *Service) CanManageConversation(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}

func (s *Service) CanManageAppKey(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}

func (s *Service) CanManageModelKey(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}

func (s *Service) CanManageIdentity(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}

func (s *Service) CanManageSession(pr principal.Principal, ownerUserID string) bool {
	return s.CanAccessUser(pr, ownerUserID)
}
