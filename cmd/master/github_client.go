package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/master/mirrorsync"
)

func newGitHubHTTPClient(scan config.Scan) (*http.Client, time.Duration, error) {
	timeout := mirrorsync.DefaultGitHubClientTimeout
	if scan.GitHubTimeout != "" {
		parsed, err := time.ParseDuration(scan.GitHubTimeout)
		if err != nil {
			return nil, 0, err
		}
		timeout = parsed
	}
	if timeout <= 0 {
		timeout = mirrorsync.DefaultGitHubClientTimeout
	}
	client := &http.Client{Timeout: timeout}
	if !scan.Socks5.Enabled {
		return client, timeout, nil
	}
	dialer, err := newSocks5Dialer(scan.Socks5)
	if err != nil {
		return nil, 0, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	client.Transport = transport
	return client, timeout, nil
}

type socks5Dialer struct {
	proxyAddr string
	remoteDNS bool
	username  string
	password  string
	base      net.Dialer
}

func newSocks5Dialer(cfg config.Socks5Proxy) (*socks5Dialer, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	if u.Host == "" || (u.Scheme != "socks5" && u.Scheme != "socks5h") {
		return nil, errors.New("scan.socks5.url 必须使用 socks5 或 socks5h URL")
	}
	return &socks5Dialer{
		proxyAddr: u.Host,
		remoteDNS: u.Scheme == "socks5h",
		username:  cfg.Username,
		password:  cfg.Password,
	}, nil
}

func (d *socks5Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("SOCKS5 仅支持 tcp 网络：%s", network)
	}
	conn, err := d.base.DialContext(ctx, "tcp", d.proxyAddr)
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer conn.SetDeadline(time.Time{})
	}
	if err := d.handshake(ctx, conn, address); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d *socks5Dialer) handshake(ctx context.Context, conn net.Conn, address string) error {
	method := byte(0x00)
	if d.username != "" || d.password != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, 0x01, method}); err != nil {
		return err
	}
	reply := []byte{0, 0}
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x05 || reply[1] == 0xff {
		return errors.New("SOCKS5 代理不接受认证方式")
	}
	if reply[1] != method {
		return errors.New("SOCKS5 代理返回了不匹配的认证方式")
	}
	if method == 0x02 {
		if err := d.authenticate(conn); err != nil {
			return err
		}
	}
	req, err := d.connectRequest(ctx, address)
	if err != nil {
		return err
	}
	if _, err := conn.Write(req); err != nil {
		return err
	}
	header := []byte{0, 0, 0, 0}
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return errors.New("SOCKS5 代理响应版本异常")
	}
	if header[1] != 0x00 {
		return fmt.Errorf("SOCKS5 代理连接失败，响应码=%d", header[1])
	}
	return readSocks5BindAddress(conn, header[3])
}

func (d *socks5Dialer) authenticate(conn net.Conn) error {
	if len(d.username) > 255 || len(d.password) > 255 {
		return errors.New("SOCKS5 用户名或密码过长")
	}
	req := []byte{0x01, byte(len(d.username))}
	req = append(req, d.username...)
	req = append(req, byte(len(d.password)))
	req = append(req, d.password...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	reply := []byte{0, 0}
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x01 || reply[1] != 0x00 {
		return errors.New("SOCKS5 用户名密码认证失败")
	}
	return nil
}

func (d *socks5Dialer) connectRequest(ctx context.Context, address string) ([]byte, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("SOCKS5 目标端口无效：%s", portText)
	}
	req := []byte{0x05, 0x01, 0x00}
	if !d.remoteDNS {
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, item := range ips {
			if ip := item.IP.To4(); ip != nil {
				req = append(req, 0x01)
				req = append(req, ip...)
				return appendSocks5Port(req, port), nil
			}
			if ip := item.IP.To16(); ip != nil {
				req = append(req, 0x04)
				req = append(req, ip...)
				return appendSocks5Port(req, port), nil
			}
		}
		return nil, fmt.Errorf("SOCKS5 本地解析无可用地址：%s", host)
	}
	if len(host) > 255 {
		return nil, errors.New("SOCKS5 目标域名过长")
	}
	req = append(req, 0x03, byte(len(host)))
	req = append(req, host...)
	return appendSocks5Port(req, port), nil
}

func appendSocks5Port(req []byte, port int) []byte {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, uint16(port))
	return append(req, buf...)
}

func readSocks5BindAddress(conn net.Conn, atyp byte) error {
	var length int
	switch atyp {
	case 0x01:
		length = net.IPv4len
	case 0x03:
		size := []byte{0}
		if _, err := io.ReadFull(conn, size); err != nil {
			return err
		}
		length = int(size[0])
	case 0x04:
		length = net.IPv6len
	default:
		return errors.New("SOCKS5 代理响应地址类型异常")
	}
	buf := make([]byte, length+2)
	_, err := io.ReadFull(conn, buf)
	return err
}
