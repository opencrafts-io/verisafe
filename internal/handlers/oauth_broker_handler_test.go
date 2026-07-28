package handlers_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/handlers"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func brokerHandler() *handlers.OAuthBrokerHandler {
	cfg := &config.Config{}
	cfg.AuthenticationConfig.AuthAddress = "https://verisafe.example.com"

	return &handlers.OAuthBrokerHandler{
		Cfg:      cfg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Registry: providers.NewRegistry(cfg),
	}
}

// newBrokerRequest builds a request in the state IsAuthenticated would have
// left it, so the handler's own branches can be exercised without a database.
func newBrokerRequest(
	t *testing.T,
	provider, body string,
	asService bool,
) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	req := httptest.NewRequest("POST", "/oauth/"+provider+"/token", strings.NewReader(body))
	req.SetPathValue("provider", provider)
	if asService {
		req = req.WithContext(
			middleware.WithServiceToken(req.Context(), true),
		)
	}
	return httptest.NewRecorder(), req
}

// The permission alone must not be enough. A human holding
// read:provider_token:any could otherwise read any user's Google token from
// their own logged-in session.
func TestIssueProviderToken_RejectsHumanJWTCaller(t *testing.T) {
	h := brokerHandler()
	rr, req := newBrokerRequest(t, "google", `{"account_id":"9f1c8b2e-0000-4000-8000-000000000001","capabilities":["calendar"]}`, false)

	err := h.IssueProviderToken(rr, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrForbidden)
	assert.Contains(t, err.Error(), "service token")
}

func TestIssueProviderToken_InputValidation(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr error
		wantSub string
	}{
		{
			name:    "malformed json",
			body:    `{"account_id":`,
			wantErr: core.ErrInvalidInput,
			wantSub: "malformed",
		},
		{
			name:    "account_id not a uuid",
			body:    `{"account_id":"not-a-uuid","capabilities":["calendar"]}`,
			wantErr: core.ErrInvalidInput,
			wantSub: "UUID",
		},
		{
			name:    "no capabilities",
			body:    `{"account_id":"9f1c8b2e-0000-4000-8000-000000000001","capabilities":[]}`,
			wantErr: core.ErrInvalidInput,
			wantSub: "at least one capability",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := brokerHandler()
			rr, req := newBrokerRequest(t, "google", tc.body, true)

			err := h.IssueProviderToken(rr, req)

			require.Error(t, err)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

func TestListAccountGrants_RejectsHumanJWTCaller(t *testing.T) {
	h := brokerHandler()
	req := httptest.NewRequest("GET", "/oauth/grants?account_id=9f1c8b2e-0000-4000-8000-000000000001", nil)
	rr := httptest.NewRecorder()

	err := h.ListAccountGrants(rr, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrForbidden)
}

func TestListAccountGrants_BadAccountID(t *testing.T) {
	h := brokerHandler()
	req := httptest.NewRequest("GET", "/oauth/grants?account_id=nope", nil)
	req = req.WithContext(
		middleware.WithServiceToken(req.Context(), true),
	)
	rr := httptest.NewRecorder()

	err := h.ListAccountGrants(rr, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
}

func TestReconcileGrant_InputValidation(t *testing.T) {
	h := brokerHandler()
	req := httptest.NewRequest("POST", "/oauth/google/reconcile", strings.NewReader(`{"account_id":"bad"}`))
	req.SetPathValue("provider", "google")
	rr := httptest.NewRecorder()

	err := h.ReconcileGrant(rr, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrInvalidInput)
}

// The insufficient_scope body is a contract: downstream services parse these
// exact keys to decide whether and how to prompt a user to re-authorize.
// Renaming one silently breaks every consumer, so pin the shape literally.
func TestInsufficientScopeResponse_GoldenShape(t *testing.T) {
	body := handlers.InsufficientScopeResponse{
		Error:               "insufficient_scope",
		Provider:            "google",
		AccountID:           "9f1c8b2e-0000-4000-8000-000000000001",
		MissingScopes:       []string{"https://www.googleapis.com/auth/calendar"},
		MissingCapabilities: []string{"calendar"},
		GrantedCapabilities: []string{"identity"},
		AuthorizationURL:    "https://verisafe.example.com/oauth/google/authorize",
		AuthorizationMethod: "POST",
		AuthorizationBody:   map[string][]string{"capabilities": {"calendar"}},
	}

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"error": "insufficient_scope",
		"provider": "google",
		"account_id": "9f1c8b2e-0000-4000-8000-000000000001",
		"missing_scopes": ["https://www.googleapis.com/auth/calendar"],
		"missing_capabilities": ["calendar"],
		"granted_capabilities": ["identity"],
		"authorization_url": "https://verisafe.example.com/oauth/google/authorize",
		"authorization_method": "POST",
		"authorization_body": {"capabilities": ["calendar"]}
	}`, string(raw))
}

func TestReauthorizationRequiredResponse_GoldenShape(t *testing.T) {
	raw, err := json.Marshal(handlers.ReauthorizationRequiredResponse{
		Error:               "reauthorization_required",
		Reason:              "invalid_grant",
		Provider:            "google",
		AccountID:           "9f1c8b2e-0000-4000-8000-000000000001",
		AuthorizationURL:    "https://verisafe.example.com/oauth/google/authorize",
		AuthorizationMethod: "POST",
	})
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"error": "reauthorization_required",
		"reason": "invalid_grant",
		"provider": "google",
		"account_id": "9f1c8b2e-0000-4000-8000-000000000001",
		"authorization_url": "https://verisafe.example.com/oauth/google/authorize",
		"authorization_method": "POST"
	}`, string(raw))
}

// A provider token response must never carry a refresh token, whatever else
// changes about it.
func TestProviderTokenResponse_HasNoRefreshToken(t *testing.T) {
	raw, err := json.Marshal(handlers.ProviderTokenResponse{
		Provider:    "google",
		AccessToken: "ya29.token",
		TokenType:   "Bearer",
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	_, present := decoded["refresh_token"]
	assert.False(t, present, "refresh tokens must never leave Verisafe")
}
