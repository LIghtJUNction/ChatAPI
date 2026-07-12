package access

import (
	"net/http"

	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
)

func (s *Service) Lab() *labauth.Service {
	if s == nil {
		return nil
	}
	return s.lab
}

func (s *Service) EvaluateLabAccess(r *http.Request) LabDecision {
	if s == nil || s.lab == nil || !s.lab.Enabled() || !s.lab.RequiresGate() {
		return LabDecision{Kind: LabDecisionAllow}
	}
	if isLabPublicPath(r) {
		return LabDecision{Kind: LabDecisionAllow}
	}
	if s.lab.HasCookieAccess(r) {
		return LabDecision{Kind: LabDecisionAllow}
	}
	if granted, redirect := s.lab.CanGrant(r); granted {
		decision := LabDecision{Kind: LabDecisionGrant}
		if redirect {
			decision.RedirectTo = s.lab.RedirectTarget(r)
		}
		return decision
	}
	if s.lab.ShouldRenderPasswordPage(r) {
		return LabDecision{Kind: LabDecisionRender}
	}
	return LabDecision{Kind: LabDecisionDeny}
}

func (s *Service) ApplyLabGrant(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.lab == nil {
		return
	}
	s.lab.GrantIfValid(w, r)
}

func (s *Service) ShouldInjectLabActor(r *http.Request) bool {
	return s != nil && s.lab != nil && s.lab.Enabled() && !isLabPublicPath(r)
}
