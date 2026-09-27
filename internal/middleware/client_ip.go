package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP resolves the client address from the connection peer and, only
// when that peer is trusted, the X-Forwarded-For chain. Trusted proxy ranges
// must contain only reverse proxies controlled by this deployment.
func ClientIP(
	r *http.Request,
	trustedProxies []netip.Prefix,
) (netip.Addr, error) {
	peer, err := parseRequestIP(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse remote address: %w", err)
	}
	if !isTrustedProxy(peer, trustedProxies) {
		return peer, nil
	}

	var forwarded []string
	for _, value := range r.Header.Values("X-Forwarded-For") {
		forwarded = append(forwarded, strings.Split(value, ",")...)
	}
	if len(forwarded) == 0 {
		return peer, nil
	}

	// Walk from the app-facing proxy toward the original client. This skips
	// trusted proxies added to the right side of the forwarded chain and stops
	// at the first untrusted address.
	for i := len(forwarded) - 1; i >= 0; i-- {
		addr, err := parseRequestIP(forwarded[i])
		if err != nil {
			return netip.Addr{}, fmt.Errorf("parse X-Forwarded-For: %w", err)
		}
		if !isTrustedProxy(addr, trustedProxies) {
			return addr, nil
		}
	}

	// If every address belongs to a configured proxy range, use the oldest
	// address in the chain. Deployments should keep those ranges limited to
	// proxy addresses, not general-purpose private networks.
	return parseRequestIP(forwarded[0])
}

func parseRequestIP(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.Unmap(), nil
	}

	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid IP address %q", value)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid IP address %q", value)
	}
	return addr.Unmap(), nil
}

func isTrustedProxy(addr netip.Addr, proxies []netip.Prefix) bool {
	for _, proxy := range proxies {
		if proxy.Contains(addr) {
			return true
		}
	}
	return false
}
