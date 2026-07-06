package service

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

var ErrGeeTestRequired = errors.New("geetest verification is required")
var ErrGeeTestInvalidParams = errors.New("geetest verification parameters are incomplete")
var ErrGeeTestFailed = errors.New("geetest verification failed")
var ErrGeeTestUnavailable = errors.New("geetest verification is unavailable")

type GeeTestParams struct {
	LotNumber     string `json:"lot_number"`
	CaptchaOutput string `json:"captcha_output"`
	PassToken     string `json:"pass_token"`
	GenTime       string `json:"gen_time"`
}

type GeeTestService struct {
	captchaID string
	key       string
	apiServer string
	client    *http.Client
}

func NewGeeTestService(cfg config.Config, client *http.Client) *GeeTestService {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &GeeTestService{
		captchaID: strings.TrimSpace(cfg.GeetestCaptchaID),
		key:       strings.TrimSpace(cfg.GeetestCaptchaKey),
		apiServer: strings.TrimRight(strings.TrimSpace(cfg.GeetestAPIServer), "/"),
		client:    client,
	}
}

func (s *GeeTestService) Enabled() bool {
	return s != nil && s.captchaID != "" && s.key != ""
}

func (s *GeeTestService) CaptchaID() string {
	if s == nil {
		return ""
	}
	return s.captchaID
}

func (s *GeeTestService) Validate(ctx context.Context, params GeeTestParams) error {
	if !s.Enabled() {
		return nil
	}
	params.LotNumber = strings.TrimSpace(params.LotNumber)
	params.CaptchaOutput = strings.TrimSpace(params.CaptchaOutput)
	params.PassToken = strings.TrimSpace(params.PassToken)
	params.GenTime = strings.TrimSpace(params.GenTime)
	if params.LotNumber == "" && params.CaptchaOutput == "" && params.PassToken == "" && params.GenTime == "" {
		return ErrGeeTestRequired
	}
	if params.LotNumber == "" || params.CaptchaOutput == "" || params.PassToken == "" || params.GenTime == "" {
		return ErrGeeTestInvalidParams
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
	endpoint := s.apiServer + "/validate?captcha_id=" + url.QueryEscape(s.captchaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrGeeTestUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return ErrGeeTestUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrGeeTestUnavailable
	}
	var payload struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ErrGeeTestUnavailable
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Result), "success") {
		return ErrGeeTestFailed
	}
	return nil
}
