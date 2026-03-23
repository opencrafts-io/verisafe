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
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

// spotifyScopes defines all Spotify OAuth2 permission scopes Verisafe requests.
// These cover playback control, library access, and user profile reading.
var spotifyScopes = []string{
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
}

// googleScopes defines the Google OAuth2 permission scopes Verisafe requests.
// Includes profile, calendar, and tasks access.
var googleScopes = []string{
	"email",
	"profile",
	"https://www.googleapis.com/auth/calendar",
	"https://www.googleapis.com/auth/tasks",
}

// AppleSecretGenerator is a function that generates an Apple client secret JWT.
// It is defined as a type so it can be swapped out in tests with a stub,
// avoiding the need for real Apple credentials during testing.
type AppleSecretGenerator func(teamID, keyID, clientID, privateKey string) (string, error)

// Auth holds the dependencies for all authentication-related HTTP handlers.
type Auth struct {
	config       *config.Config
	logger       *slog.Logger
	eventBus     *eventbus.UserEventBus
	tokenService tokens.TokenService
}

// NewAuthenticator initialises the Auth handler, sets up the session store,
// and registers all OAuth2 providers (Google, Spotify, Apple).
//
// appleSecretGen is injected so it can be replaced in tests. Pass
// GenerateAppleClientSecret for production use.
func NewAuthenticator(
	cfg *config.Config,
	userEventBus *eventbus.UserEventBus,
	tokenService tokens.TokenService,
	logger *slog.Logger,
	appleSecretGen AppleSecretGenerator,
) (*Auth, error) {
	if err := setupSessionStore(cfg, logger); err != nil {
		return nil, err
	}

	if err := setupOAuthProviders(cfg, appleSecretGen); err != nil {
		return nil, err
	}

	logger.Info("OAuth2 providers initialised successfully")

	return &Auth{
		config:       cfg,
		logger:       logger,
		eventBus:     userEventBus,
		tokenService: tokenService,
	}, nil
}

// setupSessionStore configures the gorilla session store used by gothic
// to persist OAuth2 state between the login redirect and the callback.
func setupSessionStore(cfg *config.Config, logger *slog.Logger) error {
	secret := cfg.AuthenticationConfig.SessionSecret
	if secret == "" {
		logger.Error("session secret is empty")
		return fmt.Errorf("session secret must not be empty")
	}

	store := sessions.NewCookieStore([]byte(secret))
	store.MaxAge(86400 * cfg.AuthenticationConfig.MaxAge)
	store.Options.Path = "/"
	store.Options.HttpOnly = true

	// Relax security settings in non-production environments so that
	// local development works without HTTPS.
	isProduction := cfg.AuthenticationConfig.Environment == "production" ||
		cfg.AuthenticationConfig.Environment == "staging"

	if isProduction {
		store.Options.Secure = true
		store.Options.SameSite = http.SameSiteNoneMode
	} else {
		store.Options.Secure = false
		store.Options.SameSite = http.SameSiteLaxMode
	}

	gothic.Store = store
	return nil
}

// setupOAuthProviders registers all OAuth2 providers with goth.
// Each provider's callback URL is derived from the configured AuthAddress.
func setupOAuthProviders(
	cfg *config.Config,
	appleSecretGen AppleSecretGenerator,
) error {
	callbackBase := fmt.Sprintf(
		"%s/auth/{provider}/callback",
		cfg.AuthenticationConfig.AuthAddress,
	)

	// Helper to build the per-provider callback URL.
	callbackFor := func(provider string) string {
		return strings.Replace(callbackBase, "{provider}", provider, 1)
	}

	googleProvider := google.New(
		cfg.AuthenticationConfig.GoogleClientID,
		cfg.AuthenticationConfig.GoogleClientSecret,
		callbackFor("google"),
		googleScopes...,
	)
	// offline access ensures Google returns a refresh token.
	googleProvider.SetAccessType("offline")

	spotifyProvider := spotify.New(
		cfg.AuthenticationConfig.SpotifyClientID,
		cfg.AuthenticationConfig.SpotifyClientSecret,
		callbackFor("spotify"),
		spotifyScopes...,
	)

	appleSecret, err := appleSecretGen(
		cfg.AuthenticationConfig.AppleTeamID,
		cfg.AuthenticationConfig.AppleKeyID,
		cfg.AuthenticationConfig.AppleClientID,
		cfg.AuthenticationConfig.ApplePrivateKey,
	)
	if err != nil {
		return fmt.Errorf("generate Apple client secret: %w", err)
	}

	appleProvider := apple.New(
		cfg.AuthenticationConfig.AppleClientID,
		appleSecret,
		callbackFor("apple"),
		nil, // nil uses the default HTTP client
		apple.ScopeName,
		apple.ScopeEmail,
	)

	goth.UseProviders(googleProvider, spotifyProvider, appleProvider)
	return nil
}

// Ready reports whether all expected OAuth2 providers are registered and
// reachable. Useful as a health or readiness check.
func (a *Auth) Ready() bool {
	for _, name := range []string{"google", "spotify", "apple"} {
		if _, err := goth.GetProvider(name); err != nil {
			a.logger.Warn(
				"OAuth2 provider not ready",
				slog.String("provider", name),
			)
			return false
		}
	}
	return true
}

// GetProviderName extracts the OAuth2 provider name from the URL path.
// Expects the provider to be registered as a path parameter e.g. /auth/{provider}.
func GetProviderName(r *http.Request) (string, error) {
	provider := r.PathValue("provider")
	if provider == "" {
		return "", fmt.Errorf("provider name not found in request path")
	}
	return provider, nil
}

// GenerateAppleClientSecret creates a short-lived ES256-signed JWT that Apple
// requires as the client_secret during OAuth2 token exchange.
//
// The token is valid for 6 months. Apple will reject requests signed with a
// key older than that, so this should be called fresh on each server start.
func GenerateAppleClientSecret(
	teamID, keyID, clientID, privateKeyContent string,
) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyContent))
	if block == nil {
		return "", fmt.Errorf(
			"failed to decode PEM block from Apple private key",
		)
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse Apple private key: %w", err)
	}

	ecdsaKey, ok := privateKey.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("Apple private key is not an ECDSA key")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(180 * 24 * time.Hour).Unix(),
		"aud": "https://appleid.apple.com",
		"sub": clientID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID

	signed, err := token.SignedString(ecdsaKey)
	if err != nil {
		return "", fmt.Errorf("sign Apple client secret: %w", err)
	}

	return signed, nil
}
