package cursor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

const version = "v1"

var ErrInvalid = errors.New("invalid cursor")

type Signer struct {
	key []byte
}

func NewSigner(key []byte) (*Signer, error) {
	if len(key) != 32 {
		return nil, errors.New("cursor key must be exactly 32 bytes")
	}
	return &Signer{key: append([]byte(nil), key...)}, nil
}

func (s *Signer) Encode(purpose string, payload any) (string, error) {
	if s == nil || len(s.key) != 32 || purpose == "" {
		return "", ErrInvalid
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(encodedPayload)
	message := version + "." + purpose + "." + payloadPart
	signature := hmac.New(sha256.New, derivePurposeKey(s.key, purpose))
	_, _ = signature.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil)), nil
}

func (s *Signer) Decode(purpose, token string, destination any) error {
	if s == nil || len(s.key) != 32 || purpose == "" || destination == nil {
		return ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != version || parts[1] != purpose {
		return ErrInvalid
	}
	message := strings.Join(parts[:3], ".")
	expected := hmac.New(sha256.New, derivePurposeKey(s.key, purpose))
	_, _ = expected.Write([]byte(message))
	provided, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || !hmac.Equal(provided, expected.Sum(nil)) {
		return ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || json.Unmarshal(payload, destination) != nil {
		return ErrInvalid
	}
	return nil
}

func derivePurposeKey(key []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("loomtable-cursor/" + purpose))
	return mac.Sum(nil)
}
