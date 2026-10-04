package controlv2

import (
	"errors"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

const ReplayWindow = 4
const ReplayRetryAfter = 20 * time.Second

var ErrReplayWindowFull = errors.New("control.v2 replay window full")

type replayItem struct {
	envelope protocolv2.Envelope
	key      string
	sentAt   time.Time // Zero until the writer actually sends it.
}

// EnqueueReplay bounds unacknowledged messages per type so a traffic backlog
// cannot starve task results or manifests. Entries belong to this queue/session.
func (q *Queue) EnqueueReplay(e protocolv2.Envelope, key string) error {
	q.replayMu.Lock()
	defer q.replayMu.Unlock()
	if old, ok := q.replay[e.ID]; ok {
		if old.sentAt.IsZero() || time.Since(old.sentAt) < ReplayRetryAfter {
			return nil
		}
		// Preserve the original payload across retries.
		e, key = old.envelope, old.key
	} else {
		count := 0
		for _, item := range q.replay {
			if item.envelope.Type == e.Type {
				count++
			}
		}
		if count >= ReplayWindow {
			return ErrReplayWindowFull
		}
	}
	if err := q.Enqueue(e, key); err != nil {
		return err
	}
	q.replay[e.ID] = replayItem{envelope: e, key: key}
	return nil
}

func (q *Queue) ReplaySent(id string) {
	q.replayMu.Lock()
	defer q.replayMu.Unlock()
	if item, ok := q.replay[id]; ok {
		item.sentAt = time.Now()
		q.replay[id] = item
	}
}

// Acknowledge is called only after validating and persisting the business ACK.
func (q *Queue) Acknowledge(id string) {
	q.replayMu.Lock()
	_, present := q.replay[id]
	delete(q.replay, id)
	q.replayMu.Unlock()
	if present {
		select {
		case q.replayWake <- struct{}{}:
		default:
		}
	}
}

func (q *Queue) ReplayWake() <-chan struct{} { return q.replayWake }

func (q *Queue) ReplayKeys(messageType string) []string {
	q.replayMu.Lock()
	defer q.replayMu.Unlock()
	var ids []string
	for _, item := range q.replay {
		if item.envelope.Type == messageType {
			ids = append(ids, item.key)
		}
	}
	return ids
}

// RetryReplay does not reload mutable domain state or hold a database cursor.
func (q *Queue) RetryReplay() error {
	q.replayMu.Lock()
	defer q.replayMu.Unlock()
	for id, item := range q.replay {
		if item.sentAt.IsZero() || time.Since(item.sentAt) < ReplayRetryAfter {
			continue
		}
		if err := q.Enqueue(item.envelope, item.key); err != nil {
			return err
		}
		item.sentAt = time.Time{}
		q.replay[id] = item
	}
	return nil
}

// SerializeDispatch prevents independent wakeups selecting the same domain
// row before its message has been admitted to the session's replay window.
func (q *Queue) Dispatch(fn func() error) error {
	q.dispatchMu.Lock()
	defer q.dispatchMu.Unlock()
	return fn()
}
