package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	httpapp "github.com/zyf2007/ChatAPI/internal/testutil/httpapp"
)

var noopEvents = chatevents.NoopPublisher{}

func TestRouterAuthPendingAndOwnerScopedQueries(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()

	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_a",
		Username: "user-a",
		Email:    "user-a@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user a: %v", err)
	}
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_b",
		Username: "user-b",
		Email:    "user-b@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user b: %v", err)
	}

	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("new logger factory: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)

	pending := pendingsvc.NewPendingRegistry()
	pending.Logger = logFactory.Layer(logging.LayerPending)
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:   st,
			Pending: pending,
		},
		Pending:            pending,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Events:             noopEvents,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}

	modelKeyService := modelkey.NewService(st, "test-master-key")
	appKeyService := appkey.NewService(st, "test-master-key")
	appKeyService.Logger = logFactory.Layer(logging.LayerAudit)

	_, modelKeyA, err := modelKeyService.CreateKey(context.Background(), "user_a", "user-a-model", "demo-query")
	if err != nil {
		t.Fatalf("create model key a: %v", err)
	}
	_, appKeyA, err := appKeyService.CreateKey(context.Background(), "user_a", "user-a-app", []string{"requests:read", "requests:respond", "conversations:read"}, nil, nil)
	if err != nil {
		t.Fatalf("create app key a: %v", err)
	}
	_, appKeyB, err := appKeyService.CreateKey(context.Background(), "user_b", "user-b-app", []string{"requests:read", "requests:respond", "conversations:read"}, nil, nil)
	if err != nil {
		t.Fatalf("create app key b: %v", err)
	}

	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	server := httptest.NewServer(httpapp.MustNewRouter(httpapp.Input{
		Config:         cfg,
		MediaProcessor: testMediaProcessor(),
		ChatRepo:       st,
		AuthRepo:       st,
		ConfigRepo:     st,
		StorageRepo:    st,
		AuditRepo:      st,
		PlatformRepo:   st,
		Turn:           turnService,
		Query:          queryService,
		ModelAPIKeys:   modelKeyService,
		AppAPIKeys:     appKeyService,
		LoggerFactory:  logFactory,
	}))
	defer server.Close()

	resultCh := make(chan map[string]any, 1)
	go func() {
		resultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-query",
			"input": "hello pending query",
		}, map[string]string{
			"Authorization": "Bearer " + modelKeyA,
		}, http.StatusOK)
	}()

	request := waitForRequestForOwner(t, queryService, "user_a")
	if request.OwnerID != "user_a" {
		t.Fatalf("unexpected request owner: %#v", request)
	}
	if request.RequestFormat != "responses" {
		t.Fatalf("unexpected request format: %#v", request)
	}
	if request.InputText != "hello pending query" {
		t.Fatalf("unexpected input text: %#v", request)
	}
	if request.RequestPath != "/v1/responses" {
		t.Fatalf("unexpected request path: %#v", request)
	}

	listRespA := getJSONWithHeaders(t, server.URL+"/api/requests", map[string]string{
		"X-ChatAPI-App-Key": appKeyA,
	}, http.StatusOK)
	itemsA := listRespA["items"].([]any)
	if len(itemsA) != 1 {
		t.Fatalf("unexpected user a request list: %#v", listRespA)
	}

	listRespB := getJSONWithHeaders(t, server.URL+"/api/requests", map[string]string{
		"X-ChatAPI-App-Key": appKeyB,
	}, http.StatusOK)
	itemsB := listRespB["items"].([]any)
	if len(itemsB) != 0 {
		t.Fatalf("unexpected user b request list: %#v", listRespB)
	}

	getJSONWithHeaders(t, server.URL+"/api/requests/"+request.RequestID, map[string]string{
		"X-ChatAPI-App-Key": appKeyA,
	}, http.StatusOK)

	status, body := getTextWithHeaders(t, server.URL+"/api/requests/"+request.RequestID, map[string]string{
		"X-ChatAPI-App-Key": appKeyB,
	})
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("unexpected user b request access: status=%d body=%q", status, body)
	}

	completeResp := postJSONWithHeaders(t, server.URL+"/api/chat/output/complete", map[string]any{
		"conversation_id": request.ConversationID,
		"request_id":      request.RequestID,
		"text":            "done",
		"mode":            "assistant_message",
	}, map[string]string{
		"X-ChatAPI-App-Key": appKeyA,
	}, http.StatusOK)
	if completeResp["output_text"] != "done" {
		t.Fatalf("unexpected complete response: %#v", completeResp)
	}

	finalResp := <-resultCh
	if finalResp["output_text"] != "done" {
		t.Fatalf("unexpected final response: %#v", finalResp)
	}

	msgRespA := getJSONWithHeaders(t, server.URL+"/api/conversations/"+request.ConversationID+"/messages", map[string]string{
		"X-ChatAPI-App-Key": appKeyA,
	}, http.StatusOK)
	msgItemsA := msgRespA["items"].([]any)
	if len(msgItemsA) != 2 {
		t.Fatalf("unexpected user a messages: %#v", msgRespA)
	}

	status, body = getTextWithHeaders(t, server.URL+"/api/conversations/"+request.ConversationID+"/messages", map[string]string{
		"X-ChatAPI-App-Key": appKeyB,
	})
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("unexpected user b conversation access: status=%d body=%q", status, body)
	}

	status, body = postTextWithHeaders(t, server.URL+"/api/chat/output/complete", map[string]any{
		"conversation_id": request.ConversationID,
		"request_id":      request.RequestID,
		"text":            "intrude",
		"mode":            "assistant_message",
	}, map[string]string{
		"X-ChatAPI-App-Key": appKeyB,
	})
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("unexpected user b turn control access: status=%d body=%q", status, body)
	}
}

func waitForRequestForOwner(t *testing.T, query *turnquerysvc.Service, ownerID string) common.Request {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := query.ListRequestsForOwner(context.Background(), ownerID)
		if err == nil && len(items) > 0 {
			return items[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for request")
	return common.Request{}
}

func waitForRequestsForOwnerCount(t *testing.T, query *turnquerysvc.Service, ownerID string, count int) []common.Request {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := query.ListRequestsForOwner(context.Background(), ownerID)
		if err == nil && len(items) >= count {
			return items
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d requests", count)
	return nil
}

func postJSONWithHeaders(t *testing.T, url string, body map[string]any, headers map[string]string, wantStatus int) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	payload := decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
	return payload
}

func postTextWithHeaders(t *testing.T, url string, body map[string]any, headers map[string]string) (int, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(bodyBytes)
}

func getJSONWithHeaders(t *testing.T, url string, headers map[string]string, wantStatus int) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	return decodeJSONBody(t, resp.Body, resp.StatusCode, wantStatus)
}

func getTextWithHeaders(t *testing.T, url string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(bodyBytes)
}

func decodeJSONBody(t *testing.T, body io.Reader, gotStatus int, wantStatus int) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if gotStatus != wantStatus {
		t.Fatalf("unexpected status %d payload=%#v", gotStatus, payload)
	}
	return payload
}
