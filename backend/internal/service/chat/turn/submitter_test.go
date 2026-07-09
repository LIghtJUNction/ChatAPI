package turn

import (
	"context"
	"errors"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
)

func TestSubmitRecordsDeletionFailureWhenRollbackCleanupFails(t *testing.T) {
	recorder := &deletionFailureRecorderStub{}
	submitter := &Submitter{
		Store: failingCreatePendingStore{},
		Materializer: &RequestMaterializer{
			Preprocessor: preprocessorStub{
				result: preprocesssvc.PreparedRequest{
					Request: protocol.TurnRequest{
						Protocol: protocol.ProtocolResponses,
						Model:    "demo",
						InputParts: []protocol.InputPart{
							{Type: "text", Text: "hello"},
							{Type: "image", MediaType: "image/avif", URL: "/api/media/assets/file_test"},
						},
						UserContent: "hello",
					},
					PreparedImages: []media.DraftAsset{
						{
							FileID:    "file_test",
							OwnerID:   "user_a",
							MediaType: "image/avif",
							PublicURL: "/api/media/assets/file_test",
							Data:      []byte("avif-bytes"),
							Bytes:     int64(len([]byte("avif-bytes"))),
						},
					},
				},
			},
			AssetPersister: persisterStub{
				asset: media.StoredAsset{
					FileID:    "file_test",
					OwnerID:   "user_a",
					Path:      "/tmp/file_test.avif",
					PublicURL: "/api/media/assets/file_test",
					MediaType: "image/avif",
					Bytes:     int64(len([]byte("avif-bytes"))),
				},
			},
			PreparedImageClean: cleanerStub{err: errors.New("remove failed")},
			DeletionFailures:   recorder,
		},
	}

	_, _, _, err := submitter.Submit(context.Background(), SubmitInput{
		OwnerID: "user_a",
		Request: protocol.TurnRequest{
			Protocol: protocol.ProtocolResponses,
			Model:    "demo",
			InputParts: []protocol.InputPart{
				{Type: "text", Text: "hello"},
				{Type: "image", MediaType: "image/png", URL: "ignored"},
			},
			UserContent: "hello",
		},
		RequestMeta: common.Request{},
	})
	if err == nil {
		t.Fatal("expected submit failure")
	}
	if len(recorder.items) != 1 {
		t.Fatalf("expected one deletion failure record, got %#v", recorder.items)
	}
	got := recorder.items[0]
	if got.Path != "/tmp/file_test.avif" || got.OwnerID != "user_a" || got.Filename != "file_test.avif" {
		t.Fatalf("unexpected deletion failure record: %#v", got)
	}
}

type failingCreatePendingStore struct{}

func (failingCreatePendingStore) CreatePendingTurn(context.Context, common.CreatePendingInput) (common.Conversation, common.Message, error) {
	return common.Conversation{}, common.Message{}, errors.New("create pending failed")
}

type preprocessorStub struct {
	result preprocesssvc.PreparedRequest
	err    error
}

func (s preprocessorStub) Prepare(context.Context, string, protocol.TurnRequest) (preprocesssvc.PreparedRequest, error) {
	return s.result, s.err
}

type persisterStub struct {
	asset media.StoredAsset
	err   error
}

func (s persisterStub) PersistDraft(context.Context, media.DraftAsset) (media.StoredAsset, error) {
	return s.asset, s.err
}

type cleanerStub struct {
	err error
}

func (s cleanerStub) DeletePreparedImage(context.Context, string) error {
	return s.err
}

type deletionFailureRecorderStub struct {
	items []common.UpsertStorageFileDeletionFailureInput
}

func (s *deletionFailureRecorderStub) UpsertStorageFileDeletionFailure(_ context.Context, input common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error) {
	s.items = append(s.items, input)
	return common.StorageFileDeletionFailure{
		Path:      input.Path,
		Filename:  input.Filename,
		OwnerID:   input.OwnerID,
		Bytes:     input.Bytes,
		LastError: input.LastError,
	}, nil
}
