package clientip

import (
	"net/http/httptest"
	"testing"
)

func TestPrefixIgnoresForwardedHeaderFromUntrustedRemote(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("X-Forwarded-For", "192.0.2.55")
	if got := Prefix(req, []string{"127.0.0.0/8"}); got != "198.51.100.10/32" {
		t.Fatalf("不可信代理头不应改变客户端前缀：%s", got)
	}
}

func TestPrefixUsesForwardedHeaderFromTrustedRemote(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.0.2.55, 198.51.100.10")
	if got := Prefix(req, []string{"127.0.0.0/8"}); got != "192.0.2.55/32" {
		t.Fatalf("可信代理头应解析第一个客户端地址：%s", got)
	}
}
