package password

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestHashAndVerifyArgon2id(t *testing.T) {
	encoded, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format: %q", encoded)
	}

	result, err := Verify("correct horse battery staple", encoded)
	if err != nil {
		t.Fatalf("verify password: %v", err)
	}
	if !result.OK || result.NeedsUpgrade {
		t.Fatalf("unexpected verification result: %#v", result)
	}

	result, err = Verify("wrong", encoded)
	if err != nil {
		t.Fatalf("verify wrong password should not error: %v", err)
	}
	if result.OK || result.NeedsUpgrade {
		t.Fatalf("wrong password should fail without upgrade: %#v", result)
	}
}

func TestVerifyLegacySHA256RequestsUpgrade(t *testing.T) {
	salt := "legacy-salt"
	sum := sha256.Sum256([]byte(salt + "secret"))
	legacy := fmt.Sprintf("%s$%x", salt, sum[:])

	result, err := Verify("secret", legacy)
	if err != nil {
		t.Fatalf("verify legacy password: %v", err)
	}
	if !result.OK || !result.NeedsUpgrade {
		t.Fatalf("legacy password should verify and request upgrade: %#v", result)
	}

	result, err = Verify("wrong", legacy)
	if err != nil {
		t.Fatalf("verify wrong legacy password should not error: %v", err)
	}
	if result.OK || result.NeedsUpgrade {
		t.Fatalf("wrong legacy password should fail without upgrade: %#v", result)
	}
}

func TestVerifyRejectsInvalidHash(t *testing.T) {
	if _, err := Verify("secret", "not-a-valid-hash"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("expected invalid hash error, got %v", err)
	}
	if _, err := Verify("secret", "$argon2id$v=19$m=x,t=3,p=1$salt$hash"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("expected invalid argon params error, got %v", err)
	}
}
