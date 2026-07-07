package public

import (
	"net/netip"
	"strings"
)

func parseBlockPrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
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

func matchBlocklist(entries []blocklistEntry, addr netip.Addr) (blocklistEntry, bool) {
	var matched blocklistEntry
	matchedOK := false
	for _, entry := range entries {
		if entry.prefix.Contains(addr) && (!matchedOK || entry.prefix.Bits() > matched.prefix.Bits()) {
			matched, matchedOK = entry, true
		}
	}
	return matched, matchedOK
}

func containsPrefix(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
