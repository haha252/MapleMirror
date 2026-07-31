package powtelemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Record struct {
	SchemaVersion       int      `json:"schema_version"`
	RecordedAt          string   `json:"recorded_at"`
	RequestID           string   `json:"request_id"`
	AuthorizationID     string   `json:"authorization_id"`
	ChallengeID         string   `json:"challenge_id"`
	AssetID             string   `json:"asset_id"`
	AssetSizeBytes      int64    `json:"asset_size_bytes"`
	SourceKind          string   `json:"source_kind"`
	ProtocolVersion     string   `json:"protocol_version"`
	PoWAlgorithm        string   `json:"pow_algorithm"`
	PoWIterations       uint64   `json:"pow_iterations"`
	PoWMultiplier       int      `json:"pow_multiplier"`
	SolveElapsedMS      *int64   `json:"solve_elapsed_ms,omitempty"`
	ChallengeAgeMS      int64    `json:"challenge_age_ms"`
	UserAgent           string   `json:"user_agent,omitempty"`
	Platform            string   `json:"platform,omitempty"`
	HardwareConcurrency *int     `json:"hardware_concurrency,omitempty"`
	DeviceMemoryGiB     *float64 `json:"device_memory_gib,omitempty"`
}

type Writer struct {
	mu        sync.Mutex
	directory string
	retention int
	location  *time.Location
	now       func() time.Time
	date      string
	file      *os.File
}

func New(directory string, retention int, location *time.Location) (*Writer, error) {
	return NewWithClock(directory, retention, location, time.Now)
}

func NewWithClock(directory string, retention int, location *time.Location,
	now func() time.Time) (*Writer, error) {
	if location == nil {
		location = time.UTC
	}
	if now == nil {
		now = time.Now
	}
	if retention <= 0 {
		return nil, fmt.Errorf("PoW 遥测保留天数必须大于零")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("创建 PoW 遥测目录失败：%w", err)
	}
	return &Writer{directory: directory, retention: retention, location: location, now: now}, nil
}

func (w *Writer) Write(record Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now().In(w.location)
	date := now.Format("2006-01-02")
	if w.file == nil || w.date != date {
		if err := w.rotate(date, now); err != nil {
			return err
		}
	}
	record.SchemaVersion = 1
	record.RecordedAt = now.Format(time.RFC3339Nano)
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("编码 PoW 遥测失败：%w", err)
	}
	line = append(line, '\n')
	if _, err := w.file.Write(line); err != nil {
		return fmt.Errorf("写入 PoW 遥测失败：%w", err)
	}
	return nil
}

func (w *Writer) rotate(date string, now time.Time) error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	path := filepath.Join(w.directory, date+".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("打开 PoW 遥测日志失败：%w", err)
	}
	w.file, w.date = file, date
	w.cleanup(now)
	return nil
}

func (w *Writer) cleanup(now time.Time) {
	entries, err := os.ReadDir(w.directory)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -w.retention)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(name, ".jsonl"), w.location)
		if err == nil && date.Before(cutoff) {
			_ = os.Remove(filepath.Join(w.directory, name))
		}
	}
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
