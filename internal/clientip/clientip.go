package clientip

import (
	"net"
	"net/http"
	"strings"
)

func Prefix(r *http.Request, trustedCIDRs []string) string {
	ip := Address(r, trustedCIDRs)
	if ip == "unknown" {
		return ip
	}
	if strings.Contains(ip, ":") {
		return ip + "/128"
	}
	return ip + "/32"
}

func Address(r *http.Request, trustedCIDRs []string) string {
	ip := remoteIP(r.RemoteAddr)
	if ip == nil {
		return "unknown"
	}
	if trusted(ip, trustedCIDRs) {
		if forwarded, present, valid := forwardedIP(r, ip, trustedCIDRs); present && !valid {
			return "unknown"
		} else if forwarded != nil {
			ip = forwarded
		}
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func remoteIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

func forwardedIP(r *http.Request, remote net.IP, trustedCIDRs []string) (net.IP, bool, bool) {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		chain := forwardedChain(xff)
		if len(chain) == 0 {
			return nil, true, false
		}
		current := remote
		for i := len(chain) - 1; i >= 0; i-- {
			if !trusted(current, trustedCIDRs) {
				return current, true, true
			}
			current = chain[i]
		}
		return current, true, true
	}
	realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if realIP == "" {
		return nil, false, true
	}
	parsed := net.ParseIP(realIP)
	return parsed, true, parsed != nil
}

func forwardedChain(value string) []net.IP {
	var out []net.IP
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		ip := net.ParseIP(raw)
		if ip == nil {
			return nil
		}
		out = append(out, ip)
	}
	return out
}

func trusted(ip net.IP, cidrs []string) bool {
	for _, raw := range cidrs {
		_, network, err := net.ParseCIDR(raw)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
