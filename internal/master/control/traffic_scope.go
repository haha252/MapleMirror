package control

import (
	"net/netip"
	"strings"
)

type trafficScope struct {
	Kind string
	Key  string
}

func trafficScopes(prefix string) ([2]trafficScope, error) {
	host := strings.TrimSuffix(strings.TrimSuffix(prefix, "/32"), "/128")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return [2]trafficScope{}, err
	}
	if addr.Is4() {
		return [2]trafficScope{
			{"ipv4_32", addr.String() + "/32"},
			{"ipv4_24", netip.PrefixFrom(addr, 24).Masked().String()},
		}, nil
	}
	return [2]trafficScope{
		{"ipv6_128", addr.String() + "/128"},
		{"ipv6_64", netip.PrefixFrom(addr, 64).Masked().String()},
	}, nil
}
