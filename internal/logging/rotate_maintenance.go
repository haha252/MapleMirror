package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (w *dailyWriter) runMaintenanceLoop() {
	defer close(w.done)
	retryPending := false
	for {
		now := time.Now().In(w.location)
		delay := nextDailyMaintenanceDelay(now)
		if retryPending && w.maintenanceRetryDelay() < delay {
			delay = w.maintenanceRetryDelay()
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-w.maintenanceWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-w.stop:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
		if err := w.maintain(time.Now().In(w.location)); err != nil {
			retryPending = true
			w.reportMaintenanceError(err)
		} else {
			retryPending = false
		}
	}
}

func (w *dailyWriter) maintenanceRetryDelay() time.Duration {
	if w.retryDelay > 0 {
		return w.retryDelay
	}
	return defaultMaintenanceRetryDelay
}

func (w *dailyWriter) reportMaintenanceError(err error) {
	output := w.errorOutput
	if output == nil {
		output = os.Stderr
	}
	fmt.Fprintf(output, "日志后台维护失败，将在 %s 后重试：%v\n", w.maintenanceRetryDelay(), err)
}

func (w *dailyWriter) requestMaintenance() {
	select {
	case w.maintenanceWake <- struct{}{}:
	default:
	}
}

func nextDailyMaintenanceDelay(now time.Time) time.Duration {
	nextDay := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 1, 0, 0, now.Location())
	return nextDay.Sub(now)
}

func (w *dailyWriter) maintain(now time.Time) error {
	w.mu.Lock()
	date := now.Format(logDateLayout)
	if w.file != nil && date != w.date {
		_ = w.file.Close()
		w.file = nil
		w.date = ""
		w.size = 0
	}
	w.mu.Unlock()
	if err := w.compressClosedLogs(now); err != nil {
		return err
	}
	return w.cleanup(now)
}

func (w *dailyWriter) compressClosedLogs(now time.Time) error {
	entries, err := os.ReadDir(w.directory)
	if err != nil {
		return fmt.Errorf("读取日志目录失败：%w", err)
	}
	currentDate, err := time.ParseInLocation(logDateLayout, now.Format(logDateLayout), w.location)
	if err != nil {
		return fmt.Errorf("解析当前日志日期失败：%w", err)
	}
	for _, entry := range entries {
		logFile, ok := w.parseLogFile(entry.Name())
		if entry.IsDir() || !ok || logFile.compressed {
			continue
		}
		if logFile.segment == 0 && !logFile.date.Before(currentDate) {
			continue
		}
		path := filepath.Join(w.directory, entry.Name())
		compress := w.compress
		if compress == nil {
			compress = compressLogFile
		}
		if err := compress(path); err != nil {
			return err
		}
	}
	return nil
}

func (w *dailyWriter) cleanup(now time.Time) error {
	entries, err := os.ReadDir(w.directory)
	if err != nil {
		return fmt.Errorf("读取日志目录失败：%w", err)
	}
	cutoff := now.AddDate(0, 0, -w.retention)
	for _, entry := range entries {
		logFile, ok := w.parseLogFile(entry.Name())
		if entry.IsDir() || !ok {
			continue
		}
		if logFile.date.Before(cutoff) {
			if err := os.Remove(filepath.Join(w.directory, entry.Name())); err != nil {
				return fmt.Errorf("清理过期日志失败：%w", err)
			}
		}
	}
	return nil
}

type parsedLogFile struct {
	date       time.Time
	compressed bool
	segment    int
}

func (w *dailyWriter) parseLogFile(name string) (parsedLogFile, bool) {
	prefix := w.component + "-"
	if !strings.HasPrefix(name, prefix) {
		return parsedLogFile{}, false
	}
	compressed := false
	dateText := strings.TrimPrefix(name, prefix)
	if strings.HasSuffix(dateText, ".log.gz") {
		compressed = true
		dateText = strings.TrimSuffix(dateText, ".log.gz")
	} else if strings.HasSuffix(dateText, ".log") {
		dateText = strings.TrimSuffix(dateText, ".log")
	} else {
		return parsedLogFile{}, false
	}
	parts := strings.Split(dateText, ".")
	if len(parts) > 2 || len(parts) == 0 {
		return parsedLogFile{}, false
	}
	segment := 0
	if len(parts) == 2 {
		parsedSegment, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil || parsedSegment <= 0 {
			return parsedLogFile{}, false
		}
		segment = parsedSegment
	}
	date, err := time.ParseInLocation(logDateLayout, parts[0], w.location)
	if err != nil {
		return parsedLogFile{}, false
	}
	return parsedLogFile{date: date, compressed: compressed, segment: segment}, true
}
