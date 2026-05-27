package requestid

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareCreatesNewIDAndKeepsValidParent(t *testing.T) {
	var gotID, gotParent string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID, gotParent = FromContext(r.Context()), ParentFromContext(r.Context())
	}), "X-Request-ID", "X-Request-ID")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "parent-001")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if gotID == "" || gotID == "parent-001" || res.Header().Get("X-Request-ID") != gotID {
		t.Fatal("请求 ID 未独立生成并返回响应头")
	}
	if gotParent != "parent-001" {
		t.Fatal("合法父关联值未保留")
	}
}

func TestMiddlewareRejectsUnsafeParent(t *testing.T) {
	var gotParent string
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotParent = ParentFromContext(r.Context())
	}), "X-Request-ID", "X-Request-ID")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "bad value")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if gotParent != "" {
		t.Fatal("非法父关联值不得进入上下文")
	}
}
