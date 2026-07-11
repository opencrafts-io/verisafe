package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/google"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestIsRedirectAllowed(t *testing.T) {
	allowed := []string{"https://app.example.com/callback", "myapp://auth"}

	cases := []struct {
		name string
		uri  string
		want bool
	}{
		{"exact match", "https://app.example.com/callback", true},
		{"case-insensitive match", "HTTPS://APP.EXAMPLE.COM/CALLBACK", true},
		{"not in allowlist", "https://evil.example.com", false},
		{"prefix of an allowed URI is not enough", "https://app.example.com", false},
		{"empty allowlist entry never matches empty input", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isRedirectAllowed(tc.uri, allowed))
		})
	}
}

func TestEncodeDecodeState_RoundTrip(t *testing.T) {
	original := StateData{
		Platform:    authPlatformWebValue,
		RedirectURI: "https://app.example.com/callback",
		DeepLink:    "myapp://auth/callback",
		DeviceName:  "iPhone 15",
		DeviceToken: "tok_abc123",
	}

	encoded := encodeState(original)

	req := httptest.NewRequest("GET", "/auth/google/callback?state="+encoded, nil)
	decoded, err := decodeState(req)

	require.NoError(t, err)
	assert.Equal(t, original, *decoded)
}

func TestDecodeState_KnownLimitations(t *testing.T) {
	t.Run("missing state parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/auth/google/callback", nil)
		_, err := decodeState(req)
		assert.ErrorContains(t, err, "missing state parameter")
	})

	t.Run("invalid base64 encoding", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/auth/google/callback?state=not-valid-base64!!!", nil)
		_, err := decodeState(req)
		assert.ErrorContains(t, err, "invalid state encoding")
	})

	t.Run("documents the pipe-delimiter escaping limitation", func(t *testing.T) {
		// A DeviceName containing a literal "|" is not escaped, so it
		// splits into an extra field and corrupts every field after it.
		// This is a known limitation (see docs/AUTHENTICATION.md), not
		// desired behavior — this test documents today's actual output
		// rather than silently fixing it.
		original := StateData{
			Platform:    authPlatformMobileValue,
			DeviceName:  "my|weird|device",
			DeviceToken: "tok_abc123",
		}
		encoded := encodeState(original)
		req := httptest.NewRequest("GET", "/auth/google/callback?state="+encoded, nil)

		decoded, err := decodeState(req)

		require.NoError(t, err)
		assert.NotEqual(t, original.DeviceName, decoded.DeviceName,
			"documents that embedded '|' corrupts decoding today")
	})
}

func generateTestApplePrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return string(pem.EncodeToMemory(block))
}

func TestGenerateAppleClientSecret(t *testing.T) {
	t.Run("valid ECDSA key succeeds", func(t *testing.T) {
		keyPEM := generateTestApplePrivateKeyPEM(t)

		secret, err := GenerateAppleClientSecret(
			"TEAM123", "KEY456", "com.example.app", keyPEM,
		)

		require.NoError(t, err)
		assert.NotEmpty(t, secret)
	})

	t.Run("malformed PEM fails", func(t *testing.T) {
		_, err := GenerateAppleClientSecret(
			"TEAM123", "KEY456", "com.example.app", "not a pem block",
		)
		assert.ErrorContains(t, err, "failed to decode PEM block")
	})

	t.Run("non-ECDSA key fails", func(t *testing.T) {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
		require.NoError(t, err)
		keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

		_, err = GenerateAppleClientSecret(
			"TEAM123", "KEY456", "com.example.app", keyPEM,
		)
		assert.ErrorContains(t, err, "not an ECDSA key")
	})
}

// setupLoginHandlerTest registers a dummy Google provider and session store
// so LoginHandler's happy path can reach gothic.GetAuthURL without any real
// network access — goth's "begin auth" step builds the authorization URL
// locally and never makes an HTTP call.
func setupLoginHandlerTest(t *testing.T, allowedRedirects []string) *AuthHandler {
	t.Helper()
	gothic.Store = sessions.NewCookieStore([]byte("test-session-secret"))
	goth.UseProviders(google.New("dummy-id", "dummy-secret", "http://localhost/auth/google/callback", "email"))

	cfg := &config.Config{}
	cfg.JWTConfig.AllowedRedirectURIs = allowedRedirects

	return &AuthHandler{
		auth:   &Auth{config: cfg, logger: testLogger()},
		logger: testLogger(),
	}
}

func TestLoginHandler(t *testing.T) {
	t.Run("missing provider", func(t *testing.T) {
		h := setupLoginHandlerTest(t, nil)
		req := httptest.NewRequest("GET", "/auth/", nil)
		rr := httptest.NewRecorder()

		h.LoginHandler(rr, req)

		assert.Equal(t, 400, rr.Code)
		assert.JSONEq(t, `{"error":"provider name not found in request path"}`, rr.Body.String())
	})

	t.Run("missing redirect_uri for web platform", func(t *testing.T) {
		h := setupLoginHandlerTest(t, nil)
		req := httptest.NewRequest("GET", "/auth/google?platform=web", nil)
		req.SetPathValue("provider", "google")
		rr := httptest.NewRecorder()

		h.LoginHandler(rr, req)

		assert.Equal(t, 400, rr.Code)
		assert.JSONEq(t, `{"error":"missing redirect_uri for web platform"}`, rr.Body.String())
	})

	t.Run("disallowed redirect_uri", func(t *testing.T) {
		h := setupLoginHandlerTest(t, []string{"https://app.example.com/callback"})
		req := httptest.NewRequest(
			"GET", "/auth/google?platform=web&redirect_uri=https://evil.example.com", nil,
		)
		req.SetPathValue("provider", "google")
		rr := httptest.NewRecorder()

		h.LoginHandler(rr, req)

		assert.Equal(t, 400, rr.Code)
		assert.JSONEq(t, `{"error":"redirect_uri not allowed"}`, rr.Body.String())
	})

	t.Run("valid mobile login redirects to provider", func(t *testing.T) {
		h := setupLoginHandlerTest(t, nil)
		req := httptest.NewRequest("GET", "/auth/google", nil)
		req.SetPathValue("provider", "google")
		rr := httptest.NewRecorder()

		h.LoginHandler(rr, req)

		assert.Equal(t, 302, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "accounts.google.com")
	})

	t.Run("valid web login with allowed redirect_uri redirects to provider", func(t *testing.T) {
		h := setupLoginHandlerTest(t, []string{"https://app.example.com/callback"})
		req := httptest.NewRequest(
			"GET", "/auth/google?platform=web&redirect_uri=https://app.example.com/callback", nil,
		)
		req.SetPathValue("provider", "google")
		rr := httptest.NewRecorder()

		h.LoginHandler(rr, req)

		assert.Equal(t, 302, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "accounts.google.com")
	})
}
