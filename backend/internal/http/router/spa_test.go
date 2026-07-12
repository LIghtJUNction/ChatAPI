package router

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAFallbackServesAssetsAndBrowserRoutes(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<main>app</main>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('app')")},
	}
	handler := spaFallback(fs.FS(assets))

	tests := []struct {
		method string
		path   string
		status int
		body   string
	}{
		{http.MethodGet, "/assets/app.js", http.StatusOK, "console.log('app')"},
		{http.MethodGet, "/app", http.StatusOK, "<main>app</main>"},
		{http.MethodGet, "/admin/settings/overview", http.StatusOK, "<main>app</main>"},
		{http.MethodHead, "/app", http.StatusOK, ""},
		{http.MethodGet, "/api/missing", http.StatusNotFound, "404 page not found\n"},
		{http.MethodGet, "/v1/missing", http.StatusNotFound, "404 page not found\n"},
		{http.MethodPost, "/app", http.StatusNotFound, "404 page not found\n"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Body.String() != test.body {
				t.Fatalf("unexpected response: status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}
