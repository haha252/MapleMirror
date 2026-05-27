package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type dailyWriter struct {
	mu        sync.Mutex
	directory string
	component string
	retention int
	location  *time.Location
	date      string
	file      *os.File
}

func newDailyWriter(directory, component string, retention int, location *time.Location) (*dailyWriter, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败：%w", err)
	}
	w := &dailyWriter{directory: directory, component: component, retention: retention, location: location}
	if err := w.cleanup(time.Now().In(location)); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now().In(w.location)
	date := now.Format("2006-01-02")
	if w.file == nil || date != w.date {
		if err := w.rotate(date, now); err != nil {
			return 0, err
		}
	}
	return w.file.Write(data)
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *dailyWriter) rotate(date string, now time.Time) error {
	if w.file != nil {
		_ = w.file.Close()
	}
	path := filepath.Join(w.directory, w.component+"-"+date+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开日志文件失败：%w", err)
	}
	w.file, w.date = file, date
	return w.cleanup(now)
}

func (w *dailyWriter) cleanup(now time.Time) error {
	entries, err := os.ReadDir(w.directory)
	if err != nil {
		return fmt.Errorf("读取日志目录失败：%w", err)
	}
	cutoff := now.AddDate(0, 0, -w.retention)
	prefix := w.component + "-"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".log")
		date, err := time.ParseInLocation("2006-01-02", dateText, w.location)
		if err == nil && date.Before(cutoff) {
			if err := os.Remove(filepath.Join(w.directory, name)); err != nil {
				return fmt.Errorf("清理过期日志失败：%w", err)
			}
		}
	}
	return nil
}
