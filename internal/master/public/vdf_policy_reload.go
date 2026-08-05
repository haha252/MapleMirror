package public

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

const vdfPolicyReloadInterval = 2 * time.Second

type vdfPolicyReloader struct {
	service    *vdfService
	path       string
	vdfConfig  config.VDF
	logger     *logging.Logger
	reloadMu   sync.Mutex
	stateMu    sync.Mutex
	observed   string
	generation uint64
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

func newVDFPolicyReloader(service *vdfService, path string, vdfConfig config.VDF,
	logger *logging.Logger) *vdfPolicyReloader {
	if service == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	reloader := &vdfPolicyReloader{
		service: service, path: path, vdfConfig: vdfConfig, logger: logger,
		observed: vdfConfigFingerprint(path), generation: 1,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go reloader.reloadLoop()
	return reloader
}

func (r *vdfPolicyReloader) reloadLoop() {
	ticker := time.NewTicker(vdfPolicyReloadInterval)
	defer ticker.Stop()
	defer close(r.done)
	for {
		select {
		case <-ticker.C:
			r.reloadIfChanged()
		case <-r.stop:
			return
		}
	}
}

func (r *vdfPolicyReloader) reloadIfChanged() bool {
	if r == nil {
		return false
	}
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()

	fingerprint := vdfConfigFingerprint(r.path)
	r.stateMu.Lock()
	if fingerprint == r.observed {
		r.stateMu.Unlock()
		return false
	}
	r.observed = fingerprint
	r.stateMu.Unlock()

	tiers, err := config.LoadVDFSizeTiers(r.path)
	if err != nil {
		r.logFailure(err)
		return false
	}
	next, err := newVDFPolicy(tiers, r.vdfConfig)
	if err != nil {
		r.logFailure(err)
		return false
	}
	current := r.service.currentPolicy()
	if equalVDFPolicies(current, next) {
		return false
	}
	r.service.setPolicy(next)

	r.stateMu.Lock()
	r.generation++
	generation := r.generation
	r.stateMu.Unlock()
	if r.logger != nil {
		r.logger.Info(context.Background(), "VDF 分档策略已热重载",
			slog.Uint64("generation", generation), slog.Int("tiers", len(tiers)))
	}
	return true
}

func (r *vdfPolicyReloader) logFailure(err error) {
	if r.logger != nil {
		r.logger.Warn(context.Background(), "VDF 分档配置热重载失败，沿用上一份有效配置",
			slog.String("error", err.Error()))
	}
}

func (r *vdfPolicyReloader) close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() { close(r.stop) })
	<-r.done
}

func vdfConfigFingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "error:" + err.Error()
	}
	return fmt.Sprintf("%d|%d", info.ModTime().UnixNano(), info.Size())
}
