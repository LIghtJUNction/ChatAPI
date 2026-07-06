package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	platformemail "github.com/zyf/chatapi/internal/platform/email"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/store"
)

func TestEmailCodeServiceCleanupExpired(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.DB().Close()
	})
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite store: %v", err)
	}

	svc := NewEmailCodeService(st, "test-master-key", platformemail.SMTPConfig{}, nil)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	if _, err := st.UpsertAuthVerificationCode(context.Background(), store.UpsertAuthVerificationCodeInput{
		Email:          "expired@example.com",
		Purpose:        PurposeRegister(),
		CodeHash:       "expired-hash",
		FailedAttempts: 0,
		ExpiresAt:      now.Add(-time.Minute),
		LastSentAt:     now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("create expired code: %v", err)
	}
	if _, err := st.UpsertAuthVerificationCode(context.Background(), store.UpsertAuthVerificationCodeInput{
		Email:          "active@example.com",
		Purpose:        PurposePasswordReset(),
		CodeHash:       "active-hash",
		FailedAttempts: 0,
		ExpiresAt:      now.Add(time.Minute),
		LastSentAt:     now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("create active code: %v", err)
	}

	deleted, err := svc.CleanupExpired(context.Background(), now)
	if err != nil {
		t.Fatalf("cleanup expired codes: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one deleted code, got %d", deleted)
	}
	if _, err := st.GetAuthVerificationCode(context.Background(), "expired@example.com", PurposeRegister()); err == nil {
		t.Fatal("expected expired code to be deleted")
	}
	if _, err := st.GetAuthVerificationCode(context.Background(), "active@example.com", PurposePasswordReset()); err != nil {
		t.Fatalf("expected active code to remain: %v", err)
	}
}
