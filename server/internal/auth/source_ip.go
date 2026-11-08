package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func trustedIP(ip netip.Addr, networks []netip.Prefix) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Trust only the suffix added by configured proxy hops, never the leftmost
// user-supplied XFF entry. Malformed chains fall back to the TCP peer.
func SourceIP(r *http.Request, networks []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	if !trustedIP(peer, networks) {
		return peer.String()
	}
	if headers, present := r.Header["X-Forwarded-For"]; present {
		raw := strings.Join(headers, ",")
		parts := strings.Split(raw, ",")
		if len(raw) > 4096 || len(parts) > 64 {
			return peer.String()
		}
		chain := make([]netip.Addr, len(parts))
		for i, part := range parts {
			ip, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil || ip.Zone() != "" {
				return peer.String()
			}
			chain[i] = ip.Unmap()
		}
		for i := len(chain) - 1; i >= 0; i-- {
			if !trustedIP(chain[i], networks) || i == 0 {
				return chain[i].String()
			}
		}
		return peer.String()
	}
	if headers := r.Header.Values("X-Real-IP"); len(headers) == 1 {
		ip, err := netip.ParseAddr(strings.TrimSpace(headers[0]))
		if err == nil && ip.Zone() == "" {
			return ip.Unmap().String()
		}
	}
	return peer.String()
}
