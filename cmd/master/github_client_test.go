package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/master/mirrorsync"
)

func TestNewGitHubHTTPClientTimeouts(t *testing.T) {
	client, timeout, err := newGitHubHTTPClient(config.Scan{})
	if err != nil {
		t.Fatal(err)
	}
	if timeout != mirrorsync.DefaultGitHubClientTimeout || client.Timeout != mirrorsync.DefaultGitHubClientTimeout {
		t.Fatalf("default timeout mismatch: timeout=%s client=%s", timeout, client.Timeout)
	}
	client, timeout, err = newGitHubHTTPClient(config.Scan{GitHubTimeout: "45s"})
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 45*time.Second || client.Timeout != 45*time.Second {
		t.Fatalf("custom timeout mismatch: timeout=%s client=%s", timeout, client.Timeout)
	}
}

func TestSocks5DialerUsesSeparateCredentialsAndRemoteDNS(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	serverErr := make(chan error, 1)
	go serveOneSocks5Handshake(serverConn, serverErr)
	dialer := socks5Dialer{remoteDNS: true, username: "mirror", password: "secret"}
	if err := dialer.handshake(context.Background(), clientConn, "api.github.com:443"); err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestSocks5ConnectRequestLocalDNSForSocks5(t *testing.T) {
	dialer := socks5Dialer{}
	req, err := dialer.connectRequest(context.Background(), "127.0.0.1:443")
	if err != nil {
		t.Fatal(err)
	}
	if len(req) != 10 || req[0] != 5 || req[1] != 1 || req[3] != 1 ||
		req[4] != 127 || req[5] != 0 || req[6] != 0 || req[7] != 1 ||
		req[8] != 1 || req[9] != 187 {
		t.Fatalf("unexpected socks5 local request: %v", req)
	}
}

func serveOneSocks5Handshake(conn net.Conn, result chan<- error) {
	defer conn.Close()
	if err := expectBytes(conn, []byte{5, 1, 2}); err != nil {
		result <- err
		return
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		result <- err
		return
	}
	if err := expectBytes(conn, []byte{1, 6, 'm', 'i', 'r', 'r', 'o', 'r', 6, 's', 'e', 'c', 'r', 'e', 't'}); err != nil {
		result <- err
		return
	}
	if _, err := conn.Write([]byte{1, 0}); err != nil {
		result <- err
		return
	}
	if err := expectBytes(conn, []byte{5, 1, 0, 3, 14, 'a', 'p', 'i', '.', 'g', 'i', 't', 'h', 'u', 'b', '.', 'c', 'o', 'm', 1, 187}); err != nil {
		result <- err
		return
	}
	_, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	result <- err
}

func expectBytes(r io.Reader, want []byte) error {
	got := make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil {
		return err
	}
	for i := range want {
		if got[i] != want[i] {
			return &byteMismatch{got: got, want: want}
		}
	}
	return nil
}

type byteMismatch struct {
	got  []byte
	want []byte
}

func (e *byteMismatch) Error() string {
	return "SOCKS5 bytes mismatch"
}
