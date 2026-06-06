package adminui

import (
	"crypto/x509"
	"net"
	"net/http"
)

func (s *Server) requireHighRisk(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.highRiskSessionAllowed {
		username, _ := r.Context().Value(usernameKey{}).(string)
		if username == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "该操作需要管理面板登录会话"})
			return "", false
		}
		return username, true
	}
	if loopbackIP(s.clientIP(r)) {
		return "local-admin", true
	}
	identity, ok := clientIdentity(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "该操作需要管理员 mTLS"})
		return "", false
	}
	return identity, true
}

func loopbackIP(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && ip.IsLoopback()
}

func clientIdentity(r *http.Request) (string, bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", false
	}
	cert := r.TLS.PeerCertificates[0]
	if !hasClientAuth(r) {
		return "", false
	}
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName, true
	}
	return cert.SerialNumber.String(), true
}

func hasClientAuth(r *http.Request) bool {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return false
	}
	for _, usage := range r.TLS.PeerCertificates[0].ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			return true
		}
	}
	return false
}
