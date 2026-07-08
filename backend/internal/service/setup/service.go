package setup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

var ErrUnavailable = errors.New("setup is unavailable")
var ErrEnvExists = errors.New(".env already exists")

type Service struct {
	store store.Store
	cfg   config.Config
}

type Status struct {
	Available     bool      `json:"available"`
	EnvPath       string    `json:"env_path"`
	ExistingEnv   bool      `json:"existing_env"`
	HasAdmin      bool      `json:"has_admin"`
	Reason        string    `json:"reason,omitempty"`
	GeneratedAt   time.Time `json:"generated_at"`
	GeneratedKeys []string  `json:"generated_keys"`
}

type ApplyInput struct {
	AdminPassword string
	WriteEnv      bool
	Force         bool
}

type Report struct {
	OK            bool     `json:"ok"`
	EnvPath       string   `json:"env_path"`
	Written       bool     `json:"written"`
	GeneratedKeys []string `json:"generated_keys"`
	ExistingEnv   bool     `json:"existing_env"`
	NextSteps     []string `json:"next_steps"`
	EnvTemplate   string   `json:"env_template,omitempty"`
	Error         string   `json:"error,omitempty"`
	GeneratedAt   string   `json:"generated_at"`
}

func NewService(dataStore store.Store, cfg config.Config) *Service {
	return &Service{store: dataStore, cfg: cfg}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{
		Available:   false,
		EnvPath:     strings.TrimSpace(s.cfg.EnvFilePath),
		GeneratedAt: time.Now().UTC(),
		GeneratedKeys: []string{
			"CHATAPI_MASTER_KEY",
			"CHATAPI_SESSION_SECRET",
			"CHATAPI_ADMIN_PASSWORD",
		},
	}
	if s == nil || s.store == nil {
		status.Reason = "store_unavailable"
		return status, ErrUnavailable
	}
	status.ExistingEnv = fileExists(status.EnvPath)
	hasAdmin, err := s.hasAdminAccess(ctx)
	if err != nil {
		return status, err
	}
	status.HasAdmin = hasAdmin
	if s.cfg.Mode == config.ModeLab {
		status.Reason = "lab_mode"
		return status, ErrUnavailable
	}
	if hasAdmin {
		status.Reason = "admin_already_configured"
		return status, ErrUnavailable
	}
	status.Available = true
	return status, nil
}

func (s *Service) Run(ctx context.Context, input ApplyInput) (Report, error) {
	status, statusErr := s.Status(ctx)
	report := Report{
		OK:          status.Available,
		EnvPath:     status.EnvPath,
		ExistingEnv: status.ExistingEnv,
		GeneratedKeys: []string{
			"CHATAPI_MASTER_KEY",
			"CHATAPI_SESSION_SECRET",
			"CHATAPI_ADMIN_PASSWORD",
		},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		NextSteps: []string{
			"review generated .env values",
			"run chatapi doctor serve",
			"run chatapi migrate up",
			"start chatapi serve",
		},
	}
	if statusErr != nil && !status.Available {
		report.OK = false
		report.Error = status.Reason
		return report, statusErr
	}
	template, err := BuildEnvTemplate(strings.TrimSpace(input.AdminPassword))
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	if !input.WriteEnv {
		report.OK = true
		report.EnvTemplate = template
		return report, nil
	}
	if status.ExistingEnv && !input.Force {
		report.OK = false
		report.Error = ErrEnvExists.Error()
		return report, ErrEnvExists
	}
	if err := os.MkdirAll(filepath.Dir(status.EnvPath), 0o755); err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	if err := os.WriteFile(status.EnvPath, []byte(template), 0o600); err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	report.OK = true
	report.Written = true
	return report, nil
}

func BuildEnvTemplate(adminPassword string) (string, error) {
	adminPassword = strings.TrimSpace(adminPassword)
	if adminPassword == "" {
		value, err := randomURLToken(24)
		if err != nil {
			return "", err
		}
		adminPassword = value
	}
	masterKey, err := randomURLToken(48)
	if err != nil {
		return "", err
	}
	sessionSecret, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		"CHATAPI_MASTER_KEY=" + masterKey,
		"CHATAPI_SESSION_SECRET=" + sessionSecret,
		"CHATAPI_ADMIN_PASSWORD=" + adminPassword,
		"CHATAPI_DB_DRIVER=sqlite",
		"CHATAPI_DB_DSN=./data/chatapi.sqlite3",
		"CHATAPI_DATA_DIR=./data",
		"CHATAPI_LOG_LEVEL=info",
		"CHATAPI_METRICS_ENABLED=0",
		"",
	}, "\n"), nil
}

func (s *Service) hasAdminAccess(ctx context.Context) (bool, error) {
	if strings.TrimSpace(s.cfg.AdminPassword) != "" {
		return true, nil
	}
	if envHasAdminPassword(strings.TrimSpace(s.cfg.EnvFilePath)) {
		return true, nil
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, user := range users {
		if !user.IsActive {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(user.Role), "admin") || user.LocalAdmin {
			return true, nil
		}
	}
	return false, nil
}

func randomURLToken(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func envHasAdminPassword(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CHATAPI_ADMIN_PASSWORD=") && strings.TrimSpace(strings.TrimPrefix(line, "CHATAPI_ADMIN_PASSWORD=")) != "" {
			return true
		}
	}
	return false
}
