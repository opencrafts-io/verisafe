// Package config loads Verisafe's configuration from environment variables
// (via a .env file) into a single Config struct.
package config

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/opencrafts-io/verisafe/internal/secrets"
)

type Config struct {
	// JWT token configuration
	JWTConfig struct {
		ApiSecret           string   `envconfig:"API_SECRET"`
		ExpireDelta         int      `envconfig:"EXPIRE_DELTA"`
		RefreshExpireDelta  int      `envconfig:"REFRESH_EXPIRE_DELTA"`
		ServiceExpireDelta  int      `envconfig:"SERVICE_EXPIRE_DELTA"`
		AllowedRedirectURIs []string `envconfig:"ALLOWED_REDIRECT_URIS"`
	}

	// Authentication configuration
	AuthenticationConfig struct {
		AppleClientID         string `envconfig:"APPLE_CLIENT_ID"`
		AppleTeamID           string `envconfig:"APPLE_TEAM_ID"`
		AppleKeyID            string `envconfig:"APPLE_KEY_ID"`
		ApplePrivateKeyBase64 string `envconfig:"APPLE_PRIVATE_KEY_BASE64"`
		ApplePrivateKey       string
		GoogleClientID        string `envconfig:"GOOGLE_CLIENT_ID"`
		GoogleClientSecret    string `envconfig:"GOOGLE_CLIENT_SECRET"`
		SpotifyClientID       string `envconfig:"SPOTIFY_CLIENT_ID"`
		SpotifyClientSecret   string `envconfig:"SPOTIFY_CLIENT_SECRET"`
		MaxAge                int    `envconfig:"AUTH_MAX_AGE"`
		SessionSecret         string `envconfig:"SESSION_SECRET"`
		Environment           string `envconfig:"AUTH_ENV"`
		AuthAddress           string `envconfig:"AUTH_ADDRESS"`
	}

	// Application configuration
	AppConfig struct {
		Port    int    `envconfig:"VERISAFE_PORT"`
		Address string `envconfig:"VERISAFE_ADDRESS"`
	}

	// Database configuration
	DatabaseConfig struct {
		DatabaseHost                      string `envconfig:"DB_HOST"`
		DatabaseDriver                    string `envconfig:"DB_DRIVER"`
		DatabaseUser                      string `envconfig:"DB_USER"`
		DatabasePassword                  string `envconfig:"DB_PASSWORD"`
		DatabaseName                      string `envconfig:"DB_NAME"`
		DatabasePort                      int32  `envconfig:"DB_PORT"`
		DatabasePoolMaxConnections        int32  `envconfig:"DB_MAX_CON"`
		DatabasePoolMinConnections        int32  `envconfig:"DB_POOL_MIN_CON"`
		DatabasePoolMaxConnectionLifetime int    `envconfig:"DB_POOL_MAX_LIFETIME"`
	}

	// RabbitMQ configuration
	RabbitMQConfig struct {
		RabbitMQUser    string `envconfig:"RABBITMQ_USER"`
		RabbitMQPass    string `envconfig:"RABBITMQ_PASSWORD"`
		RabbitMQAddress string `envconfig:"RABBITMQ_ADDRESS"`
		RabbitMQPort    int    `envconfig:"RABBITMQ_PORT"`
		Exchange        string `envconfig:"RABBITMQ_EXCHANGE"`
	}

	RedisConfig struct {
		RedisAddress  string `envconfig:"REDIS_ADDRESS"`
		RedisDB       int    `envconfig:"REDIS_DB"`
		RedisPassword string `envconfig:"REDIS_PASSWORD"`
	}

	// ProviderTokensConfig governs third-party (Google/Spotify/...) OAuth
	// token storage, refresh, and the incremental-scope flow.
	ProviderTokensConfig struct {
		// EncryptionKeys is a comma-separated list of versioned AES-256 keys,
		// "1:<base64-32-bytes>,2:<base64-32-bytes>". Old versions must be
		// retained so existing ciphertext stays readable; see internal/secrets.
		EncryptionKeys string `envconfig:"PROVIDER_TOKEN_ENC_KEYS"`
		// ActiveKeyVersion selects which key seals new ciphertext.
		ActiveKeyVersion int `envconfig:"PROVIDER_TOKEN_ENC_ACTIVE_KEY"`

		// RefreshSkewSeconds treats a token expiring within this window as
		// already stale, so callers never receive one about to die in flight.
		RefreshSkewSeconds int `envconfig:"PROVIDER_TOKEN_REFRESH_SKEW_SECONDS"`
		// CacheTTLSeconds caps how long a brokered access token is cached.
		CacheTTLSeconds int `envconfig:"PROVIDER_TOKEN_CACHE_TTL_SECONDS"`
		// ScopeUpgradeTTLSeconds bounds the incremental-authorization window.
		ScopeUpgradeTTLSeconds int `envconfig:"OAUTH_SCOPE_UPGRADE_STATE_TTL_SECONDS"`

		// MinimalLoginScopes requests only identity scopes at sign-in, leaving
		// everything else to the incremental flow. Kept as a flag so the
		// rollout can be reversed with a restart rather than a rebuild.
		MinimalLoginScopes bool `envconfig:"OAUTH_MINIMAL_LOGIN_SCOPES"`

		// ReconcileEnabled runs the background worker that converts presumed
		// scope grants into provider-verified ones.
		ReconcileEnabled    bool `envconfig:"OAUTH_RECONCILE_ENABLED"`
		ReconcileRatePerMin int  `envconfig:"OAUTH_RECONCILE_RATE_PER_MINUTE"`
	}
}

// Provider token defaults, applied by Validate when the corresponding env var
// is unset or non-positive.
const (
	defaultRefreshSkewSeconds     = 120
	defaultCacheTTLSeconds        = 300
	defaultScopeUpgradeTTLSeconds = 600
	defaultReconcileRatePerMin    = 60
)

// LoadConfig loads the env file specified and returns
// a valid configuration object ready for use
func LoadConfig() (*Config, error) {
	cfg := Config{}

	// load the configs
	if err := godotenv.Load(".env"); err != nil {
		return nil, fmt.Errorf("failed to load environment variables: %v", err)
	}
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to load environment variables: %v", err)
	}

	if cfg.AuthenticationConfig.ApplePrivateKeyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(
			cfg.AuthenticationConfig.ApplePrivateKeyBase64,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to decode Apple private key from base64: %v",
				err,
			)
		}
		cfg.AuthenticationConfig.ApplePrivateKey = string(decoded)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate fails fast on configuration that would otherwise only surface as
// an error the first time it's used (e.g. the first Apple login attempt).
// See ADR 0004 (docs/adrs/0004-apple-client-secret-lifecycle.md).
func (cfg *Config) Validate() error {
	if cfg.AuthenticationConfig.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET must not be empty")
	}

	if cfg.AuthenticationConfig.ApplePrivateKeyBase64 != "" {
		if err := validateApplePrivateKey(cfg.AuthenticationConfig.ApplePrivateKey); err != nil {
			return fmt.Errorf("APPLE_PRIVATE_KEY_BASE64 is set but invalid: %w", err)
		}
		if cfg.AuthenticationConfig.AppleTeamID == "" {
			return fmt.Errorf("APPLE_TEAM_ID must not be empty when APPLE_PRIVATE_KEY_BASE64 is set")
		}
		if cfg.AuthenticationConfig.AppleKeyID == "" {
			return fmt.Errorf("APPLE_KEY_ID must not be empty when APPLE_PRIVATE_KEY_BASE64 is set")
		}
		if cfg.AuthenticationConfig.AppleClientID == "" {
			return fmt.Errorf("APPLE_CLIENT_ID must not be empty when APPLE_PRIVATE_KEY_BASE64 is set")
		}
	}

	if err := cfg.validateProviderTokens(); err != nil {
		return err
	}

	return nil
}

// validateProviderTokens fails fast on the third-party token encryption
// settings and applies defaults for the tuning knobs.
//
// The encryption key is deliberately mandatory rather than optional-with-
// fallback: refresh tokens for Google and Spotify are long-lived, replayable
// credentials, and a deployment that silently stored them in plaintext because
// an env var was missing would be worse than one that refused to start.
func (cfg *Config) validateProviderTokens() error {
	pt := &cfg.ProviderTokensConfig

	if strings.TrimSpace(pt.EncryptionKeys) == "" {
		return fmt.Errorf(
			"PROVIDER_TOKEN_ENC_KEYS must be set (format \"1:<base64-32-bytes>\"; " +
				"generate one with: openssl rand -base64 32)",
		)
	}

	keys, err := secrets.ParseKeySpec(pt.EncryptionKeys)
	if err != nil {
		return fmt.Errorf("PROVIDER_TOKEN_ENC_KEYS is invalid: %w", err)
	}

	if pt.ActiveKeyVersion == 0 && len(keys) == 1 {
		// Unambiguous: one key, so it is the active one.
		for version := range keys {
			pt.ActiveKeyVersion = int(version)
		}
	}
	if pt.ActiveKeyVersion <= 0 {
		return fmt.Errorf(
			"PROVIDER_TOKEN_ENC_ACTIVE_KEY must be set when PROVIDER_TOKEN_ENC_KEYS defines more than one key",
		)
	}
	if _, ok := keys[int16(pt.ActiveKeyVersion)]; !ok {
		return fmt.Errorf(
			"PROVIDER_TOKEN_ENC_ACTIVE_KEY is %d but PROVIDER_TOKEN_ENC_KEYS has no key for that version",
			pt.ActiveKeyVersion,
		)
	}

	if pt.RefreshSkewSeconds <= 0 {
		pt.RefreshSkewSeconds = defaultRefreshSkewSeconds
	}
	if pt.CacheTTLSeconds <= 0 {
		pt.CacheTTLSeconds = defaultCacheTTLSeconds
	}
	if pt.ScopeUpgradeTTLSeconds <= 0 {
		pt.ScopeUpgradeTTLSeconds = defaultScopeUpgradeTTLSeconds
	}
	if pt.ReconcileRatePerMin <= 0 {
		pt.ReconcileRatePerMin = defaultReconcileRatePerMin
	}

	return nil
}

// RefreshSkew returns the staleness window as a duration.
func (cfg *Config) RefreshSkew() time.Duration {
	return time.Duration(cfg.ProviderTokensConfig.RefreshSkewSeconds) * time.Second
}

// ProviderTokenCacheTTL caps how long a brokered access token stays cached.
func (cfg *Config) ProviderTokenCacheTTL() time.Duration {
	return time.Duration(cfg.ProviderTokensConfig.CacheTTLSeconds) * time.Second
}

// ScopeUpgradeStateTTL bounds an in-flight incremental-authorization request.
func (cfg *Config) ScopeUpgradeStateTTL() time.Duration {
	return time.Duration(cfg.ProviderTokensConfig.ScopeUpgradeTTLSeconds) * time.Second
}

// validateApplePrivateKey confirms the decoded key is a well-formed PEM/PKCS8
// ECDSA private key — i.e. that auth.GenerateAppleClientSecret would actually
// succeed with it, rather than failing at first Apple login.
func validateApplePrivateKey(pemContent string) error {
	block, _ := pem.Decode([]byte(pemContent))
	if block == nil {
		return fmt.Errorf("not a valid PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("not a valid PKCS8 private key: %w", err)
	}

	if _, ok := key.(*ecdsa.PrivateKey); !ok {
		return fmt.Errorf("private key is not an ECDSA key")
	}

	return nil
}
