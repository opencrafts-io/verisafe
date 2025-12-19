package config

import (
	"encoding/base64"
	"fmt"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {

	// JWT token configuration
	JWTConfig struct {
		ApiSecret          string `envconfig:"API_SECRET"`
		ExpireDelta        int    `envconfig:"EXPIRE_DELTA"`
		RefreshExpireDelta int    `envconfig:"REFRESH_EXPIRE_DELTA"`
		ServiceExpireDelta int    `envconfig:"SERVICE_EXPIRE_DELTA"`
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
}

// The LoadConfig function loads the env file specified and returns
// a valid configuration object ready for use
func LoadConfig() (*Config, error) {
	cfg := Config{}

	// 1. Attempt to load .env file.
	// We ignore the error so it doesn't crash if the file is missing.
	_ = godotenv.Load()

	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("Failed to load environment variables: %v", err)
	}

	if cfg.AuthenticationConfig.ApplePrivateKeyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(cfg.AuthenticationConfig.ApplePrivateKeyBase64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode Apple private key from base64: %v", err)
		}
		cfg.AuthenticationConfig.ApplePrivateKey = string(decoded)
	}

	return &cfg, nil
}
