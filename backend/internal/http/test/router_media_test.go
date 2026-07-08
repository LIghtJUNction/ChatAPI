package httpapi_test

import (
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	httpapi "github.com/zyf2007/ChatAPI/internal/http"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
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
			Store:              st,
			Pending:            pendingRegistry,
			Realtime:           noopRealtime{},
			PreparedImageClean: localstore.Store{RootDir: cfg.MediaDerivedDir},
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

type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesBuffer) Bytes() []byte { return append([]byte(nil), b.data...) }
