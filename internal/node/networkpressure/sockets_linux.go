//go:build linux

package networkpressure

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const sockDiagByFamily = 20

// Read INET_DIAG_INFO through the kernel UAPI (linux/inet_diag.h, linux/tcp.h).
// No shell commands, payload capture, or ownership of nginx's sockets is needed.
func readSockets() ([]socketSample, error) {
	var out []socketSample
	for _, family := range []byte{unix.AF_INET, unix.AF_INET6} {
		items, err := readSocketFamily(family)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func readSocketFamily(family byte) ([]socketSample, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, err
	}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Usec: 500000}); err != nil {
		return nil, err
	}
	request := make([]byte, unix.NLMSG_HDRLEN+56)
	order := binary.NativeEndian
	order.PutUint32(request, uint32(len(request)))
	order.PutUint16(request[4:], sockDiagByFamily)
	order.PutUint16(request[6:], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	order.PutUint32(request[8:], 1)
	body := request[unix.NLMSG_HDRLEN:]
	body[0], body[1], body[2] = family, unix.IPPROTO_TCP, 2 // INET_DIAG_INFO extension.
	order.PutUint32(body[4:], 1<<1)                         // TCP_ESTABLISHED only.
	order.PutUint32(body[48:], ^uint32(0))
	order.PutUint32(body[52:], ^uint32(0))
	if err := unix.Sendto(fd, request, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, err
	}
	buffer := make([]byte, 64*1024)
	deadline := time.Now().Add(time.Second)
	var out []socketSample
	for received := 0; received < 8*1024*1024 && time.Now().Before(deadline); {
		n, _, flags, sender, err := unix.Recvmsg(fd, buffer, nil, 0)
		if err != nil {
			return nil, err
		}
		if sender, ok := sender.(*unix.SockaddrNetlink); !ok || sender.Pid != 0 {
			return nil, fmt.Errorf("unexpected socket diagnostic sender")
		}
		if flags&unix.MSG_TRUNC != 0 {
			return nil, fmt.Errorf("socket diagnostic dump truncated")
		}
		received += n
		messages, err := syscall.ParseNetlinkMessage(buffer[:n])
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if message.Header.Seq != 1 {
				continue
			}
			if message.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
				return nil, fmt.Errorf("socket diagnostic dump interrupted")
			}
			switch message.Header.Type {
			case unix.NLMSG_DONE:
				return out, nil
			case unix.NLMSG_ERROR:
				return nil, fmt.Errorf("socket diagnostic request rejected")
			case sockDiagByFamily:
				if item, ok := parseSocket(message.Data); ok {
					out = append(out, item)
				}
			}
		}
	}
	return nil, fmt.Errorf("socket diagnostic dump exceeded sampling budget")
}

func parseSocket(data []byte) (socketSample, bool) {
	if len(data) < 72 {
		return socketSample{}, false
	}
	var peer netip.Addr
	switch data[0] {
	case unix.AF_INET:
		peer = netip.AddrFrom4([4]byte(data[24:28]))
	case unix.AF_INET6:
		peer = netip.AddrFrom16([16]byte(data[24:40])).Unmap()
	default:
		return socketSample{}, false
	}
	if peer.IsLoopback() || peer.IsUnspecified() {
		return socketSample{}, false
	}
	order := binary.NativeEndian
	for attrs := data[72:]; len(attrs) >= 4; {
		length := int(order.Uint16(attrs))
		if length < 4 || length > len(attrs) {
			return socketSample{}, false
		}
		if order.Uint16(attrs[2:])&0x3fff == 2 { // INET_DIAG_INFO
			info := attrs[4:length]
			// Older kernels lacking busy/window counters provide no evidence.
			if len(info) < 192 {
				return socketSample{}, false
			}
			return socketSample{ID: string(data[:1]) + string(data[4:52]), Peer: peer,
				Acked: order.Uint64(info[120:]), Pending: order.Uint32(info[144:]),
				Busy: order.Uint64(info[168:]), RwndLimited: order.Uint64(info[176:]),
				SndbufLimited: order.Uint64(info[184:])}, true
		}
		aligned := (length + 3) &^ 3
		if aligned > len(attrs) {
			break
		}
		attrs = attrs[aligned:]
	}
	return socketSample{}, false
}
