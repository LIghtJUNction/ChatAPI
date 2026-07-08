package logging

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/config"
)

const (
	LayerApp         = "app"
	LayerHTTP        = "http"
	LayerAuth        = "auth"
	LayerTurn        = "turn"
	LayerTurnQuery   = "turnquery"
	LayerPending     = "pending"
	LayerUserControl = "usercontrol"
	LayerRepository  = "repository"
	LayerMigrate     = "migrate"
	LayerPlatform    = "platform"
	LayerAudit       = "audit"
)

type Config struct {
	Level  string
	Format string
}

type Factory struct {
	root *zap.Logger
}

type contextKey struct{}

func NewConfig(cfg config.Config) Config {
	return Config{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
	}
}

func NewFactory(cfg Config) (*Factory, error) {
	zcfg := zap.NewProductionConfig()
	if strings.EqualFold(strings.TrimSpace(cfg.Format), "console") {
		zcfg.Encoding = "console"
	} else {
		zcfg.Encoding = "json"
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Level), "debug") {
		zcfg.Development = true
	}
	zcfg.DisableStacktrace = true
	zcfg.EncoderConfig.TimeKey = "ts"
	zcfg.EncoderConfig.LevelKey = "level"
	zcfg.EncoderConfig.MessageKey = "msg"
	zcfg.EncoderConfig.NameKey = "logger"
	zcfg.EncoderConfig.CallerKey = "caller"
	zcfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	zcfg.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	if err := zcfg.Level.UnmarshalText([]byte(normalizeLevel(cfg.Level))); err != nil {
		return nil, err
	}
	logger, err := zcfg.Build(zap.AddCaller(), zap.AddCallerSkip(1))
	if err != nil {
		return nil, err
	}
	return &Factory{root: logger}, nil
}

func (f *Factory) Root() *zap.Logger {
	if f == nil || f.root == nil {
		return zap.NewNop()
	}
	return f.root
}

func (f *Factory) Layer(name string, fields ...zap.Field) *zap.Logger {
	logger := f.Root()
	name = strings.TrimSpace(name)
	if name != "" {
		logger = logger.Named(name).With(zap.String("layer", name))
	}
	if len(fields) > 0 {
		logger = logger.With(fields...)
	}
	return logger
}

func (f *Factory) ForContext(ctx context.Context, layer string, fields ...zap.Field) *zap.Logger {
	logger := f.Layer(layer, fields...)
	if ctx == nil {
		return logger
	}
	if contextual, ok := FromContext(ctx); ok {
		logger = contextual.With(fields...)
	}
	if requestActor, ok := actor.FromContext(ctx); ok {
		logger = logger.With(ActorFields(requestActor)...)
	}
	return logger
}

func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	if logger == nil {
		logger = zap.NewNop()
	}
	return context.WithValue(ctx, contextKey{}, logger)
}

func FromContext(ctx context.Context) (*zap.Logger, bool) {
	logger, ok := ctx.Value(contextKey{}).(*zap.Logger)
	return logger, ok
}

func BindContext(base *zap.Logger, ctx context.Context, fields ...zap.Field) *zap.Logger {
	if base == nil {
		base = zap.NewNop()
	}
	logger := base
	if ctx != nil {
		if contextual, ok := FromContext(ctx); ok {
			logger = contextual
		}
		if requestActor, ok := actor.FromContext(ctx); ok {
			logger = logger.With(ActorFields(requestActor)...)
		}
	}
	if len(fields) > 0 {
		logger = logger.With(fields...)
	}
	return logger
}

func ActorFields(value actor.Actor) []zap.Field {
	fields := make([]zap.Field, 0, 6)
	if value.UserID != "" {
		fields = append(fields, zap.String("actor.user_id", value.UserID))
	}
	if value.Username != "" {
		fields = append(fields, zap.String("actor.username", value.Username))
	}
	if value.Role != "" {
		fields = append(fields, zap.String("actor.role", value.Role))
	}
	if value.Source != "" {
		fields = append(fields, zap.String("actor.source", value.Source))
	}
	if value.PrincipalID != "" {
		fields = append(fields, zap.String("actor.principal_id", value.PrincipalID))
	}
	if value.EntryPoint != "" {
		fields = append(fields, zap.String("actor.entry_point", value.EntryPoint))
	}
	return fields
}

func normalizeLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug", "info", "warn", "error", "dpanic", "panic", "fatal":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "info"
	}
}
