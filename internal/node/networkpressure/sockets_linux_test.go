//go:build linux

package networkpressure

import (
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestParseSocketKernelLayout(t *testing.T) {
	for _, family := range []byte{unix.AF_INET, unix.AF_INET6} {
		data := make([]byte, 72+4+192)
		data[0] = family
		want := netip.MustParseAddr("192.0.2.1")
		if family == unix.AF_INET {
			copy(data[24:28], []byte{192, 0, 2, 1})
		} else {
			want = netip.MustParseAddr("2001:db8::1")
			ip := want.As16()
			copy(data[24:40], ip[:])
		}
		binary.NativeEndian.PutUint16(data[72:], 196)
		binary.NativeEndian.PutUint16(data[74:], 2)
		info := data[76:]
		binary.NativeEndian.PutUint64(info[120:], 10000000)
		binary.NativeEndian.PutUint32(info[144:], 32768)
		binary.NativeEndian.PutUint64(info[168:], 1000000)
		binary.NativeEndian.PutUint64(info[176:], 100000)
		binary.NativeEndian.PutUint64(info[184:], 200000)
		s, ok := parseSocket(data)
		if !ok || s.Peer != want || s.Acked != 10000000 || s.Pending != 32768 || s.Busy != 1000000 || s.RwndLimited != 100000 || s.SndbufLimited != 200000 {
			t.Fatalf("parsed=%+v ok=%v", s, ok)
		}
		for _, truncated := range []int{0, 71, 75, 100, len(data) - 1} {
			if _, ok := parseSocket(data[:truncated]); ok {
				t.Fatalf("accepted truncated dump length %d", truncated)
			}
		}
	}
}

func TestReadKernelSocketDiagnostics(t *testing.T) {
	if _, err := readSockets(); err != nil {
		t.Skipf("kernel socket diagnostics unavailable: %v", err)
	}
	// Connect through a local non-loopback address so the production filter
	// includes this socket, then verify actual kernel ACK counters and identity.
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var address net.IP
	for _, a := range addresses {
		if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() {
			address = ip.IP
			break
		}
	}
	if address == nil {
		t.Skip("no non-loopback IPv4 address")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: address})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.SetDeadline(time.Now().Add(2 * time.Second))
	client.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := server.Write(make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(client, make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		sockets, err := readSockets()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sockets {
			if s.Peer.String() == address.String() && binary.BigEndian.Uint16([]byte(s.ID)[1:3]) == port && s.Acked >= 4096 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("kernel dump did not contain ACKed server socket")
}
