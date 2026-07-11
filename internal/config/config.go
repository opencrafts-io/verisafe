// Package config loads Verisafe's configuration from environment variables
// (via a .env file) into a single Config struct.
package config

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
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
}

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

	return nil
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
