package adminui

import (
	"net/http"

	"mirror-server/internal/clientip"
)

func (s *Server) clientIP(r *http.Request) string {
	return clientip.Address(r, s.trustedCIDRs)
}
