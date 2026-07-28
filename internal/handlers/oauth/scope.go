package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

// OAuthScopeHandler implements incremental authorization: letting a user who
// is already signed in grant an additional capability without signing in
// again.
//
// It deliberately does not reuse the login callback. That handler
// unconditionally upserts the account, registers a device, mints a new token
// family and issues a fresh token pair — correct for a login, wrong for a
// scope upgrade, where the user already has a live session that must survive
// untouched. Threading a mode flag through the single most load-bearing
// function in the service would make any mistake a login outage, so this is a
// separate route that cannot regress sign-in.
type OAuthScopeHandler struct {
	DB        core.IDBProvider
	Cacher    core.Cacher
	Cfg       *config.Config
	Logger    *slog.Logger
	Registry  *providers.Registry
	Exchanger providers.TokenExchanger
	Sealer    *secrets.Sealer
}

// ScopeAuthorizeRequest starts an incremental authorization.
type ScopeAuthorizeRequest struct {
	// Capabilities are abstract names ("calendar"), resolved to provider
	// scopes by the registry.
	Capabilities []string `json:"capabilities"`
	// Platform is "web" or "mobile"; mobile returns to a deep link.
	Platform string `json:"platform"`
	// RedirectURI is where to return a web user. Must be allowlisted.
	RedirectURI string `json:"redirect_uri"`
	// DeepLink is where to return a mobile user. Must be allowlisted.
	DeepLink string `json:"deep_link"`
}

// ScopeAuthorizeResponse carries the URL the client should open.
type ScopeAuthorizeResponse struct {
	// AuthorizationURL is empty when AlreadyGranted is true.
	AuthorizationURL string    `json:"authorization_url,omitempty"`
	State            string    `json:"state,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	// AlreadyGranted short-circuits the round trip when the user has already
	// granted everything asked for and the provider has confirmed it.
	AlreadyGranted  bool     `json:"already_granted"`
	RequestedScopes []string `json:"requested_scopes"`
}

// ScopesResponse describes every provider an account has connected.
type ScopesResponse struct {
	Grants []grants.GrantView `json:"grants"`
	// AvailableCapabilities lets a settings UI render connect-this toggles
	// generically, so adding a provider grows the UI with no client change.
	AvailableCapabilities map[string][]providers.Capability `json:"available_capabilities"`
}

func (h *OAuthScopeHandler) RegisterHandlers(router *http.ServeMux) {
	router.Handle("POST /oauth/{provider}/authorize",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasPermission([]string{"manage:oauth_grant:own"}),
		)(core.AppHandler(h.StartScopeUpgrade)),
	)

	// Public: the provider redirects the user's browser here, with no
	// Authorization header. Authenticity comes from the single-use state
	// handle, which binds the callback to the account that started the flow.
	router.Handle("GET /oauth/{provider}/callback",
		core.AppHandler(h.CompleteScopeUpgrade),
	)

	router.Handle("GET /oauth/scopes",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasPermission([]string{"manage:oauth_grant:own"}),
		)(core.AppHandler(h.ListMyScopes)),
	)

	router.Handle("DELETE /oauth/{provider}/grant",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.Cfg, h.DB, h.Cacher, h.Logger),
			middleware.HasPermission([]string{"manage:oauth_grant:own"}),
		)(core.AppHandler(h.DisconnectProvider)),
	)
}

// StartScopeUpgrade godoc
//
// @Summary      Begin authorizing additional provider capabilities
// @Description  Starts incremental authorization for the signed-in user and returns a provider URL to open. The user's session, devices and tokens are untouched — this grants a capability, it does not sign anyone in again. Returns already_granted when the user has already granted everything asked for. A URL is returned rather than a redirect because this endpoint needs the caller's Bearer token, which a browser navigation cannot supply and which mobile clients must hand to a Custom Tab or ASWebAuthenticationSession themselves.
// @Tags         oauth
// @Accept       json
// @Produce      json
// @Param        provider  path      string                          true  "OAuth2 provider"  Enums(google, spotify)
// @Param        request   body      oauth.ScopeAuthorizeRequest  true  "Capabilities to request and where to return the user"
// @Success      200  {object}  oauth.ScopeAuthorizeResponse
// @Failure      400  {object}  core.APIError  "Unknown capability, provider without incremental support, or a redirect target that is not allowlisted"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      404  {object}  core.APIError  "Unknown provider"
// @Security     BearerToken
// @Router       /oauth/{provider}/authorize [post]
func (h *OAuthScopeHandler) StartScopeUpgrade(
	w http.ResponseWriter,
	r *http.Request,
) error {
	// The account comes from the caller's own token and nowhere else. This is
	// what stops one user attaching another user's provider account, and it is
	// why the callback never needs to be trusted about identity.
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		return fmt.Errorf("%w: missing claims", core.ErrUnauthorized)
	}
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return fmt.Errorf("%w: invalid subject in token", core.ErrUnauthorized)
	}

	descriptor, ok := h.Registry.Get(r.PathValue("provider"))
	if !ok {
		return fmt.Errorf("%w: unknown provider", core.ErrNotFound)
	}
	if !descriptor.SupportsIncremental {
		return fmt.Errorf(
			"%w: %s does not support granting additional scopes; the user must sign in again",
			core.ErrInvalidInput,
			descriptor.Name,
		)
	}

	var req ScopeAuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("%w: malformed request body", core.ErrInvalidInput)
	}
	if len(req.Capabilities) == 0 {
		return fmt.Errorf("%w: at least one capability is required", core.ErrInvalidInput)
	}

	capabilities := make([]providers.Capability, 0, len(req.Capabilities))
	for _, c := range req.Capabilities {
		capabilities = append(capabilities, providers.Capability(c))
	}

	requested, err := descriptor.ScopesFor(capabilities)
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrInvalidInput, err)
	}

	state := scopeUpgradeState{
		AccountID:       accountID,
		Provider:        descriptor.Name,
		Capabilities:    req.Capabilities,
		RequestedScopes: requested,
		Platform:        req.Platform,
		RedirectURI:     req.RedirectURI,
		DeepLink:        req.DeepLink,
		CreatedAt:       time.Now().UTC(),
	}

	if err := h.validateReturnTarget(&state); err != nil {
		return err
	}

	// Read the current grant to decide whether this trip is needed at all,
	// and to pin which provider account the callback may write to.
	existing, err := h.currentGrant(r, accountID, descriptor.Name)
	if err != nil {
		return err
	}

	needsConsent := true
	if existing != nil {
		state.ExpectedExternalUserID = existing.ExternalUserID

		missing := descriptor.MissingScopes(existing.GrantedScopes, requested)
		// Only short-circuit on a verified grant. An unverified one is a
		// presumption, and running the flow converts it into fact cheaply.
		if len(missing) == 0 && existing.ScopesVerified {
			core.WriteJSON(w, http.StatusOK, ScopeAuthorizeResponse{
				AlreadyGranted:  true,
				RequestedScopes: requested,
			})
			return nil
		}
		// Google withholds a refresh token on re-consent unless forced. Ask
		// for it only when we do not already hold one, so returning users are
		// not shown a redundant consent screen.
		needsConsent = !existing.RefreshAvailable
	}

	handle, err := newStateHandle()
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrInternal, err)
	}

	extra := map[string]string{}
	if needsConsent {
		extra["prompt"] = "consent"
	}

	if descriptor.SupportsPKCE {
		verifier, challenge, err := pkcePair()
		if err != nil {
			return fmt.Errorf("%w: %v", core.ErrInternal, err)
		}
		state.PKCEVerifier = verifier
		extra["code_challenge"] = challenge
		extra["code_challenge_method"] = "S256"
	}

	conf, err := descriptor.OAuthConfig(h.Cfg, h.callbackURL(descriptor.Name), requested)
	if err != nil {
		return fmt.Errorf("%w: %v", core.ErrInternal, err)
	}

	authURL := conf.AuthCodeURL(handle, descriptor.AuthCodeOptions(extra)...)

	if err := h.putState(r.Context(), handle, state); err != nil {
		return fmt.Errorf("%w: could not store authorization state", core.ErrInternal)
	}

	h.Logger.Info(
		"scope upgrade started",
		slog.String("provider", descriptor.Name),
		slog.String("account_id", accountID.String()),
		slog.Any("capabilities", req.Capabilities),
	)

	core.WriteJSON(w, http.StatusOK, ScopeAuthorizeResponse{
		AuthorizationURL: authURL,
		State:            handle,
		ExpiresAt:        time.Now().UTC().Add(h.Cfg.ScopeUpgradeStateTTL()),
		RequestedScopes:  requested,
	})
	return nil
}

// CompleteScopeUpgrade godoc
//
// @Summary      Provider callback for incremental authorization
// @Description  Completes a scope upgrade started by the authorize endpoint. Not called by clients — the provider redirects the user's browser here. Records the scopes the provider reports as granted and returns the user to the app. Deliberately issues no session: no token family, no device registration, no cookies, so the caller's existing session survives the round trip.
// @Tags         oauth
// @Produce      json
// @Param        provider  path   string  true   "OAuth2 provider"  Enums(google, spotify)
// @Param        state     query  string  true   "Opaque state handle issued by the authorize endpoint"
// @Param        code      query  string  false  "Authorization code from the provider"
// @Param        error     query  string  false  "Set when the user declined"
// @Success      302  "Redirects to the originating app with scope_upgrade=success or =denied"
// @Failure      400  {object}  core.APIError  "Missing, expired, replayed, or mismatched state"
// @Router       /oauth/{provider}/callback [get]
func (h *OAuthScopeHandler) CompleteScopeUpgrade(
	w http.ResponseWriter,
	r *http.Request,
) error {
	state, err := h.takeState(r.Context(), r.FormValue("state"))
	if err != nil {
		if errors.Is(err, errStateNotFound) {
			return fmt.Errorf(
				"%w: authorization state is invalid or has expired, please try again",
				core.ErrInvalidInput,
			)
		}
		return fmt.Errorf("%w: %v", core.ErrInternal, err)
	}

	// The path must agree with what the flow was started for, so a callback
	// cannot be steered at a different provider's descriptor.
	pathProvider := strings.ToLower(r.PathValue("provider"))
	if pathProvider != state.Provider {
		return fmt.Errorf("%w: provider does not match the authorization request", core.ErrInvalidInput)
	}

	descriptor, ok := h.Registry.Get(state.Provider)
	if !ok {
		return fmt.Errorf("%w: unknown provider", core.ErrNotFound)
	}

	// The user declined, or the provider refused. Nothing to record.
	if providerErr := r.FormValue("error"); providerErr != "" {
		h.Logger.Info(
			"scope upgrade declined",
			slog.String("provider", state.Provider),
			slog.String("account_id", state.AccountID.String()),
			slog.String("reason", providerErr),
		)
		h.redirectBack(w, r, state, "denied", providerErr, nil)
		return nil
	}

	code := r.FormValue("code")
	if code == "" {
		return fmt.Errorf("%w: missing authorization code", core.ErrInvalidInput)
	}

	token, err := h.Exchanger.Exchange(
		r.Context(),
		descriptor,
		code,
		h.callbackURL(descriptor.Name),
		state.PKCEVerifier,
	)
	if err != nil {
		h.Logger.Error(
			"scope upgrade token exchange failed",
			slog.String("provider", state.Provider),
			slog.Any("error", err),
		)
		h.redirectBack(w, r, state, "failed", "exchange_failed", nil)
		return nil
	}

	// Refuse an upgrade that would attach a different provider account to this
	// user's record. Without this a user could graft a second Google account's
	// calendar onto their profile, and a confused-deputy link becomes possible.
	if state.ExpectedExternalUserID != "" {
		subject := subjectFromIDToken(token.IDToken)
		if subject != "" && subject != state.ExpectedExternalUserID {
			h.Logger.Warn(
				"scope upgrade rejected: provider account mismatch",
				slog.String("provider", state.Provider),
				slog.String("account_id", state.AccountID.String()),
			)
			h.redirectBack(w, r, state, "denied", "account_mismatch", nil)
			return nil
		}
	}

	granted := token.Scopes
	verified := descriptor.ReportsScope && granted != nil
	if granted == nil {
		granted = state.RequestedScopes
	}

	err = h.withGrantService(r, func(svc grants.GrantService) error {
		return svc.RecordGrant(r.Context(), grants.RecordGrantInput{
			AccountID:      state.AccountID,
			Provider:       descriptor.Name,
			ExternalUserID: subjectFromIDToken(token.IDToken),
			AccessToken:    token.AccessToken,
			RefreshToken:   token.RefreshToken,
			ExpiresAt:      token.ExpiresAt,
			GrantedScopes:  granted,
			ScopesVerified: verified,
		})
	})
	if err != nil {
		h.Logger.Error(
			"failed to record upgraded grant",
			slog.String("provider", state.Provider),
			slog.Any("error", err),
		)
		h.redirectBack(w, r, state, "failed", "storage_failed", nil)
		return nil
	}

	// A newly widened grant invalidates any cached token issued under the old,
	// narrower scope set.
	if err := h.Cacher.Delete(
		r.Context(),
		fmt.Sprintf("oauth:at:%s:%s", state.AccountID, descriptor.Name),
	); err != nil {
		h.Logger.Warn("failed to invalidate provider token cache", slog.Any("error", err))
	}

	grantedCaps := descriptor.CapabilitiesFor(granted)
	h.Logger.Info(
		"scope upgrade completed",
		slog.String("provider", state.Provider),
		slog.String("account_id", state.AccountID.String()),
		slog.Any("granted_capabilities", grantedCaps),
	)

	h.redirectBack(w, r, state, "success", "", grantedCaps)
	return nil
}

// ListMyScopes godoc
//
// @Summary      List the signed-in user's third-party connections
// @Description  What each connected provider covers, plus every capability each provider could offer, so a settings UI can render connect toggles without knowing the provider list. Never includes tokens.
// @Tags         oauth
// @Produce      json
// @Success      200  {object}  oauth.ScopesResponse
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Security     BearerToken
// @Router       /oauth/scopes [get]
func (h *OAuthScopeHandler) ListMyScopes(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		return fmt.Errorf("%w: missing claims", core.ErrUnauthorized)
	}
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return fmt.Errorf("%w: invalid subject in token", core.ErrUnauthorized)
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

	core.WriteJSON(w, http.StatusOK, ScopesResponse{
		Grants:                views,
		AvailableCapabilities: h.Registry.AvailableCapabilities(),
	})
	return nil
}

// DisconnectProvider godoc
//
// @Summary      Disconnect a third-party provider
// @Description  Revokes the signed-in user's grant and destroys the stored credentials. Does not sign the user out; it only ends Verisafe's access to that provider on their behalf.
// @Tags         oauth
// @Produce      json
// @Param        provider  path  string  true  "OAuth2 provider"  Enums(google, spotify, apple)
// @Success      204  "No Content"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      404  {object}  core.APIError  "No grant for this provider"
// @Security     BearerToken
// @Router       /oauth/{provider}/grant [delete]
func (h *OAuthScopeHandler) DisconnectProvider(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		return fmt.Errorf("%w: missing claims", core.ErrUnauthorized)
	}
	accountID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return fmt.Errorf("%w: invalid subject in token", core.ErrUnauthorized)
	}

	provider := r.PathValue("provider")
	if provider == "" {
		return fmt.Errorf("%w: missing provider", core.ErrInvalidInput)
	}

	err = h.withGrantService(r, func(svc grants.GrantService) error {
		return svc.RevokeGrant(r.Context(), accountID, provider, "user_disconnected")
	})
	if err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- helpers ---

// validateReturnTarget checks where the user will be sent back to.
//
// Both platforms are validated against the same allowlist. The login flow does
// not currently validate deep_link at all; that gap is deliberately not
// reproduced here.
func (h *OAuthScopeHandler) validateReturnTarget(state *scopeUpgradeState) error {
	allowed := h.Cfg.JWTConfig.AllowedRedirectURIs

	if state.Platform == "web" {
		if state.RedirectURI == "" {
			return fmt.Errorf("%w: redirect_uri is required for platform=web", core.ErrInvalidInput)
		}
		if !isReturnTargetAllowed(state.RedirectURI, allowed) {
			return fmt.Errorf("%w: redirect_uri is not allowed", core.ErrInvalidInput)
		}
		return nil
	}

	if state.DeepLink == "" {
		return fmt.Errorf("%w: deep_link is required for mobile", core.ErrInvalidInput)
	}
	if !isReturnTargetAllowed(state.DeepLink, allowed) {
		return fmt.Errorf("%w: deep_link is not allowed", core.ErrInvalidInput)
	}
	return nil
}

// isReturnTargetAllowed does an exact, case-insensitive match, mirroring the
// login flow's allowlist semantics. No prefix or wildcard matching, so every
// valid target must be listed verbatim.
func isReturnTargetAllowed(target string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), target) {
			return true
		}
	}
	return false
}

// redirectBack returns the user to the app that started the flow.
//
// The target is re-validated here even though it was checked at start time,
// because between the two it lived in Redis. A tampered value must not become
// an open redirect.
func (h *OAuthScopeHandler) redirectBack(
	w http.ResponseWriter,
	r *http.Request,
	state *scopeUpgradeState,
	status, reason string,
	granted []providers.Capability,
) {
	target := state.RedirectURI
	if state.Platform != "web" {
		target = state.DeepLink
	}

	if target == "" || !isReturnTargetAllowed(target, h.Cfg.JWTConfig.AllowedRedirectURIs) {
		h.Logger.Warn(
			"scope upgrade return target rejected, responding directly",
			slog.String("provider", state.Provider),
		)
		if status == "success" {
			core.WriteJSON(w, http.StatusOK, map[string]any{
				"scope_upgrade": status,
				"provider":      state.Provider,
			})
			return
		}
		core.WriteError(w, http.StatusBadRequest, "authorization could not be completed")
		return
	}

	u, err := url.Parse(target)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid return target")
		return
	}

	q := u.Query()
	q.Set("scope_upgrade", status)
	q.Set("provider", state.Provider)
	if reason != "" {
		q.Set("reason", reason)
	}
	if len(granted) > 0 {
		names := make([]string, 0, len(granted))
		for _, c := range granted {
			names = append(names, string(c))
		}
		q.Set("granted", strings.Join(names, ","))
	}
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
}

// currentGrant loads the caller's existing connection, or nil if there is none.
func (h *OAuthScopeHandler) currentGrant(
	r *http.Request,
	accountID uuid.UUID,
	provider string,
) (*grants.GrantView, error) {
	var grant *grants.GrantView

	err := h.withGrantService(r, func(svc grants.GrantService) error {
		found, err := svc.GetGrant(r.Context(), accountID, provider)
		if err != nil {
			if errors.Is(err, grants.ErrNoGrant) {
				return nil
			}
			return err
		}
		grant = found
		return nil
	})
	if err != nil {
		return nil, err
	}
	return grant, nil
}

func (h *OAuthScopeHandler) callbackURL(provider string) string {
	return fmt.Sprintf(
		"%s/oauth/%s/callback",
		strings.TrimSuffix(h.Cfg.AuthenticationConfig.AuthAddress, "/"),
		provider,
	)
}

func (h *OAuthScopeHandler) withGrantService(
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

// subjectFromIDToken reads the "sub" claim without verifying the signature.
//
// That is safe specifically here and would not be elsewhere: this token was
// received over TLS directly from the provider's token endpoint in response to
// our own authenticated request, not forwarded by a client. OpenID Connect
// Core 3.1.3.7 permits skipping signature validation on exactly this path.
// The value is only ever used to compare against a previously stored subject,
// never to establish identity.
func subjectFromIDToken(idToken string) string {
	if idToken == "" {
		return ""
	}

	parsed, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return ""
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}

	subject, _ := claims["sub"].(string)
	return subject
}
