package auth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/markbates/goth/gothic"
)

// LoginHandler initiates the OAuth2 authentication flow.
// It redirects the user to the selected OAuth provider's login page.
// This handler should be mapped to a route like `/auth/{provider}` or `/login`
// where the provider is passed as a query parameter (e.g., `/login?provider=google`).
func (a *Auth) LoginHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		a.logger.Warn("Failed to get provider name for login", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	a.logger.Info("Initiating OAuth login", "provider", provider)
	gothic.BeginAuthHandler(w, r)
}

// CallbackHandler processes the OAuth2 callback from the provider.
// After the user authenticates with the provider, they are redirected back to this handler.
// This handler should be mapped to the `callback` URL configured with the OAuth provider,
// e.g., `/auth/{provider}/callback`.
func (a *Auth) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		a.logger.Warn("Failed to get provider name for callback", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		a.logger.Error("Ran into an error when perfoming auth flow", slog.Any("error", err))
		http.Error(w, "Authentication flow failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": fmt.Sprintf("Welcome %s from %s", user.FirstName, provider)})
}

// LogoutHandler logs the user out from the OAuth provider and clears Goth's session data.
// It assumes the provider name is passed as a query parameter (e.g., `/logout?provider=google`).
// You would also typically clear your application's session/JWT here.
func (a *Auth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		a.logger.Warn("Failed to get provider name for logout", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := gothic.Logout(w, r); err != nil {
		a.logger.Error("Error logging out from OAuth provider", "provider", provider, "error", err)
		http.Error(w, fmt.Sprintf("Error logging out from %s: %v", provider, err), http.StatusInternalServerError)
		return
	}

	// Optionally, clear your application's session/JWT here as well
	// e.g., if using gorilla/sessions: sessions.Default(r).Clear() or sessions.Default(r).Options.MaxAge = -1

	a.logger.Info("Successfully logged out", "provider", provider)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect) // Redirect to homepage
}
