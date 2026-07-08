package app

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/service/auth/authz/decision"
	"go.uber.org/zap"
)

func (s *Service) AdmitRequest(ctx context.Context, input AdmissionInput) decision.AuthResult[AdmissionValue] {
	principal, err := s.Authenticate(ctx, input.RawKey)
	if err != nil {
		result := decision.Deny(decision.SubjectAppAPIKey, decision.ReasonUnauthorized, 401, "app api key unauthorized", "unauthorized")
		logging.BindContext(s.Logger, ctx,
			zap.String("auth.kind", "app_api_key"),
			zap.String("auth.subject", string(result.Subject)),
			zap.String("auth.reason", string(result.Reason)),
			zap.String("auth.error_code", result.ErrorCode),
			zap.String("http.path", strings.TrimSpace(input.Route)),
		).Warn("app api key authentication failed")
		return decision.DenyWith[AdmissionValue](result)
	}
	ctx = ContextWithPrincipal(ctx, principal)
	ctx = actor.WithActor(ctx, principal.Actor())

	if !s.AllowSourceIP(principal, input.SourceIP) {
		result := decision.Deny(decision.SubjectAppAPIKey, decision.ReasonSourceIP, 403, "app api key source ip forbidden", "source_ip_forbidden")
		logging.BindContext(s.Logger, ctx,
			zap.String("auth.kind", "app_api_key"),
			zap.String("auth.subject", string(result.Subject)),
			zap.String("auth.reason", string(result.Reason)),
			zap.String("auth.error_code", result.ErrorCode),
			zap.String("http.path", strings.TrimSpace(input.Route)),
			zap.String("source_ip", strings.TrimSpace(input.SourceIP)),
		).Warn("app api key source ip rejected")
		s.RecordAudit(ctx, principal, input.Route, result.StatusCode, result.ErrorCode)
		return decision.DenyWith[AdmissionValue](result)
	}

	for _, scope := range input.RequiredScopes {
		if _, ok := principal.Scopes[scope]; !ok {
			result := decision.Deny(decision.SubjectAppAPIKey, decision.ReasonForbidden, 403, "app api key forbidden", "forbidden")
			logging.BindContext(s.Logger, ctx,
				zap.String("auth.kind", "app_api_key"),
				zap.String("auth.subject", string(result.Subject)),
				zap.String("auth.reason", string(result.Reason)),
				zap.String("auth.error_code", result.ErrorCode),
				zap.String("http.path", strings.TrimSpace(input.Route)),
				zap.String("required.scope", scope),
			).Warn("app api key scope rejected")
			s.RecordAudit(ctx, principal, input.Route, result.StatusCode, result.ErrorCode)
			return decision.DenyWith[AdmissionValue](result)
		}
	}

	if !s.AllowRequestNow(principal) {
		result := decision.Deny(decision.SubjectAppAPIKey, decision.ReasonRateLimited, 429, "app api key rate limited", "rate_limited")
		logging.BindContext(s.Logger, ctx,
			zap.String("auth.kind", "app_api_key"),
			zap.String("auth.subject", string(result.Subject)),
			zap.String("auth.reason", string(result.Reason)),
			zap.String("auth.error_code", result.ErrorCode),
			zap.String("http.path", strings.TrimSpace(input.Route)),
		).Warn("app api key rate limited")
		s.RecordAudit(ctx, principal, input.Route, result.StatusCode, result.ErrorCode)
		return decision.DenyWith[AdmissionValue](result)
	}

	logging.BindContext(s.Logger, ctx,
		zap.String("auth.kind", "app_api_key"),
		zap.String("auth.subject", string(decision.SubjectAppAPIKey)),
		zap.String("http.path", strings.TrimSpace(input.Route)),
		zap.Strings("auth.required_scopes", input.RequiredScopes),
	).Info("app api key authenticated")

	value := AdmissionValue{Principal: principal, Actor: principal.Actor()}
	return decision.AllowWith(decision.SubjectAppAPIKey, value, func(bindCtx context.Context) context.Context {
		bindCtx = ContextWithPrincipal(bindCtx, principal)
		return actor.WithActor(bindCtx, value.Actor)
	})
}
