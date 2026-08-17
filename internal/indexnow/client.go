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
	Location   *time.Location
	Debounce   time.Duration
	HTTPClient *http.Client
	Logger     *logging.Logger
}

// Page 是一个公开页面快照或一次页面变更。Present=false 表示页面已删除或停用。
type Page struct {
	Path        string
	Fingerprint string
	Present     bool
}

type PageChange = Page

type Snapshot map[string]Page

type pendingPage struct {
	Page
	Force bool
}

type Manager struct {
	key       string
	host      string
	baseURL   string
	keyURL    string
	endpoint  string
	statePath string
	state     persistedState
	client    *http.Client
	logger    *logging.Logger

	mu              sync.Mutex
	pending         map[string]pendingPage
	legacyState     bool
	bootstrapActive bool
	location        *time.Location
	debounce        time.Duration
	wake            chan struct{}
	flushNow        chan struct{}
	stop            chan struct{}
	done            chan struct{}
	closeOnce       sync.Once
	flushMu         sync.Mutex
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
	location := options.Location
	if location == nil {
		location = time.Local
	}
	debounce := options.Debounce
	if debounce <= 0 {
		debounce = 5 * time.Minute
	}
	loaded, legacy, stateErr := loadState(options.StateFile)
	state := emptyState(material.Value)
	if !legacy && loaded.Key == material.Value {
		state = loaded
	} else if !legacy && loaded.Key != "" {
		legacy = true
		stateErr = errors.New("IndexNow state 的 key 与当前 key 不一致")
	}
	keyURL := strings.TrimRight(base.String(), "/") + "/" + material.Value + ".txt"
	m := &Manager{
		key: material.Value, host: base.Hostname(), baseURL: strings.TrimRight(base.String(), "/"),
		keyURL: keyURL, endpoint: endpoint.String(), statePath: options.StateFile, state: state,
		client: client, logger: options.Logger, pending: make(map[string]pendingPage),
		legacyState: legacy, location: location, debounce: debounce,
		wake: make(chan struct{}, 1), flushNow: make(chan struct{}, 1),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go m.run()
	if material.Created {
		m.logInfo(context.Background(), "IndexNow key 已自动生成", slog.String("key_file", options.KeyFile))
	}
	if stateErr != nil {
		m.logWarn(context.Background(), "IndexNow state 读取异常，已要求首次全量同步",
			slog.String("error", stateErr.Error()))
	} else if legacy {
		m.logInfo(context.Background(), "IndexNow 旧状态需要首次全量同步")
	}
	return m, nil
}

func parseBaseURL(value string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(value))
	if err != nil || base.Scheme != "https" || base.Hostname() == "" || base.User != nil ||
		(base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("IndexNow public_base_url 必须是没有路径和查询参数的 HTTPS URL")
	}
	return base, nil
}

func (m *Manager) Key() string { return m.key }

func (m *Manager) Host() string { return m.host }

func (m *Manager) KeyPath() string { return "/" + m.key + ".txt" }

func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		close(m.stop)
		<-m.done
	})
}
