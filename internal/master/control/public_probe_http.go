package control

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

var publicProbeLookupIPAddr = net.DefaultResolver.LookupIPAddr
var publicProbeDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

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
	var lastErr error
	for _, addr := range resolved {
		conn, err := publicProbeDialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
