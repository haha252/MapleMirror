package controlv2

import protocolv2 "mirror-server/internal/protocol/v2"

func (q *Queue) ReplayEnvelope(id string) (protocolv2.Envelope, bool) {
	q.replayMu.Lock()
	defer q.replayMu.Unlock()
	item, ok := q.replay[id]
	return item.envelope, ok
}

// Receipts retain only bounded correlation hashes, never large payloads or
// authorization tokens. Business handlers validate identity before recording.
func (q *Queue) CompletedACK(replyTo, identity string) bool {
	q.replayMu.Lock()
	defer q.replayMu.Unlock()
	return q.receipts[replyTo] == identity
}

func (q *Queue) CompleteACK(replyTo, identity string) {
	q.replayMu.Lock()
	if _, exists := q.receipts[replyTo]; !exists {
		if len(q.receiptOrder) == 128 {
			delete(q.receipts, q.receiptOrder[0])
			q.receiptOrder = q.receiptOrder[1:]
		}
		q.receiptOrder = append(q.receiptOrder, replyTo)
	}
	q.receipts[replyTo] = identity
	q.replayMu.Unlock()
	q.Acknowledge(replyTo)
}
