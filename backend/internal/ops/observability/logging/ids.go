package logging

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	RequestIDHeader = "X-Request-ID"
)

type requestIDKey struct{}
type connectionIDKey struct{}

func NewRequestID() string {
	return strings.TrimSpace(uuid.NewString())
}

func NewConnectionID(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return NewRequestID()
	}
	return prefix + "_" + NewRequestID()
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(requestIDKey{}).(string)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func WithConnectionID(ctx context.Context, connectionID string) context.Context {
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" {
		return ctx
	}
	return context.WithValue(ctx, connectionIDKey{}, connectionID)
}

func ConnectionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(connectionIDKey{}).(string)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func RequestIDField(ctx context.Context) zap.Field {
	if value, ok := RequestIDFromContext(ctx); ok {
		return zap.String("request_id", value)
	}
	return zap.Skip()
}

func ConnectionIDField(ctx context.Context) zap.Field {
	if value, ok := ConnectionIDFromContext(ctx); ok {
		return zap.String("connection_id", value)
	}
	return zap.Skip()
}
