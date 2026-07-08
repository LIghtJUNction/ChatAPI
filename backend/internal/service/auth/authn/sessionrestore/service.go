package sessionrestore

import (
	"context"
	"errors"
	"net/http"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/service/auth/authz/principal"
	sessionsvc "github.com/zyf/chatapi/internal/service/auth/authz/session"
)

type Result struct {
	Principal   principal.Principal
	Claims      sessionsvc.Claims
	Actor       actor.Actor
	Found       bool
	ClearCookie bool
	Err         error
}

type Service struct {
	Sessions *sessionsvc.Service
}

func NewService(sessions *sessionsvc.Service) *Service {
	return &Service{Sessions: sessions}
}

func (s *Service) Restore(r *http.Request) Result {
	if s == nil || s.Sessions == nil {
		return Result{}
	}
	pr, claims, err := s.Sessions.PrincipalFromRequest(r)
	if err != nil {
		if errors.Is(err, sessionsvc.ErrMissingCookie) {
			return Result{}
		}
		return Result{
			ClearCookie: true,
			Err:         err,
		}
	}
	return Result{
		Principal: pr,
		Claims:    claims,
		Actor:     pr.Actor(),
		Found:     true,
	}
}

func (r Result) BindContext(ctx context.Context) context.Context {
	ctx = sessionsvc.ContextWithPrincipal(ctx, r.Principal)
	ctx = sessionsvc.ContextWithClaims(ctx, r.Claims)
	return actor.WithActor(ctx, r.Actor)
}
