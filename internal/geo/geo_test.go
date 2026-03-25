package geo_test

import (
	"net/netip"
	"testing"

	"github.com/opencrafts-io/verisafe/internal/geo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeoIP_Lookup(t *testing.T) {
	locator, err := geo.NewGeoIPLocater(
		"../../database/mmdb/GeoLite2-City.mmdb",
		"../../database/mmdb/GeoLite2-ASN.mmdb",
	)
	require.NoError(t, err)
	defer locator.Close()

	ip := netip.MustParseAddr("81.2.69.142")

	got, err := locator.Lookup(ip)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "GB", got.Country.ISOCode)
	assert.Equal(t, "United Kingdom", got.Country.Name)
	assert.Equal(t, "Kettering", got.City.Name)
	assert.Equal(t, "Europe/London", got.City.TimeZone)
	assert.NotEmpty(t, got.Network.Organization)
}
