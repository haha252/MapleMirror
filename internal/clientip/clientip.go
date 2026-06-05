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
		if forwarded := forwardedIP(r); forwarded != nil {
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

func forwardedIP(r *http.Request) net.IP {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, value := range strings.Split(xff, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			return net.ParseIP(value)
		}
		return nil
	}
	return net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP")))
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
