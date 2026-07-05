package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

func Prefix(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) <= 12 {
		return raw
	}
	return raw[:12]
}

func Hash(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func Verify(raw string, expectedHash string) bool {
	actual := Hash(raw)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedHash)) == 1
}
