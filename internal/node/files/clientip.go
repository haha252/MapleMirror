package files

import (
	"net/http"

	"mirror-server/internal/clientip"
)

func (h *Handler) clientPrefix(r *http.Request) string {
	return clientip.Prefix(r, h.TrustedCIDRs)
}

func (h *Handler) clientIP(r *http.Request) string {
	return clientip.Address(r, h.TrustedCIDRs)
}
