package access

import (
	"net"
	"net/http"
	"strings"
)

// trustedProxies is the set of peers allowed to assert a client address.
type trustedProxies struct {
	networks []*net.IPNet
}

func parseTrustedProxies(cidrs []string) *trustedProxies {
	t := &trustedProxies{}
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(cidr); err == nil {
			t.networks = append(t.networks, network)
			continue
		}
		if ip := net.ParseIP(cidr); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			t.networks = append(t.networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return t
}

// contains reports whether the peer address is a trusted proxy.
func (t *trustedProxies) contains(ip net.IP) bool {
	if t == nil || ip == nil {
		return false
	}
	for _, network := range t.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP resolves the client address.
//
// Forwarding headers are honoured only when the immediate peer is a trusted
// proxy. Otherwise the peer address is the answer: trusting X-Forwarded-For from
// an arbitrary client lets it choose whatever address it likes in the logs.
func clientIP(r *http.Request, trusted *trustedProxies) string {
	peer := peerIP(r.RemoteAddr)

	if trusted.contains(peer) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// The leftmost entry is the original client; skip trusted hops.
			for _, candidate := range strings.Split(xff, ",") {
				ip := net.ParseIP(strings.TrimSpace(candidate))
				if ip == nil || trusted.contains(ip) {
					continue
				}
				return ip.String()
			}
		}
		if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			return ip.String()
		}
	}

	if peer != nil {
		return peer.String()
	}
	return r.RemoteAddr
}

func peerIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return net.ParseIP(remoteAddr)
	}
	return net.ParseIP(host)
}
