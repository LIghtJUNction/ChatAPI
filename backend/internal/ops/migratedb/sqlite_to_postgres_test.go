package migratedb

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/testutil/pgtest"
)

func TestSafePostgresTargetRemovesCredentialsAndQuery(t *testing.T) {
	t.Parallel()

	got := safePostgresTarget("postgres://chatapi:very-secret@db.internal:5432/chatapi?sslmode=disable&password=also-secret")
	if want := "postgres://db.internal:5432/chatapi"; got != want {
		t.Fatalf("safePostgresTarget() = %q, want %q", got, want)
	}
}

func TestSQLiteToPostgresRejectsUndeclaredSourceTable(t *testing.T) {
	ctx := context.Background()
	sqlitePath := filepath.Join(t.TempDir(), "chatapi.sqlite3")
	source, err := sqlitestore.Open(sqlitePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Bootstrap(ctx, source.DB()); err != nil {
		t.Fatal(err)
	}
	if _, err := source.DB().ExecContext(ctx, `CREATE TABLE future_business_data(id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = SQLiteToPostgres(ctx, sqlitePath, pgtest.IsolatedDSN(t))
	if err == nil || !strings.Contains(err.Error(), "future_business_data") {
		t.Fatalf("undeclared table migration error=%v", err)
	}
}

func TestSQLiteToPostgresRejectsUnexpectedSourceColumn(t *testing.T) {
	ctx := context.Background()
	sqlitePath := filepath.Join(t.TempDir(), "chatapi.sqlite3")
	source, err := sqlitestore.Open(sqlitePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Bootstrap(ctx, source.DB()); err != nil {
		t.Fatal(err)
	}
	if _, err := source.DB().ExecContext(ctx, `ALTER TABLE users ADD COLUMN future_business_value TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = SQLiteToPostgres(ctx, sqlitePath, pgtest.IsolatedDSN(t))
	if err == nil || !strings.Contains(err.Error(), "users") {
		t.Fatalf("unexpected column migration error=%v", err)
	}
}

func TestValidateSQLiteValuesRejectsInvalidJSONAndTime(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		seed    string
		wantErr string
	}{
		{
			name: "json",
			seed: `INSERT INTO config(key, value_json, created_at, updated_at)
				VALUES ('invalid', '{', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			wantErr: "config.value_json",
		},
		{
			name: "required time",
			seed: `INSERT INTO users(id, username, email, created_at, updated_at)
				VALUES ('invalid-time', 'invalid-time', 'invalid-time@example.com', 'not-a-time', '2026-01-01T00:00:00Z')`,
			wantErr: "users.created_at",
		},
		{
			name: "optional time",
			seed: `INSERT INTO users(id, username, email, created_at, updated_at, last_login_at)
				VALUES ('invalid-optional-time', 'invalid-optional-time', 'invalid-optional@example.com', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'not-a-time')`,
			wantErr: "users.last_login_at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			if err := migrations.Bootstrap(ctx, source.DB()); err != nil {
				t.Fatal(err)
			}
			if _, err := source.DB().ExecContext(ctx, tt.seed); err != nil {
				t.Fatal(err)
			}
			err = validateSQLiteValues(ctx, source.DB())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validation error=%v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestSafePostgresTargetHidesUnsupportedDSNForms(t *testing.T) {
	t.Parallel()

	if got := safePostgresTarget("host=db.internal password=very-secret"); got != "postgresql" {
		t.Fatalf("safePostgresTarget() = %q, want postgresql", got)
	}
}

func TestSQLiteToPostgresPreservesAPIKeySecrets(t *testing.T) {
	ctx := context.Background()
	sqlitePath := filepath.Join(t.TempDir(), "chatapi.sqlite3")
	source, err := sqlitestore.Open(sqlitePath)
	if err != nil {
		t.Fatalf("open sqlite source: %v", err)
	}
	if err := migrations.Bootstrap(ctx, source.DB()); err != nil {
		t.Fatalf("bootstrap sqlite source: %v", err)
	}
	if _, err := source.DB().ExecContext(ctx, `
		INSERT INTO users(id, username, email, created_at, updated_at) VALUES ('user_1', 'user', 'user@example.com', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		INSERT INTO user_api_keys(id, user_id, name, key_ciphertext, key_hash, key_prefix, model, created_at)
		VALUES ('model_key_1', 'user_1', 'model key', 'model-ciphertext', 'model-hash', 'mk-prefix', 'model-a', '2026-01-01T00:00:00Z');
		INSERT INTO user_app_api_keys(id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, created_at)
		VALUES ('app_key_1', 'user_1', 'app key', 'app-hash', 'app-ciphertext', 'ak-prefix', '["chat"]', '{"requests":10}', '2026-01-01T00:00:00Z');
	`); err != nil {
		t.Fatalf("seed sqlite source: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close sqlite source: %v", err)
	}

	postgresDSN := pgtest.IsolatedDSN(t)
	if _, err := SQLiteToPostgres(ctx, sqlitePath, postgresDSN); err != nil {
		t.Fatalf("migrate sqlite to postgresql: %v", err)
	}
	pool, err := pgxpool.New(ctx, postgresDSN)
	if err != nil {
		t.Fatalf("open postgresql target: %v", err)
	}
	defer pool.Close()

	var modelCiphertext, modelHash string
	if err := pool.QueryRow(ctx, `SELECT key_ciphertext, key_hash FROM user_api_keys WHERE id='model_key_1'`).Scan(&modelCiphertext, &modelHash); err != nil {
		t.Fatalf("read migrated model key: %v", err)
	}
	if modelCiphertext != "model-ciphertext" || modelHash != "model-hash" {
		t.Fatalf("model key secrets changed: ciphertext=%q hash=%q", modelCiphertext, modelHash)
	}

	var appHash, appCiphertext string
	if err := pool.QueryRow(ctx, `SELECT key_hash, key_ciphertext FROM user_app_api_keys WHERE id='app_key_1'`).Scan(&appHash, &appCiphertext); err != nil {
		t.Fatalf("read migrated app key: %v", err)
	}
	if appHash != "app-hash" || appCiphertext != "app-ciphertext" {
		t.Fatalf("app key secrets changed: hash=%q ciphertext=%q", appHash, appCiphertext)
	}
}
