package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// ParseTrustedProxies accepts the IPs and CIDRs of proxies that append X-Forwarded-For.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var trusted []netip.Prefix
	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			ip, err := netip.ParseAddr(item)
			if err != nil || ip.Zone() != "" {
				return nil, fmt.Errorf("DOCKET_TRUSTED_PROXIES: invalid IP or CIDR %q", item)
			}
			ip = ip.Unmap()
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		trusted = append(trusted, prefix.Masked())
	}
	return trusted, nil
}
