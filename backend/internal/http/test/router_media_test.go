package httpapi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	httpapi "github.com/zyf2007/ChatAPI/internal/http"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/identity"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/verification"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	adminops "github.com/zyf2007/ChatAPI/internal/service/admincontrol/ops"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
)

func TestBase64ImageSubmitDeleteConversationAndCleanupOrphan(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_media",
		Username: "media",
		Email:    "media@example.com",
		PasswordHash: mustPasswordHash(t, "media-pass"),
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := st.UpsertUserIdentity(context.Background(), common.UpsertUserIdentityInput{
		ID:            "identity_local_media",
		UserID:        "user_media",
		Provider:      "local",
		Subject:       "media@example.com",
		Email:         "media@example.com",
		EmailVerified: true,
	}); err != nil {
		t.Fatalf("create user identity: %v", err)
	}

	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	cfg.MediaDerivedDir = filepath.Join(t.TempDir(), "derived")
	policies := policy.NewService()
	sessionService, err := session.NewService(session.Config{Secret: "01234567890123456789012345678901"})
	if err != nil {
		t.Fatalf("new session service: %v", err)
	}
	verificationService := verification.NewService(st, &memorySender{})
	accountService := account.NewService(st)
	localService := localauth.NewService(accountService, st, policies, sessionService, verificationService)
	identityService := identity.NewService(accountService)

	pendingRegistry := pending.NewPendingRegistry()
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:    st,
			Pending:  pendingRegistry,
			Realtime: noopRealtime{},
		},
		Pending:            pendingRegistry,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	modelKeyService := modelkey.NewService(st, "test-master-key")
	_, modelKey, err := modelKeyService.CreateKey(context.Background(), "user_media", "media-model", "demo-media")
	if err != nil {
		t.Fatalf("create model key: %v", err)
	}

	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:        cfg,
		ChatRepo:      st,
		AuthRepo:      st,
		ConfigRepo:    st,
		StorageRepo:   st,
		AuditRepo:     st,
		PlatformRepo:  st,
		LocalAuth:     localService,
		Verification:  verificationService,
		Policy:        policies,
		Accounts:      accountService,
		Identity:      identityService,
		UserSessions:  sessionService,
		Turn:          turnService,
		Query:         queryService,
		ModelAPIKeys:  modelKeyService,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	resultCh := make(chan map[string]any, 1)
	go func() {
		resultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-media",
			"input": []any{
				map[string]any{"type": "input_text", "text": "see image"},
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNGBytes(t))},
			},
		}, map[string]string{
			"Authorization": "Bearer " + modelKey,
		}, http.StatusOK)
	}()

	req := waitForRequestForOwner(t, queryService, "user_media")
	requestJSON, err := json.Marshal(req.RequestBody)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	if strings.Contains(string(requestJSON), "data:image/") || strings.Contains(string(requestJSON), "iVBOR") {
		t.Fatalf("request body still contains base64 payload: %s", string(requestJSON))
	}
	assets, err := st.ListMediaAssets(context.Background())
	if err != nil {
		t.Fatalf("list media assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 media asset, got %#v", assets)
	}
	if _, err := os.Stat(assets[0].Path); err != nil {
		t.Fatalf("expected asset file to exist: %v", err)
	}
	cookie := loginAndGetCookie(t, server.URL, "media@example.com", "media-pass")
	status, _ := getTextWithCookie(t, server.URL+"/api/media/assets/"+assets[0].FileID, cookie)
	if status != http.StatusOK {
		t.Fatalf("expected authenticated image fetch status 200, got %d", status)
	}

	if _, err := turnService.CompleteConversation(context.Background(), common.CompletePendingInput{
		ConversationID: req.ConversationID,
		ResponseID:     "resp_done",
		OutputText:     "done",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete conversation: %v", err)
	}
	<-resultCh

	deleteResult, err := st.DeleteConversations(context.Background(), []string{req.ConversationID})
	if err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	if deleteResult.DeletedConversations != 1 || deleteResult.DeletedAssetRefs != 1 {
		t.Fatalf("unexpected delete result: %#v", deleteResult)
	}
	if _, err := os.Stat(assets[0].Path); err != nil {
		t.Fatalf("expected file to remain before orphan cleanup: %v", err)
	}
	orphanAssets, err := st.ListOrphanMediaAssets(context.Background())
	if err != nil {
		t.Fatalf("list orphan media assets: %v", err)
	}
	if len(orphanAssets) != 1 {
		t.Fatalf("expected orphan asset after conversation delete: %#v", orphanAssets)
	}

	opsService := adminops.New(st, localstore.Store{RootDir: cfg.MediaDerivedDir})
	cleanupResult, err := opsService.CleanupOrphanImages(context.Background())
	if err != nil {
		t.Fatalf("cleanup orphan images: %v", err)
	}
	if cleanupResult.DeletedAssetRecords != 1 {
		t.Fatalf("unexpected cleanup result: %#v", cleanupResult)
	}
	if _, err := os.Stat(assets[0].Path); !os.IsNotExist(err) {
		t.Fatalf("expected file removed by orphan cleanup, stat err=%v", err)
	}
}

func TestResponsesMessageContentDataURLTranscodesToAVIF(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_media_resp",
		Username: "mediaresp",
		Email:    "mediaresp@example.com",
		PasswordHash: mustPasswordHash(t, "mediaresp-pass"),
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	cfg.MediaDerivedDir = filepath.Join(t.TempDir(), "derived")

	pendingRegistry := pending.NewPendingRegistry()
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:    st,
			Pending:  pendingRegistry,
			Realtime: noopRealtime{},
		},
		Pending:            pendingRegistry,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	modelKeyService := modelkey.NewService(st, "test-master-key")
	_, modelKey, err := modelKeyService.CreateKey(context.Background(), "user_media_resp", "media-model", "demo-media")
	if err != nil {
		t.Fatalf("create model key: %v", err)
	}

	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:        cfg,
		ChatRepo:      st,
		AuthRepo:      st,
		ConfigRepo:    st,
		StorageRepo:   st,
		AuditRepo:     st,
		PlatformRepo:  st,
		Turn:          turnService,
		Query:         queryService,
		ModelAPIKeys:  modelKeyService,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	resultCh := make(chan map[string]any, 1)
	go func() {
		resultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-media",
			"input": []any{
				map[string]any{
					"content": []any{
						map[string]any{"type": "input_text", "text": "see image"},
						map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNGBytes(t))},
					},
				},
			},
		}, map[string]string{
			"Authorization": "Bearer " + modelKey,
		}, http.StatusOK)
	}()

	req := waitForRequestForOwner(t, queryService, "user_media_resp")
	assets, err := st.ListMediaAssets(context.Background())
	if err != nil {
		t.Fatalf("list media assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 media asset, got %#v", assets)
	}
	if filepath.Ext(assets[0].Path) != ".avif" {
		t.Fatalf("expected avif asset path, got %#v", assets[0])
	}
	inputItems := req.RequestBody["input"].([]any)
	if len(inputItems) != 2 {
		t.Fatalf("unexpected sanitized request body input: %#v", req.RequestBody)
	}
	imageItem := inputItems[1].(map[string]any)
	if !strings.Contains(imageItem["image_url"].(string), "/api/media/assets/") {
		t.Fatalf("expected sanitized request body image url: %#v", req.RequestBody)
	}

	if _, err := turnService.CompleteConversation(context.Background(), common.CompletePendingInput{
		ConversationID: req.ConversationID,
		ResponseID:     "resp_done",
		OutputText:     "done",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete conversation: %v", err)
	}
	<-resultCh
}

func TestJPEGDataURLWithWhitespaceTranscodesToAVIF(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	logFactory, err := logging.NewFactory(logging.Config{Level: "debug", Format: "json"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	st.Logger = logFactory.Layer(logging.LayerRepository)
	if _, err := st.CreateUser(context.Background(), common.CreateUserInput{
		ID:       "user_media_jpeg",
		Username: "mediajpeg",
		Email:    "mediajpeg@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	cfg.MediaDerivedDir = filepath.Join(t.TempDir(), "derived")

	pendingRegistry := pending.NewPendingRegistry()
	turnService := &turnsvc.Service{
		Submitter: &turnsvc.Submitter{
			Store:    st,
			Pending:  pendingRegistry,
			Realtime: noopRealtime{},
		},
		Pending:            pendingRegistry,
		Store:              st,
		OwnerIDFromContext: actor.OwnerIDFromContext,
		ActorFromContext:   actor.FromContext,
		Logger:             logFactory.Layer(logging.LayerTurn),
	}
	queryService := &turnquerysvc.Service{Store: st, Logger: logFactory.Layer(logging.LayerTurnQuery)}
	modelKeyService := modelkey.NewService(st, "test-master-key")
	_, modelKey, err := modelKeyService.CreateKey(context.Background(), "user_media_jpeg", "media-model", "demo-media")
	if err != nil {
		t.Fatalf("create model key: %v", err)
	}

	server := httptest.NewServer(httpapi.NewRouter(httpapi.RouterDeps{
		Config:        cfg,
		ChatRepo:      st,
		AuthRepo:      st,
		ConfigRepo:    st,
		StorageRepo:   st,
		AuditRepo:     st,
		PlatformRepo:  st,
		Turn:          turnService,
		Query:         queryService,
		ModelAPIKeys:  modelKeyService,
		LoggerFactory: logFactory,
	}))
	defer server.Close()

	jpegPayload := base64.StdEncoding.EncodeToString(tinyJPEGBytes(t))
	resultCh := make(chan map[string]any, 1)
	go func() {
		resultCh <- postJSONWithHeaders(t, server.URL+"/v1/responses", map[string]any{
			"model": "demo-media",
			"input": []any{
				map[string]any{"type": "input_text", "text": "see image"},
				map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + jpegPayload[:32] + " \n\t" + jpegPayload[32:]},
			},
		}, map[string]string{
			"Authorization": "Bearer " + modelKey,
		}, http.StatusOK)
	}()

	req := waitForRequestForOwner(t, queryService, "user_media_jpeg")
	assets, err := st.ListMediaAssets(context.Background())
	if err != nil {
		t.Fatalf("list media assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 media asset, got %#v", assets)
	}
	if filepath.Ext(assets[0].Path) != ".avif" {
		t.Fatalf("expected avif asset path, got %#v", assets[0])
	}

	if _, err := turnService.CompleteConversation(context.Background(), common.CompletePendingInput{
		ConversationID: req.ConversationID,
		ResponseID:     "resp_done",
		OutputText:     "done",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete conversation: %v", err)
	}
	<-resultCh
}

func tinyPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{B: 255, A: 255})
	var buf bytesBuffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func tinyJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{B: 255, A: 255})
	var buf bytesBuffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesBuffer) Bytes() []byte { return append([]byte(nil), b.data...) }

func mustPasswordHash(t *testing.T, raw string) string {
	t.Helper()
	value, err := passwordHash(raw)
	if err != nil {
		t.Fatalf("password hash: %v", err)
	}
	return value
}
