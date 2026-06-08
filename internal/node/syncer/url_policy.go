package syncer

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func validateSourceURL(rawURL string, allowPrivate bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	return validateParsedSourceURL(parsed, allowPrivate)
}

func validateParsedSourceURL(parsed *url.URL, allowPrivate bool) error {
	if parsed == nil || parsed.Host == "" {
		return errors.New("源站地址不完整")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New("源站地址只允许 http 或 https")
	}
	if parsed.User != nil {
		return errors.New("源站地址不得包含用户信息")
	}
	if allowPrivate {
		return nil
	}
	if privateSourceHost(parsed.Hostname()) {
		return errors.New("源站地址不得指向本机或内网地址")
	}
	return nil
}

func privateSourceHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func sourceHTTPClient(base *http.Client, allowPrivate bool) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	copy := *base
	previous := base.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validateParsedSourceURL(req.URL, allowPrivate); err != nil {
			return err
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	return &copy
}
