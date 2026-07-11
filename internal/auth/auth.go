// Package auth implements OAuth2 login (Google, Spotify, Apple) and the
// session/state plumbing gothic needs to complete a login round-trip. It
// covers provider setup and the six /auth/* HTTP endpoints; JWT/refresh-token
// issuance itself lives in the sibling package internal/tokens.
//
// # Login flow
//
// GET /auth/{provider} starts a login: the platform (mobile vs web) and,
// for web, a redirect_uri are captured into a pipe-delimited, base64-encoded
// state blob (see encodeState/decodeState in auth_handler.go), then gothic
// redirects the client to the provider.
//
// /auth/{provider}/callback completes it: gothic exchanges the provider's
// code for a profile, the account/social-connection rows are upserted, a
// device is registered, and a token pair is issued — all inside one DB
// transaction. Mobile clients get a one-time opaque code redirected via deep
// link (see ExchangeAuthCodeHandler); web clients get the token pair set
// directly as cookies.
//
// # Apple client secret
//
// Apple does not accept a static client secret. GenerateAppleClientSecret
// signs a fresh short-lived JWT on every server start (see
// appleClientSecretValidity and ADR 0004 in docs/adrs/ for the operational
// risk this implies if a server runs unrestarted past that window).
//
// # Known limitation
//
// encodeState/decodeState split fields on "|" with no escaping — a
// DeviceName/DeviceToken/DeepLink containing a literal "|" would decode
// incorrectly. See docs/AUTHENTICATION.md.
//
// Usage:
//
//	authenticator, err := auth.NewAuthenticator(cfg, logger, auth.GenerateAppleClientSecret)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	authHandler := auth.NewAuthHandler(authenticator, db, cacher, userEventBus, logger, geoLocator)
//	authHandler.RegisterHandlers(router)
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

// supportedProviders lists every OAuth2 provider Verisafe registers with
// goth. Used both to register providers (setupOAuthProviders) and to check
// they came up successfully (Ready).
var supportedProviders = []string{"google", "spotify", "apple"}

// appleClientSecretValidity is how long a generated Apple client secret JWT
// is valid for. Apple's maximum allowed lifetime is 6 months; a server that
// runs unrestarted past this window will start failing Apple logins with no
// advance warning, since nothing currently re-signs the secret proactively.
// See ADR 0004 (docs/adrs/0004-apple-client-secret-lifecycle.md).
const appleClientSecretValidity = 180 * 24 * time.Hour

// secondsPerDay converts AuthenticationConfig.MaxAge — configured in days —
// into the seconds gorilla/sessions' CookieStore.MaxAge expects.
const secondsPerDay = 24 * 60 * 60

// AppleSecretGenerator is a function that generates an Apple client secret JWT.
// It is defined as a type so it can be swapped out in tests with a stub,
// avoiding the need for real Apple credentials during testing.
type AppleSecretGenerator func(teamID, keyID, clientID, privateKey string) (string, error)

// Auth is responsible solely for OAuth2 provider setup and session store
// configuration. It has no knowledge of application services or business logic.
//
// Use NewAuthenticator to create an instance, then pass it to NewAuthHandler
// to wire in the services needed for request handling.
type Auth struct {
	config *config.Config
	logger *slog.Logger
}

// NewAuthenticator initialises OAuth2 providers and the session store.
// It does not require any application services — it is pure configuration.
//
// Pass GenerateAppleClientSecret as appleSecretGen in production.
// In tests, pass a stub that returns a dummy string to avoid needing
// real Apple credentials.
func NewAuthenticator(
	cfg *config.Config,
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
		config: cfg,
		logger: logger,
	}, nil
}

// Ready reports whether all expected OAuth2 providers are registered.
// Useful as a health or readiness check.
func (a *Auth) Ready() bool {
	for _, name := range supportedProviders {
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

// setupSessionStore configures the gorilla session store used by gothic
// to persist OAuth2 state between the login redirect and the callback.
func setupSessionStore(cfg *config.Config, logger *slog.Logger) error {
	secret := cfg.AuthenticationConfig.SessionSecret
	if secret == "" {
		logger.Error("session secret is empty")
		return fmt.Errorf("session secret must not be empty")
	}

	store := sessions.NewCookieStore([]byte(secret))
	store.MaxAge(secondsPerDay * cfg.AuthenticationConfig.MaxAge)
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

	// callbackFor builds the per-provider callback URL.
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
		"exp": now.Add(appleClientSecretValidity).Unix(),
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
