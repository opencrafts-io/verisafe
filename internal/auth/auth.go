package auth

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/google"
	"github.com/opencrafts-io/verisafe/internal/config"
)

type Auth struct {
	config *config.Config
	logger *slog.Logger
}

func NewAuthenticator(cfg *config.Config, logger *slog.Logger) *Auth {
	sessionSecret := cfg.AuthenticationConfig.SessionSecret

	if sessionSecret == "" {
		logger.Error("Session secret is empty")
		return nil
	}

	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.MaxAge(86400 * cfg.AuthenticationConfig.MaxAge) // Session expires in 30 days
	store.Options.Path = "/"
	store.Options.HttpOnly = true
	store.Options.SameSite = http.SameSiteLaxMode

	if cfg.AuthenticationConfig.Environment == "production" {
		store.Options.Secure = true
	} else {
		store.Options.Secure = false
	}

	gothic.Store = store

	goth.UseProviders(
		google.New(
			cfg.AuthenticationConfig.GoogleClientID,
			cfg.AuthenticationConfig.GoogleClientSecret,
			fmt.Sprintf("http://%s:%d/auth/google/callback",
				cfg.AppConfig.Addres,
				cfg.AppConfig.Port,
			),
			"email", "profile",
		),
	)

	logger.Info("Goth Oauth2 providers initialized successfully")

	return &Auth{
		config: cfg,
		logger: logger,
	}
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
