package app

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/principal"
)

type principalContextKey struct{}

func ContextWithPrincipal(ctx context.Context, pr Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, pr)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	pr, ok := ctx.Value(principalContextKey{}).(Principal)
	return pr, ok
}

func (p Principal) AuthPrincipal() principal.Principal {
	return principal.Principal{
		Kind:       principal.KindAppAPIKey,
		SubjectID:  p.KeyID,
		UserID:     p.UserID,
		Username:   p.Name,
		Role:       "app_api",
		IsAdmin:    false,
		Source:     "app_api_key",
		EntryPoint: "app_api",
		AuthMethod: "api_key",
	}
}

func (p Principal) Actor() actor.Actor {
	return actor.Actor{
		UserID:      p.UserID,
		Username:    p.Name,
		Role:        "app_api",
		Source:      "app_api_key",
		PrincipalID: p.KeyID,
		EntryPoint:  "app_api",
	}
}
