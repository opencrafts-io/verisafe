package grants

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/secrets"
)

// Grant-specific errors. These wrap core sentinels so core.HandleError maps
// them to the right status without the handler restating the mapping, while
// errors.As on the typed ones still yields the structured detail a caller
// needs to act.
var (
	// ErrNoGrant means the account has never connected this provider.
	ErrNoGrant = fmt.Errorf(
		"%w: no grant for this account and provider",
		core.ErrNotFound,
	)

	// ErrGrantRevoked means the stored credentials are gone or dead. The user
	// must re-authorize; no amount of retrying helps.
	ErrGrantRevoked = fmt.Errorf(
		"%w: the grant has been revoked",
		core.ErrForbidden,
	)

	// ErrRefreshUnsupported means the provider cannot refresh tokens and the
	// stored one has expired.
	ErrRefreshUnsupported = fmt.Errorf(
		"%w: provider does not support token refresh",
		core.ErrConflict,
	)

	// ErrProviderUnavailable means the provider failed transiently.
	ErrProviderUnavailable = fmt.Errorf(
		"%w: oauth provider is unavailable",
		core.ErrUnavailable,
	)
)

// ErrInsufficientScope reports that a grant exists but lacks what was asked
// for. It carries the specifics so the broker can tell a downstream service
// exactly which capabilities to send the user back to authorize.
type ErrInsufficientScope struct {
	Provider            string
	MissingScopes       []string
	MissingCapabilities []providers.Capability
	GrantedScopes       []string
}

func (e *ErrInsufficientScope) Error() string {
	return fmt.Sprintf(
		"insufficient scope for provider %s: missing %v",
		e.Provider,
		e.MissingScopes,
	)
}

// Is lets core.HandleError map this to 403 without knowing the type.
func (e *ErrInsufficientScope) Is(target error) bool {
	return target == core.ErrForbidden
}

// AccessTokenRequest asks for a usable provider access token.
type AccessTokenRequest struct {
	AccountID    uuid.UUID
	Provider     string
	Capabilities []providers.Capability
}

// AccessTokenResult is a token the caller can put straight on the wire.
type AccessTokenResult struct {
	AccessToken   string
	ExpiresAt     time.Time
	GrantedScopes []string
	// ScopesVerified is false when the scope list is still what we presumed
	// from historical logins rather than what the provider confirmed. Callers
	// can use it to degrade gracefully rather than trusting the list.
	ScopesVerified bool
	Refreshed      bool
	FromCache      bool
}

// RecordGrantInput persists tokens obtained from a login or a scope upgrade.
type RecordGrantInput struct {
	AccountID      uuid.UUID
	Provider       string
	ExternalUserID string
	AccessToken    string
	RefreshToken   string
	ExpiresAt      time.Time
	// GrantedScopes is what the provider reported. Leave nil when the provider
	// does not report scopes; the previously stored list is then kept.
	GrantedScopes []string
	// ScopesVerified marks the scope list as provider-confirmed rather than
	// presumed. Only set it when GrantedScopes came from a token response.
	ScopesVerified bool
}

// GrantView is the read model for a connected provider. It deliberately has
// no token fields — the repository row does, and must never be serialized.
type GrantView struct {
	Provider              string                 `json:"provider"`
	ExternalUserID        string                 `json:"external_user_id,omitempty"`
	GrantedScopes         []string               `json:"granted_scopes"`
	GrantedCapabilities   []providers.Capability `json:"granted_capabilities"`
	ScopesVerified        bool                   `json:"scopes_verified"`
	RefreshAvailable      bool                   `json:"refresh_available"`
	SupportsIncremental   bool                   `json:"supports_incremental"`
	ConnectedAt           *time.Time             `json:"connected_at,omitempty"`
	LastRefreshedAt       *time.Time             `json:"last_refreshed_at,omitempty"`
	ExpiresAt             *time.Time             `json:"expires_at,omitempty"`
	Revoked               bool                   `json:"revoked"`
	RevokedReason         string                 `json:"revoked_reason,omitempty"`
	AvailableCapabilities []providers.Capability `json:"available_capabilities"`
}

// GrantService owns third-party OAuth credentials: storing them, keeping them
// fresh, and answering whether a grant covers what a caller needs.
//
// Everything meaningful lives here rather than in the handler, above the
// repository.New(tx) boundary, so it can be exercised with a mocked Querier
// and a fake exchanger instead of a live database and a live provider.
type GrantService interface {
	// GetAccessToken returns a usable access token, refreshing if needed.
	GetAccessToken(
		ctx context.Context,
		in AccessTokenRequest,
	) (*AccessTokenResult, error)

	// RecordGrant stores tokens from a login or scope upgrade.
	RecordGrant(ctx context.Context, in RecordGrantInput) error

	// ListGrants returns every provider an account has connected.
	ListGrants(ctx context.Context, accountID uuid.UUID) ([]GrantView, error)

	// GetGrant returns a single connection, or ErrNoGrant.
	GetGrant(
		ctx context.Context,
		accountID uuid.UUID,
		provider string,
	) (*GrantView, error)

	// Reconcile refreshes a grant purely to learn its true scopes. Takes a
	// grant id because the reconciliation worker iterates over rows.
	Reconcile(ctx context.Context, grantID uuid.UUID) error

	// ReconcileAccount is Reconcile addressed the way an API caller thinks:
	// by account and provider rather than by row id.
	ReconcileAccount(
		ctx context.Context,
		accountID uuid.UUID,
		provider string,
	) error

	// RevokeGrant marks a connection unusable and destroys its credentials.
	RevokeGrant(
		ctx context.Context,
		accountID uuid.UUID,
		provider, reason string,
	) error
}

type grantService struct {
	repo      repository.Querier
	cacher    core.Cacher
	registry  *providers.Registry
	sealer    *secrets.Sealer
	exchanger providers.TokenExchanger
	cfg       *config.Config
	logger    *slog.Logger
}

// NewGrantService builds a GrantService.
func NewGrantService(
	repo repository.Querier,
	cacher core.Cacher,
	registry *providers.Registry,
	sealer *secrets.Sealer,
	exchanger providers.TokenExchanger,
	cfg *config.Config,
	logger *slog.Logger,
) GrantService {
	return &grantService{
		repo:      repo,
		cacher:    cacher,
		registry:  registry,
		sealer:    sealer,
		exchanger: exchanger,
		cfg:       cfg,
		logger:    logger,
	}
}

// cachedToken is what the broker stores in Redis between calls.
type cachedToken struct {
	AccessToken    string    `json:"access_token"`
	ExpiresAt      time.Time `json:"expires_at"`
	GrantedScopes  []string  `json:"granted_scopes"`
	ScopesVerified bool      `json:"scopes_verified"`
}

func tokenCacheKey(accountID uuid.UUID, provider string) string {
	return fmt.Sprintf("oauth:at:%s:%s", accountID, provider)
}

func refreshLockKey(accountID uuid.UUID, provider string) string {
	return fmt.Sprintf("oauth:refresh_lock:%s:%s", accountID, provider)
}

// GetAccessToken returns a token that is valid now and covers the requested
// capabilities, refreshing against the provider when necessary.
func (s *grantService) GetAccessToken(
	ctx context.Context,
	in AccessTokenRequest,
) (*AccessTokenResult, error) {
	descriptor, ok := s.registry.Get(in.Provider)
	if !ok {
		return nil, fmt.Errorf(
			"%w: unknown provider %q",
			core.ErrNotFound,
			in.Provider,
		)
	}

	required, err := descriptor.ScopesFor(in.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrInvalidInput, err)
	}

	if result, ok := s.readTokenCache(
		ctx,
		descriptor,
		in.AccountID,
		required,
	); ok {
		return result, nil
	}

	grant, err := s.repo.GetOAuthGrant(ctx, repository.GetOAuthGrantParams{
		AccountID: in.AccountID,
		Lower:     descriptor.Name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoGrant
		}
		return nil, fmt.Errorf("%w: look up grant: %v", core.ErrInternal, err)
	}

	if grant.RevokedAt != nil {
		return nil, ErrGrantRevoked
	}

	verified := grant.ScopesVerifiedAt != nil

	// A verified grant is authoritative, so a missing scope is a real denial.
	// An unverified one is only our presumption about what was granted years
	// ago, so denying on it would be guessing. Fall through instead and let
	// the provider adjudicate at step "re-check" below — the cost of a wrong
	// presumption is then one extra round trip, never a wrong refusal.
	if missing := descriptor.MissingScopes(
		grant.GrantedScopes,
		required,
	); len(missing) > 0 &&
		verified {
		return nil, s.insufficientScope(
			descriptor,
			grant.GrantedScopes,
			missing,
		)
	}

	// A NULL expiry means unknown, which every backfilled row has. Treating it
	// as stale is what makes verify-on-first-use work: the first broker call
	// for any migrated account triggers a refresh whose response tells us the
	// real scopes.
	if s.isFresh(grant) && verified {
		token, err := s.openAccessToken(grant)
		if err == nil && token != "" {
			result := &AccessTokenResult{
				AccessToken:    token,
				ExpiresAt:      derefTime(grant.ExpiresAt),
				GrantedScopes:  grant.GrantedScopes,
				ScopesVerified: true,
			}
			s.writeTokenCache(ctx, in.AccountID, descriptor.Name, result)
			return result, nil
		}
		// Falling through to refresh is the right move on a decrypt failure:
		// the row may predate a key rotation, and a refresh re-seals it.
		if err != nil {
			s.logger.Warn(
				"stored access token could not be opened, refreshing instead",
				slog.String("provider", descriptor.Name),
				slog.String("account_id", in.AccountID.String()),
				slog.Any("error", err),
			)
		}
	}

	if !descriptor.SupportsRefresh {
		return nil, ErrRefreshUnsupported
	}

	refreshToken, err := s.openRefreshToken(grant)
	if err != nil || refreshToken == "" {
		s.markRevoked(ctx, grant.ID, "no_refresh_token")
		return nil, ErrGrantRevoked
	}

	return s.refreshAndStore(ctx, descriptor, grant, refreshToken, required)
}

// refreshAndStore performs the provider round trip under a distributed lock
// and persists the result.
func (s *grantService) refreshAndStore(
	ctx context.Context,
	descriptor providers.Descriptor,
	grant repository.OauthGrant,
	refreshToken string,
	required []string,
) (*AccessTokenResult, error) {
	unlock := s.acquireRefreshLock(
		ctx,
		grant.AccountID,
		descriptor.Name,
		required,
	)
	if unlock == nil {
		// Another caller won the race and published a usable token.
		if result, ok := s.readTokenCache(
			ctx,
			descriptor,
			grant.AccountID,
			required,
		); ok {
			return result, nil
		}
	} else {
		defer unlock()
	}

	token, err := s.exchanger.Refresh(ctx, descriptor, refreshToken)
	if err != nil {
		return nil, s.handleRefreshError(ctx, descriptor, grant, err)
	}

	// Only a provider that reports scopes can move a grant from presumed to
	// verified. For the others the previously stored list stands, and the
	// grant stays presumed forever — which is honest.
	granted := grant.GrantedScopes
	verified := grant.ScopesVerifiedAt != nil
	if descriptor.ReportsScope && token.Scopes != nil {
		granted = token.Scopes
		verified = true
	}

	if err := s.persistToken(
		ctx,
		descriptor,
		grant,
		token,
		granted,
		verified,
	); err != nil {
		return nil, err
	}

	// Re-check against what the provider just told us. This is the other half
	// of not denying on a presumption: if the presumption was wrong, we find
	// out here, with authority, rather than having guessed earlier.
	if missing := descriptor.MissingScopes(
		granted,
		required,
	); len(
		missing,
	) > 0 {
		return nil, s.insufficientScope(descriptor, granted, missing)
	}

	result := &AccessTokenResult{
		AccessToken:    token.AccessToken,
		ExpiresAt:      token.ExpiresAt,
		GrantedScopes:  granted,
		ScopesVerified: verified,
		Refreshed:      true,
	}
	s.writeTokenCache(ctx, grant.AccountID, descriptor.Name, result)
	return result, nil
}

// persistToken re-seals both tokens under the active key and writes them.
//
// Both must be sealed together: the row carries one enc_key_version covering
// both columns, so preserving one ciphertext while bumping the version would
// make it permanently unreadable. Sealing both every time also means key
// rotation needs no backfill — rows migrate as they are used.
func (s *grantService) persistToken(
	ctx context.Context,
	descriptor providers.Descriptor,
	grant repository.OauthGrant,
	token *providers.Token,
	granted []string,
	verified bool,
) error {
	accessEnc, version, err := s.sealer.Seal(
		token.AccessToken,
		secrets.GrantAAD(grant.AccountID, descriptor.Name, "access_token"),
	)
	if err != nil {
		return fmt.Errorf("%w: seal access token: %v", core.ErrInternal, err)
	}

	var refreshEnc []byte
	if token.RefreshToken != "" {
		refreshEnc, _, err = s.sealer.Seal(
			token.RefreshToken,
			secrets.GrantAAD(grant.AccountID, descriptor.Name, "refresh_token"),
		)
		if err != nil {
			return fmt.Errorf(
				"%w: seal refresh token: %v",
				core.ErrInternal,
				err,
			)
		}
	}

	now := time.Now().UTC()
	params := repository.UpsertOAuthGrantParams{
		AccountID:       grant.AccountID,
		Provider:        descriptor.Name,
		ExternalUserID:  grant.ExternalUserID,
		AccessTokenEnc:  accessEnc,
		RefreshTokenEnc: refreshEnc,
		EncKeyVersion:   version,
		GrantedScopes:   granted,
		LastRefreshedAt: &now,
	}
	if verified {
		params.ScopesVerifiedAt = &now
	}
	if !token.ExpiresAt.IsZero() {
		expiry := token.ExpiresAt.UTC()
		params.ExpiresAt = &expiry
	}

	if _, err := s.repo.UpsertOAuthGrant(ctx, params); err != nil {
		return fmt.Errorf("%w: persist grant: %v", core.ErrInternal, err)
	}
	return nil
}

// handleRefreshError decides whether a failed refresh kills the grant.
//
// The distinction matters enormously: invalid_grant means the user really did
// revoke us and the credentials are dead, but a 5xx or a timeout means Google
// is having a bad minute. Revoking on the latter would disconnect every user
// at once and force them all to re-authorize.
func (s *grantService) handleRefreshError(
	ctx context.Context,
	descriptor providers.Descriptor,
	grant repository.OauthGrant,
	err error,
) error {
	switch {
	case errors.Is(err, providers.ErrInvalidGrant):
		s.logger.Info(
			"provider rejected refresh token, revoking grant",
			slog.String("provider", descriptor.Name),
			slog.String("account_id", grant.AccountID.String()),
		)
		s.markRevoked(ctx, grant.ID, "invalid_grant")
		s.invalidateTokenCache(ctx, grant.AccountID, descriptor.Name)
		return ErrGrantRevoked

	case errors.Is(err, providers.ErrRefreshUnsupported):
		return ErrRefreshUnsupported

	case errors.Is(err, providers.ErrProviderUnavailable):
		s.recordFailure(ctx, grant.ID, err)
		return ErrProviderUnavailable

	default:
		s.recordFailure(ctx, grant.ID, err)
		return fmt.Errorf("%w: refresh failed: %v", core.ErrInternal, err)
	}
}

// RecordGrant stores tokens obtained from a login or a scope upgrade.
func (s *grantService) RecordGrant(
	ctx context.Context,
	in RecordGrantInput,
) error {
	descriptor, ok := s.registry.Get(in.Provider)
	if !ok {
		return fmt.Errorf(
			"%w: unknown provider %q",
			core.ErrNotFound,
			in.Provider,
		)
	}

	// Read the existing row so a provider that declines to reissue a refresh
	// token does not cost the user their connection. Google only returns one
	// on first consent, so this is the common case, not the edge case.
	existing, err := s.repo.GetOAuthGrant(ctx, repository.GetOAuthGrantParams{
		AccountID: in.AccountID,
		Lower:     descriptor.Name,
	})
	hasExisting := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: look up grant: %v", core.ErrInternal, err)
	}

	refreshToken := in.RefreshToken
	if refreshToken == "" && hasExisting {
		if prior, openErr := s.openRefreshToken(existing); openErr == nil {
			refreshToken = prior
		}
	}

	granted := descriptor.NormalizeAll(in.GrantedScopes)
	verified := in.ScopesVerified && len(granted) > 0
	if len(granted) == 0 && hasExisting {
		granted = existing.GrantedScopes
		verified = existing.ScopesVerifiedAt != nil
	}

	externalID := in.ExternalUserID
	var externalIDPtr *string
	if externalID != "" {
		externalIDPtr = &externalID
	} else if hasExisting {
		externalIDPtr = existing.ExternalUserID
	}

	return s.persistToken(
		ctx,
		descriptor,
		repository.OauthGrant{
			ID:             existing.ID,
			AccountID:      in.AccountID,
			ExternalUserID: externalIDPtr,
		},
		&providers.Token{
			AccessToken:  in.AccessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    in.ExpiresAt,
		},
		granted,
		verified,
	)
}

// ListGrants returns every provider an account has connected.
func (s *grantService) ListGrants(
	ctx context.Context,
	accountID uuid.UUID,
) ([]GrantView, error) {
	rows, err := s.repo.ListOAuthGrantsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("%w: list grants: %v", core.ErrInternal, err)
	}

	out := make([]GrantView, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.toView(row))
	}
	return out, nil
}

// GetGrant returns a single connection.
func (s *grantService) GetGrant(
	ctx context.Context,
	accountID uuid.UUID,
	provider string,
) (*GrantView, error) {
	descriptor, ok := s.registry.Get(provider)
	if !ok {
		return nil, fmt.Errorf(
			"%w: unknown provider %q",
			core.ErrNotFound,
			provider,
		)
	}

	row, err := s.repo.GetOAuthGrant(ctx, repository.GetOAuthGrantParams{
		AccountID: accountID,
		Lower:     descriptor.Name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoGrant
		}
		return nil, fmt.Errorf("%w: look up grant: %v", core.ErrInternal, err)
	}

	view := s.toView(row)
	return &view, nil
}

// Reconcile refreshes a grant solely to learn what it actually covers.
//
// Used by the background worker to drain accounts nobody has brokered a token
// for, so the presumed-scope population shrinks to zero and the transitional
// plaintext columns can eventually be dropped.
func (s *grantService) Reconcile(ctx context.Context, grantID uuid.UUID) error {
	grant, err := s.repo.GetOAuthGrantByID(ctx, grantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoGrant
		}
		return fmt.Errorf("%w: look up grant: %v", core.ErrInternal, err)
	}

	descriptor, ok := s.registry.Get(grant.Provider)
	if !ok || !descriptor.SupportsRefresh {
		return ErrRefreshUnsupported
	}

	refreshToken, err := s.openRefreshToken(grant)
	if err != nil || refreshToken == "" {
		s.markRevoked(ctx, grant.ID, "no_refresh_token")
		return ErrGrantRevoked
	}

	token, err := s.exchanger.Refresh(ctx, descriptor, refreshToken)
	if err != nil {
		return s.handleRefreshError(ctx, descriptor, grant, err)
	}

	granted := grant.GrantedScopes
	verified := grant.ScopesVerifiedAt != nil
	if descriptor.ReportsScope && token.Scopes != nil {
		granted = token.Scopes
		verified = true
	}

	return s.persistToken(ctx, descriptor, grant, token, granted, verified)
}

// ReconcileAccount resolves an account and provider to a grant, then
// reconciles it.
func (s *grantService) ReconcileAccount(
	ctx context.Context,
	accountID uuid.UUID,
	provider string,
) error {
	descriptor, ok := s.registry.Get(provider)
	if !ok {
		return fmt.Errorf("%w: unknown provider %q", core.ErrNotFound, provider)
	}

	grant, err := s.repo.GetOAuthGrant(ctx, repository.GetOAuthGrantParams{
		AccountID: accountID,
		Lower:     descriptor.Name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoGrant
		}
		return fmt.Errorf("%w: look up grant: %v", core.ErrInternal, err)
	}

	return s.Reconcile(ctx, grant.ID)
}

// RevokeGrant destroys the stored credentials for a connection.
func (s *grantService) RevokeGrant(
	ctx context.Context,
	accountID uuid.UUID,
	provider, reason string,
) error {
	descriptor, ok := s.registry.Get(provider)
	if !ok {
		return fmt.Errorf("%w: unknown provider %q", core.ErrNotFound, provider)
	}

	grant, err := s.repo.GetOAuthGrant(ctx, repository.GetOAuthGrantParams{
		AccountID: accountID,
		Lower:     descriptor.Name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoGrant
		}
		return fmt.Errorf("%w: look up grant: %v", core.ErrInternal, err)
	}

	if err := s.repo.MarkOAuthGrantRevoked(
		ctx,
		repository.MarkOAuthGrantRevokedParams{
			ID:     grant.ID,
			Reason: &reason,
		},
	); err != nil {
		return fmt.Errorf("%w: revoke grant: %v", core.ErrInternal, err)
	}

	s.invalidateTokenCache(ctx, accountID, descriptor.Name)
	return nil
}

// --- helpers ---

func (s *grantService) toView(row repository.OauthGrant) GrantView {
	descriptor, known := s.registry.Get(row.Provider)

	view := GrantView{
		Provider:       row.Provider,
		GrantedScopes:  row.GrantedScopes,
		ScopesVerified: row.ScopesVerifiedAt != nil,
		RefreshAvailable: len(row.RefreshTokenEnc) > 0 ||
			row.RefreshTokenPlain != nil,
		LastRefreshedAt: row.LastRefreshedAt,
		ExpiresAt:       row.ExpiresAt,
		Revoked:         row.RevokedAt != nil,
	}

	if row.ExternalUserID != nil {
		view.ExternalUserID = *row.ExternalUserID
	}
	if row.RevokedReason != nil {
		view.RevokedReason = *row.RevokedReason
	}
	view.ConnectedAt = &row.CreatedAt
	if known {
		view.GrantedCapabilities = descriptor.CapabilitiesFor(row.GrantedScopes)
		view.AvailableCapabilities = descriptor.CapabilityNames()
		view.SupportsIncremental = descriptor.SupportsIncremental
	}

	// A revoked grant has no usable credentials regardless of what the
	// nullable columns happen to say.
	if view.Revoked {
		view.RefreshAvailable = false
	}

	return view
}

func (s *grantService) insufficientScope(
	descriptor providers.Descriptor,
	granted, missing []string,
) error {
	var missingCaps []providers.Capability
	for capability, scopes := range descriptor.Capabilities {
		if len(descriptor.MissingScopes(granted, scopes)) > 0 {
			for _, m := range missing {
				if contains(descriptor.NormalizeAll(scopes), m) {
					missingCaps = append(missingCaps, capability)
					break
				}
			}
		}
	}

	return &ErrInsufficientScope{
		Provider:            descriptor.Name,
		MissingScopes:       missing,
		MissingCapabilities: missingCaps,
		GrantedScopes:       granted,
	}
}

func (s *grantService) isFresh(grant repository.OauthGrant) bool {
	if grant.ExpiresAt == nil {
		return false
	}
	return grant.ExpiresAt.After(time.Now().Add(s.cfg.RefreshSkew()))
}

func (s *grantService) openAccessToken(
	grant repository.OauthGrant,
) (string, error) {
	if len(grant.AccessTokenEnc) > 0 {
		return s.sealer.Open(
			grant.AccessTokenEnc,
			grant.EncKeyVersion,
			secrets.GrantAAD(grant.AccountID, grant.Provider, "access_token"),
		)
	}
	if grant.AccessTokenPlain != nil {
		return *grant.AccessTokenPlain, nil
	}
	return "", nil
}

// openRefreshToken reads the refresh token, falling back to the transitional
// plaintext column that the socials backfill populated. Rows drain off that
// column on their first successful refresh, since the upsert always clears it.
func (s *grantService) openRefreshToken(
	grant repository.OauthGrant,
) (string, error) {
	if len(grant.RefreshTokenEnc) > 0 {
		return s.sealer.Open(
			grant.RefreshTokenEnc,
			grant.EncKeyVersion,
			secrets.GrantAAD(grant.AccountID, grant.Provider, "refresh_token"),
		)
	}
	if grant.RefreshTokenPlain != nil {
		return *grant.RefreshTokenPlain, nil
	}
	return "", nil
}

func (s *grantService) markRevoked(
	ctx context.Context,
	grantID uuid.UUID,
	reason string,
) {
	if err := s.repo.MarkOAuthGrantRevoked(
		ctx,
		repository.MarkOAuthGrantRevokedParams{
			ID:     grantID,
			Reason: &reason,
		},
	); err != nil {
		s.logger.Error(
			"failed to mark grant revoked",
			slog.String("grant_id", grantID.String()),
			slog.Any("error", err),
		)
	}
}

func (s *grantService) recordFailure(
	ctx context.Context,
	grantID uuid.UUID,
	cause error,
) {
	message := cause.Error()
	if err := s.repo.RecordOAuthGrantRefreshFailure(
		ctx,
		repository.RecordOAuthGrantRefreshFailureParams{
			ID:               grantID,
			LastRefreshError: &message,
		},
	); err != nil {
		s.logger.Error(
			"failed to record refresh failure",
			slog.String("grant_id", grantID.String()),
			slog.Any("error", err),
		)
	}
}

// acquireRefreshLock serializes refreshes for one account and provider.
//
// Returns nil when the lock was not won, meaning another caller is refreshing.
// The caller then polls the token cache, and if the winner never publishes,
// proceeds anyway: a duplicate refresh is recoverable, whereas failing the
// request would turn a slow peer into an outage for a downstream service.
//
// A Postgres row lock would give stricter mutual exclusion, but only by
// holding a pooled connection across an outbound HTTPS call to the provider.
// Under provider latency that starves the pool and takes unrelated endpoints
// down with it, so the transaction boundaries stay short and the lock lives
// in Redis instead.
func (s *grantService) acquireRefreshLock(
	ctx context.Context,
	accountID uuid.UUID,
	provider string,
	_ []string,
) func() {
	key := refreshLockKey(accountID, provider)

	won, err := s.cacher.SetNX(
		ctx,
		key,
		time.Now().UTC().String(),
		15*time.Second,
	)
	if err != nil {
		// Redis being down must not stop us serving a token. Degrading to
		// "possible duplicate refresh" is benign.
		s.logger.Warn(
			"refresh lock unavailable, proceeding without it",
			slog.String("provider", provider),
			slog.Any("error", err),
		)
		return func() {}
	}
	if !won {
		return nil
	}

	return func() {
		// Detached from ctx so the lock is released even when the caller's
		// request has already been cancelled.
		if err := s.cacher.Delete(context.WithoutCancel(ctx), key); err != nil {
			s.logger.Warn(
				"failed to release refresh lock",
				slog.String("provider", provider),
				slog.Any("error", err),
			)
		}
	}
}

func (s *grantService) readTokenCache(
	ctx context.Context,
	descriptor providers.Descriptor,
	accountID uuid.UUID,
	required []string,
) (*AccessTokenResult, bool) {
	var cached cachedToken
	if err := s.cacher.Get(
		ctx,
		tokenCacheKey(accountID, descriptor.Name),
		&cached,
	); err != nil {
		return nil, false
	}

	if cached.AccessToken == "" ||
		!cached.ExpiresAt.After(time.Now().Add(s.cfg.RefreshSkew())) {
		return nil, false
	}

	// A cached token only helps if it covers what this caller asked for.
	if len(descriptor.MissingScopes(cached.GrantedScopes, required)) > 0 {
		return nil, false
	}

	return &AccessTokenResult{
		AccessToken:    cached.AccessToken,
		ExpiresAt:      cached.ExpiresAt,
		GrantedScopes:  cached.GrantedScopes,
		ScopesVerified: cached.ScopesVerified,
		FromCache:      true,
	}, true
}

func (s *grantService) writeTokenCache(
	ctx context.Context,
	accountID uuid.UUID,
	provider string,
	result *AccessTokenResult,
) {
	ttl := s.cfg.ProviderTokenCacheTTL()
	if !result.ExpiresAt.IsZero() {
		// Never cache past the token's own life, less a minute of headroom.
		if until := time.Until(result.ExpiresAt) - time.Minute; until < ttl {
			ttl = until
		}
	}
	if ttl <= 0 {
		return
	}

	if err := s.cacher.Set(ctx, tokenCacheKey(accountID, provider), cachedToken{
		AccessToken:    result.AccessToken,
		ExpiresAt:      result.ExpiresAt,
		GrantedScopes:  result.GrantedScopes,
		ScopesVerified: result.ScopesVerified,
	}, ttl); err != nil {
		s.logger.Warn(
			"failed to cache provider access token",
			slog.String("provider", provider),
			slog.Any("error", err),
		)
	}
}

func (s *grantService) invalidateTokenCache(
	ctx context.Context,
	accountID uuid.UUID,
	provider string,
) {
	if err := s.cacher.Delete(
		ctx,
		tokenCacheKey(accountID, provider),
	); err != nil {
		s.logger.Warn(
			"failed to invalidate provider token cache",
			slog.String("provider", provider),
			slog.Any("error", err),
		)
	}
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
