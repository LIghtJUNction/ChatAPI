package model

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
		Kind:       principal.KindModelAPIKey,
		SubjectID:  p.KeyID,
		UserID:     p.UserID,
		Username:   p.Name,
		Role:       "model_api",
		IsAdmin:    false,
		Source:     "model_api_key",
		EntryPoint: "virtual_model",
		AuthMethod: "api_key",
	}
}

func (p Principal) Actor() actor.Actor {
	return actor.Actor{
		UserID:      p.UserID,
		Username:    p.Name,
		Role:        "model_api",
		Source:      "model_api_key",
		PrincipalID: p.KeyID,
		EntryPoint:  "virtual_model",
	}
}
