package files

import (
	"sync"
	"testing"

	"mirror-server/internal/downloadtoken"
)

func TestRecordTrafficAllocatesUniqueSequencesConcurrently(t *testing.T) {
	db, _, _ := prepareNodeFile(t)
	handler := &Handler{DB: db}
	claims := downloadtoken.Claims{AuthorizationID: "auth-1", RequestID: "master-req-1"}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- handler.recordTraffic(claims, "asset-1", "node-req-1", 1)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var rows, distinctSeq, totalBytes int64
	err := db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT event_sequence),
		COALESCE(SUM(sent_bytes), 0) FROM pending_traffic_events`).
		Scan(&rows, &distinctSeq, &totalBytes)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 20 || distinctSeq != 20 || totalBytes != 20 {
		t.Fatalf("并发流量事件序号不唯一：rows=%d distinct=%d bytes=%d", rows, distinctSeq, totalBytes)
	}
}
