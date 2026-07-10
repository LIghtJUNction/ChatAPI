package turn_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	"github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputasset"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputpolicy"
	"github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	"github.com/zyf2007/ChatAPI/internal/service/chat/protocolruntime"
	"github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

func TestUpdateDraftAutomaticallyCompletesOnCrossChunkStop(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "gpt-4o",
		Options:  protocol.TurnOptions{Stop: []string{"END"}},
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_stop", RequestID: "req_stop", ResponseID: "resp_stop",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		RequestID: "req_stop", ResponseID: "resp_stop", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_stop"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	first, err := service.UpdateDraft(ctx, conversation.ID, "answer E", "answer", "")
	if err != nil {
		t.Fatal(err)
	}
	if first["draft_text"] != "answer " {
		t.Fatalf("stop prefix was not withheld: %#v", first)
	}
	second, err := service.UpdateDraft(ctx, conversation.ID, "ND ignored", "answer", "")
	if err != nil {
		t.Fatal(err)
	}
	if second["auto_completed"] != true || second["output_text"] != "answer" {
		t.Fatalf("unexpected automatic completion: %#v", second)
	}
	if _, ok := registry.GetByConversationID(conversation.ID); ok {
		t.Fatal("automatically completed turn remained pending")
	}
	messages, err := store.ListMessages(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	policy, _ := last.Metadata["output_policy"].(map[string]any)
	if policy["finish_reason"] != "stop_sequence" || policy["stop_sequence"] != "END" {
		t.Fatalf("missing final output policy metadata: %#v", last.Metadata)
	}
}

func TestConcurrentDeltasKeepGuardAndPersistedDraftInSync(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{Protocol: protocol.ProtocolResponses, Model: "gpt-4o"}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_concurrent", RequestID: "req_concurrent", ResponseID: "resp_concurrent",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		RequestID: "req_concurrent", ResponseID: "resp_concurrent", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_concurrent"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	start := make(chan struct{})
	errorsByDelta := make(chan error, 2)
	var wait sync.WaitGroup
	for _, delta := range []string{"A", "B"} {
		wait.Add(1)
		go func(text string) {
			defer wait.Done()
			<-start
			_, updateErr := service.UpdateDraft(ctx, conversation.ID, text, "answer", "")
			errorsByDelta <- updateErr
		}(delta)
	}
	close(start)
	wait.Wait()
	close(errorsByDelta)
	for updateErr := range errorsByDelta {
		if updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	result, err := service.CompleteConversation(ctx, common.CompletePendingInput{
		ConversationID: conversation.ID, ResponseID: "resp_concurrent", Mode: "answer",
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result["output_text"].(string)
	if output != "AB" && output != "BA" {
		t.Fatalf("concurrent delta was lost from persisted output: %q", output)
	}
}

func TestAutomaticThinkingCompletionPersistsMaterializedContentOnce(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "gpt-4o",
		Options:  protocol.TurnOptions{Stop: []string{"END"}},
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_thinking", RequestID: "req_thinking", ResponseID: "resp_thinking",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		RequestID: "req_thinking", ResponseID: "resp_thinking", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_thinking"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	if _, err := service.UpdateDraft(ctx, conversation.ID, "reason E", "thinking", "reasoning"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateDraft(ctx, conversation.ID, "ND", "thinking", "reasoning"); err != nil {
		t.Fatal(err)
	}
	messages, err := store.ListMessages(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	content := messages[len(messages)-1].Content
	if content != "<think>reason </think>" {
		t.Fatalf("thinking content was rematerialized by persistence: %q", content)
	}
	completed, err := store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.LastMessagePreview != "reason " {
		t.Fatalf("thinking markup leaked into conversation preview: %q", completed.LastMessagePreview)
	}
}

func TestImageGenerationPersistsURLAndOnlyStreamsDerivedBase64(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_image", RequestID: "req_image", ResponseID: "resp_image",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "draw",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses, Model: "gpt-4o",
		BuiltinTools: []protocol.BuiltinTool{{Kind: "image_generation", Type: "image_generation"}},
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	pendingTurn := &turn.PendingTurn{
		RequestID: "req_image", ResponseID: "resp_image", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_image"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	}
	registry := pending.NewPendingRegistry()
	registry.Add(pendingTurn)
	assetService := outputasset.New(config.Config{
		UploadMaxBytes: 1 << 20, MediaMaxBytes: 1 << 20, MediaAVIFQuality: 50,
	}, store, localstore.Store{RootDir: t.TempDir()})
	service := &turn.Service{
		Store: store, Pending: registry, OutputAssets: assetService,
		OwnerIDFromContext: func(context.Context) string { return "user_a" },
	}
	uploaded, err := service.UploadOutputImage(ctx, "user_a", conversation.ID, "result.png", "image/png", bytes.NewReader(outputTestPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := service.EmitBuiltinTool(ctx, turn.TurnControlCommand{
		ConversationID: conversation.ID,
		Action: turn.OutputAction{
			Kind: turn.TurnControlBuiltinTool, BuiltinToolKind: "image_generation", BuiltinToolAssetID: uploaded.AssetID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := body["event"].(common.ConversationEvent)
	metadataJSON, _ := json.Marshal(event.Metadata)
	if strings.Contains(string(metadataJSON), "base64") || strings.Contains(string(metadataJSON), uploaded.URL) {
		t.Fatalf("timeline metadata must not duplicate media authority: %s", metadataJSON)
	}
	if len(event.MediaAssets) != 1 || event.MediaAssets[0].URL != uploaded.URL {
		t.Fatalf("event media ref did not retain URL: %#v", event.MediaAssets)
	}
	var refCount, stagingCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM media_asset_event_refs WHERE event_id = ?`, event.ID).Scan(&refCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM media_asset_staging WHERE asset_id = ?`, uploaded.AssetID).Scan(&stagingCount); err != nil {
		t.Fatal(err)
	}
	if refCount != 1 || stagingCount != 0 {
		t.Fatalf("unexpected asset lifecycle: refs=%d staging=%d", refCount, stagingCount)
	}
	pendingEvent := <-pendingTurn.Events
	var resultBase64 string
	for _, streamEvent := range pendingEvent.StreamEvents {
		if streamEvent.Event != "response.output_item.done" {
			continue
		}
		data, _ := streamEvent.Data.(map[string]any)
		item, _ := data["item"].(map[string]any)
		resultBase64, _ = item["result"].(string)
	}
	decoded, err := base64.StdEncoding.DecodeString(resultBase64)
	if err != nil || len(decoded) == 0 {
		t.Fatalf("missing protocol image base64: %v", err)
	}
	mediaType, _, _, err := media.InspectImageBytes(decoded)
	if err != nil || mediaType != "image/avif" {
		t.Fatalf("protocol result was not AVIF: media=%q err=%v", mediaType, err)
	}
}

func outputTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{B: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
