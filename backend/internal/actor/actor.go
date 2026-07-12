package actor

import (
	"context"
	"strings"
)

type Actor struct {
	UserID      string
	Username    string
	Role        string
	Source      string
	PrincipalID string
	EntryPoint  string
}

type contextKey struct{}

func WithActor(ctx context.Context, value Actor) context.Context {
	return context.WithValue(ctx, contextKey{}, value)
}

func FromContext(ctx context.Context) (Actor, bool) {
	value, ok := ctx.Value(contextKey{}).(Actor)
	return value, ok
}

func OwnerIDFromContext(ctx context.Context) string {
	value, ok := FromContext(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value.UserID)
}
