package pgtest

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func BaseDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHATAPI_PG_TEST_DSN"))
	if dsn == "" {
		t.Skip("CHATAPI_PG_TEST_DSN is not set")
	}
	return dsn
}

func IsolatedDSN(t *testing.T) string {
	t.Helper()
	baseDSN := BaseDSN(t)
	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("open postgresql admin pool: %v", err)
	}
	defer adminPool.Close()

	schemaName := testSchemaName(t.Name())
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+quoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create postgresql test schema %q: %v", schemaName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = adminPool.Exec(dropCtx, `DROP SCHEMA IF EXISTS `+quoteIdentifier(schemaName)+` CASCADE`)
	})

	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse postgresql dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func testSchemaName(testName string) string {
	name := strings.ToLower(testName)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	var builder strings.Builder
	builder.WriteString("chatapi_test_")
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			builder.WriteRune(ch)
		}
	}
	builder.WriteString("_")
	builder.WriteString(strings.ReplaceAll(uuid.NewString(), "-", ""))
	return builder.String()
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
