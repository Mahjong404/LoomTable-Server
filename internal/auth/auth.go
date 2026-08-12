package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

func HashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func VerifyBearer(header, expectedHash string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) || strings.TrimSpace(expectedHash) == "" {
		return false
	}

	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return false
	}

	actual := HashToken(token)
	expected := strings.ToLower(strings.TrimSpace(expectedHash))
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}