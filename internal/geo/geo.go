// Package geo provides IP-based geolocation using MaxMind GeoLite2 databases.
// It resolves IP addresses to country, city, and network carrier information.
//
// Two database files are required:
//   - GeoLite2-City.mmdb    — country, city, timezone, coordinates
//   - GeoLite2-ASN.mmdb     — network carrier / autonomous system
//
// Usage:
//
//	locator, err := geo.NewGeoIPLocater(
//	    "path/to/GeoLite2-City.mmdb",
//	    "path/to/GeoLite2-ASN.mmdb",
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer locator.Close()
//
//	info, err := locator.Lookup(ip)
//	fmt.Println(info.Country.ISOCode)   // "KE"
//	fmt.Println(info.City.Name)         // "Nairobi"
//	fmt.Println(info.Network.carrier)   // "Safaricom"
package geo

import (
	"errors"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

// CountryInfo holds country-level details for a given IP address.
type CountryInfo struct {
	// ISOCode is the two-letter ISO 3166-1 alpha-2 code e.g. "KE", "US", "GB".
	ISOCode string

	// Name is the full English country name e.g. "Kenya".
	Name string

	// ContinentCode is the two-letter continent code e.g. "AF", "EU".
	ContinentCode string

	// ContinentName is the full English continent name e.g. "Africa".
	ContinentName string

	// IsInEuropeanUnion indicates whether the country is an EU member state.
	IsInEuropeanUnion bool
}

// CityInfo holds city-level details for a given IP address.
type CityInfo struct {
	// Name is the English city name e.g. "Nairobi".
	Name string

	// Subdivision is the state, province, or region e.g. "Nairobi County".
	Subdivision string

	// PostalCode is the postal code where available.
	PostalCode string

	// Latitude and Longitude are approximate coordinates for the IP.
	Latitude  float64
	Longitude float64

	// TimeZone is the IANA time zone string e.g. "Africa/Nairobi".
	TimeZone string
}

// NetworkInfo holds carrier/ISP details for a given IP address.
type NetworkInfo struct {
	// ASN is the Autonomous System Number e.g. 12345.
	ASN uint

	// Organization is the network operator or ISP name e.g. "Safaricom Limited".
	Organization string
}

// LocationInfo is the full resolved result for an IP address,
// combining country, city, and network carrier information.
type LocationInfo struct {
	Country CountryInfo
	City    CityInfo
	Network NetworkInfo
}

// IPLocater resolves an IP address to full location information.
// Satisfied by GeoIPLocater in production and can be mocked in tests.
type IPLocater interface {
	Lookup(ip netip.Addr) (*LocationInfo, error)
}

// GeoIPLocater resolves IP addresses using local MaxMind GeoLite2 database
// files. It is safe for concurrent use.
type GeoIPLocater struct {
	cityDb *geoip2.Reader
	asnDb  *geoip2.Reader
}

// NewGeoIPLocater opens the GeoLite2-City and GeoLite2-ASN databases at the
// given paths. The caller must call Close when done to release file handles.
//
// Returns an error if either file is missing, unreadable, or not a valid
// MaxMind database.
func NewGeoIPLocater(cityDbPath, asnDbPath string) (*GeoIPLocater, error) {
	cityDb, err := geoip2.Open(cityDbPath)
	if err != nil {
		return nil, err
	}

	asnDb, err := geoip2.Open(asnDbPath)
	if err != nil {
		cityDb.Close() // clean up the already-opened db
		return nil, err
	}

	return &GeoIPLocater{cityDb: cityDb, asnDb: asnDb}, nil
}

// Lookup resolves the given IP address to a LocationInfo containing country,
// city, and network carrier details.
//
// Returns ErrLocaterNotInitialized if either database was not opened.
// Returns an error if the IP cannot be resolved in either database.
func (gil *GeoIPLocater) Lookup(ip netip.Addr) (*LocationInfo, error) {
	if gil.cityDb == nil || gil.asnDb == nil {
		return nil, ErrLocaterNotInitialized
	}

	cityRecord, err := gil.cityDb.City(ip)
	if err != nil {
		return nil, err
	}

	asnRecord, err := gil.asnDb.ASN(ip)
	if err != nil {
		return nil, err
	}

	subdivision := ""
	if len(cityRecord.Subdivisions) > 0 {
		subdivision = cityRecord.Subdivisions[0].Names.English
	}

	return &LocationInfo{
		Country: CountryInfo{
			ISOCode:           cityRecord.Country.ISOCode,
			Name:              cityRecord.Country.Names.English,
			ContinentCode:     cityRecord.Continent.Code,
			ContinentName:     cityRecord.Continent.Names.English,
			IsInEuropeanUnion: cityRecord.Country.IsInEuropeanUnion,
		},
		City: CityInfo{
			Name:        cityRecord.City.Names.English,
			Subdivision: subdivision,
			PostalCode:  cityRecord.Postal.Code,
			Latitude:    *cityRecord.Location.Latitude,
			Longitude:   *cityRecord.Location.Longitude,
			TimeZone:    cityRecord.Location.TimeZone,
		},
		Network: NetworkInfo{
			ASN:          asnRecord.AutonomousSystemNumber,
			Organization: asnRecord.AutonomousSystemOrganization,
		},
	}, nil
}

// Close releases the underlying database file handles.
// Safe to call even if the databases were not successfully opened.
func (gil *GeoIPLocater) Close() {
	if gil.cityDb != nil {
		gil.cityDb.Close()
	}
	if gil.asnDb != nil {
		gil.asnDb.Close()
	}
}

// ErrLocaterNotInitialized is returned when Lookup is called on a
// GeoIPLocater whose databases have not been successfully opened.
var ErrLocaterNotInitialized = errors.New(
	"GeoIPLocater has not been initialized",
)
