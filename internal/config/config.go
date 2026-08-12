package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/auth"
)

type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	AuthTokenHash        string
	ServerVersion        string
	APIVersion           string
	MinPluginVersion     string
	Capabilities         []string
	ChangeRetention      string
	IdempotencyRetention string
	MigrationRequired    bool
}

func Load() Config {
	tokenHash := strings.TrimSpace(os.Getenv("LOOMTABLE_AUTH_TOKEN_HASH"))
	if tokenHash == "" {
		if token := os.Getenv("LOOMTABLE_AUTH_TOKEN"); token != "" {
			tokenHash = auth.HashToken(token)
		}
	}

	return Config{
		HTTPAddr:             value("LOOMTABLE_HTTP_ADDR", ":3000"),
		DatabaseURL:          strings.TrimSpace(os.Getenv("LOOMTABLE_DATABASE_URL")),
		AuthTokenHash:        tokenHash,
		ServerVersion:        value("LOOMTABLE_SERVER_VERSION", "dev"),
		APIVersion:           value("LOOMTABLE_API_VERSION", "v1"),
		MinPluginVersion:     value("LOOMTABLE_MIN_PLUGIN_VERSION", "0.1.0"),
		Capabilities:         []string{"grid", "map"},
		ChangeRetention:      value("LOOMTABLE_CHANGE_RETENTION", "30d"),
		IdempotencyRetention: value("LOOMTABLE_IDEMPOTENCY_RETENTION", "30d"),
		MigrationRequired:    boolean("LOOMTABLE_MIGRATION_REQUIRED", false),
	}
}

func value(name, fallback string) string {
	if current := strings.TrimSpace(os.Getenv(name)); current != "" {
		return current
	}
	return fallback
}

func boolean(name string, fallback bool) bool {
	current := strings.TrimSpace(os.Getenv(name))
	if current == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(current)
	if err != nil {
		return fallback
	}
	return parsed
}