package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Mode string

const (
	ModeServe Mode = "serve"
	ModeLab   Mode = "lab"
)

type Config struct {
	Mode           Mode
	Host           string
	Port           int
	BaseURL        string
	WebDistDir     string
	DataDir        string
	DatabaseDriver string
	DatabaseDSN    string
	AllowRemoteLab bool
	OpenBrowser    bool
	LabToken       string
	LabPassword    string
	LogLevel       string
	CORSOrigins    []string
}

func LoadEnv(backendRoot string) error {
	candidates := []string{
		filepath.Join(backendRoot, ".env"),
		filepath.Join(backendRoot, ".env.local"),
		filepath.Join(filepath.Dir(backendRoot), ".env"),
	}
	if external := strings.TrimSpace(os.Getenv("CHATAPI_ENV_FILE")); external != "" {
		candidates = append(candidates, external)
	}
	for _, path := range candidates {
		_ = godotenv.Overload(path)
	}
	return nil
}

func Default(mode Mode, backendRoot string) Config {
	projectRoot := filepath.Dir(backendRoot)
	dataDir := filepath.Join(backendRoot, "data")
	host := "0.0.0.0"
	openBrowser := false
	if mode == ModeLab {
		host = "127.0.0.1"
		openBrowser = true
	}
	return Config{
		Mode:           mode,
		Host:           host,
		Port:           5000,
		BaseURL:        "",
		WebDistDir:     filepath.Join(projectRoot, "frontend", "dist"),
		DataDir:        dataDir,
		DatabaseDriver: "sqlite",
		DatabaseDSN:    filepath.Join(dataDir, "chatapi.sqlite3"),
		AllowRemoteLab: false,
		OpenBrowser:    openBrowser,
		LabToken:       "",
		LabPassword:    "",
		LogLevel:       "info",
		CORSOrigins:    []string{"http://localhost:5173", "http://127.0.0.1:5173"},
	}
}

func FromEnv(mode Mode, backendRoot string) (Config, error) {
	cfg := Default(mode, backendRoot)
	cfg.Host = firstNonEmpty(os.Getenv("CHATAPI_HOST"), cfg.Host)
	cfg.BaseURL = strings.TrimSpace(os.Getenv("CHATAPI_BASE_URL"))
	cfg.WebDistDir = firstNonEmpty(os.Getenv("CHATAPI_WEB_DIST_DIR"), cfg.WebDistDir)
	cfg.DataDir = firstNonEmpty(os.Getenv("CHATAPI_DATA_DIR"), cfg.DataDir)
	cfg.DatabaseDriver = firstNonEmpty(os.Getenv("CHATAPI_DB_DRIVER"), cfg.DatabaseDriver)
	cfg.DatabaseDSN = firstNonEmpty(os.Getenv("CHATAPI_DB_DSN"), cfg.DatabaseDSN)
	cfg.LogLevel = strings.ToLower(firstNonEmpty(os.Getenv("CHATAPI_LOG_LEVEL"), cfg.LogLevel))
	cfg.LabToken = strings.TrimSpace(os.Getenv("CHATAPI_LAB_TOKEN"))
	cfg.LabPassword = strings.TrimSpace(os.Getenv("CHATAPI_LAB_PASSWORD"))

	if raw := strings.TrimSpace(os.Getenv("CHATAPI_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_PORT: %w", err)
		}
		cfg.Port = port
	}

	if raw := strings.TrimSpace(os.Getenv("CHATAPI_CORS_ORIGINS")); raw != "" {
		cfg.CORSOrigins = splitCSV(raw)
	}

	cfg.AllowRemoteLab = parseBool(os.Getenv("CHATAPI_ALLOW_REMOTE_LAB"), cfg.AllowRemoteLab)
	cfg.OpenBrowser = parseBool(os.Getenv("CHATAPI_OPEN_BROWSER"), cfg.OpenBrowser)

	if !filepath.IsAbs(cfg.WebDistDir) {
		cfg.WebDistDir = filepath.Join(backendRoot, cfg.WebDistDir)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		cfg.DataDir = filepath.Join(backendRoot, cfg.DataDir)
	}
	if cfg.DatabaseDriver == "sqlite" && !filepath.IsAbs(cfg.DatabaseDSN) {
		cfg.DatabaseDSN = filepath.Join(backendRoot, cfg.DatabaseDSN)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return errors.New("port must be within 1-65535")
	}
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("host is required")
	}
	if c.Mode == ModeLab && c.AllowRemoteLab && c.Host == "127.0.0.1" {
		return errors.New("remote lab requires non-loopback host")
	}
	if c.Mode == ModeLab && c.Host != "127.0.0.1" && !c.AllowRemoteLab {
		return errors.New("non-local lab host requires CHATAPI_ALLOW_REMOTE_LAB=1")
	}
	if c.Mode == ModeLab && strings.TrimSpace(c.LabToken) == "" && strings.TrimSpace(c.LabPassword) == "" && c.Host != "127.0.0.1" {
		return errors.New("remote lab requires token or password")
	}
	if c.DatabaseDriver == "sqlite" && strings.TrimSpace(c.DatabaseDSN) == "" {
		return errors.New("sqlite database dsn is required")
	}
	return nil
}

func (c Config) ListenAddr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseBool(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
