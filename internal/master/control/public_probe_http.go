package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

var publicProbeLookupIPAddr = net.DefaultResolver.LookupIPAddr

func publicProbeHTTPClient(base *http.Client, timeout time.Duration) *http.Client {
	if base == nil {
		base = &http.Client{Timeout: timeout}
	}
	copy := *base
	copy.Transport = safePublicProbeTransport(base.Transport)
	return &copy
}

func safePublicProbeTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	transport, ok := base.(*http.Transport)
	if !ok {
		return base
	}
	copy := transport.Clone()
	copy.DialContext = safePublicProbeDialContext
	copy.DialTLSContext = nil
	return copy
}

func safePublicProbeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	resolved, err := publicProbeLookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("公网探测地址 %s 无法解析", host)
	}
	for _, addr := range resolved {
		if privatePublicProbeIP(addr.IP) {
			return nil, errors.New("公网探测地址不得解析到本机或内网地址")
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

func privatePublicProbeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
