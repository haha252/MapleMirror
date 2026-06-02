package public

import (
	"net/http"

	"mirror-server/internal/clientip"
)

func (s Server) clientPrefix(r *http.Request) string {
	return clientip.Prefix(r, s.TrustedCIDRs)
}

func (s Server) clientIP(r *http.Request) string {
	return clientip.Address(r, s.TrustedCIDRs)
}
