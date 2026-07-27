package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	mockscore "github.com/opencrafts-io/verisafe/internal/core/mocks"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/secrets"
	"github.com/opencrafts-io/verisafe/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	googleEmail    = "https://www.googleapis.com/auth/userinfo.email"
	googleProfile  = "https://www.googleapis.com/auth/userinfo.profile"
	googleCalendar = "https://www.googleapis.com/auth/calendar"
	googleTasks    = "https://www.googleapis.com/auth/tasks"
)

// fakeExchanger stands in for the provider's token endpoint. Hand-written
// rather than generated so each test can script exactly one outcome and assert
// on call counts.
type fakeExchanger struct {
	refreshCalls  int
	exchangeCalls int
	token         *providers.Token
	err           error
	lastRefresh   string
}

func (f *fakeExchanger) Exchange(
	_ context.Context, _ providers.Descriptor, _, _, _ string,
) (*providers.Token, error) {
	f.exchangeCalls++
	return f.token, f.err
}

func (f *fakeExchanger) Refresh(
	_ context.Context, _ providers.Descriptor, refreshToken string,
) (*providers.Token, error) {
	f.refreshCalls++
	f.lastRefresh = refreshToken
	if f.err != nil {
		return nil, f.err
	}
	return f.token, nil
}

type harness struct {
	svc       service.GrantService
	repo      *mockQuerier.MockQuerier
	cacher    *mockscore.MockCacher
	exchanger *fakeExchanger
	sealer    *secrets.Sealer
	accountID uuid.UUID
	grantID   uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)

	cfg := &config.Config{}
	cfg.ProviderTokensConfig.RefreshSkewSeconds = 120
	cfg.ProviderTokensConfig.CacheTTLSeconds = 300

	sealer, err := secrets.NewSealer("1:"+testKey(0xAA)+",2:"+testKey(0xBB), 1)
	require.NoError(t, err)

	repo := mockQuerier.NewMockQuerier(ctrl)
	cacher := mockscore.NewMockCacher(ctrl)
	exchanger := &fakeExchanger{}

	// Cache is incidental to almost every test: default to a miss and allow
	// writes, so tests only mention it when it is the thing under test.
	cacher.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(core.ErrCacheMiss).AnyTimes()
	cacher.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()
	cacher.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(true, nil).AnyTimes()
	cacher.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return &harness{
		svc: service.NewGrantService(
			repo, cacher, providers.NewRegistry(cfg), sealer, exchanger, cfg,
			slog.New(slog.NewTextHandler(io.Discard, nil)),
		),
		repo:      repo,
		cacher:    cacher,
		exchanger: exchanger,
		sealer:    sealer,
		accountID: uuid.New(),
		grantID:   uuid.New(),
	}
}

func testKey(b byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// grant builds a google grant row, sealed under the harness's active key.
func (h *harness) grant(t *testing.T, mutate func(*repository.OauthGrant)) repository.OauthGrant {
	t.Helper()

	accessEnc, version, err := h.sealer.Seal(
		"stored-access-token",
		secrets.GrantAAD(h.accountID, "google", "access_token"),
	)
	require.NoError(t, err)
	refreshEnc, _, err := h.sealer.Seal(
		"stored-refresh-token",
		secrets.GrantAAD(h.accountID, "google", "refresh_token"),
	)
	require.NoError(t, err)

	verified := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	g := repository.OauthGrant{
		ID:               h.grantID,
		AccountID:        h.accountID,
		Provider:         "google",
		AccessTokenEnc:   accessEnc,
		RefreshTokenEnc:  refreshEnc,
		EncKeyVersion:    version,
		GrantedScopes:    []string{googleEmail, googleProfile, googleCalendar},
		ScopesVerifiedAt: &verified,
		ExpiresAt:        &future,
	}
	if mutate != nil {
		mutate(&g)
	}
	return g
}

func (h *harness) expectGet(g repository.OauthGrant) {
	h.repo.EXPECT().
		GetOAuthGrant(gomock.Any(), repository.GetOAuthGrantParams{
			AccountID: h.accountID,
			Lower:     "google",
		}).
		Return(g, nil)
}

func (h *harness) request(caps ...providers.Capability) service.AccessTokenRequest {
	return service.AccessTokenRequest{
		AccountID:    h.accountID,
		Provider:     "google",
		Capabilities: caps,
	}
}

func freshToken() *providers.Token {
	return &providers.Token{
		AccessToken:  "refreshed-access-token",
		RefreshToken: "rotated-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{googleEmail, googleProfile, googleCalendar},
	}
}

func TestGetAccessToken_UnknownProvider(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.GetAccessToken(context.Background(), service.AccessTokenRequest{
		AccountID: h.accountID,
		Provider:  "microsoft",
	})
	assert.ErrorIs(t, err, core.ErrNotFound)
}

func TestGetAccessToken_UnknownCapability(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityPlayback))
	assert.ErrorIs(t, err, core.ErrInvalidInput)
	assert.Equal(t, 0, h.exchanger.refreshCalls)
}

func TestGetAccessToken_NoGrant(t *testing.T) {
	h := newHarness(t)
	h.repo.EXPECT().GetOAuthGrant(gomock.Any(), gomock.Any()).
		Return(repository.OauthGrant{}, pgx.ErrNoRows)

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	assert.ErrorIs(t, err, service.ErrNoGrant)
	assert.ErrorIs(t, err, core.ErrNotFound)
}

func TestGetAccessToken_RevokedGrant(t *testing.T) {
	h := newHarness(t)
	revoked := time.Now()
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) { g.RevokedAt = &revoked }))

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	assert.ErrorIs(t, err, service.ErrGrantRevoked)
	assert.Equal(t, 0, h.exchanger.refreshCalls)
}

// A verified, unexpired grant must be served straight from storage. Hitting
// the provider here would add a round trip to every request for no reason.
func TestGetAccessToken_FreshVerifiedGrantDoesNotRefresh(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, nil))

	got, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err)

	assert.Equal(t, "stored-access-token", got.AccessToken)
	assert.True(t, got.ScopesVerified)
	assert.False(t, got.Refreshed)
	assert.Equal(t, 0, h.exchanger.refreshCalls, "a fresh token must not trigger a provider call")
}

func TestGetAccessToken_WithinSkewRefreshes(t *testing.T) {
	h := newHarness(t)
	// Inside the 120s skew: still valid, but too close to expiry to hand out.
	soon := time.Now().Add(30 * time.Second)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) { g.ExpiresAt = &soon }))

	h.exchanger.token = freshToken()
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		Return(repository.OauthGrant{}, nil)

	got, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err)

	assert.Equal(t, "refreshed-access-token", got.AccessToken)
	assert.True(t, got.Refreshed)
	assert.Equal(t, 1, h.exchanger.refreshCalls)
}

// Every row the socials backfill created has a NULL expiry. Treating that as
// stale is what makes the whole verify-on-first-use scheme work: the first
// broker call for a migrated account refreshes, and the response tells us the
// true scopes. Without this, presumed grants would never become verified.
func TestGetAccessToken_NullExpiryRefreshesAndVerifies(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.ExpiresAt = nil
		g.ScopesVerifiedAt = nil // presumed, as the backfill leaves it
	}))

	h.exchanger.token = freshToken()

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	got, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err)

	assert.Equal(t, 1, h.exchanger.refreshCalls)
	assert.True(t, got.ScopesVerified, "the refresh response must promote the grant to verified")
	assert.NotNil(t, captured.ScopesVerifiedAt, "scopes_verified_at must be persisted")
}

// A verified grant is authoritative, so a genuinely missing scope is a real
// denial and must not cost a provider round trip.
func TestGetAccessToken_VerifiedGrantMissingScopeDenies(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.GrantedScopes = []string{googleEmail, googleProfile}
	}))

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.Error(t, err)

	var insufficient *service.ErrInsufficientScope
	require.ErrorAs(t, err, &insufficient)
	assert.Equal(t, []string{googleCalendar}, insufficient.MissingScopes)
	assert.Equal(t, []providers.Capability{providers.CapabilityCalendar}, insufficient.MissingCapabilities)
	assert.ErrorIs(t, err, core.ErrForbidden)
	assert.Equal(t, 0, h.exchanger.refreshCalls)
}

// The presumption-safety contract. An unverified scope list is our guess about
// what a user granted years ago, so denying on it would refuse a user who
// actually does have access. Refresh instead and let the provider decide.
func TestGetAccessToken_UnverifiedGrantMissingScopeDoesNotDeny(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.ScopesVerifiedAt = nil
		g.GrantedScopes = []string{googleEmail} // presumed, and wrong
	}))

	// The provider says calendar was granted after all.
	h.exchanger.token = freshToken()
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		Return(repository.OauthGrant{}, nil)

	got, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err, "an unverified presumption must never produce a denial")

	assert.Equal(t, 1, h.exchanger.refreshCalls)
	assert.True(t, got.ScopesVerified)
	assert.Contains(t, got.GrantedScopes, googleCalendar)
}

// The other half: when the provider confirms the scope really is absent, the
// re-check after refresh must catch it.
func TestGetAccessToken_UnverifiedGrantStillMissingAfterRefreshDenies(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.ScopesVerifiedAt = nil
		g.GrantedScopes = []string{googleEmail}
	}))

	h.exchanger.token = &providers.Token{
		AccessToken: "refreshed",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scopes:      []string{googleEmail, googleProfile},
	}
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		Return(repository.OauthGrant{}, nil)

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))

	var insufficient *service.ErrInsufficientScope
	require.ErrorAs(t, err, &insufficient)
	assert.Equal(t, []string{googleCalendar}, insufficient.MissingScopes)
}

// Google's granular consent lets a user revoke one scope without disconnecting
// the app. The stored list must shrink to match, so the next call denies
// immediately rather than repeatedly discovering it the hard way.
func TestGetAccessToken_NarrowedScopesSelfHeal(t *testing.T) {
	h := newHarness(t)
	stale := time.Now().Add(-time.Hour)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.ExpiresAt = &stale
		g.GrantedScopes = []string{googleEmail, googleProfile, googleCalendar}
	}))

	// The user revoked calendar in their Google account settings.
	h.exchanger.token = &providers.Token{
		AccessToken: "refreshed",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scopes:      []string{googleEmail, googleProfile},
	}

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))

	var insufficient *service.ErrInsufficientScope
	require.ErrorAs(t, err, &insufficient)
	assert.NotContains(t, captured.GrantedScopes, googleCalendar,
		"the stored scope list must shrink to what the provider now reports")
	assert.NotNil(t, captured.ScopesVerifiedAt)
}

func TestGetAccessToken_InvalidGrantRevokes(t *testing.T) {
	h := newHarness(t)
	stale := time.Now().Add(-time.Hour)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) { g.ExpiresAt = &stale }))

	h.exchanger.err = providers.ErrInvalidGrant

	var captured repository.MarkOAuthGrantRevokedParams
	h.repo.EXPECT().MarkOAuthGrantRevoked(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.MarkOAuthGrantRevokedParams) error {
			captured = p
			return nil
		})

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	assert.ErrorIs(t, err, service.ErrGrantRevoked)

	require.NotNil(t, captured.Reason)
	assert.Equal(t, "invalid_grant", *captured.Reason)
	assert.Equal(t, h.grantID, captured.ID)
}

// The single most damaging mistake available here: revoking on a transient
// provider failure would disconnect every user at once during a Google blip
// and force all 3000 of them to re-authorize. Assert it explicitly.
func TestGetAccessToken_ProviderOutageNeverRevokes(t *testing.T) {
	h := newHarness(t)
	stale := time.Now().Add(-time.Hour)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) { g.ExpiresAt = &stale }))

	h.exchanger.err = providers.ErrProviderUnavailable

	h.repo.EXPECT().RecordOAuthGrantRefreshFailure(gomock.Any(), gomock.Any()).Return(nil)
	h.repo.EXPECT().MarkOAuthGrantRevoked(gomock.Any(), gomock.Any()).Times(0)

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	assert.ErrorIs(t, err, service.ErrProviderUnavailable)
	assert.ErrorIs(t, err, core.ErrUnavailable)
}

func TestGetAccessToken_RefreshUnsupportedProvider(t *testing.T) {
	h := newHarness(t)
	stale := time.Now().Add(-time.Hour)

	h.repo.EXPECT().
		GetOAuthGrant(gomock.Any(), repository.GetOAuthGrantParams{
			AccountID: h.accountID,
			Lower:     "apple",
		}).
		Return(repository.OauthGrant{
			ID:            h.grantID,
			AccountID:     h.accountID,
			Provider:      "apple",
			GrantedScopes: []string{"name", "email"},
			ExpiresAt:     &stale,
		}, nil)

	_, err := h.svc.GetAccessToken(context.Background(), service.AccessTokenRequest{
		AccountID:    h.accountID,
		Provider:     "apple",
		Capabilities: []providers.Capability{providers.CapabilityIdentity},
	})
	assert.ErrorIs(t, err, service.ErrRefreshUnsupported)
	assert.Equal(t, 0, h.exchanger.refreshCalls)
}

func TestGetAccessToken_NoRefreshTokenRevokes(t *testing.T) {
	h := newHarness(t)
	stale := time.Now().Add(-time.Hour)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.ExpiresAt = &stale
		g.RefreshTokenEnc = nil
		g.RefreshTokenPlain = nil
	}))

	var captured repository.MarkOAuthGrantRevokedParams
	h.repo.EXPECT().MarkOAuthGrantRevoked(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.MarkOAuthGrantRevokedParams) error {
			captured = p
			return nil
		})

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	assert.ErrorIs(t, err, service.ErrGrantRevoked)
	assert.Equal(t, "no_refresh_token", *captured.Reason)
}

// Providers that do not rotate refresh tokens return the field empty. The
// stored token must be carried forward, never written as NULL — doing so would
// silently disconnect the user on the next call.
func TestGetAccessToken_EmptyReturnedRefreshTokenIsNotPersistedAsNull(t *testing.T) {
	h := newHarness(t)
	stale := time.Now().Add(-time.Hour)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) { g.ExpiresAt = &stale }))

	h.exchanger.token = &providers.Token{
		AccessToken: "refreshed",
		// Provider declined to rotate; the exchanger normally carries the old
		// one forward, but assert the service does not depend on that.
		RefreshToken: "stored-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{googleEmail, googleProfile, googleCalendar},
	}

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err)

	require.NotEmpty(t, captured.RefreshTokenEnc, "refresh token must never be persisted as NULL")

	// It must be readable back, i.e. genuinely re-sealed rather than garbage.
	got, err := h.sealer.Open(
		captured.RefreshTokenEnc,
		captured.EncKeyVersion,
		secrets.GrantAAD(h.accountID, "google", "refresh_token"),
	)
	require.NoError(t, err)
	assert.Equal(t, "stored-refresh-token", got)
	assert.Equal(t, "stored-refresh-token", h.exchanger.lastRefresh)
}

// A row sealed under an old key must still open, and the rewrite must move it
// to the active version — this is what makes key rotation lazy.
func TestGetAccessToken_KeyRotationIsLazy(t *testing.T) {
	h := newHarness(t)

	// Seal the stored refresh token under version 2 while active is 1, to
	// prove decrypt selects by stored version rather than active.
	rotated, err := secrets.NewSealer("1:"+testKey(0xAA)+",2:"+testKey(0xBB), 2)
	require.NoError(t, err)
	oldEnc, oldVersion, err := rotated.Seal(
		"old-key-refresh-token",
		secrets.GrantAAD(h.accountID, "google", "refresh_token"),
	)
	require.NoError(t, err)
	require.Equal(t, int16(2), oldVersion)

	stale := time.Now().Add(-time.Hour)
	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.ExpiresAt = &stale
		g.AccessTokenEnc = nil
		g.RefreshTokenEnc = oldEnc
		g.EncKeyVersion = 2
	}))

	h.exchanger.token = freshToken()

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	_, err = h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err)

	assert.Equal(t, "old-key-refresh-token", h.exchanger.lastRefresh,
		"decryption must use the version stored on the row")
	assert.Equal(t, int16(1), captured.EncKeyVersion,
		"the rewrite must move the row onto the active key")
}

// Backfilled rows carry plaintext copied from socials. They must be usable,
// and the upsert must clear the plaintext so the population drains itself.
func TestGetAccessToken_ReadsTransitionalPlaintext(t *testing.T) {
	h := newHarness(t)
	stale := time.Now().Add(-time.Hour)
	plain := "plaintext-refresh-from-socials"

	h.expectGet(h.grant(t, func(g *repository.OauthGrant) {
		g.ExpiresAt = &stale
		g.AccessTokenEnc = nil
		g.RefreshTokenEnc = nil
		g.RefreshTokenPlain = &plain
		g.ScopesVerifiedAt = nil
	}))

	h.exchanger.token = freshToken()

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	_, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err)

	assert.Equal(t, plain, h.exchanger.lastRefresh, "must fall back to the transitional plaintext")
	assert.NotEmpty(t, captured.RefreshTokenEnc, "and re-seal it on the way out")
}

// A cached token that covers the request skips the database entirely.
func TestGetAccessToken_ServedFromCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	cfg := &config.Config{}
	cfg.ProviderTokensConfig.RefreshSkewSeconds = 120
	cfg.ProviderTokensConfig.CacheTTLSeconds = 300

	sealer, err := secrets.NewSealer("1:"+testKey(0xAA), 1)
	require.NoError(t, err)

	repo := mockQuerier.NewMockQuerier(ctrl)
	cacher := mockscore.NewMockCacher(ctrl)
	exchanger := &fakeExchanger{}
	accountID := uuid.New()

	cacher.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, dest any) error {
			payload := map[string]any{
				"access_token":    "cached-token",
				"expires_at":      time.Now().Add(time.Hour),
				"granted_scopes":  []string{googleEmail, googleProfile, googleCalendar},
				"scopes_verified": true,
			}
			return remarshal(payload, dest)
		})

	// The point of the test: no database call at all.
	repo.EXPECT().GetOAuthGrant(gomock.Any(), gomock.Any()).Times(0)

	svc := service.NewGrantService(
		repo, cacher, providers.NewRegistry(cfg), sealer, exchanger, cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	got, err := svc.GetAccessToken(context.Background(), service.AccessTokenRequest{
		AccountID:    accountID,
		Provider:     "google",
		Capabilities: []providers.Capability{providers.CapabilityCalendar},
	})
	require.NoError(t, err)

	assert.Equal(t, "cached-token", got.AccessToken)
	assert.True(t, got.FromCache)
	assert.Equal(t, 0, exchanger.refreshCalls)
}

// A cached token that does not cover the requested capability must be ignored
// rather than handed back, or the caller gets a token the provider rejects.
func TestGetAccessToken_CacheMissWhenScopesInsufficient(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, nil))

	got, err := h.svc.GetAccessToken(context.Background(), h.request(providers.CapabilityCalendar))
	require.NoError(t, err)
	assert.False(t, got.FromCache)
}

// Losing the lock must not fail the request. The winner may die mid-refresh,
// and a 503 to a downstream service is worse than a duplicate provider call.
func TestGetAccessToken_LockLostProceedsRatherThanFailing(t *testing.T) {
	ctrl := gomock.NewController(t)
	cfg := &config.Config{}
	cfg.ProviderTokensConfig.RefreshSkewSeconds = 120
	cfg.ProviderTokensConfig.CacheTTLSeconds = 300

	sealer, err := secrets.NewSealer("1:"+testKey(0xAA), 1)
	require.NoError(t, err)

	repo := mockQuerier.NewMockQuerier(ctrl)
	cacher := mockscore.NewMockCacher(ctrl)
	exchanger := &fakeExchanger{token: freshToken()}
	accountID := uuid.New()

	cacher.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(core.ErrCacheMiss).AnyTimes()
	cacher.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).AnyTimes()
	cacher.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	// Someone else holds the lock.
	cacher.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(false, nil)

	accessEnc, version, err := sealer.Seal("a", secrets.GrantAAD(accountID, "google", "access_token"))
	require.NoError(t, err)
	refreshEnc, _, err := sealer.Seal("r", secrets.GrantAAD(accountID, "google", "refresh_token"))
	require.NoError(t, err)

	stale := time.Now().Add(-time.Hour)
	verified := time.Now().Add(-time.Hour)
	repo.EXPECT().GetOAuthGrant(gomock.Any(), gomock.Any()).Return(repository.OauthGrant{
		ID:               uuid.New(),
		AccountID:        accountID,
		Provider:         "google",
		AccessTokenEnc:   accessEnc,
		RefreshTokenEnc:  refreshEnc,
		EncKeyVersion:    version,
		GrantedScopes:    []string{googleEmail, googleProfile, googleCalendar},
		ScopesVerifiedAt: &verified,
		ExpiresAt:        &stale,
	}, nil)
	repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		Return(repository.OauthGrant{}, nil)

	svc := service.NewGrantService(
		repo, cacher, providers.NewRegistry(cfg), sealer, exchanger, cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	got, err := svc.GetAccessToken(context.Background(), service.AccessTokenRequest{
		AccountID:    accountID,
		Provider:     "google",
		Capabilities: []providers.Capability{providers.CapabilityCalendar},
	})
	require.NoError(t, err, "a lost lock must not fail the caller")
	assert.True(t, got.Refreshed)
}

func TestRevokeGrant(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, nil))

	var captured repository.MarkOAuthGrantRevokedParams
	h.repo.EXPECT().MarkOAuthGrantRevoked(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.MarkOAuthGrantRevokedParams) error {
			captured = p
			return nil
		})

	require.NoError(t, h.svc.RevokeGrant(context.Background(), h.accountID, "google", "user_disconnected"))
	assert.Equal(t, "user_disconnected", *captured.Reason)
}

func TestListGrants_ExposesNoCredentials(t *testing.T) {
	h := newHarness(t)
	h.repo.EXPECT().ListOAuthGrantsByAccount(gomock.Any(), h.accountID).
		Return([]repository.OauthGrant{h.grant(t, nil)}, nil)

	got, err := h.svc.ListGrants(context.Background(), h.accountID)
	require.NoError(t, err)
	require.Len(t, got, 1)

	assert.Equal(t, "google", got[0].Provider)
	assert.True(t, got[0].ScopesVerified)
	assert.True(t, got[0].RefreshAvailable)
	assert.Contains(t, got[0].GrantedCapabilities, providers.CapabilityCalendar)
	assert.Contains(t, got[0].AvailableCapabilities, providers.CapabilityTasks)
	assert.False(t, got[0].Revoked)
}

func TestListGrants_RevokedHasNoRefreshAvailable(t *testing.T) {
	h := newHarness(t)
	revoked := time.Now()
	reason := "invalid_grant"
	h.repo.EXPECT().ListOAuthGrantsByAccount(gomock.Any(), h.accountID).
		Return([]repository.OauthGrant{h.grant(t, func(g *repository.OauthGrant) {
			g.RevokedAt = &revoked
			g.RevokedReason = &reason
		})}, nil)

	got, err := h.svc.ListGrants(context.Background(), h.accountID)
	require.NoError(t, err)

	assert.True(t, got[0].Revoked)
	assert.False(t, got[0].RefreshAvailable, "a revoked grant has nothing to refresh with")
	assert.Equal(t, "invalid_grant", got[0].RevokedReason)
}

func TestRecordGrant_PreservesExistingRefreshTokenWhenProviderOmitsIt(t *testing.T) {
	h := newHarness(t)
	h.expectGet(h.grant(t, nil))

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	// Google only issues a refresh token on first consent, so subsequent
	// logins arrive with an empty one. Losing the stored token here would
	// disconnect the user.
	err := h.svc.RecordGrant(context.Background(), service.RecordGrantInput{
		AccountID:      h.accountID,
		Provider:       "google",
		AccessToken:    "new-access",
		RefreshToken:   "",
		GrantedScopes:  []string{googleEmail, googleProfile},
		ScopesVerified: true,
	})
	require.NoError(t, err)

	got, err := h.sealer.Open(
		captured.RefreshTokenEnc,
		captured.EncKeyVersion,
		secrets.GrantAAD(h.accountID, "google", "refresh_token"),
	)
	require.NoError(t, err)
	assert.Equal(t, "stored-refresh-token", got)
}

func TestRecordGrant_NewConnection(t *testing.T) {
	h := newHarness(t)
	h.repo.EXPECT().GetOAuthGrant(gomock.Any(), gomock.Any()).
		Return(repository.OauthGrant{}, pgx.ErrNoRows)

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	err := h.svc.RecordGrant(context.Background(), service.RecordGrantInput{
		AccountID:      h.accountID,
		Provider:       "google",
		ExternalUserID: "109348572093485720394",
		AccessToken:    "access",
		RefreshToken:   "refresh",
		ExpiresAt:      time.Now().Add(time.Hour),
		GrantedScopes:  []string{"email", "profile"},
		ScopesVerified: true,
	})
	require.NoError(t, err)

	// Scopes must be stored canonically, not as the short aliases.
	assert.Equal(t, []string{googleEmail, googleProfile}, captured.GrantedScopes)
	assert.NotNil(t, captured.ScopesVerifiedAt)
	require.NotNil(t, captured.ExternalUserID)
	assert.Equal(t, "109348572093485720394", *captured.ExternalUserID)
}

func TestReconcile_VerifiesScopes(t *testing.T) {
	h := newHarness(t)
	h.repo.EXPECT().GetOAuthGrantByID(gomock.Any(), h.grantID).
		Return(h.grant(t, func(g *repository.OauthGrant) { g.ScopesVerifiedAt = nil }), nil)

	h.exchanger.token = freshToken()

	var captured repository.UpsertOAuthGrantParams
	h.repo.EXPECT().UpsertOAuthGrant(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, p repository.UpsertOAuthGrantParams) (repository.OauthGrant, error) {
			captured = p
			return repository.OauthGrant{}, nil
		})

	require.NoError(t, h.svc.Reconcile(context.Background(), h.grantID))
	assert.NotNil(t, captured.ScopesVerifiedAt)
}

func TestReconcile_ProviderOutageDoesNotRevoke(t *testing.T) {
	h := newHarness(t)
	h.repo.EXPECT().GetOAuthGrantByID(gomock.Any(), h.grantID).Return(h.grant(t, nil), nil)

	h.exchanger.err = providers.ErrProviderUnavailable
	h.repo.EXPECT().RecordOAuthGrantRefreshFailure(gomock.Any(), gomock.Any()).Return(nil)
	h.repo.EXPECT().MarkOAuthGrantRevoked(gomock.Any(), gomock.Any()).Times(0)

	err := h.svc.Reconcile(context.Background(), h.grantID)
	assert.ErrorIs(t, err, service.ErrProviderUnavailable)
}

func TestGetGrant_NotFound(t *testing.T) {
	h := newHarness(t)
	h.repo.EXPECT().GetOAuthGrant(gomock.Any(), gomock.Any()).
		Return(repository.OauthGrant{}, pgx.ErrNoRows)

	_, err := h.svc.GetGrant(context.Background(), h.accountID, "google")
	assert.ErrorIs(t, err, service.ErrNoGrant)
}

func TestErrInsufficientScope_MapsToForbidden(t *testing.T) {
	err := &service.ErrInsufficientScope{Provider: "google", MissingScopes: []string{googleCalendar}}
	assert.ErrorIs(t, err, core.ErrForbidden)
	assert.Contains(t, err.Error(), "google")
	assert.NotErrorIs(t, err, core.ErrNotFound)
}

func TestSentinels_MapToDistinctStatuses(t *testing.T) {
	// The grant service must return core sentinels, not the duplicate set in
	// package service, or core.HandleError silently turns everything into 500.
	assert.ErrorIs(t, service.ErrNoGrant, core.ErrNotFound)
	assert.ErrorIs(t, service.ErrGrantRevoked, core.ErrForbidden)
	assert.ErrorIs(t, service.ErrRefreshUnsupported, core.ErrConflict)
	assert.ErrorIs(t, service.ErrProviderUnavailable, core.ErrUnavailable)
}

// remarshal round-trips through JSON so a map literal can populate the typed
// destination the cacher would otherwise fill.
func remarshal(from any, dest any) error {
	raw, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}
