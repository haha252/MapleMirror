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

func TestPrefixDoesNotSkipInvalidForwardedClient(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "unknown, 192.0.2.55")
	if got := Prefix(req, []string{"127.0.0.0/8"}); got != "127.0.0.1/32" {
		t.Fatalf("畸形 X-Forwarded-For 首项不得跳过后继续采信：%s", got)
	}
}

func TestPrefixUsesFirstNonEmptyForwardedClient(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", " , 192.0.2.55, 198.51.100.10")
	if got := Prefix(req, []string{"127.0.0.0/8"}); got != "192.0.2.55/32" {
		t.Fatalf("可信代理头应解析第一个非空客户端地址：%s", got)
	}
}

func TestPrefixUsesRealIPWhenForwardedForMissing(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Real-IP", "192.0.2.88")
	if got := Prefix(req, []string{"127.0.0.0/8"}); got != "192.0.2.88/32" {
		t.Fatalf("缺少 X-Forwarded-For 时应允许可信代理的 X-Real-IP：%s", got)
	}
}
