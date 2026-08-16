package indexnow

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/logging"
)

const (
	maxURLsPerBatch = 10000
	maxAttempts     = 4
)

type Options struct {
	BaseURL    string
	Endpoint   string
	KeyFile    string
	StateFile  string
	Timeout    time.Duration
	HTTPClient *http.Client
	Logger     *logging.Logger
}

type Manager struct {
	key      string
	host     string
	baseURL  string
	keyURL   string
	endpoint string
	state    string
	client   *http.Client
	logger   *logging.Logger

	mu                sync.Mutex
	pending           map[string]struct{}
	bootstrapPending  map[string]struct{}
	bootstrapRevision string
	wake              chan struct{}
	stop              chan struct{}
	done              chan struct{}
	closeOnce         sync.Once
}

type requestPayload struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation"`
	URLList     []string `json:"urlList"`
}

func New(options Options) (*Manager, error) {
	base, err := parseBaseURL(options.BaseURL)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(strings.TrimSpace(options.Endpoint))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, errors.New("IndexNow endpoint 必须是 HTTPS URL")
	}
	material, err := LoadOrCreateKey(options.KeyFile)
	if err != nil {
		return nil, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	keyURL := strings.TrimRight(base.String(), "/") + "/" + material.Value + ".txt"
	m := &Manager{
		key: material.Value, host: base.Hostname(), baseURL: strings.TrimRight(base.String(), "/"),
		keyURL: keyURL, endpoint: endpoint.String(), state: options.StateFile, client: client,
		logger: options.Logger, pending: map[string]struct{}{}, bootstrapPending: map[string]struct{}{},
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go m.run()
	if material.Created {
		m.logInfo(context.Background(), "IndexNow key 已自动生成", slog.String("key_file", options.KeyFile))
	}
	return m, nil
}

func parseBaseURL(value string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(value))
	if err != nil || base.Scheme != "https" || base.Hostname() == "" || base.User != nil ||
		(base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("IndexNow public_base_url 必须是没有路径和查询参数的 HTTPS URL")
	}
	if !strings.EqualFold(base.Hostname(), "fyhub.cn") {
		return nil, errors.New("IndexNow public_base_url 必须使用 fyhub.cn，不支持其他主机名或 www.fyhub.cn")
	}
	return base, nil
}

func (m *Manager) Key() string { return m.key }

func (m *Manager) KeyPath() string { return "/" + m.key + ".txt" }

func (m *Manager) Bootstrap(projectIDs []string, revision string) {
	paths := []string{"/", "/about", "/api-docs", "/stats", "/changelog"}
	for _, projectID := range projectIDs {
		projectID = strings.Trim(strings.TrimSpace(projectID), "/")
		if projectID != "" && !strings.Contains(projectID, "/") {
			paths = append(paths, "/"+url.PathEscape(projectID)+"/")
		}
	}
	state := readState(m.state)
	if state.Key == m.key && state.Revision == revision {
		return
	}
	m.mu.Lock()
	m.bootstrapRevision = revision
	m.bootstrapPending = map[string]struct{}{}
	for _, path := range paths {
		if normalized, ok := m.normalizePath(path); ok {
			m.pending[normalized] = struct{}{}
			m.bootstrapPending[normalized] = struct{}{}
		}
	}
	m.mu.Unlock()
	m.signal()
}

func (m *Manager) NotifyPaths(_ context.Context, paths []string) {
	m.mu.Lock()
	for _, path := range paths {
		if normalized, ok := m.normalizePath(path); ok {
			m.pending[normalized] = struct{}{}
		}
	}
	m.mu.Unlock()
	m.signal()
}

func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		close(m.stop)
		<-m.done
	})
}
