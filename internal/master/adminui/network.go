package adminui

import (
	"net"
	"net/http"

	"mirror-server/internal/clientip"
)

func (s *Server) networkGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedIP(s.clientIP(r)) {
			http.Error(w, "管理来源暂不可用", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowedIP(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	for _, network := range s.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) clientIP(r *http.Request) string {
	return clientip.Address(r, s.trustedCIDRs)
}

func parseNetworks(values []string) ([]*net.IPNet, error) {
	var networks []*net.IPNet
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		networks = append(networks, network)
	}
	return networks, nil
}
