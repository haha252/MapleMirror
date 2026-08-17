package indexnow

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

func (m *Manager) run() {
	defer close(m.done)
	var timer *time.Timer
	var timerC <-chan time.Time
	startTimer := func(delay time.Duration) {
		if delay <= 0 {
			return
		}
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(delay)
		timerC = timer.C
	}
	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer = nil
		timerC = nil
	}
	for {
		select {
		case <-m.stop:
			stopTimer()
			return
		case <-m.wake:
			if timerC == nil {
				startTimer(m.debounce)
			}
		case <-m.flushNow:
			stopTimer()
			if delay := m.flushPending(); delay > 0 {
				startTimer(delay)
			}
		case <-timerC:
			stopTimer()
			if delay := m.flushPending(); delay > 0 {
				startTimer(delay)
			} else if m.hasPending() {
				startTimer(m.debounce)
			}
		}
	}
}

func (m *Manager) flushPending() time.Duration {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	for {
		batch := m.takeBatch()
		if len(batch) == 0 {
			return 0
		}
		result, err := m.submitWithRetry(batch)
		if err != nil {
			var permanent *permanentSubmissionError
			if errors.As(err, &permanent) {
				if stateErr := m.recordPermanentFailure(batch, err); stateErr != nil {
					m.requeue(batch)
					attrs := submissionAttrs(len(batch), result, true)
					attrs = append(attrs, slog.String("failure_kind", "state_write_error"),
						slog.String("error", stateErr.Error()))
					m.logError(context.Background(), "IndexNow state 写入失败，批次已重新排队", attrs...)
					return retryDelay(result)
				}
				m.logSubmissionFailure(batch, result, err, false)
				continue
			}
			m.requeue(batch)
			m.logSubmissionFailure(batch, result, err, true)
			return retryDelay(result)
		}
		if stateErr := m.markSubmitted(batch); stateErr != nil {
			m.requeue(batch)
			attrs := submissionAttrs(len(batch), result, true)
			attrs = append(attrs, slog.String("failure_kind", "state_write_error"),
				slog.String("error", stateErr.Error()))
			m.logError(context.Background(), "IndexNow state 写入失败，批次已重新排队", attrs...)
			return retryDelay(result)
		}
		m.logInfo(context.Background(), "IndexNow URL 提交成功",
			submissionAttrs(len(batch), result, false)...)
	}
}

func retryDelay(result submissionResult) time.Duration {
	minimum := 5 * time.Minute
	if result.RetryAfter > minimum {
		return result.RetryAfter
	}
	return minimum
}

func (m *Manager) signalWake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) signalNow() {
	select {
	case m.flushNow <- struct{}{}:
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
