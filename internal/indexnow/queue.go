package indexnow

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"
)

type permanentSubmissionError struct{ err error }

func (e *permanentSubmissionError) Error() string { return e.err.Error() }

func (e *permanentSubmissionError) Unwrap() error { return e.err }

type submissionItem struct {
	Path        string
	Fingerprint string
	Present     bool
	Force       bool
	URL         string
}

func (m *Manager) reconcileChanges(changes []PageChange, force bool) int {
	m.mu.Lock()
	queued := 0
	for _, change := range changes {
		normalized, ok := m.normalizePath(change.Path)
		if !ok {
			continue
		}
		change.Path = normalized
		if strings.TrimSpace(change.Fingerprint) == "" {
			change.Fingerprint = revisionFingerprint("unknown", normalized)
		}
		change.Fingerprint = strings.TrimSpace(change.Fingerprint)
		if !force && m.alreadyHandledLocked(change) {
			continue
		}
		current, exists := m.pending[normalized]
		if exists && current.Fingerprint == change.Fingerprint && current.Present == change.Present {
			if force {
				current.Force = true
				m.pending[normalized] = current
			}
			continue
		}
		m.pending[normalized] = pendingPage{Page: change, Force: force}
		queued++
	}
	m.mu.Unlock()
	if queued > 0 {
		m.signalWake()
	}
	return queued
}

func (m *Manager) alreadyHandledLocked(change PageChange) bool {
	if previous, ok := m.state.Pages[change.Path]; ok && change.Present &&
		previous.Fingerprint == change.Fingerprint {
		return true
	}
	if failure, ok := m.state.Errors[change.Path]; ok &&
		failure.Fingerprint == change.Fingerprint {
		return true
	}
	return false
}

func (m *Manager) takeBatch() []submissionItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return nil
	}
	paths := make([]string, 0, len(m.pending))
	for path := range m.pending {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > maxURLsPerBatch {
		paths = paths[:maxURLsPerBatch]
	}
	batch := make([]submissionItem, 0, len(paths))
	for _, path := range paths {
		pending := m.pending[path]
		batch = append(batch, submissionItem{Path: path, Fingerprint: pending.Fingerprint,
			Present: pending.Present, Force: pending.Force, URL: m.baseURL + path})
		delete(m.pending, path)
	}
	return batch
}

func (m *Manager) requeue(batch []submissionItem) {
	m.mu.Lock()
	for _, item := range batch {
		current, exists := m.pending[item.Path]
		if exists && (current.Fingerprint != item.Fingerprint || current.Present != item.Present) {
			continue
		}
		m.pending[item.Path] = pendingPage{Page: Page{Path: item.Path,
			Fingerprint: item.Fingerprint, Present: item.Present}, Force: item.Force}
	}
	m.mu.Unlock()
}

func (m *Manager) markSubmitted(batch []submissionItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneState(m.state)
	next.Version = stateVersion
	next.Key = m.key
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range batch {
		if item.Present {
			next.Pages[item.Path] = pageState{Fingerprint: item.Fingerprint, SubmittedAt: now}
		} else {
			delete(next.Pages, item.Path)
		}
		delete(next.Errors, item.Path)
	}
	if err := writeState(m.statePath, next); err != nil {
		return err
	}
	m.state = next
	m.legacyState = false
	if m.bootstrapActive && len(m.pending) == 0 {
		m.bootstrapActive = false
		m.logInfo(context.Background(), "IndexNow bootstrap 已完成")
	}
	return nil
}

func (m *Manager) recordPermanentFailure(batch []submissionItem, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneState(m.state)
	next.Version = stateVersion
	next.Key = m.key
	now := time.Now().UTC().Format(time.RFC3339Nano)
	message := err.Error()
	for _, item := range batch {
		if current, exists := m.pending[item.Path]; exists &&
			(current.Fingerprint != item.Fingerprint || current.Present != item.Present) {
			continue
		}
		next.Errors[item.Path] = errorState{Fingerprint: item.Fingerprint,
			Kind: "permanent", Message: message, RecordedAt: now}
	}
	if err := writeState(m.statePath, next); err != nil {
		return err
	}
	m.state = next
	return nil
}

func (m *Manager) hasPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending) > 0
}

func (m *Manager) normalizePath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "?") || strings.Contains(path, "#") {
		return "", false
	}
	parsed, err := url.Parse(path)
	if err != nil || parsed.Path == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	return parsed.EscapedPath(), true
}

type submissionResult struct {
	Attempts   int
	Status     int
	Duration   time.Duration
	RetryAfter time.Duration
}

func submissionAttrs(urlCount int, result submissionResult, retryScheduled bool) []slog.Attr {
	return []slog.Attr{
		slog.Int("url_count", urlCount), slog.Int("attempts", result.Attempts),
		slog.Int("status", result.Status), slog.Int64("duration_ms", result.Duration.Milliseconds()),
		slog.Bool("retry_scheduled", retryScheduled),
	}
}

func failureKind(err error) string {
	var permanent *permanentSubmissionError
	if errors.As(err, &permanent) {
		return "permanent_error"
	}
	return "retry_exhausted"
}

func errorSummary(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func (m *Manager) logSubmissionFailure(batch []submissionItem, result submissionResult, err error, retry bool) {
	attrs := submissionAttrs(len(batch), result, retry)
	attrs = append(attrs, slog.String("failure_kind", failureKind(err)),
		slog.String("error", errorSummary(err)))
	m.logError(context.Background(), "IndexNow URL 批次最终失败", attrs...)
}
