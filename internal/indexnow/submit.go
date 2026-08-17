package indexnow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (m *Manager) submitWithRetry(batch []submissionItem) (result submissionResult, err error) {
	started := time.Now()
	defer func() { result.Duration = time.Since(started) }()
	urls := make([]string, 0, len(batch))
	for _, item := range batch {
		urls = append(urls, item.URL)
	}
	payload, err := json.Marshal(requestPayload{Host: m.host, Key: m.key,
		KeyLocation: m.keyURL, URLList: urls})
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
			result.RetryAfter = maxDuration(result.RetryAfter, parseRetryAfter(response.Header.Get("Retry-After")))
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
			delay := time.Duration(1<<(attempt-1)) * 250 * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-m.stop:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return result, context.Canceled
			}
		}
	}
	return result, lastErr
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := time.Until(when)
	if delay < 0 {
		return 0
	}
	return delay
}

func maxDuration(left, right time.Duration) time.Duration {
	if right > left {
		return right
	}
	return left
}
