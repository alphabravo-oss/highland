package middleware

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP sets r.RemoteAddr to the real client IP, honoring forwarding headers
// ONLY when the direct connection comes from a configured trusted proxy CIDR.
//
// This replaces chi's RealIP, which trusts X-Forwarded-For / X-Real-IP /
// True-Client-IP from ANY peer and is therefore spoofable (chi deprecated it for
// exactly this reason). With no trusted proxies configured the socket peer is
// used unchanged, so an attacker cannot forge the login-limiter key or the audit
// source IP by sending a header.
func ClientIP(trustedCIDRs []string) func(http.Handler) http.Handler {
	trusted := parseCIDRs(trustedCIDRs)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(trusted) > 0 {
				if peer := hostIP(r.RemoteAddr); peer != nil && inCIDRs(peer, trusted) {
					if c := clientFromHeaders(r, trusted); c != "" {
						r.RemoteAddr = net.JoinHostPort(c, "0")
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientFromHeaders derives the client IP from a trusted peer's forwarding
// headers: the right-most X-Forwarded-For entry that is not itself a trusted
// proxy (proxies append the immediate peer, so walking right skips the proxy
// chain), then X-Real-IP / True-Client-IP as fallbacks.
func clientFromHeaders(r *http.Request, trusted []*net.IPNet) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				continue
			}
			if !inCIDRs(ip, trusted) {
				return ip.String()
			}
		}
		// Every hop was trusted → the left-most is the originating client.
		if ip := net.ParseIP(strings.TrimSpace(parts[0])); ip != nil {
			return ip.String()
		}
	}
	for _, h := range []string{"True-Client-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			if ip := net.ParseIP(v); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// Accept a bare IP as a /32 or /128.
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil {
				if ip.To4() != nil {
					c += "/32"
				} else {
					c += "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func inCIDRs(ip net.IP, cidrs []*net.IPNet) bool {
	for _, n := range cidrs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func hostIP(remoteAddr string) net.IP {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return net.ParseIP(host)
}
