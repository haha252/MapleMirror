package controlv2

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	protocolv2 "mirror-server/internal/protocol/v2"
)

var (
	ErrQueueFull   = errors.New("control.v2 outbound queue full")
	ErrQueueClosed = errors.New("control.v2 outbound queue closed")
)

const defaultHighPriorityBurst = 32

type queuedItem struct {
	envelope    protocolv2.Envelope
	priority    uint8
	coalesceKey string
	size        int
	order       uint64
}

type Queue struct {
	dispatchMu   sync.Mutex
	replayMu     sync.Mutex
	replay       map[string]replayItem
	replayWake   chan struct{}
	receipts     map[string]string
	receiptOrder []string
	mu           sync.Mutex
	buckets      [protocolv2.PriorityLowest + 1][]*queuedItem
	coalesced    map[string]*queuedItem
	messages     int
	bytes        int
	maxMsg       int
	maxBytes     int
	nextOrder    uint64
	burst        int
	burstMax     int
	closed       bool
	notify       chan struct{}
}

func NewQueue(maxMessages, maxBytes int) *Queue {
	if maxMessages <= 0 {
		maxMessages = 512
	}
	if maxBytes <= 0 {
		maxBytes = 4 << 20
	}
	return &Queue{
		replay: make(map[string]replayItem), replayWake: make(chan struct{}, 1), receipts: make(map[string]string),
		coalesced: make(map[string]*queuedItem), maxMsg: maxMessages, maxBytes: maxBytes,
		burstMax: defaultHighPriorityBurst, notify: make(chan struct{}, 1),
	}
}

func (q *Queue) Enqueue(envelope protocolv2.Envelope, coalesceKey string) error {
	priority, err := protocolv2.Priority(envelope.Type)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if len(encoded) > protocolv2.MaxMessageBytes {
		return errors.New("control.v2 message exceeds hard limit")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrQueueClosed
	}
	if coalesceKey != "" {
		spec, _ := protocolv2.Spec(envelope.Type)
		if !spec.Coalescible {
			return errors.New("message type is not coalescible")
		}
		mapKey := envelope.Type + "\x00" + coalesceKey
		if existing := q.coalesced[mapKey]; existing != nil {
			nextBytes := q.bytes - existing.size + len(encoded)
			if nextBytes > q.maxBytes {
				return ErrQueueFull
			}
			q.bytes = nextBytes
			existing.envelope = envelope
			existing.size = len(encoded)
			q.signalLocked()
			return nil
		}
	}
	if q.messages+1 > q.maxMsg || q.bytes+len(encoded) > q.maxBytes {
		return ErrQueueFull
	}
	q.nextOrder++
	item := &queuedItem{envelope: envelope, priority: priority, coalesceKey: coalesceKey, size: len(encoded), order: q.nextOrder}
	q.buckets[priority] = append(q.buckets[priority], item)
	q.messages++
	q.bytes += item.size
	if coalesceKey != "" {
		q.coalesced[envelope.Type+"\x00"+coalesceKey] = item
	}
	q.signalLocked()
	return nil
}

func (q *Queue) Dequeue(ctx context.Context) (protocolv2.Envelope, error) {
	for {
		q.mu.Lock()
		if item := q.takeLocked(); item != nil {
			q.mu.Unlock()
			return item.envelope, nil
		}
		if q.closed {
			q.mu.Unlock()
			return protocolv2.Envelope{}, ErrQueueClosed
		}
		notify := q.notify
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return protocolv2.Envelope{}, ctx.Err()
		case <-notify:
		}
	}
}

func (q *Queue) takeLocked() *queuedItem {
	if q.messages == 0 {
		return nil
	}
	priority := q.firstPriorityLocked()
	if q.burst >= q.burstMax {
		if oldest := q.oldestPriorityLocked(); oldest != 0 && oldest != priority {
			priority = oldest
			q.burst = 0
		}
	}
	bucket := q.buckets[priority]
	item := bucket[0]
	q.buckets[priority] = bucket[1:]
	q.messages--
	q.bytes -= item.size
	if item.coalesceKey != "" {
		delete(q.coalesced, item.envelope.Type+"\x00"+item.coalesceKey)
	}
	if priority <= 25 {
		q.burst++
	} else {
		q.burst = 0
	}
	if q.messages > 0 {
		q.signalLocked()
	}
	return item
}

func (q *Queue) firstPriorityLocked() uint8 {
	for p := uint8(protocolv2.PriorityHighest); p <= uint8(protocolv2.PriorityLowest); p++ {
		if len(q.buckets[p]) > 0 {
			return p
		}
	}
	return 0
}

func (q *Queue) oldestPriorityLocked() uint8 {
	var selected uint8
	var oldest uint64
	for p := uint8(protocolv2.PriorityHighest); p <= uint8(protocolv2.PriorityLowest); p++ {
		if len(q.buckets[p]) == 0 {
			continue
		}
		order := q.buckets[p][0].order
		if selected == 0 || order < oldest {
			selected, oldest = p, order
		}
	}
	return selected
}

func (q *Queue) Stats() (messages, bytes int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.messages, q.bytes
}

func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.signalLocked()
	q.mu.Unlock()
}

func (q *Queue) signalLocked() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
