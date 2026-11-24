package selfhost

import (
	"net/http"
	"net/netip"
	"strings"
)

// Trust each hop only when its immediate downstream peer is a configured proxy.
func (h *handler) clientIP(r *http.Request) string {
	if h.VercelIngress {
		if ip, err := netip.ParseAddr(r.Header.Get("X-Vercel-Forwarded-For")); err == nil && ip.Zone() == "" {
			return ip.Unmap().String()
		}
	}
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	ip := peer.Addr()
	if err != nil {
		ip, err = netip.ParseAddr(r.RemoteAddr)
		if err != nil {
			return "unknown"
		}
	}
	ip = ip.Unmap()
	fallback := ip.String()
	lines := r.Header.Values("X-Forwarded-For")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		for {
			if !h.trustedProxy(ip) {
				return ip.String()
			}
			comma := strings.LastIndexByte(line, ',')
			ip, err = netip.ParseAddr(strings.TrimSpace(line[comma+1:]))
			if err != nil || ip.Zone() != "" {
				return fallback
			}
			ip = ip.Unmap()
			if comma < 0 {
				break
			}
			line = line[:comma]
		}
	}
	return ip.String()
}

func (h *handler) trustedProxy(ip netip.Addr) bool {
	for _, network := range h.TrustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
