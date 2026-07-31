package powtelemetry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecordSchemaHasNoSensitivePayloadFields(t *testing.T) {
	encoded, err := json.Marshal(Record{RequestID: "req", ChallengeID: "challenge"})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"solution", "expected", "trapdoor", "cookie", "download_token", "request_body", "modulus"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("sensitive field %q in %s", forbidden, encoded)
		}
	}
}

func TestWriterRotatesValidJSONAndCleansRetention(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "pow")
	location := time.FixedZone("BJT", 8*60*60)
	now := time.Date(2026, 8, 1, 23, 59, 0, 0, location)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(directory, "2026-07-01.jsonl")
	if err := os.WriteFile(old, []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	writer, err := NewWithClock(directory, 7, location, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	record := Record{RequestID: "req-1", ChallengeID: "challenge-1", PoWAlgorithm: "rsa-repeated-squaring-v1"}
	if err := writer.Write(record); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := writer.Write(record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired file should be removed: %v", err)
	}
	for _, date := range []string{"2026-08-01", "2026-08-02"} {
		assertJSONLines(t, filepath.Join(directory, date+".jsonl"), 1)
	}
}

func TestWriterSerializesConcurrentRecords(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "pow")
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	writer, err := NewWithClock(directory, 30, time.UTC, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var group sync.WaitGroup
	for index := 0; index < 20; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := writer.Write(Record{ChallengeID: "safe"}); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	assertJSONLines(t, filepath.Join(directory, "2026-08-01.jsonl"), 20)
}

func assertJSONLines(t *testing.T, path string, want int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid JSONL: %v", err)
		}
		if record["schema_version"] != float64(1) {
			t.Fatalf("schema version missing: %+v", record)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("line count=%d want=%d", count, want)
	}
}
