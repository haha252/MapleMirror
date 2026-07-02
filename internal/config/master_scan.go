package config

import (
	"errors"
	"net/url"
)

func validateScanSocks5(proxy Socks5Proxy) error {
	if !proxy.Enabled {
		return nil
	}
	if proxy.URL == "" {
		return errors.New("启用 scan.socks5.enabled 时 scan.socks5.url 不能为空")
	}
	u, err := url.Parse(proxy.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("scan.socks5.url 必须是合法 SOCKS5 代理 URL")
	}
	if u.User != nil {
		return errors.New("scan.socks5.url 不得包含用户名或密码，请使用 scan.socks5.username 和 scan.socks5.password")
	}
	switch u.Scheme {
	case "socks5", "socks5h":
		return nil
	default:
		return errors.New("scan.socks5.url 仅支持 socks5 或 socks5h")
	}
}
