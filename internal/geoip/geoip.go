package geoip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mirror-server/internal/logging"
)

type Region string

const (
	RegionUnknown         Region = "unknown"
	RegionMainlandChina   Region = "mainland_china"
	RegionOutsideMainland Region = "outside_mainland_china"
)

func ValidNodeRegion(region Region) bool {
	return region == RegionUnknown || region == RegionMainlandChina || region == RegionOutsideMainland
}

const (
	defaultUpdateInterval = 24 * time.Hour
	defaultHTTPTimeout    = 2 * time.Minute
	sourceURL             = "https://github.com/ipverse/country-ip-blocks/releases/latest/download/country-ip-blocks.tar.gz"
	maxArchiveBytes       = 128 << 20
	maxCountryFileBytes   = 8 << 20
)

type Classifier interface {
	Classify(netip.Addr) Region
}

type Options struct {
	Client         *http.Client
	CachePath      string
	UpdateInterval time.Duration
	SourceURL      string
	Logger         *logging.Logger
}

type Manager struct {
	client    *http.Client
	cachePath string
	interval  time.Duration
	sourceURL string
	logger    *logging.Logger

	current atomic.Pointer[snapshot]

	startOnce sync.Once
	closeOnce sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

type snapshot struct {
	ipv4      prefixTree
	ipv6      prefixTree
	updatedAt string
}

func New(opts Options) (*Manager, error) {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	interval := opts.UpdateInterval
	if interval <= 0 {
		interval = defaultUpdateInterval
	}
	endpoint := strings.TrimSpace(opts.SourceURL)
	if endpoint == "" {
		endpoint = sourceURL
	}
	m := &Manager{
		client:    client,
		cachePath: strings.TrimSpace(opts.CachePath),
		interval:  interval,
		sourceURL: endpoint,
		logger:    opts.Logger,
		done:      make(chan struct{}),
	}
	m.loadCache()
	return m, nil
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.startOnce.Do(func() {
		var runCtx context.Context
		runCtx, m.cancel = context.WithCancel(ctx)
		go m.run(runCtx)
	})
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		if m.cancel == nil {
			return
		}
		m.cancel()
		<-m.done
	})
}

func (m *Manager) Classify(addr netip.Addr) Region {
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if m == nil || !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return RegionUnknown
	}
	current := m.current.Load()
	if current == nil {
		return RegionUnknown
	}
	if addr.Is4() {
		return current.ipv4.lookup(addr)
	}
	return current.ipv6.lookup(addr)
}

func (m *Manager) Refresh(ctx context.Context) error {
	if m == nil {
		return errors.New("geoip manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.sourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/gzip")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求国家 IP 数据库失败：%w", err)
	}
	if resp.Body == nil {
		return errors.New("国家 IP 数据库响应没有内容")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("国家 IP 数据库响应异常：%s", resp.Status)
	}
	archiveData, err := readLimited(resp.Body, maxArchiveBytes)
	if err != nil {
		return fmt.Errorf("读取国家 IP 数据库失败：%w", err)
	}
	doc, snap, err := parseArchive(archiveData)
	if err != nil {
		return fmt.Errorf("解析国家 IP 数据库失败：%w", err)
	}
	doc.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	snap.updatedAt = doc.UpdatedAt
	if err := m.writeCache(doc); err != nil {
		m.warn("国家 IP 数据库缓存写入失败，继续使用内存快照", slog.String("error", err.Error()))
	}
	m.current.Store(&snap)
	m.info("国家 IP 数据库已更新",
		slog.String("source", m.sourceURL), slog.String("updated_at", snap.updatedAt),
		slog.Int("prefixes", len(doc.Prefixes)))
	return nil
}

func (m *Manager) run(ctx context.Context) {
	defer close(m.done)
	if err := m.Refresh(ctx); err != nil {
		m.warn("国家 IP 数据库更新失败，保留最近一次有效数据", slog.String("error", err.Error()))
	}
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Refresh(ctx); err != nil {
				m.warn("国家 IP 数据库更新失败，保留最近一次有效数据", slog.String("error", err.Error()))
			}
		}
	}
}

func (m *Manager) info(message string, attrs ...slog.Attr) {
	if m != nil && m.logger != nil {
		m.logger.Info(context.Background(), message, attrs...)
	}
}

func (m *Manager) warn(message string, attrs ...slog.Attr) {
	if m != nil && m.logger != nil {
		m.logger.Warn(context.Background(), message, attrs...)
	}
}
