package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/netip"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildSnapshotClassifiesIPv4IPv6AndUnknown(t *testing.T) {
	_, snap, err := buildSnapshot([]prefixRecord{
		{Prefix: "1.1.0.0/16", Region: RegionOutsideMainland},
		{Prefix: "1.1.1.0/24", Region: RegionMainlandChina},
		{Prefix: "2606:4700::/32", Region: RegionOutsideMainland},
		{Prefix: "10.0.0.0/8", Region: RegionOutsideMainland},
		{Prefix: "fc00::/7", Region: RegionOutsideMainland},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{}
	manager.current.Store(&snap)
	for _, test := range []struct {
		name   string
		addr   string
		region Region
	}{
		{name: "longest IPv4 match", addr: "1.1.1.1", region: RegionMainlandChina},
		{name: "shorter IPv4 match", addr: "1.1.9.9", region: RegionOutsideMainland},
		{name: "IPv6 match", addr: "2606:4700::1", region: RegionOutsideMainland},
		{name: "unmatched public IP", addr: "9.9.9.9", region: RegionUnknown},
		{name: "private IP", addr: "10.0.0.1", region: RegionUnknown},
		{name: "private IPv6", addr: "fd00::1", region: RegionUnknown},
		{name: "IPv4 mapped IPv6", addr: "::ffff:1.1.9.9", region: RegionOutsideMainland},
	} {
		t.Run(test.name, func(t *testing.T) {
			addr := netip.MustParseAddr(test.addr)
			if got := manager.Classify(addr); got != test.region {
				t.Fatalf("region=%s want %s", got, test.region)
			}
		})
	}
}

func TestBuildSnapshotRejectsConflictingPrefix(t *testing.T) {
	_, _, err := buildSnapshot([]prefixRecord{
		{Prefix: "1.1.1.0/24", Region: RegionMainlandChina},
		{Prefix: "1.1.1.0/24", Region: RegionOutsideMainland},
	})
	if err == nil {
		t.Fatal("conflicting prefix should be rejected")
	}
}

func TestParseArchiveAndCacheRoundTrip(t *testing.T) {
	data := makeCountryArchive(t, map[string]string{
		"country/cn/aggregated.json": `{"countryCode":"CN","prefixes":{"ipv4":["1.1.1.0/24"],"ipv6":["240e::/16"]}}`,
		"country/us/aggregated.json": `{"countryCode":"US","prefixes":{"ipv4":["2.2.2.0/24"],"ipv6":["2606:4700::/32"]}}`,
	})
	doc, snap, err := parseArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || len(doc.Prefixes) != 4 {
		t.Fatalf("cache document=%+v", doc)
	}
	manager := &Manager{}
	manager.current.Store(&snap)
	if got := manager.Classify(netip.MustParseAddr("1.1.1.1")); got != RegionMainlandChina {
		t.Fatalf("CN region=%s", got)
	}
}

func TestManagerRefreshKeepsSnapshotOnFailureAndLoadsCache(t *testing.T) {
	archive := makeCountryArchive(t, map[string]string{
		"country/cn/aggregated.json": `{"countryCode":"CN","prefixes":{"ipv4":["1.1.1.0/24"]}}`,
		"country/us/aggregated.json": `{"countryCode":"US","prefixes":{"ipv4":["2.2.2.0/24"]}}`,
	})
	failed := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := archive
		if failed {
			status = http.StatusBadGateway
			body = nil
		}
		return &http.Response{
			StatusCode: status, Status: http.StatusText(status),
			Body: io.NopCloser(bytes.NewReader(body)),
		}, nil
	})}

	cachePath := filepath.Join(t.TempDir(), "country-ip.json")
	manager, err := New(Options{Client: client, SourceURL: "https://test.invalid/archive", CachePath: cachePath})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Refresh(nil); err != nil {
		t.Fatal(err)
	}
	if got := manager.Classify(netip.MustParseAddr("1.1.1.1")); got != RegionMainlandChina {
		t.Fatalf("initial region=%s", got)
	}

	failed = true
	if err := manager.Refresh(nil); err == nil {
		t.Fatal("failed refresh should return an error")
	}
	if got := manager.Classify(netip.MustParseAddr("1.1.1.1")); got != RegionMainlandChina {
		t.Fatalf("region after failed refresh=%s", got)
	}

	loaded, err := New(Options{CachePath: cachePath})
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Classify(netip.MustParseAddr("2.2.2.1")); got != RegionOutsideMainland {
		t.Fatalf("cached region=%s", got)
	}
}

func TestManagerStartRefreshesImmediatelyAndOnInterval(t *testing.T) {
	archive := makeCountryArchive(t, map[string]string{
		"country/cn/aggregated.json": `{"countryCode":"CN","prefixes":{"ipv4":["1.1.1.0/24"]}}`,
	})
	requests := make(chan struct{}, 4)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests <- struct{}{}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK",
			Body: io.NopCloser(bytes.NewReader(archive)),
		}, nil
	})}
	manager, err := New(Options{
		Client:         client,
		SourceURL:      "https://test.invalid/archive",
		UpdateInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.Start(context.Background())
	defer manager.Close()
	for i := 0; i < 2; i++ {
		select {
		case <-requests:
		case <-time.After(time.Second):
			t.Fatalf("refresh %d did not run", i+1)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func makeCountryArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	gzipWriter := gzip.NewWriter(&out)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, body := range files {
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
