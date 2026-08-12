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

func BearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return token, token != ""
}

func VerifyBearer(header, expectedHash string) bool {
	if strings.TrimSpace(expectedHash) == "" {
		return false
	}
	token, ok := BearerToken(header)
	if !ok {
		return false
	}

	actual := HashToken(token)
	expected := strings.ToLower(strings.TrimSpace(expectedHash))
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}
