package control

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

type testHTTPServer struct {
	URL    string
	client *http.Client
}

var (
	testHTTPServerSeq      atomic.Uint64
	testHTTPServerHandlers = struct {
		sync.RWMutex
		items map[string]http.Handler
	}{items: map[string]http.Handler{}}
	testHTTPServerClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		testHTTPServerHandlers.RLock()
		handler := testHTTPServerHandlers.items[req.URL.Host]
		testHTTPServerHandlers.RUnlock()
		if handler == nil {
			handler = http.NotFoundHandler()
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		resp := rec.Result()
		resp.Request = req
		return resp, nil
	})}
)

func (s *testHTTPServer) Close() {}

func (s *testHTTPServer) Client() *http.Client {
	return s.client
}

func newTestServer(t *testing.T, handler http.Handler) *testHTTPServer {
	t.Helper()
	if handler == nil {
		handler = http.DefaultServeMux
	}
	host := fmt.Sprintf("server-%d.test:8080", testHTTPServerSeq.Add(1))
	server := &testHTTPServer{
		URL: "http://" + host,
	}
	testHTTPServerHandlers.Lock()
	testHTTPServerHandlers.items[host] = handler
	testHTTPServerHandlers.Unlock()
	server.client = testHTTPServerClient
	return server
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
