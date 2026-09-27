package config

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTrustedProxyCIDRsNormalizesIPv4MappedPrefixes(t *testing.T) {
	prefixes, err := parseTrustedProxyCIDRs([]string{
		"::ffff:192.0.2.129/120",
	})

	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
	}, prefixes)
}

func TestParseTrustedProxyCIDRsRejectsMappedPrefixesShorterThan96(t *testing.T) {
	_, err := parseTrustedProxyCIDRs([]string{"::ffff:192.0.2.1/95"})

	require.ErrorContains(t, err, "shorter than /96")
}
