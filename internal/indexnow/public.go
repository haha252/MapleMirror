package indexnow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"
	"strings"
)

func (m *Manager) Bootstrap(projectIDs []string, revision string) {
	paths := []string{"/", "/about", "/api-docs", "/stats", "/changelog"}
	for _, projectID := range projectIDs {
		projectID = strings.Trim(strings.TrimSpace(projectID), "/")
		if projectID != "" && !strings.Contains(projectID, "/") {
			paths = append(paths, "/"+url.PathEscape(projectID)+"/")
		}
	}
	changes := make([]PageChange, 0, len(paths))
	for _, path := range paths {
		normalized, ok := m.normalizePath(path)
		if !ok {
			continue
		}
		changes = append(changes, PageChange{Path: normalized,
			Fingerprint: revisionFingerprint(revision, normalized), Present: true})
	}
	queued := m.reconcileChanges(changes, false)
	if queued > 0 {
		m.logInfo(context.Background(), "IndexNow bootstrap 已排队",
			slog.Int("url_count", queued), slog.String("revision", revision))
	}
}

func revisionFingerprint(revision, path string) string {
	sum := sha256.Sum256([]byte("config|" + strings.TrimSpace(revision) + "|" + path))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (m *Manager) ReconcileSnapshot(_ context.Context, snapshot Snapshot) int {
	m.mu.Lock()
	previousPages := make(map[string]pageState, len(m.state.Pages))
	for path, page := range m.state.Pages {
		previousPages[path] = page
	}
	legacy := m.legacyState
	m.mu.Unlock()
	changes := make([]PageChange, 0, len(snapshot)+len(previousPages))
	for path, page := range snapshot {
		page.Path = path
		page.Present = true
		changes = append(changes, page)
	}
	for path, previous := range previousPages {
		if _, exists := snapshot[path]; exists {
			continue
		}
		changes = append(changes, PageChange{Path: path,
			Fingerprint: deletionFingerprint(previous.Fingerprint), Present: false})
	}
	queued := m.reconcileChanges(changes, false)
	if queued > 0 {
		message := "IndexNow 启动补提交已排队"
		if legacy {
			message = "IndexNow bootstrap 已排队"
		}
		m.logInfo(context.Background(), message, slog.Int("url_count", queued))
	}
	if legacy {
		m.mu.Lock()
		m.bootstrapActive = queued > 0 || len(m.pending) > 0
		m.mu.Unlock()
	}
	return queued
}

func deletionFingerprint(previous string) string {
	return revisionFingerprint("deleted|"+previous, "")
}

func (m *Manager) NotifyChanges(_ context.Context, changes []PageChange) {
	queued := m.reconcileChanges(changes, false)
	if queued > 0 {
		m.logInfo(context.Background(), "IndexNow 内容变更已排队", slog.Int("url_count", queued))
	}
}

func (m *Manager) NotifyPaths(ctx context.Context, paths []string) {
	changes := make([]PageChange, 0, len(paths))
	for _, path := range paths {
		if normalized, ok := m.normalizePath(path); ok {
			changes = append(changes, PageChange{Path: normalized,
				Fingerprint: revisionFingerprint("path", normalized), Present: true})
		}
	}
	m.NotifyChanges(ctx, changes)
}

func (m *Manager) NotifyPathsNow(_ context.Context, paths []string) int {
	changes := make([]PageChange, 0, len(paths))
	for _, path := range paths {
		if normalized, ok := m.normalizePath(path); ok {
			changes = append(changes, PageChange{Path: normalized,
				Fingerprint: revisionFingerprint("manual", normalized), Present: true})
		}
	}
	queued := m.reconcileChanges(changes, true)
	if queued > 0 {
		m.logInfo(context.Background(), "IndexNow URL 已立即排队", slog.Int("url_count", queued))
	}
	if len(changes) > 0 {
		m.signalNow()
	}
	return queued
}

func (m *Manager) NotifySnapshotNow(_ context.Context, snapshot Snapshot) int {
	m.mu.Lock()
	previousPages := make(map[string]pageState, len(m.state.Pages))
	for path, page := range m.state.Pages {
		previousPages[path] = page
	}
	m.mu.Unlock()
	changes := make([]PageChange, 0, len(snapshot)+len(previousPages))
	for path, page := range snapshot {
		page.Path = path
		page.Present = true
		changes = append(changes, page)
	}
	for path, previous := range previousPages {
		if _, exists := snapshot[path]; !exists {
			changes = append(changes, PageChange{Path: path,
				Fingerprint: deletionFingerprint(previous.Fingerprint), Present: false})
		}
	}
	queued := m.reconcileChanges(changes, true)
	if queued > 0 {
		m.logInfo(context.Background(), "IndexNow 全量 URL 已立即排队", slog.Int("url_count", queued))
	}
	m.signalNow()
	return queued
}
