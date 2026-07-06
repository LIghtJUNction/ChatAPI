package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/zyf/chatapi/internal/config"
)

func TestGeeTestServiceValidate(t *testing.T) {
	var captured url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Query().Get("captcha_id"); got != "captcha-id" {
			t.Fatalf("unexpected captcha id: %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		captured = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	svc := NewGeeTestService(config.Config{
		GeetestCaptchaID:  "captcha-id",
		GeetestCaptchaKey: "captcha-key",
		GeetestAPIServer:  server.URL,
	}, server.Client())
	params := GeeTestParams{
		LotNumber:     "lot-123",
		CaptchaOutput: "captcha-output",
		PassToken:     "pass-token",
		GenTime:       "1720000000",
	}
	if err := svc.Validate(context.Background(), params); err != nil {
		t.Fatalf("validate geetest: %v", err)
	}

	wantMAC := hmac.New(sha256.New, []byte("captcha-key"))
	wantMAC.Write([]byte("lot-123"))
	if captured.Get("sign_token") != hex.EncodeToString(wantMAC.Sum(nil)) {
		t.Fatalf("unexpected sign token: %#v", captured)
	}
	if captured.Get("captcha_output") != "captcha-output" || captured.Get("pass_token") != "pass-token" || captured.Get("gen_time") != "1720000000" {
		t.Fatalf("unexpected geetest form: %#v", captured)
	}
}

func TestGeeTestServiceValidateErrors(t *testing.T) {
	svc := NewGeeTestService(config.Config{
		GeetestCaptchaID:  "captcha-id",
		GeetestCaptchaKey: "captcha-key",
		GeetestAPIServer:  "https://gcaptcha4.geetest.com",
	}, nil)
	if err := svc.Validate(context.Background(), GeeTestParams{}); err != ErrGeeTestRequired {
		t.Fatalf("expected ErrGeeTestRequired, got %v", err)
	}
	if err := svc.Validate(context.Background(), GeeTestParams{LotNumber: "only-lot"}); err != ErrGeeTestInvalidParams {
		t.Fatalf("expected ErrGeeTestInvalidParams, got %v", err)
	}

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"fail"}`))
	}))
	defer failServer.Close()
	svc = NewGeeTestService(config.Config{
		GeetestCaptchaID:  "captcha-id",
		GeetestCaptchaKey: "captcha-key",
		GeetestAPIServer:  failServer.URL,
	}, failServer.Client())
	err := svc.Validate(context.Background(), GeeTestParams{
		LotNumber:     "lot-123",
		CaptchaOutput: "captcha-output",
		PassToken:     "pass-token",
		GenTime:       "1720000000",
	})
	if err != ErrGeeTestFailed {
		t.Fatalf("expected ErrGeeTestFailed, got %v", err)
	}
}
