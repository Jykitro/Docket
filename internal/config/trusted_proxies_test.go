package config

import (
	"net/netip"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	ps, err := ParseTrustedProxies("127.0.0.1, ::ffff:10.42.0.0/112, 2001:db8::/48")
	if err != nil || len(ps) != 3 {
		t.Fatalf("prefixes: %v", err)
	}
	for i, ip := range []string{"127.0.0.1", "10.42.3.2", "2001:db8::4"} {
		if !ps[i].Contains(netip.MustParseAddr(ip)) {
			t.Fatalf("missing %s", ip)
		}
	}
	for _, bad := range []string{"proxy.local", "10.0.0.1,", "::1%lo0", "10.0.0.0/33"} {
		if _, err = ParseTrustedProxies(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if ps, err = ParseTrustedProxies(""); err != nil || len(ps) != 0 {
		t.Fatal("empty config should trust no proxies")
	}
}
