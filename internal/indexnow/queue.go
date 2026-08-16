package indexnow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type permanentSubmissionError struct{ err error }

func (e *permanentSubmissionError) Error() string { return e.err.Error() }

func (e *permanentSubmissionError) Unwrap() error { return e.err }

func (m *Manager) run() {
	defer close(m.done)
	for {
		select {
		case <-m.stop:
			return
		case <-m.wake:
			for {
				batch := m.takeBatch()
				if len(batch) == 0 {
					break
				}
				result, err := m.submitWithRetry(batch)
				if err != nil {
					var permanent *permanentSubmissionError
					retryScheduled := !errors.As(err, &permanent)
					failureKind := "retry_exhausted"
					if permanent != nil {
						failureKind = "permanent_error"
					}
					if retryScheduled {
						m.requeue(batch)
						time.AfterFunc(time.Minute, m.signal)
					}
					attrs := submissionAttrs(len(batch), result, retryScheduled)
					attrs = append(attrs, slog.String("failure_kind", failureKind), slog.String("error", err.Error()))
					m.logError(context.Background(), "IndexNow URL 批次最终失败", attrs...)
					break
				}
				m.markSubmitted(batch)
				m.logInfo(context.Background(), "IndexNow URL 提交成功",
					submissionAttrs(len(batch), result, false)...)
			}
		}
	}
}

func (m *Manager) takeBatch() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return nil
	}
	limit := maxURLsPerBatch
	if len(m.pending) < limit {
		limit = len(m.pending)
	}
	batch := make([]string, 0, limit)
	for path := range m.pending {
		batch = append(batch, m.baseURL+path)
		delete(m.pending, path)
		if len(batch) == limit {
			break
		}
	}
	return batch
}

func (m *Manager) requeue(urls []string) {
	m.mu.Lock()
	for _, fullURL := range urls {
		if parsed, err := url.Parse(fullURL); err == nil {
			m.pending[parsed.EscapedPath()] = struct{}{}
		}
	}
	m.mu.Unlock()
}

func (m *Manager) markSubmitted(urls []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, fullURL := range urls {
		parsed, err := url.Parse(fullURL)
		if err != nil {
			continue
		}
		delete(m.bootstrapPending, parsed.EscapedPath())
	}
	if len(m.bootstrapPending) == 0 && m.bootstrapRevision != "" {
		if err := writeState(m.state, persistedState{Key: m.key, Revision: m.bootstrapRevision}); err != nil {
			m.logWarn(context.Background(), "IndexNow state 写入失败", slog.String("error", err.Error()))
		} else {
			m.bootstrapRevision = ""
		}
	}
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
	Attempts int
	Status   int
	Duration time.Duration
}

func submissionAttrs(urlCount int, result submissionResult, retryScheduled bool) []slog.Attr {
	return []slog.Attr{
		slog.Int("url_count", urlCount), slog.Int("attempts", result.Attempts),
		slog.Int("status", result.Status), slog.Int64("duration_ms", result.Duration.Milliseconds()),
		slog.Bool("retry_scheduled", retryScheduled),
	}
}

func (m *Manager) submitWithRetry(urls []string) (result submissionResult, err error) {
	started := time.Now()
	defer func() { result.Duration = time.Since(started) }()
	payload, err := json.Marshal(requestPayload{Host: m.host, Key: m.key, KeyLocation: m.keyURL, URLList: urls})
	if err != nil {
		return result, err
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.Attempts = attempt
		request, err := http.NewRequest(http.MethodPost, m.endpoint, strings.NewReader(string(payload)))
		if err != nil {
			return result, err
		}
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		response, err := m.client.Do(request)
		if err == nil {
			result.Status = response.StatusCode
			_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 512))
			_ = response.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusAccepted {
				return result, nil
			} else {
				lastErr = fmt.Errorf("IndexNow endpoint 返回 HTTP %d", response.StatusCode)
				if response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
					return result, &permanentSubmissionError{err: lastErr}
				}
			}
		} else {
			lastErr = err
		}
		if attempt < maxAttempts {
			timer := time.NewTimer(time.Duration(1<<(attempt-1)) * 250 * time.Millisecond)
			select {
			case <-timer.C:
			case <-m.stop:
				if !timer.Stop() {
					<-timer.C
				}
				return result, context.Canceled
			}
		}
	}
	return result, lastErr
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) logInfo(ctx context.Context, message string, attrs ...slog.Attr) {
	if m.logger != nil {
		m.logger.Info(ctx, message, attrs...)
	}
}

func (m *Manager) logWarn(ctx context.Context, message string, attrs ...slog.Attr) {
	if m.logger != nil {
		m.logger.Warn(ctx, message, attrs...)
	}
}

func (m *Manager) logError(ctx context.Context, message string, attrs ...slog.Attr) {
	if m.logger != nil {
		m.logger.Error(ctx, message, attrs...)
	}
}
