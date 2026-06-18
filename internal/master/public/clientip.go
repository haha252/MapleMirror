package public

import (
	"net/http"

	"mirror-server/internal/clientip"
)

func (s Server) clientPrefix(r *http.Request) string {
	return clientPrefixFromRequest(r, s.TrustedCIDRs)
}

func (s Server) clientIP(r *http.Request) string {
	return clientip.Address(r, s.TrustedCIDRs)
}

func clientPrefixFromRequest(r *http.Request, trusted []string) string {
	return clientip.Prefix(r, trusted)
}
