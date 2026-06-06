package downloadurl

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var ErrInvalidBaseURL = errors.New("下载节点公网基址无效")

func NormalizeBase(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && !(scheme == "http" && loopbackHost(parsed.Hostname())) {
		return "", false
	}
	return (&url.URL{Scheme: scheme, Host: strings.ToLower(parsed.Host)}).String(), true
}

func Join(baseURL, escapedPath string) (string, error) {
	base, ok := NormalizeBase(baseURL)
	if !ok {
		return "", ErrInvalidBaseURL
	}
	if !strings.HasPrefix(escapedPath, "/") ||
		strings.ContainsAny(escapedPath, "?#") {
		return "", ErrInvalidBaseURL
	}
	return base + escapedPath, nil
}

func loopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
