package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	mockscore "github.com/opencrafts-io/verisafe/internal/core/mocks"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/opencrafts-io/verisafe/internal/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const testDeepLink = "myapp://oauth/upgraded"
const testRedirectURI = "https://app.example.com/settings/integrations"

func scopeHandler(t *testing.T) (*OAuthScopeHandler, *mockscore.MockCacher) {
	t.Helper()
	ctrl := gomock.NewController(t)

	cfg := &config.Config{}
	cfg.AuthenticationConfig.AuthAddress = "https://verisafe.example.com"
	cfg.AuthenticationConfig.GoogleClientID = "cid.apps.googleusercontent.com"
	cfg.AuthenticationConfig.GoogleClientSecret = "client-secret"
	cfg.JWTConfig.AllowedRedirectURIs = []string{testRedirectURI, testDeepLink}
	cfg.ProviderTokensConfig.ScopeUpgradeTTLSeconds = 600

	cacher := mockscore.NewMockCacher(ctrl)

	return &OAuthScopeHandler{
		Cacher:   cacher,
		Cfg:      cfg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Registry: providers.NewRegistry(cfg),
	}, cacher
}

// --- state handling ---

func TestStateHandle_IsUnguessableAndUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		handle, err := newStateHandle()
		require.NoError(t, err)
		// 32 random bytes, RawURLEncoded.
		assert.GreaterOrEqual(t, len(handle), 42)
		_, dup := seen[handle]
		require.False(t, dup, "state handles must not repeat")
		seen[handle] = struct{}{}
	}
}

// The Redis key must not be the handle itself, so a dump of the keyspace does
// not hand an attacker usable states.
func TestStateKey_HashesTheHandle(t *testing.T) {
	const handle = "a-known-handle"
	key := stateKey(handle)

	assert.True(t, strings.HasPrefix(key, scopeUpgradeStatePrefix))
	assert.NotContains(t, key, handle)
	assert.Equal(t, key, stateKey(handle), "hashing must be deterministic")
}

func TestTakeState_RoundTripThenReplayFails(t *testing.T) {
	h, cacher := scopeHandler(t)
	accountID := uuid.New()

	stored := scopeUpgradeState{
		AccountID:       accountID,
		Provider:        "google",
		RequestedScopes: []string{"https://www.googleapis.com/auth/calendar"},
		Platform:        "web",
		RedirectURI:     testRedirectURI,
	}

	handle, err := newStateHandle()
	require.NoError(t, err)

	cacher.EXPECT().
		Set(gomock.Any(), stateKey(handle), gomock.Any(), 600*time.Second).
		Return(nil)
	require.NoError(t, h.putState(context.Background(), handle, stored))

	cacher.EXPECT().
		Get(gomock.Any(), stateKey(handle), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, dest any) error {
			raw, _ := json.Marshal(stored)
			return json.Unmarshal(raw, dest)
		})
	cacher.EXPECT().Delete(gomock.Any(), stateKey(handle)).Return(nil)

	got, err := h.takeState(context.Background(), handle)
	require.NoError(t, err)
	assert.Equal(t, accountID, got.AccountID)
	assert.Equal(t, "google", got.Provider)

	// Reading consumed it, so a captured callback URL is worthless on replay.
	cacher.EXPECT().
		Get(gomock.Any(), stateKey(handle), gomock.Any()).
		Return(core.ErrCacheMiss)

	_, err = h.takeState(context.Background(), handle)
	assert.ErrorIs(t, err, errStateNotFound)
}

func TestTakeState_EmptyHandle(t *testing.T) {
	h, _ := scopeHandler(t)
	_, err := h.takeState(context.Background(), "")
	assert.ErrorIs(t, err, errStateNotFound)
}

func TestPKCEPair(t *testing.T) {
	verifier, challenge, err := pkcePair()
	require.NoError(t, err)

	assert.NotEmpty(t, verifier)
	assert.NotEmpty(t, challenge)
	assert.NotEqual(t, verifier, challenge, "the challenge must be a digest, not the verifier")

	// Must be URL-safe: these travel in a query string.
	assert.NotContains(t, verifier, "+")
	assert.NotContains(t, verifier, "=")
	assert.NotContains(t, challenge, "/")

	second, _, err := pkcePair()
	require.NoError(t, err)
	assert.NotEqual(t, verifier, second)
}

// --- authorize ---

func newAuthorizeRequest(t *testing.T, provider, body, subject string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	req := httptest.NewRequest("POST", "/oauth/"+provider+"/authorize", strings.NewReader(body))
	req.SetPathValue("provider", provider)

	if subject != "" {
		claims := &tokens.VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: subject},
		}
		req = req.WithContext(
			middleware.WithClaims(req.Context(), claims),
		)
	}
	return httptest.NewRecorder(), req
}

func TestStartScopeUpgrade_RequiresClaims(t *testing.T) {
	h, _ := scopeHandler(t)
	rr, req := newAuthorizeRequest(t, "google", `{"capabilities":["calendar"]}`, "")

	err := h.StartScopeUpgrade(rr, req)
	assert.ErrorIs(t, err, core.ErrUnauthorized)
}

func TestStartScopeUpgrade_UnknownProvider(t *testing.T) {
	h, _ := scopeHandler(t)
	rr, req := newAuthorizeRequest(t, "microsoft", `{"capabilities":["calendar"]}`, uuid.NewString())

	err := h.StartScopeUpgrade(rr, req)
	assert.ErrorIs(t, err, core.ErrNotFound)
}

// Apple cannot do additive consent, so asking must fail loudly rather than
// producing a URL that would silently replace the user's existing grant.
func TestStartScopeUpgrade_ProviderWithoutIncrementalSupport(t *testing.T) {
	h, _ := scopeHandler(t)
	rr, req := newAuthorizeRequest(t, "apple", `{"capabilities":["identity"]}`, uuid.NewString())

	err := h.StartScopeUpgrade(rr, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
	assert.Contains(t, err.Error(), "sign in again")
}

func TestStartScopeUpgrade_InputValidation(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantSub string
	}{
		{"malformed json", `{"capabilities":`, "malformed"},
		{"no capabilities", `{"capabilities":[]}`, "at least one capability"},
		{"unknown capability", `{"capabilities":["telepathy"]}`, "telepathy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := scopeHandler(t)
			rr, req := newAuthorizeRequest(t, "google", tc.body, uuid.NewString())

			err := h.StartScopeUpgrade(rr, req)
			require.Error(t, err)
			assert.ErrorIs(t, err, core.ErrInvalidInput)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

func TestStartScopeUpgrade_ReturnTargetMustBeAllowlisted(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantSub string
	}{
		{
			name:    "web without redirect_uri",
			body:    `{"capabilities":["calendar"],"platform":"web"}`,
			wantSub: "redirect_uri is required",
		},
		{
			name:    "web with unlisted redirect_uri",
			body:    `{"capabilities":["calendar"],"platform":"web","redirect_uri":"https://evil.example.com/steal"}`,
			wantSub: "redirect_uri is not allowed",
		},
		{
			name:    "mobile without deep_link",
			body:    `{"capabilities":["calendar"],"platform":"mobile"}`,
			wantSub: "deep_link is required",
		},
		{
			// The login flow does not validate deep_link; this one does.
			name:    "mobile with unlisted deep_link",
			body:    `{"capabilities":["calendar"],"platform":"mobile","deep_link":"evil://steal"}`,
			wantSub: "deep_link is not allowed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := scopeHandler(t)
			rr, req := newAuthorizeRequest(t, "google", tc.body, uuid.NewString())

			err := h.StartScopeUpgrade(rr, req)
			require.Error(t, err)
			assert.ErrorIs(t, err, core.ErrInvalidInput)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

// --- callback ---

func newCallbackRequest(t *testing.T, provider, query string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	req := httptest.NewRequest("GET", "/oauth/"+provider+"/callback?"+query, nil)
	req.SetPathValue("provider", provider)
	return httptest.NewRecorder(), req
}

func TestCompleteScopeUpgrade_UnknownStateIsRejected(t *testing.T) {
	h, cacher := scopeHandler(t)
	cacher.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(core.ErrCacheMiss)

	rr, req := newCallbackRequest(t, "google", "state=whatever&code=abc")

	err := h.CompleteScopeUpgrade(rr, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
	assert.Contains(t, err.Error(), "expired")
}

func TestCompleteScopeUpgrade_MissingState(t *testing.T) {
	h, _ := scopeHandler(t)
	rr, req := newCallbackRequest(t, "google", "code=abc")

	err := h.CompleteScopeUpgrade(rr, req)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
}

// A callback must not be steerable at a provider other than the one the flow
// was started for.
func TestCompleteScopeUpgrade_ProviderMismatch(t *testing.T) {
	h, cacher := scopeHandler(t)

	expectStateRead(cacher, scopeUpgradeState{
		AccountID:   uuid.New(),
		Provider:    "google",
		Platform:    "web",
		RedirectURI: testRedirectURI,
	})

	rr, req := newCallbackRequest(t, "spotify", "state=handle&code=abc")

	err := h.CompleteScopeUpgrade(rr, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
	assert.Contains(t, err.Error(), "does not match")
}

// Declining must return the user to the app cleanly and write nothing.
func TestCompleteScopeUpgrade_UserDeclined(t *testing.T) {
	h, cacher := scopeHandler(t)

	expectStateRead(cacher, scopeUpgradeState{
		AccountID:   uuid.New(),
		Provider:    "google",
		Platform:    "web",
		RedirectURI: testRedirectURI,
	})

	rr, req := newCallbackRequest(t, "google", "state=handle&error=access_denied")

	// A nil DB would panic if the handler touched the database.
	require.NoError(t, h.CompleteScopeUpgrade(rr, req))

	assert.Equal(t, 302, rr.Code)
	location, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "denied", location.Query().Get("scope_upgrade"))
	assert.Equal(t, "access_denied", location.Query().Get("reason"))
	assert.Equal(t, "google", location.Query().Get("provider"))
}

func TestCompleteScopeUpgrade_MissingCode(t *testing.T) {
	h, cacher := scopeHandler(t)

	expectStateRead(cacher, scopeUpgradeState{
		AccountID:   uuid.New(),
		Provider:    "google",
		Platform:    "web",
		RedirectURI: testRedirectURI,
	})

	rr, req := newCallbackRequest(t, "google", "state=handle")

	err := h.CompleteScopeUpgrade(rr, req)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
}

// A tampered Redis value must not turn the callback into an open redirect.
func TestRedirectBack_RejectsUnlistedTarget(t *testing.T) {
	h, _ := scopeHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/google/callback", nil)

	h.redirectBack(rr, req, &scopeUpgradeState{
		Provider:    "google",
		Platform:    "web",
		RedirectURI: "https://evil.example.com/steal",
	}, "success", "", nil)

	assert.NotEqual(t, 302, rr.Code, "must not redirect to an unlisted target")
	assert.Empty(t, rr.Header().Get("Location"))
}

func TestRedirectBack_MobileUsesDeepLink(t *testing.T) {
	h, _ := scopeHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/google/callback", nil)

	h.redirectBack(rr, req, &scopeUpgradeState{
		Provider: "google",
		Platform: "mobile",
		DeepLink: testDeepLink,
	}, "success", "", []providers.Capability{providers.CapabilityCalendar, providers.CapabilityTasks})

	require.Equal(t, 302, rr.Code)
	location, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "myapp", location.Scheme)
	assert.Equal(t, "success", location.Query().Get("scope_upgrade"))
	assert.Equal(t, "calendar,tasks", location.Query().Get("granted"))
}

func TestIsReturnTargetAllowed(t *testing.T) {
	allowed := []string{testRedirectURI, testDeepLink}

	assert.True(t, isReturnTargetAllowed(testRedirectURI, allowed))
	assert.True(t, isReturnTargetAllowed(strings.ToUpper(testDeepLink), allowed))
	assert.False(t, isReturnTargetAllowed("https://evil.example.com", allowed))
	assert.False(t, isReturnTargetAllowed("", allowed))
	// Exact match only, so a prefix must not pass.
	assert.False(t, isReturnTargetAllowed(testRedirectURI+"/extra", allowed))
}

func TestCallbackURL(t *testing.T) {
	h, _ := scopeHandler(t)
	assert.Equal(
		t,
		"https://verisafe.example.com/oauth/google/callback",
		h.callbackURL("google"),
	)

	h.Cfg.AuthenticationConfig.AuthAddress = "https://verisafe.example.com/"
	assert.Equal(
		t,
		"https://verisafe.example.com/oauth/google/callback",
		h.callbackURL("google"),
		"a trailing slash in AUTH_ADDRESS must not double up",
	)
}

func TestSubjectFromIDToken(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "109348572093485720394",
		"iss": "https://accounts.google.com",
	})
	signed, err := token.SignedString([]byte("irrelevant-secret"))
	require.NoError(t, err)

	assert.Equal(t, "109348572093485720394", subjectFromIDToken(signed))
	assert.Empty(t, subjectFromIDToken(""))
	assert.Empty(t, subjectFromIDToken("not-a-jwt"))
}

// expectStateRead scripts a successful single-use state read.
func expectStateRead(cacher *mockscore.MockCacher, state scopeUpgradeState) {
	cacher.EXPECT().
		Get(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, dest any) error {
			raw, _ := json.Marshal(state)
			return json.Unmarshal(raw, dest)
		})
	cacher.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil)
}
