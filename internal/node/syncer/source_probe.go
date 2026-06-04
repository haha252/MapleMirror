package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"mirror-server/internal/protocol"
)

const sourceProbeTTL = time.Minute

type SourceProbe struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]probeResult
	active map[string]chan struct{}
}

type probeResult struct {
	ok      bool
	checked time.Time
	err     string
}

func NewSourceProbe(client *http.Client) *SourceProbe {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &SourceProbe{client: client, cache: map[string]probeResult{}, active: map[string]chan struct{}{}}
}

func (e Executor) fetchPrimary(ctx context.Context, task protocol.SyncTask, tmpPath string) (string, int64, error) {
	if e.Probe == nil {
		return e.fetch(ctx, task.Asset.DownloadURL, tmpPath)
	}
	if err := e.Probe.Check(ctx, task.Asset.DownloadURL); err != nil {
		return "", 0, err
	}
	return e.fetch(ctx, task.Asset.DownloadURL, tmpPath)
}

func (p *SourceProbe) Check(ctx context.Context, rawURL string) error {
	origin, err := sourceOrigin(rawURL)
	if err != nil {
		return err
	}
	now := time.Now()
	p.mu.Lock()
	if cached, ok := p.cache[origin]; ok && now.Sub(cached.checked) < sourceProbeTTL {
		p.mu.Unlock()
		return cached.errValue()
	}
	if wait, ok := p.active[origin]; ok {
		p.mu.Unlock()
		select {
		case <-wait:
			return p.cachedResult(origin)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	wait := make(chan struct{})
	p.active[origin] = wait
	p.mu.Unlock()
	err = p.probe(ctx, origin)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, origin)
	defer close(wait)
	if err != nil {
		p.cache[origin] = probeResult{checked: now, err: err.Error()}
		return err
	}
	p.cache[origin] = probeResult{ok: true, checked: now}
	return nil
}

func (p *SourceProbe) cachedResult(origin string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cache[origin].errValue()
}

func (r probeResult) errValue() error {
	if r.ok {
		return nil
	}
	if r.err == "" {
		return errors.New("源站连通性探测失败")
	}
	return errors.New(r.err)
}

func (p *SourceProbe) probe(ctx context.Context, origin string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, origin, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("源站连通性探测失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("源站连通性探测响应异常: %s", resp.Status)
	}
	return nil
}

func sourceOrigin(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("源站地址不完整")
	}
	return parsed.Scheme + "://" + parsed.Host + "/", nil
}
