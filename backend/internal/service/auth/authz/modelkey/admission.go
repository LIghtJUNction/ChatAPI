package model

import (
	"context"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/service/auth/authz/decision"
	"go.uber.org/zap"
)

func (s *Service) AdmitRequest(ctx context.Context, input AdmissionInput) decision.AuthResult[AdmissionValue] {
	principal, err := s.Authenticate(ctx, input.RawKey)
	if err != nil {
		result := decision.Deny(decision.SubjectModelAPIKey, decision.ReasonUnauthorized, 401, "model api key unauthorized", "unauthorized")
		logging.BindContext(s.Logger, ctx,
			zap.String("auth.kind", "model_api_key"),
			zap.String("auth.subject", string(result.Subject)),
			zap.String("auth.reason", string(result.Reason)),
			zap.String("auth.error_code", result.ErrorCode),
		).Warn("model api key authentication failed")
		return decision.DenyWith[AdmissionValue](result)
	}
	value := AdmissionValue{Principal: principal, Actor: principal.Actor()}
	logging.BindContext(s.Logger, ctx,
		zap.String("auth.kind", "model_api_key"),
		zap.String("auth.subject", string(decision.SubjectModelAPIKey)),
	).Info("model api key authenticated")
	return decision.AllowWith(decision.SubjectModelAPIKey, value, func(bindCtx context.Context) context.Context {
		bindCtx = ContextWithPrincipal(bindCtx, principal)
		return actor.WithActor(bindCtx, value.Actor)
	})
}
