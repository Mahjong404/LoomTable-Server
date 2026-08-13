package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

type adminStore struct {
	state     string
	name      string
	nameKey   string
	tokenHash string
}

func (s *adminStore) BootstrapState(context.Context) (string, error) { return s.state, nil }

func (s *adminStore) BootstrapAuth(_ context.Context, actorID, tokenID, name, nameKey, tokenHash string) (TokenMetadata, bool, error) {
	s.name, s.nameKey, s.tokenHash = name, nameKey, tokenHash
	return TokenMetadata{ID: tokenID, ActorID: actorID, Name: name}, true, nil
}

func (s *adminStore) CreateAuthToken(_ context.Context, tokenID, name, nameKey, tokenHash string) (TokenMetadata, error) {
	s.name, s.nameKey, s.tokenHash = name, nameKey, tokenHash
	return TokenMetadata{ID: tokenID, ActorID: "act_00000000000000000000000000", Name: name}, nil
}

func (s *adminStore) ListAuthTokens(context.Context) ([]TokenMetadata, error) {
	return []TokenMetadata{}, nil
}

func (s *adminStore) RevokeAuthToken(context.Context, string) (TokenMetadata, error) {
	return TokenMetadata{}, nil
}

func TestBootstrapNormalizesNameAndReturnsSecretOnlyWhenCreated(t *testing.T) {
	store := &adminStore{state: "required"}
	service := NewAdminWithGenerators(store, func(prefix string) (string, error) {
		return prefix + strings.Repeat("0", 26), nil
	}, func(destination []byte) (int, error) {
		for index := range destination {
			destination[index] = byte(index)
		}
		return len(destination), nil
	})

	result, err := service.Bootstrap(context.Background(), "  Personal\u0301  ")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Token == nil || !strings.HasPrefix(result.Token.Secret, "ltp_") {
		t.Fatalf("result = %#v", result)
	}
	if store.name != "Personaĺ" || store.nameKey == "" {
		t.Fatalf("normalized name = %q, key = %q", store.name, store.nameKey)
	}
	if store.tokenHash != HashToken(result.Token.Secret) || strings.Contains(store.tokenHash, result.Token.Secret) {
		t.Fatal("store did not receive only the Token hash")
	}
	if !strings.HasPrefix(result.ActorID, id.ActorPrefix) || !strings.HasPrefix(result.Token.ID, id.TokenPrefix) {
		t.Fatalf("typed IDs = %q %q", result.ActorID, result.Token.ID)
	}
}

func TestBootstrapCompleteDoesNotGenerateASecret(t *testing.T) {
	store := &adminStore{state: "complete"}
	generated := false
	service := NewAdminWithGenerators(store, id.New, func([]byte) (int, error) {
		generated = true
		return 0, nil
	})
	result, err := service.Bootstrap(context.Background(), "Personal")
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.Token != nil || generated {
		t.Fatalf("result = %#v, generated = %v", result, generated)
	}
}
