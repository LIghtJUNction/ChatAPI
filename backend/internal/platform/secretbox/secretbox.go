package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrMissingMasterKey = errors.New("master key is required")

func Seal(plaintext string, masterKey string) (string, error) {
	masterKey = strings.TrimSpace(masterKey)
	if masterKey == "" {
		return "", ErrMissingMasterKey
	}
	gcm, err := newGCM(masterKey)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return "v1." + base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func Open(sealed string, masterKey string) (string, error) {
	masterKey = strings.TrimSpace(masterKey)
	if masterKey == "" {
		return "", ErrMissingMasterKey
	}
	parts := strings.Split(strings.TrimSpace(sealed), ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return "", errors.New("invalid sealed secret format")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	gcm, err := newGCM(masterKey)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("open secret: %w", err)
	}
	return string(plaintext), nil
}

func newGCM(masterKey string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(masterKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return gcm, nil
}
