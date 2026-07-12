package password

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	defaultMemory  uint32 = 64 * 1024
	defaultTime    uint32 = 3
	defaultThreads uint8  = 1
	defaultKeyLen  uint32 = 32
	defaultSaltLen        = 16
)

var ErrInvalidHash = errors.New("invalid password hash")

type VerificationResult struct {
	OK           bool
	NeedsUpgrade bool
}

func Hash(plain string) (string, error) {
	salt := make([]byte, defaultSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plain), salt, defaultTime, defaultMemory, defaultThreads, defaultKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		defaultMemory,
		defaultTime,
		defaultThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func Verify(plain string, encoded string) (VerificationResult, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return VerificationResult{}, ErrInvalidHash
	}
	if strings.HasPrefix(encoded, "$argon2id$") {
		ok, err := verifyArgon2id(plain, encoded)
		return VerificationResult{OK: ok, NeedsUpgrade: false}, err
	}
	ok, err := verifyLegacySHA256(plain, encoded)
	return VerificationResult{OK: ok, NeedsUpgrade: ok}, err
}

func verifyArgon2id(plain string, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, ErrInvalidHash
	}
	params, err := parseArgon2Params(parts[3])
	if err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}
	actual := argon2.IDKey([]byte(plain), salt, params.time, params.memory, params.threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parseArgon2Params(raw string) (argon2Params, error) {
	values := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return argon2Params{}, ErrInvalidHash
		}
		values[key] = value
	}
	memory, err := parseUint32(values["m"])
	if err != nil {
		return argon2Params{}, ErrInvalidHash
	}
	timeCost, err := parseUint32(values["t"])
	if err != nil {
		return argon2Params{}, ErrInvalidHash
	}
	threads64, err := strconv.ParseUint(values["p"], 10, 8)
	if err != nil || threads64 == 0 {
		return argon2Params{}, ErrInvalidHash
	}
	if memory == 0 || timeCost == 0 {
		return argon2Params{}, ErrInvalidHash
	}
	return argon2Params{memory: memory, time: timeCost, threads: uint8(threads64)}, nil
}

func parseUint32(raw string) (uint32, error) {
	value, err := strconv.ParseUint(raw, 10, 32)
	return uint32(value), err
}

func verifyLegacySHA256(plain string, encoded string) (bool, error) {
	salt, legacyHash, ok := strings.Cut(encoded, "$")
	if !ok || salt == "" || legacyHash == "" {
		return false, ErrInvalidHash
	}
	sum := sha256.Sum256([]byte(salt + plain))
	actual := fmt.Sprintf("%x", sum[:])
	return subtle.ConstantTimeCompare([]byte(actual), []byte(legacyHash)) == 1, nil
}
