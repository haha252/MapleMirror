package admin

import (
	"crypto/subtle"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"mirror-server/internal/config"
)

type Auth struct {
	networks      []*net.IPNet
	token         string
	tokenMinBytes int
}

func NewAuth(cfg config.Administration) (Auth, error) {
	token := os.Getenv(cfg.TokenEnv)
	if token == "" && cfg.TokenFile != "" {
		data, err := os.ReadFile(cfg.TokenFile)
		if err != nil {
			return Auth{}, fmt.Errorf("读取管理令牌文件失败：%w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	if len(token) < cfg.TokenMinBytes {
		return Auth{}, fmt.Errorf("管理令牌缺失或长度不足")
	}
	var networks []*net.IPNet
	for _, value := range cfg.AllowedCIDRs {
		_, cidr, err := net.ParseCIDR(value)
		if err != nil {
			return Auth{}, err
		}
		networks = append(networks, cidr)
	}
	return Auth{networks: networks, token: token, tokenMinBytes: cfg.TokenMinBytes}, nil
}

func (a Auth) Check(r *http.Request, highRisk bool) (string, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !a.allowed(net.ParseIP(host)) {
		return "", false
	}
	got := bearerToken(r.Header.Get("Authorization"))
	if subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
		return "", false
	}
	if highRisk {
		identity, ok := adminIdentity(r)
		return identity, ok
	}
	return "", true
}

func (a Auth) allowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, cidr := range a.networks {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func bearerToken(value string) string {
	const prefix = "Bearer "
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return ""
	}
	return value[len(prefix):]
}

func adminIdentity(r *http.Request) (string, bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", false
	}
	cert := r.TLS.PeerCertificates[0]
	if !hasClientAuth(cert) {
		return "", false
	}
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName, true
	}
	return cert.SerialNumber.String(), true
}

func hasClientAuth(cert *x509.Certificate) bool {
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			return true
		}
	}
	return false
}
