package handlers

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/store"
)

func TestCaptureRequestMetaFiltersSensitiveHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://chat.example.com/v1/responses?trace=1&tag=a&tag=b", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test", "demo")

	meta := captureRequestMeta(req)
	if meta.RequestMethod != http.MethodPost || meta.RequestPath != "/v1/responses" {
		t.Fatalf("unexpected request meta basics: %#v", meta)
	}
	if !reflect.DeepEqual(meta.RequestQuery, map[string][]string{"trace": {"1"}, "tag": {"a", "b"}}) {
		t.Fatalf("unexpected request query capture: %#v", meta.RequestQuery)
	}
	if _, blocked := meta.RequestHeaders["Authorization"]; blocked {
		t.Fatalf("authorization header should be filtered: %#v", meta.RequestHeaders)
	}
	if !reflect.DeepEqual(meta.RequestHeaders["Content-Type"], []string{"application/json"}) {
		t.Fatalf("content-type should remain available: %#v", meta.RequestHeaders)
	}
}

func TestBuildReplayCurlIncludesBaseURLAndBody(t *testing.T) {
	curl := buildReplayCurl("http://chat.example.com", store.Request{
		RequestMethod: http.MethodPost,
		RequestPath:   "/v1/responses",
		RequestQuery: map[string][]string{
			"trace": {"1"},
		},
		RequestHeaders: map[string][]string{
			"Content-Type": {"application/json"},
		},
		RequestBody: map[string]any{
			"model": "demo",
			"input": "hello",
		},
	})
	for _, expected := range []string{
		"'http://chat.example.com/v1/responses?trace=1'",
		"'Content-Type: application/json'",
		"--data-raw",
		"\"model\": \"demo\"",
	} {
		if !strings.Contains(curl, expected) {
			t.Fatalf("curl command missing %q: %s", expected, curl)
		}
	}
}
