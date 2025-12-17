package auth

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/apple"
	"github.com/markbates/goth/providers/google"
	"github.com/markbates/goth/providers/spotify"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
)

type Auth struct {
	config   *config.Config
	logger   *slog.Logger
	eventBus *eventbus.UserEventBus
}

func NewAuthenticator(cfg *config.Config, userEventBus *eventbus.UserEventBus, logger *slog.Logger) (*Auth, error) {
	sessionSecret := cfg.AuthenticationConfig.SessionSecret

	if sessionSecret == "" {
		logger.Error("Session secret is empty")
		return nil, fmt.Errorf("session secret is empty")
	}

	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.MaxAge(86400 * cfg.AuthenticationConfig.MaxAge) // Session expires in 30 days
	// store.Options.Path = "/"
	// store.Options.HttpOnly = true
	//
	// if cfg.AuthenticationConfig.Environment == "production" {
	// 	store.Options.Secure = true
	// 	store.Options.SameSite = http.SameSiteNoneMode
	// } else {
	// 	store.Options.Secure = false
	// 	store.Options.SameSite = http.SameSiteLaxMode
	// }

	store.Options.Path = "/"
	store.Options.HttpOnly = true

	if cfg.AuthenticationConfig.Environment == "production" || cfg.AuthenticationConfig.Environment == "staging" {
		store.Options.Secure = true
		store.Options.SameSite = http.SameSiteNoneMode
	} else {
		store.Options.Secure = false
		store.Options.SameSite = http.SameSiteLaxMode
	}

	gothic.Store = store

	address := ""

	if cfg.AuthenticationConfig.Environment == "development" {
		address = fmt.Sprintf("%s/auth/{oauth}/callback",
			cfg.AuthenticationConfig.AuthAddress,
		)
	} else {
		address = fmt.Sprintf("%s/auth/{oauth}/callback",
			cfg.AuthenticationConfig.AuthAddress,
		)

	}

	googleProvider := google.New(
		cfg.AuthenticationConfig.GoogleClientID,
		cfg.AuthenticationConfig.GoogleClientSecret,
		strings.Replace(address, "{oauth}", "google", 1),
		"email", "profile",
		"https://www.googleapis.com/auth/calendar",
		"https://www.googleapis.com/auth/tasks",
	)

	googleProvider.SetAccessType("offline")

	spotifyProvider := spotify.New(
		cfg.AuthenticationConfig.SpotifyClientID,
		cfg.AuthenticationConfig.SpotifyClientSecret,
		strings.Replace(address, "{oauth}", "spotify", 1),
		"user-read-playback-state",
		"user-modify-playback-state",
		"user-read-currently-playing",
		"user-read-recently-played",
		"user-top-read",
		"app-remote-control",
		"playlist-read-private",
		"playlist-modify-private",
		"playlist-modify-public",
		"user-follow-modify",
		"user-follow-read",
		"user-read-email",
		"user-read-private",
	)

	appleSecret, err := generateAppleClientSecret(
		cfg.AuthenticationConfig.AppleTeamID,
		cfg.AuthenticationConfig.AppleKeyID,
		cfg.AuthenticationConfig.AppleClientID,
		cfg.AuthenticationConfig.ApplePrivateKey,
	)
	if err != nil {
		logger.Error("Failed to generate Apple client secret", "error", err)
		return nil, fmt.Errorf("failed to generate Apple client secret: %w", err)
	}

	appleProvider := apple.New(
		cfg.AuthenticationConfig.AppleClientID,
		appleSecret,
		strings.Replace(address, "{oauth}", "apple", 1),
		nil, // HTTP client (nil uses default)
		apple.ScopeName,
		apple.ScopeEmail,
	)

	goth.UseProviders(
		googleProvider,
		spotifyProvider,
		appleProvider,
	)

	logger.Info("Goth Oauth2 providers initialized successfully")

	return &Auth{
		config:   cfg,
		logger:   logger,
		eventBus: userEventBus,
	}, nil
}

func generateAppleClientSecret(teamID, keyID, clientID, privateKeyContent string) (string, error) {
	// Decode the PEM-encoded private key
	block, _ := pem.Decode([]byte(privateKeyContent))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block from private key")
	}

	// Parse the PKCS8 private key
	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Type assert to ECDSA private key
	ecdsaKey, ok := privateKey.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not an ECDSA key")
	}

	// Create JWT claims
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(180 * 24 * time.Hour).Unix(), // Valid for 6 months
		"aud": "https://appleid.apple.com",
		"sub": clientID,
	}

	// Create token with ES256 algorithm
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID

	// Sign and return the token
	signedToken, err := token.SignedString(ecdsaKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// GetProviderName extracts the OAuth provider name from the request context.
// Goth's handlers expect the 'provider' name to be available in the URL query
// parameter (e.g., ?provider=google) or set in the request context.
// For mux/router, you typically extract it from a URL path parameter.
func GetProviderName(r *http.Request) (string, error) {
	// Try to get from URL query first (e.g., /login?provider=google)
	provider := r.PathValue("provider")
	if provider != "" {
		return provider, nil
	}

	// Fallback if provider name is not found in query or path.
	return "", fmt.Errorf("Provider name not found in request")
}
