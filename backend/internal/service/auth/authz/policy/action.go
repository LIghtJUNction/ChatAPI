package policy

import "github.com/zyf/chatapi/internal/service/auth/authz/principal"

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
