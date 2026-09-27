package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestValidateServiceTokenIPWhitelistParsesAndUnmapsEntries(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::ffff:192.0.2.10]:1234"
	token := repository.ServiceToken{
		IpWhitelist: []string{
			"not-an-ip",
			"::ffff:192.0.2.10",
		},
	}

	require.NoError(t, validateServiceToken(token, req, nil))
}

func TestValidateServiceTokenIPWhitelistIgnoresInvalidEntries(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	token := repository.ServiceToken{
		IpWhitelist: []string{"not-an-ip"},
	}

	require.ErrorContains(
		t,
		validateServiceToken(token, req, nil),
		"access denied from IP address",
	)
}
