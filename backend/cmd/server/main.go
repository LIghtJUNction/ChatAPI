package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	httprouter "github.com/zyf2007/ChatAPI/internal/http/router"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	platformbrowser "github.com/zyf2007/ChatAPI/internal/platform/browser"
	platformemail "github.com/zyf2007/ChatAPI/internal/platform/email"
	auditrepo "github.com/zyf2007/ChatAPI/internal/repository/audit"
	authrepo "github.com/zyf2007/ChatAPI/internal/repository/auth"
	chatrepo "github.com/zyf2007/ChatAPI/internal/repository/chat"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	platformrepo "github.com/zyf2007/ChatAPI/internal/repository/platform"
	pgrepo "github.com/zyf2007/ChatAPI/internal/repository/postgresql"
	sqliterepo "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	storagerepo "github.com/zyf2007/ChatAPI/internal/repository/storage"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	auditsvc "github.com/zyf2007/ChatAPI/internal/service/audit"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/geetest"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/identity"
	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	oidcsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/oidc"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/ratelimit"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	superadminsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/superadmin"
	totpsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/totp"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/verification"
	appkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	sessionsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
)

type runtimeStore interface {
	authrepo.Store
	chatrepo.Store
	configrepo.Store
	storagerepo.Store
	auditrepo.Store
	platformrepo.MaintenanceStore
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "chatapi server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	backendRoot, err := detectBackendRoot()
	if err != nil {
		return err
	}
	if err := config.LoadEnv(backendRoot); err != nil {
		return fmt.Errorf("load env: %w", err)
	}
	cfg, err := config.FromEnv(config.ModeServe, backendRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logFactory, err := logging.NewFactory(logging.NewConfig(cfg))
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	appLogger := logFactory.Layer(logging.LayerApp)

	store, cleanup, err := openStore(ctx, cfg, logFactory)
	if err != nil {
		return err
	}
	defer cleanup()

	policySvc := policy.NewService()
	sessionSvc, err := sessionsvc.NewService(sessionsvc.Config{
		Secret:     cfg.SessionSecret,
		CookieName: "chatapi_session",
		TTL:        7 * 24 * time.Hour,
		SecureOnly: false,
		Path:       "/",
	})
	if err != nil {
		return fmt.Errorf("init session service: %w", err)
	}

	accountSvc := account.NewService(store)
	superAdminSvc := superadminsvc.NewService(accountSvc, cfg)
	authSettingsSvc := authsettings.NewService(store, cfg)
	emailSender := buildEmailSender(cfg)
	verificationSvc := verification.NewService(store, emailSender)
	geetestSvc := geetest.NewService(cfg, nil)
	totpSvc := totpsvc.NewService(store, cfg.MasterKey, "ChatAPI")
	oidcSvc := oidcsvc.NewService(accountSvc, cfg)
	loginLimiter := ratelimit.NewService(5, time.Minute)
	auditSvc := auditsvc.NewService(store)
	localAuthSvc := localauth.NewService(accountSvc, store, policySvc, sessionSvc, verificationSvc)
	if _, _, err := superAdminSvc.Sync(ctx); err != nil {
		return fmt.Errorf("sync super admin: %w", err)
	}
	labSvc := labauth.NewService(cfg)
	accessSettingsSvc := authaccess.NewSettingsService(store, authaccess.Settings{
		GlobalRateLimitRequests: cfg.AccessRateLimitRequests,
		GlobalRateLimitWindow:   cfg.AccessRateLimitWindow,
	})
	accessSvc := authaccess.NewService(cfg, labSvc, accessSettingsSvc)
	identitySvc := identity.NewService(accountSvc)
	appKeySvc := appkeysvc.NewService(store)
	appKeySvc.Logger = logFactory.Layer(logging.LayerAuth)
	modelKeySvc := modelkeysvc.NewService(store, cfg.MasterKey)
	modelKeySvc.Logger = logFactory.Layer(logging.LayerAuth)
	querySvc := &turnquerysvc.Service{
		Store:  store,
		Logger: logFactory.Layer(logging.LayerTurnQuery),
	}
	pendingRegistry := pendingsvc.NewPendingRegistry()
	pendingRegistry.Logger = logFactory.Layer(logging.LayerPending)
	submitter := &turnsvc.Submitter{
		Store:              store,
		Pending:            pendingRegistry,
		PreparedImageClean: nil,
	}
	turnService := &turnsvc.Service{
		Submitter: submitter,
		Pending:   pendingRegistry,
		Store:     store,
		OwnerIDFromContext: func(ctx context.Context) string {
			if act, ok := actor.FromContext(ctx); ok && strings.TrimSpace(act.UserID) != "" {
				return strings.TrimSpace(act.UserID)
			}
			return ""
		},
		ActorFromContext: func(ctx context.Context) (actor.Actor, bool) {
			return actor.FromContext(ctx)
		},
		Logger: logFactory.Layer(logging.LayerTurn),
	}
	if _, err := turnService.DisconnectRecoveredPending(ctx, "server restarted"); err != nil {
		return fmt.Errorf("disconnect recovered pending turns: %w", err)
	}

	handler := httprouter.New(httprouter.Deps{
		Config:         cfg,
		ChatRepo:       store,
		AuthRepo:       store,
		ConfigRepo:     store,
		StorageRepo:    store,
		AuditRepo:      store,
		PlatformRepo:   store,
		Turn:           turnService,
		Query:          querySvc,
		ModelAPIKeys:   modelKeySvc,
		AppAPIKeys:     appKeySvc,
		Lab:            labSvc,
		LocalAuth:      localAuthSvc,
		Verification:   verificationSvc,
		Policy:         policySvc,
		Access:         accessSvc,
		AccessSettings: accessSettingsSvc,
		AuthSettings:   authSettingsSvc,
		GeeTest:        geetestSvc,
		TOTP:           totpSvc,
		OIDC:           oidcSvc,
		LoginLimiter:   loginLimiter,
		Audit:          auditSvc,
		Accounts:       accountSvc,
		Identity:       identitySvc,
		UserSessions:   sessionSvc,
		LoggerFactory:  logFactory,
	})

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if cfg.Mode == config.ModeLab && cfg.OpenBrowser {
		go func() {
			time.Sleep(600 * time.Millisecond)
			target := fmt.Sprintf("http://%s:%d", browserHost(cfg), cfg.Port)
			_ = platformbrowser.Open(target)
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		appLogger.Info("http server starting")
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case serveErr := <-errCh:
		return serveErr
	}
}

func detectBackendRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current := wd
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("unable to locate backend root")
		}
		current = parent
	}
}

func openStore(ctx context.Context, cfg config.Config, logFactory *logging.Factory) (runtimeStore, func(), error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver))
	switch driver {
	case "sqlite":
		store, err := sqliterepo.Open(cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite store: %w", err)
		}
		store.Logger = logFactory.Layer(logging.LayerRepository)
		if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
			_ = store.Close()
			return nil, nil, fmt.Errorf("bootstrap sqlite migrations: %w", err)
		}
		return store, func() { _ = store.Close() }, nil
	case "postgres", "postgresql":
		store, err := pgrepo.Open(ctx, cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open postgresql store: %w", err)
		}
		store.Logger = logFactory.Layer(logging.LayerRepository)
		if err := pgrepo.Bootstrap(ctx, store.Pool()); err != nil {
			store.Close()
			return nil, nil, fmt.Errorf("bootstrap postgresql migrations: %w", err)
		}
		return store, func() { store.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported database driver: %s", cfg.DatabaseDriver)
	}
}

func buildEmailSender(cfg config.Config) platformemail.Sender {
	smtpCfg := platformemail.SMTPConfigFromConfig(cfg)
	if !smtpCfg.Enabled {
		return nil
	}
	return platformemail.NewSMTPSender(smtpCfg)
}

func browserHost(cfg config.Config) string {
	host := strings.TrimSpace(cfg.Host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}
