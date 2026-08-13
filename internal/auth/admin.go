package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

var ErrBootstrapRequired = errors.New("authentication bootstrap is required")

type TokenMetadata struct {
	ID        string     `json:"id"`
	ActorID   string     `json:"actorId"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

type IssuedToken struct {
	TokenMetadata
	Secret string `json:"secret"`
}

type BootstrapResult struct {
	State   string       `json:"state"`
	Created bool         `json:"created"`
	ActorID string       `json:"actorId,omitempty"`
	Token   *IssuedToken `json:"token,omitempty"`
}

type AdminStore interface {
	BootstrapState(context.Context) (string, error)
	BootstrapAuth(context.Context, string, string, string, string, string) (TokenMetadata, bool, error)
	CreateAuthToken(context.Context, string, string, string, string) (TokenMetadata, error)
	ListAuthTokens(context.Context) ([]TokenMetadata, error)
	RevokeAuthToken(context.Context, string) (TokenMetadata, error)
}

type AdminService struct {
	store     AdminStore
	newID     func(string) (string, error)
	randomKey func([]byte) (int, error)
}

func NewAdmin(store AdminStore) *AdminService {
	return NewAdminWithGenerators(store, id.New, rand.Read)
}

func NewAdminWithGenerators(store AdminStore, newID func(string) (string, error), randomKey func([]byte) (int, error)) *AdminService {
	return &AdminService{store: store, newID: newID, randomKey: randomKey}
}

func (s *AdminService) Bootstrap(ctx context.Context, name string) (BootstrapResult, error) {
	if s == nil || s.store == nil {
		return BootstrapResult{}, domain.ErrDependencyMissing
	}
	normalized, err := domain.NormalizeTokenName("/name", name)
	if err != nil {
		return BootstrapResult{}, err
	}
	state, err := s.store.BootstrapState(ctx)
	if err != nil {
		return BootstrapResult{}, err
	}
	if state == "complete" {
		return BootstrapResult{State: "complete", Created: false}, nil
	}
	actorID, err := s.newID(id.ActorPrefix)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate Actor ID: %w", err)
	}
	tokenID, err := s.newID(id.TokenPrefix)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("generate Token ID: %w", err)
	}
	secret, err := s.generateSecret()
	if err != nil {
		return BootstrapResult{}, err
	}
	metadata, created, err := s.store.BootstrapAuth(ctx, actorID, tokenID, normalized, domain.FoldKey(normalized), HashToken(secret))
	if err != nil {
		return BootstrapResult{}, err
	}
	if !created {
		return BootstrapResult{State: "complete", Created: false}, nil
	}
	issued := &IssuedToken{TokenMetadata: metadata, Secret: secret}
	return BootstrapResult{State: "complete", Created: true, ActorID: metadata.ActorID, Token: issued}, nil
}

func (s *AdminService) Create(ctx context.Context, name string) (IssuedToken, error) {
	if s == nil || s.store == nil {
		return IssuedToken{}, domain.ErrDependencyMissing
	}
	normalized, err := domain.NormalizeTokenName("/name", name)
	if err != nil {
		return IssuedToken{}, err
	}
	tokenID, err := s.newID(id.TokenPrefix)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("generate Token ID: %w", err)
	}
	secret, err := s.generateSecret()
	if err != nil {
		return IssuedToken{}, err
	}
	metadata, err := s.store.CreateAuthToken(ctx, tokenID, normalized, domain.FoldKey(normalized), HashToken(secret))
	if err != nil {
		return IssuedToken{}, err
	}
	return IssuedToken{TokenMetadata: metadata, Secret: secret}, nil
}

func (s *AdminService) List(ctx context.Context) ([]TokenMetadata, error) {
	if s == nil || s.store == nil {
		return nil, domain.ErrDependencyMissing
	}
	return s.store.ListAuthTokens(ctx)
}

func (s *AdminService) Revoke(ctx context.Context, tokenID string) (TokenMetadata, error) {
	if !id.Valid(id.TokenPrefix, tokenID) {
		return TokenMetadata{}, domain.NewValidationError(domain.ValidationIssue{Path: "/tokenId", Code: "format", Message: "tokenId must be a typed Token ID"})
	}
	if s == nil || s.store == nil {
		return TokenMetadata{}, domain.ErrDependencyMissing
	}
	return s.store.RevokeAuthToken(ctx, tokenID)
}

func (s *AdminService) generateSecret() (string, error) {
	raw := make([]byte, 32)
	n, err := s.randomKey(raw)
	if err != nil {
		return "", fmt.Errorf("generate Token Secret: %w", err)
	}
	if n != len(raw) {
		return "", errors.New("generate Token Secret: incomplete random read")
	}
	return "ltp_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
