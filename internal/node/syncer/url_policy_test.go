package syncer

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchWithTokenRejectsLoopbackBeforeRequest(t *testing.T) {
	_, _, err := (Executor{}).fetchWithToken(context.Background(),
		"http://127.0.0.1:8080/asset.zip", filepath.Join(t.TempDir(), "asset.tmp"), "", 6)
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected private source rejection, got %v", err)
	}
}

func TestFetchWithTokenRejectsDNSPrivateAddressBeforeRequest(t *testing.T) {
	restore := stubSourceLookup(t, net.ParseIP("127.0.0.1"))
	defer restore()
	_, _, err := (Executor{Client: &http.Client{Transport: &http.Transport{}}}).fetchWithToken(context.Background(),
		"http://public.example.test:8080/asset.zip", filepath.Join(t.TempDir(), "asset.tmp"), "", 6)
	if err == nil || !strings.Contains(err.Error(), "鍐呯綉") {
		t.Fatalf("expected DNS private source rejection, got %v", err)
	}
}

func TestSecureSourceDialRejectsPrivateResolvedRanges(t *testing.T) {
	for _, rawIP := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "0.0.0.0"} {
		t.Run(rawIP, func(t *testing.T) {
			restore := stubSourceLookup(t, net.ParseIP(rawIP))
			defer restore()
			_, err := secureSourceDialContext(context.Background(), "tcp", "public.example.test:80")
			if err == nil || !strings.Contains(err.Error(), "鍐呯綉") {
				t.Fatalf("expected private resolved address rejection, got %v", err)
			}
		})
	}
}

func TestFetchWithTokenRejectsRedirectToPrivateHost(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": {"http://127.0.0.1/secret"}},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}
	_, _, err := (Executor{Client: client}).fetchWithToken(context.Background(),
		"https://example.com/asset.zip", filepath.Join(t.TempDir(), "asset.tmp"), "", 6)
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected redirect target rejection, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("redirect target should be rejected before second request, requests=%d", requests)
	}
}

func TestSourceProbeRejectsLoopbackBeforeRequest(t *testing.T) {
	hits := 0
	probe := NewSourceProbe(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits++
		return nil, errors.New("unexpected request")
	})})
	err := probe.Check(context.Background(), "http://127.0.0.1/asset.zip")
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected private source rejection, got %v", err)
	}
	if hits != 0 {
		t.Fatalf("unsafe probe should not send HEAD, hits=%d", hits)
	}
}

func TestSourceProbeRejectsDNSPrivateAddressBeforeRequest(t *testing.T) {
	restore := stubSourceLookup(t, net.ParseIP("127.0.0.1"))
	defer restore()
	probe := NewSourceProbe(&http.Client{Transport: &http.Transport{}})
	err := probe.Check(context.Background(), "http://public.example.test:8080/asset.zip")
	if err == nil || !strings.Contains(err.Error(), "鍐呯綉") {
		t.Fatalf("expected DNS private probe rejection, got %v", err)
	}
}

func TestFetchFallbackRejectsUnsafePeerURL(t *testing.T) {
	task := fallbackTask("https://example.com/asset.zip", "http://127.0.0.1:8080/asset.zip", digest("abcdef"), 6)
	tmp := filepath.Join(t.TempDir(), "asset.tmp")
	_, _, err := (Executor{}).fetchFallback(context.Background(), task, tmp)
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected unsafe fallback rejection, got %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("unsafe fallback should not leave temp file, err=%v", err)
	}
}

func TestFetchFallbackRejectsDNSPrivatePeerURL(t *testing.T) {
	restore := stubSourceLookup(t, net.ParseIP("127.0.0.1"))
	defer restore()
	task := fallbackTask("https://example.com/asset.zip",
		"http://public.example.test:8080/internal/replication/asset-1", digest("abcdef"), 6)
	tmp := filepath.Join(t.TempDir(), "asset.tmp")
	_, _, err := (Executor{Client: &http.Client{Transport: &http.Transport{}}}).fetchFallback(context.Background(), task, tmp)
	if err == nil || !strings.Contains(err.Error(), "鍐呯綉") {
		t.Fatalf("expected DNS private fallback rejection, got %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("unsafe fallback should not leave temp file, err=%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubSourceLookup(t *testing.T, ip net.IP) func() {
	t.Helper()
	previous := sourceLookupIPAddr
	sourceLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: ip}}, nil
	}
	return func() { sourceLookupIPAddr = previous }
}

func publicHostURL(t *testing.T, serverURL, path string) string {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	return "http://public.example.test:" + port + path
}
