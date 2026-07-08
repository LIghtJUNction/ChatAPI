package session

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/principal"
)

type principalContextKey struct{}
type claimsContextKey struct{}

func ContextWithPrincipal(ctx context.Context, pr principal.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, pr)
}

func PrincipalFromContext(ctx context.Context) (principal.Principal, bool) {
	pr, ok := ctx.Value(principalContextKey{}).(principal.Principal)
	return pr, ok
}

func ContextWithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(Claims)
	return claims, ok
}
