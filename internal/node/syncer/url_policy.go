package syncer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var sourceLookupIPAddr = net.DefaultResolver.LookupIPAddr

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
	return privateSourceIP(ip)
}

func privateSourceIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func NewSourceHTTPClient(base *http.Client, allowPrivate bool, responseHeaderTimeout time.Duration) *http.Client {
	client := sourceHTTPClient(base, allowPrivate)
	if transport, ok := client.Transport.(*http.Transport); ok {
		clone := transport.Clone()
		clone.ResponseHeaderTimeout = responseHeaderTimeout
		client.Transport = clone
	}
	return client
}

func (e Executor) effectiveSourceClient() *http.Client {
	if e.SourceClient != nil {
		return e.SourceClient
	}
	return NewSourceHTTPClient(e.Client, e.AllowPrivateSourceURLs, swarmHeaderTimeout)
}

func sourceHTTPClient(base *http.Client, allowPrivate bool) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	copy := *base
	previous := base.CheckRedirect
	if !allowPrivate {
		copy.Transport = secureSourceTransport(base.Transport)
	}
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

func secureSourceTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	transport, ok := base.(*http.Transport)
	if !ok {
		return base
	}
	copy := transport.Clone()
	copy.DialContext = secureSourceDialContext
	copy.DialTLSContext = nil
	return copy
}

func secureSourceDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	resolved, err := sourceLookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("源站地址 %s 无法解析", host)
	}
	for _, addr := range resolved {
		if privateSourceIP(addr.IP) {
			return nil, errors.New("源站地址不得解析到本机或内网地址")
		}
	}
	dialer := net.Dialer{}
	var lastErr error
	for _, addr := range resolved {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
