package controlv2

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestReplayDeduplicatesConcurrentWakeupsAndRetriesOriginalPayload(t *testing.T) {
	q := NewQueue(32, 1<<20)
	defer q.Close()
	e, _ := protocolv2.New(protocolv2.TypeTrafficEvent, "event-1", map[string]int{"bytes": 7})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := q.EnqueueReplay(e, ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if count, _ := q.Stats(); count != 1 {
		t.Fatalf("duplicate wakeups queued %d copies", count)
	}
	first, err := q.Dequeue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	q.ReplaySent(first.ID)
	if err := q.RetryReplay(); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 0 {
		t.Fatal("retry occurred before ACK timeout")
	}
	q.replayMu.Lock()
	item := q.replay[first.ID]
	item.sentAt = time.Now().Add(-ReplayRetryAfter - time.Second)
	q.replay[first.ID] = item
	q.replayMu.Unlock()
	changed := e
	changed.Payload = []byte(`{"bytes":99}`)
	if err := q.EnqueueReplay(changed, ""); err != nil {
		t.Fatal(err)
	}
	if err := q.RetryReplay(); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 1 {
		t.Fatalf("timeout duplicated retry: %d", count)
	}
	retried, err := q.Dequeue(context.Background())
	if err != nil || string(retried.Payload) != string(first.Payload) {
		t.Fatalf("retry changed identity payload: %+v %v", retried, err)
	}
	q.Acknowledge(first.ID)
	select {
	case <-q.ReplayWake():
	default:
		t.Fatal("ACK did not wake refill")
	}
}

func TestReplayQueueFailureDoesNotReserveWindowAndTypesAreIndependent(t *testing.T) {
	q := NewQueue(1, 1<<20)
	defer q.Close()
	e, _ := protocolv2.New(protocolv2.TypeTrafficEvent, "one", struct{}{})
	if err := q.EnqueueReplay(e, ""); err != nil {
		t.Fatal(err)
	}
	e.ID = "two"
	if err := q.EnqueueReplay(e, ""); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue error=%v", err)
	}
	if _, tracked := q.ReplayEnvelope("two"); tracked {
		t.Fatal("failed enqueue consumed window")
	}
	q2 := NewQueue(32, 1<<20)
	defer q2.Close()
	for _, kind := range []string{protocolv2.TypeTrafficEvent, protocolv2.TypeSyncResult} {
		for i := 0; i < ReplayWindow; i++ {
			e, _ := protocolv2.New(kind, kind+string(rune('a'+i)), struct{}{})
			if err := q2.EnqueueReplay(e, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	e, _ = protocolv2.New(protocolv2.TypeTrafficEvent, "overflow", struct{}{})
	if err := q2.EnqueueReplay(e, ""); !errors.Is(err, ErrReplayWindowFull) {
		t.Fatalf("window error=%v", err)
	}
}
