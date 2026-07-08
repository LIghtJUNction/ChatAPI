package geetest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
)

var (
	ErrRequired      = errors.New("geetest verification is required")
	ErrInvalidParams = errors.New("geetest verification parameters are incomplete")
	ErrFailed        = errors.New("geetest verification failed")
	ErrUnavailable   = errors.New("geetest verification is unavailable")
)

type Params struct {
	LotNumber     string `json:"lot_number"`
	CaptchaOutput string `json:"captcha_output"`
	PassToken     string `json:"pass_token"`
	GenTime       string `json:"gen_time"`
}

type Service struct {
	captchaID string
	key       string
	apiServer string
	client    *http.Client
}

func NewService(cfg config.Config, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{
		captchaID: strings.TrimSpace(cfg.GeetestCaptchaID),
		key:       strings.TrimSpace(cfg.GeetestCaptchaKey),
		apiServer: strings.TrimRight(strings.TrimSpace(cfg.GeetestAPIServer), "/"),
		client:    client,
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.captchaID != "" && s.key != ""
}

func (s *Service) CaptchaID() string {
	if s == nil {
		return ""
	}
	return s.captchaID
}

func (s *Service) Validate(ctx context.Context, params Params) error {
	if !s.Enabled() {
		return nil
	}
	params.LotNumber = strings.TrimSpace(params.LotNumber)
	params.CaptchaOutput = strings.TrimSpace(params.CaptchaOutput)
	params.PassToken = strings.TrimSpace(params.PassToken)
	params.GenTime = strings.TrimSpace(params.GenTime)
	if params.LotNumber == "" && params.CaptchaOutput == "" && params.PassToken == "" && params.GenTime == "" {
		return ErrRequired
	}
	if params.LotNumber == "" || params.CaptchaOutput == "" || params.PassToken == "" || params.GenTime == "" {
		return ErrInvalidParams
	}
	sign := hmac.New(sha256.New, []byte(s.key))
	sign.Write([]byte(params.LotNumber))
	form := url.Values{
		"lot_number":     []string{params.LotNumber},
		"captcha_output": []string{params.CaptchaOutput},
		"pass_token":     []string{params.PassToken},
		"gen_time":       []string{params.GenTime},
		"sign_token":     []string{hex.EncodeToString(sign.Sum(nil))},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiServer+"/validate?captcha_id="+url.QueryEscape(s.captchaID), strings.NewReader(form.Encode()))
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUnavailable
	}
	var payload struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ErrUnavailable
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Result), "success") {
		return ErrFailed
	}
	return nil
}
