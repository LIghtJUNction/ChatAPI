package service

import (
	"context"
	"strings"
)

type RequestActor struct {
	UserID   string
	Username string
	Role     string
	Source   string
}

type requestActorContextKey struct{}

func WithRequestActor(ctx context.Context, actor RequestActor) context.Context {
	return context.WithValue(ctx, requestActorContextKey{}, actor)
}

func RequestActorFromContext(ctx context.Context) (RequestActor, bool) {
	actor, ok := ctx.Value(requestActorContextKey{}).(RequestActor)
	if !ok {
		return RequestActor{}, false
	}
	return actor, true
}

func OwnerIDFromContext(ctx context.Context) string {
	actor, ok := RequestActorFromContext(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(actor.UserID)
}

func IsInteractiveUserActor(actor RequestActor) bool {
	if strings.TrimSpace(actor.UserID) == "" {
		return false
	}
	switch strings.TrimSpace(actor.Source) {
	case "lab", "session", "oidc":
		return true
	default:
		return false
	}
}
