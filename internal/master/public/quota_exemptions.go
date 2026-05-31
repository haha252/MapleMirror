package public

import (
	"errors"
	"net/netip"
	"strings"
)

func parseQuotaPrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, errors.New("empty prefix")
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, err
	}
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32), nil
	}
	return netip.PrefixFrom(addr, 128), nil
}

func quotaExemptions(values []string) []netip.Prefix {
	seen := make(map[netip.Prefix]struct{}, len(values)+2)
	out := make([]netip.Prefix, 0, len(values)+2)
	add := func(prefix netip.Prefix) {
		prefix = prefix.Masked()
		if _, ok := seen[prefix]; ok {
			return
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}
	for _, raw := range values {
		prefix, err := parseQuotaPrefix(raw)
		if err != nil {
			continue
		}
		add(prefix)
	}
	add(netip.PrefixFrom(netip.MustParseAddr("127.0.0.1"), 32))
	add(netip.PrefixFrom(netip.MustParseAddr("::1"), 128))
	return out
}

func (p quotaPolicy) exempt(prefix string) bool {
	if len(p.exemptions) == 0 {
		return false
	}
	scope, err := netip.ParsePrefix(strings.TrimSpace(prefix))
	if err != nil {
		return false
	}
	addr := scope.Addr()
	for _, exemption := range p.exemptions {
		if exemption.Contains(addr) {
			return true
		}
	}
	return false
}
