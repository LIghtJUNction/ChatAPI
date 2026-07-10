package logging

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func DebugProtocolRequestBody(base *zap.Logger, ctx context.Context, protocol string, raw []byte) {
	logger := BindContext(base, ctx,
		zap.String("protocol", strings.TrimSpace(protocol)),
		zap.Int("request.body.bytes", len(raw)),
	)
	if !logger.Core().Enabled(zapcore.DebugLevel) {
		return
	}
	logger.Debug("protocol request body received", zap.ByteString("request.body.raw", raw))
}
