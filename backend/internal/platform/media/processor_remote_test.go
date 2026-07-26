package media

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRemoteProcessorDerivesStableContentIdempotencyKey(t *testing.T) {
	output := testAVIF(t)
	var mu sync.Mutex
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/jobs":
			mu.Lock()
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(remoteJob{ID: "job", Status: "completed", OutputURL: "/output"})
		case "/output":
			_, _ = w.Write(output)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	processor := mustRemoteProcessor(t, ProcessorConfig{URL: server.URL, APIToken: "secret"})
	for _, input := range [][]byte{[]byte("same"), []byte("same"), []byte("different")} {
		if _, err := processor.EncodeAVIF(context.Background(), ParsedImage{Bytes: input}, AVIFOptions{Quality: 60}); err != nil {
			t.Fatalf("encode remote image: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 3 || keys[0] == "" || keys[0] != keys[1] || keys[0] == keys[2] {
		t.Fatalf("unexpected idempotency keys: %#v", keys)
	}
}

func TestProcessorRejectsInvalidRemoteConfiguration(t *testing.T) {
	tests := []ProcessorConfig{
		{URL: "https://processor.example"},
		{APIToken: "secret"},
		{URL: "file:///tmp/processor", APIToken: "secret"},
	}
	for _, config := range tests {
		if _, err := NewProcessor(config); !errors.Is(err, ErrProcessorConfig) {
			t.Fatalf("expected fail-fast configuration error for %#v, got %v", config, err)
		}
	}
}

func TestRemoteProcessorBoundsWholeJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(remoteJob{ID: "job", Status: "queued"})
	}))
	defer server.Close()

	processor := mustRemoteProcessor(t, ProcessorConfig{
		URL: server.URL, APIToken: "secret", JobTimeout: 30 * time.Millisecond,
		PollInterval: 5 * time.Millisecond, MaxPollDelay: 10 * time.Millisecond,
	})
	_, err := processor.EncodeAVIF(context.Background(), ParsedImage{Bytes: []byte("image")}, AVIFOptions{Quality: 60})
	if !errors.Is(err, ErrProcessorTimeout) {
		t.Fatalf("expected processor timeout, got %v", err)
	}
}

func TestRemoteProcessorDoesNotSendCredentialsAcrossOrigins(t *testing.T) {
	output := testAVIF(t)
	credentials := make(chan http.Header, 1)
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentials <- r.Header.Clone()
		_, _ = w.Write(output)
	}))
	defer external.Close()

	var processorServer *httptest.Server
	processorServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/jobs":
			if r.Header.Get("Authorization") != "Bearer secret" {
				t.Errorf("processor request missing authorization")
			}
			_ = json.NewEncoder(w).Encode(remoteJob{ID: "job", Status: "completed", OutputURL: "/redirect"})
		case "/redirect":
			http.Redirect(w, r, external.URL+"/object?signature=value", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer processorServer.Close()

	processor := mustRemoteProcessor(t, ProcessorConfig{URL: processorServer.URL, APIToken: "secret"})
	if _, err := processor.EncodeAVIF(context.Background(), ParsedImage{Bytes: []byte("image")}, AVIFOptions{Quality: 60}); err != nil {
		t.Fatalf("encode remote image: %v", err)
	}
	header := <-credentials
	if header.Get("Authorization") != "" || header.Get("X-Image-Processor-Tenant") != "" {
		t.Fatalf("processor credentials leaked across origins: %#v", header)
	}
}

func TestRemoteProcessorDoesNotAuthenticateExternalOutputURL(t *testing.T) {
	output := testAVIF(t)
	credentials := make(chan http.Header, 1)
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentials <- r.Header.Clone()
		_, _ = w.Write(output)
	}))
	defer external.Close()

	processorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(remoteJob{ID: "job", Status: "completed", OutputURL: external.URL + "/object?signature=value"})
	}))
	defer processorServer.Close()

	processor := mustRemoteProcessor(t, ProcessorConfig{URL: processorServer.URL, APIToken: "secret"})
	if _, err := processor.EncodeAVIF(context.Background(), ParsedImage{Bytes: []byte("image")}, AVIFOptions{Quality: 60}); err != nil {
		t.Fatalf("encode remote image: %v", err)
	}
	header := <-credentials
	if header.Get("Authorization") != "" || header.Get("X-Image-Processor-Tenant") != "" {
		t.Fatalf("processor credentials sent to external output URL: %#v", header)
	}
}

func TestRemoteProcessorRejectsNonAVIFOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/jobs" {
			_ = json.NewEncoder(w).Encode(remoteJob{ID: "job", Status: "completed", OutputURL: "/output"})
			return
		}
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	processor := mustRemoteProcessor(t, ProcessorConfig{URL: server.URL, APIToken: "secret"})
	_, err := processor.EncodeAVIF(context.Background(), ParsedImage{Bytes: []byte("image")}, AVIFOptions{Quality: 60})
	if !errors.Is(err, ErrProcessorFailed) {
		t.Fatalf("expected invalid processor output failure, got %v", err)
	}
}

func TestRemoteProcessorClassifiesHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	processor := mustRemoteProcessor(t, ProcessorConfig{URL: server.URL, APIToken: "secret"})
	_, err := processor.EncodeAVIF(context.Background(), ParsedImage{Bytes: []byte("image")}, AVIFOptions{Quality: 60})
	if !errors.Is(err, ErrProcessorUnavailable) {
		t.Fatalf("expected unavailable classification, got %v", err)
	}
}

func mustRemoteProcessor(t *testing.T, config ProcessorConfig) Processor {
	t.Helper()
	processor, err := NewProcessor(config)
	if err != nil {
		t.Fatalf("create remote processor: %v", err)
	}
	return processor
}

func testAVIF(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(testAVIFBase64)
	if err != nil {
		t.Fatalf("decode AVIF fixture: %v", err)
	}
	return data
}

const testAVIFBase64 = "AAAAIGZ0eXBhdmlmAAAAAGF2aWZtaWYxbWlhZk1BMUIAAAD5bWV0YQAAAAAAAAAvaGRscgAAAAAAAAAAcGljdAAAAAAAAAAAAAAAAFBpY3R1cmVIYW5kbGVyAAAAAA5waXRtAAAAAAABAAAAHmlsb2MAAAAARAAAAQABAAAAAQAAASEAAAAbAAAAKGlpbmYAAAAAAAEAAAAaaW5mZQIAAAAAAQAAYXYwMUNvbG9yAAAAAGppcHJwAAAAS2lwY28AAAAUaXNwZQAAAAAAAAACAAAAAgAAABBwaXhpAAAAAAMICAgAAAAMYXYxQ4EADAAAAAATY29scm5jbHgAAgACAAIAAAAAF2lwbWEAAAAAAAAAAQABBAECgwQAAAAjbWRhdAoFGAA2wCAyEhgAAABQAABAA1Lt5xf080WmIA=="
