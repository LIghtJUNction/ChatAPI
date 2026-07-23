package migratedb

import "testing"

func TestSafePostgresTargetRemovesCredentialsAndQuery(t *testing.T) {
	t.Parallel()

	got := safePostgresTarget("postgres://chatapi:very-secret@db.internal:5432/chatapi?sslmode=disable&password=also-secret")
	if want := "postgres://db.internal:5432/chatapi"; got != want {
		t.Fatalf("safePostgresTarget() = %q, want %q", got, want)
	}
}

func TestSafePostgresTargetHidesUnsupportedDSNForms(t *testing.T) {
	t.Parallel()

	if got := safePostgresTarget("host=db.internal password=very-secret"); got != "postgresql" {
		t.Fatalf("safePostgresTarget() = %q, want postgresql", got)
	}
}
