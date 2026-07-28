package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/secrets"
	"github.com/opencrafts-io/verisafe/internal/service/grants"
)

// OAuthBrokerHandler serves third-party OAuth access tokens to other services.
//
// It is the single chokepoint through which a provider token can leave
// Verisafe. Refresh tokens never do — a caller receives a short-lived access
// token and nothing else, so a compromised downstream service loses access
// when that token expires rather than retaining indefinite reach into the
// user's Google account.
type OAuthBrokerHandler struct {
	DB        core.IDBProvider
	Cacher    core.Cacher
	Cfg       *config.Config
	Logger    *slog.Logger
	Registry  *providers.Registry
	Exchanger providers.TokenExchanger
	Sealer    *secrets.Sealer
}

// ProviderTokenRequest asks for a usable provider access token.
type ProviderTokenRequest struct {
	AccountID string `json:"account_id"`
	// Capabilities are abstract names ("calendar"), not raw provider scopes.
	// There is deliberately no raw-scope escape hatch: a caller needing
	// something unmapped adds it to the provider registry, which keeps the
	// registry authoritative and the insufficient_scope response meaningful.
	Capabilities []string `json:"capabilities"`
}

// ProviderTokenResponse is a token the caller can use immediately.
type ProviderTokenResponse struct {
	Provider      string    `json:"provider"`
	AccountID     string    `json:"account_id"`
	AccessToken   string    `json:"access_token"`
	TokenType     string    `json:"token_type"`
	ExpiresAt     time.Time `json:"expires_at"`
	ExpiresIn     int       `json:"expires_in"`
	GrantedScopes []string  `json:"granted_scopes"`
	// ScopesVerified is false when the scope list is still what Verisafe
	// presumed from historical logins rather than what the provider confirmed.
	// A caller that gets a 403 from the provider despite this being false
	// should simply re-call the broker: the failed attempt will have converted
	// the grant to verified.
	ScopesVerified bool `json:"scopes_verified"`
	Refreshed      bool `json:"refreshed"`
	FromCache      bool `json:"from_cache"`
}

// InsufficientScopeResponse tells a caller exactly what is missing and where
// to send the user to fix it. Downstream services parse these keys, so treat
// the shape as a contract.
type InsufficientScopeResponse struct {
	Error               string   `json:"error"`
	Provider            string   `json:"provider"`
	AccountID           string   `json:"account_id"`
	MissingScopes       []string `json:"missing_scopes"`
	MissingCapabilities []string `json:"missing_capabilities"`
	GrantedCapabilities []string `json:"granted_capabilities"`
	// AuthorizationURL points at Verisafe's own scope-upgrade endpoint, not at
	// the provider. It cannot be a directly openable provider URL: starting an
	// authorization requires the *user's* JWT, which a service token holder
	// does not have. The service relays this to its own client, which calls
	// the endpoint with the user's token and opens the URL it returns.
	AuthorizationURL    string              `json:"authorization_url"`
	AuthorizationMethod string              `json:"authorization_method"`
	AuthorizationBody   map[string][]string `json:"authorization_body"`
}

// ReauthorizationRequiredResponse reports a grant the provider has rejected.
type ReauthorizationRequiredResponse struct {
	Error               string `json:"error"`
	Reason              string `json:"reason"`
	Provider            string `json:"provider"`
	AccountID           string `json:"account_id"`
	AuthorizationURL    string `json:"authorization_url"`
	AuthorizationMethod string `json:"authorization_method"`
}

func (h *OAuthBrokerHandler) RegisterHandlers(router *http.ServeMux) {
	router.Handle("POST /oauth/{provider}/token",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasPermission([]string{"read:provider_token:any"}),
		)(core.AppHandler(h.IssueProviderToken)),
	)

	router.Handle("GET /oauth/grants",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasPermission([]string{"read:provider_token:any"}),
		)(core.AppHandler(h.ListAccountGrants)),
	)

	router.Handle("POST /oauth/{provider}/reconcile",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasPermission([]string{"manage:provider_token:any"}),
		)(core.AppHandler(h.ReconcileGrant)),
	)
}

// IssueProviderToken godoc
//
// @Summary      Obtain a third-party OAuth access token on behalf of a user
// @Description  Returns a usable provider access token for the given account, refreshing it against the provider if the stored one is stale. Requires a service token (X-API-Key) and the read:provider_token:any permission — a human Bearer JWT is rejected even if it holds the permission. Refresh tokens are never returned. When the user has not granted the requested capabilities, responds 403 with a machine-readable insufficient_scope body naming what is missing and where to send the user.
// @Tags         oauth
// @Accept       json
// @Produce      json
// @Param        provider  path      string                         true  "OAuth2 provider"  Enums(google, spotify)
// @Param        request   body      oauth.ProviderTokenRequest  true  "Account and capabilities required"
// @Success      200  {object}  oauth.ProviderTokenResponse
// @Failure      400  {object}  core.APIError  "Malformed body, bad account_id, or unknown capability"
// @Failure      401  {object}  core.APIError  "Missing or invalid credentials"
// @Failure      403  {object}  oauth.InsufficientScopeResponse  "Caller is not a service token, lacks the permission, or the user has not granted the capability"
// @Failure      404  {object}  core.APIError  "Unknown provider, or the account has never connected it"
// @Failure      409  {object}  core.APIError  "Provider cannot refresh tokens and the stored one has expired"
// @Failure      503  {object}  core.APIError  "Provider is temporarily unavailable"
// @Security     ApiKey
// @Router       /oauth/{provider}/token [post]
func (h *OAuthBrokerHandler) IssueProviderToken(
	w http.ResponseWriter,
	r *http.Request,
) error {
	// The permission alone is not enough. A human holding it could otherwise
	// read any user's Google token with their own login; this endpoint exists
	// only for service-to-service use.
	if !middleware.IsServiceToken(r.Context()) {
		return fmt.Errorf(
			"%w: this endpoint requires a service token (X-API-Key)",
			core.ErrForbidden,
		)
	}

	provider := r.PathValue("provider")
	if provider == "" {
		return fmt.Errorf("%w: missing provider", core.ErrInvalidInput)
	}

	var req ProviderTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("%w: malformed request body", core.ErrInvalidInput)
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return fmt.Errorf("%w: account_id must be a valid UUID", core.ErrInvalidInput)
	}

	if len(req.Capabilities) == 0 {
		return fmt.Errorf(
			"%w: at least one capability is required",
			core.ErrInvalidInput,
		)
	}

	capabilities := make([]providers.Capability, 0, len(req.Capabilities))
	for _, c := range req.Capabilities {
		capabilities = append(capabilities, providers.Capability(c))
	}

	var result *grants.AccessTokenResult
	err = h.withGrantService(r, func(svc grants.GrantService) error {
		var innerErr error
		result, innerErr = svc.GetAccessToken(r.Context(), grants.AccessTokenRequest{
			AccountID:    accountID,
			Provider:     provider,
			Capabilities: capabilities,
		})
		return innerErr
	})
	if err != nil {
		return h.writeTokenError(w, r, provider, accountID, req.Capabilities, err)
	}

	callerID := ""
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = claims.Subject
	}
	// Deliberately no token value and no audit table: this is a read on the
	// hot path, and turning it into a write would cost a round trip per call.
	h.Logger.Info(
		"provider token issued",
		slog.String("provider", provider),
		slog.String("account_id", accountID.String()),
		slog.String("caller_account_id", callerID),
		slog.Any("capabilities", req.Capabilities),
		slog.Bool("refreshed", result.Refreshed),
		slog.Bool("from_cache", result.FromCache),
		slog.Bool("scopes_verified", result.ScopesVerified),
	)

	expiresIn := 0
	if !result.ExpiresAt.IsZero() {
		if remaining := time.Until(result.ExpiresAt); remaining > 0 {
			expiresIn = int(remaining.Seconds())
		}
	}

	core.WriteJSON(w, http.StatusOK, ProviderTokenResponse{
		Provider:       provider,
		AccountID:      accountID.String(),
		AccessToken:    result.AccessToken,
		TokenType:      "Bearer",
		ExpiresAt:      result.ExpiresAt,
		ExpiresIn:      expiresIn,
		GrantedScopes:  result.GrantedScopes,
		ScopesVerified: result.ScopesVerified,
		Refreshed:      result.Refreshed,
		FromCache:      result.FromCache,
	})
	return nil
}

// ListAccountGrants godoc
//
// @Summary      List an account's third-party connections
// @Description  Pre-flight for the broker: which providers an account has connected and what each grant covers. Never includes tokens.
// @Tags         oauth
// @Produce      json
// @Param        account_id  query  string  true  "Account ID"
// @Success      200  {array}   grants.GrantView
// @Failure      400  {object}  core.APIError  "Missing or malformed account_id"
// @Failure      403  {object}  core.APIError  "Caller is not a service token or lacks the permission"
// @Security     ApiKey
// @Router       /oauth/grants [get]
func (h *OAuthBrokerHandler) ListAccountGrants(
	w http.ResponseWriter,
	r *http.Request,
) error {
	if !middleware.IsServiceToken(r.Context()) {
		return fmt.Errorf(
			"%w: this endpoint requires a service token (X-API-Key)",
			core.ErrForbidden,
		)
	}

	accountID, err := uuid.Parse(r.URL.Query().Get("account_id"))
	if err != nil {
		return fmt.Errorf("%w: account_id must be a valid UUID", core.ErrInvalidInput)
	}

	var views []grants.GrantView
	err = h.withGrantService(r, func(svc grants.GrantService) error {
		var innerErr error
		views, innerErr = svc.ListGrants(r.Context(), accountID)
		return innerErr
	})
	if err != nil {
		return err
	}

	core.WriteJSON(w, http.StatusOK, views)
	return nil
}

// ReconcileGrant godoc
//
// @Summary      Force a grant to be re-verified against the provider
// @Description  Refreshes an account's grant purely to learn the scopes the provider reports, converting a presumed scope list into a verified one. Operational endpoint; the background reconciler does this automatically.
// @Tags         oauth
// @Accept       json
// @Produce      json
// @Param        provider  path  string  true  "OAuth2 provider"  Enums(google, spotify)
// @Param        request   body  oauth.ProviderTokenRequest  true  "Account to reconcile (capabilities ignored)"
// @Success      204  "No Content"
// @Failure      400  {object}  core.APIError  "Malformed body or bad account_id"
// @Failure      403  {object}  core.APIError  "Lacks manage:provider_token:any"
// @Failure      404  {object}  core.APIError  "No grant for this account and provider"
// @Failure      503  {object}  core.APIError  "Provider is temporarily unavailable"
// @Security     ApiKey
// @Router       /oauth/{provider}/reconcile [post]
func (h *OAuthBrokerHandler) ReconcileGrant(
	w http.ResponseWriter,
	r *http.Request,
) error {
	provider := r.PathValue("provider")
	if provider == "" {
		return fmt.Errorf("%w: missing provider", core.ErrInvalidInput)
	}

	var req ProviderTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("%w: malformed request body", core.ErrInvalidInput)
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return fmt.Errorf("%w: account_id must be a valid UUID", core.ErrInvalidInput)
	}

	err = h.withGrantService(r, func(svc grants.GrantService) error {
		return svc.ReconcileAccount(r.Context(), accountID, provider)
	})
	if err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// writeTokenError renders the structured error bodies downstream services
// depend on, falling back to the standard sentinel mapping for everything
// else. Returning nil after writing is the AppHandler contract.
func (h *OAuthBrokerHandler) writeTokenError(
	w http.ResponseWriter,
	r *http.Request,
	provider string,
	accountID uuid.UUID,
	requested []string,
	err error,
) error {
	authorizeURL := fmt.Sprintf(
		"%s/oauth/%s/authorize",
		h.Cfg.AuthenticationConfig.AuthAddress,
		provider,
	)

	var insufficient *grants.ErrInsufficientScope
	if errors.As(err, &insufficient) {
		missingCaps := make([]string, 0, len(insufficient.MissingCapabilities))
		for _, c := range insufficient.MissingCapabilities {
			missingCaps = append(missingCaps, string(c))
		}
		// Fall back to what was asked for, so the body is never empty and the
		// caller always has something actionable to relay.
		if len(missingCaps) == 0 {
			missingCaps = requested
		}

		granted := []string{}
		if d, ok := h.Registry.Get(provider); ok {
			for _, c := range d.CapabilitiesFor(insufficient.GrantedScopes) {
				granted = append(granted, string(c))
			}
		}

		h.Logger.Info(
			"provider token denied for insufficient scope",
			slog.String("provider", provider),
			slog.String("account_id", accountID.String()),
			slog.Any("missing_capabilities", missingCaps),
		)

		core.WriteJSON(w, http.StatusForbidden, InsufficientScopeResponse{
			Error:               "insufficient_scope",
			Provider:            provider,
			AccountID:           accountID.String(),
			MissingScopes:       insufficient.MissingScopes,
			MissingCapabilities: missingCaps,
			GrantedCapabilities: granted,
			AuthorizationURL:    authorizeURL,
			AuthorizationMethod: http.MethodPost,
			AuthorizationBody:   map[string][]string{"capabilities": missingCaps},
		})
		return nil
	}

	if errors.Is(err, grants.ErrGrantRevoked) {
		core.WriteJSON(w, http.StatusForbidden, ReauthorizationRequiredResponse{
			Error:               "reauthorization_required",
			Reason:              "invalid_grant",
			Provider:            provider,
			AccountID:           accountID.String(),
			AuthorizationURL:    authorizeURL,
			AuthorizationMethod: http.MethodPost,
		})
		return nil
	}

	if errors.Is(err, grants.ErrNoGrant) {
		core.WriteJSON(w, http.StatusNotFound, ReauthorizationRequiredResponse{
			Error:               "no_grant",
			Reason:              "not_connected",
			Provider:            provider,
			AccountID:           accountID.String(),
			AuthorizationURL:    authorizeURL,
			AuthorizationMethod: http.MethodPost,
		})
		return nil
	}

	if errors.Is(err, grants.ErrProviderUnavailable) {
		h.Logger.Warn(
			"provider unavailable while brokering token",
			slog.String("provider", provider),
			slog.Any("error", err),
		)
	}

	// Everything else goes through the sentinel mapping.
	return err
}

// withGrantService runs fn against a GrantService bound to a transaction.
func (h *OAuthBrokerHandler) withGrantService(
	r *http.Request,
	fn func(svc grants.GrantService) error,
) error {
	conn, err := h.DB.Acquire(r.Context())
	if err != nil {
		return fmt.Errorf("%w: failed to acquire connection", core.ErrInternal)
	}

	return core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		return fn(grants.NewGrantService(
			repository.New(tx),
			h.Cacher,
			h.Registry,
			h.Sealer,
			h.Exchanger,
			h.Cfg,
			h.Logger,
		))
	})
}
