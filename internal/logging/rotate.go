package logging

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const logDateLayout = "2006-01-02"

const defaultMaintenanceRetryDelay = 5 * time.Minute

type dailyWriter struct {
	mu              sync.Mutex
	directory       string
	component       string
	retention       int
	location        *time.Location
	date            string
	file            *os.File
	maintenanceWake chan struct{}
	stop            chan struct{}
	done            chan struct{}
	closeOnce       sync.Once
	compress        func(string) error
	errorOutput     io.Writer
	retryDelay      time.Duration
}

func newDailyWriter(directory, component string, retention int, location *time.Location) (*dailyWriter, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败：%w", err)
	}
	w := &dailyWriter{
		directory:       directory,
		component:       component,
		retention:       retention,
		location:        location,
		maintenanceWake: make(chan struct{}, 1),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
		compress:        compressLogFile,
		errorOutput:     os.Stderr,
		retryDelay:      defaultMaintenanceRetryDelay,
	}
	go w.runMaintenanceLoop()
	w.requestMaintenance()
	return w, nil
}

func (w *dailyWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	now := time.Now().In(w.location)
	date := now.Format(logDateLayout)
	rotated := false
	if w.file == nil || date != w.date {
		if err := w.rotate(date); err != nil {
			w.mu.Unlock()
			return 0, err
		}
		rotated = true
	}
	n, err := w.file.Write(data)
	w.mu.Unlock()
	if rotated {
		w.requestMaintenance()
	}
	return n, err
}

func (w *dailyWriter) Close() error {
	w.closeOnce.Do(func() {
		close(w.stop)
		<-w.done
	})
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyWriter) rotate(date string) error {
	if w.file != nil {
		_ = w.file.Close()
	}
	path := filepath.Join(w.directory, w.component+"-"+date+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开日志文件失败：%w", err)
	}
	w.file, w.date = file, date
	return nil
}

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
		if !logFile.date.Before(currentDate) {
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
	date, err := time.ParseInLocation(logDateLayout, dateText, w.location)
	if err != nil {
		return parsedLogFile{}, false
	}
	return parsedLogFile{date: date, compressed: compressed}, true
}

func compressLogFile(path string) error {
	gzipPath := path + ".gz"
	if _, err := os.Stat(gzipPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查压缩日志失败：%w", err)
	}
	tempPath := gzipPath + ".tmp"
	input, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开待压缩日志失败：%w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建压缩日志失败：%w", err)
	}
	gzipWriter, err := gzip.NewWriterLevel(output, gzip.BestSpeed)
	if err != nil {
		_ = output.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("创建 gzip 压缩器失败：%w", err)
	}
	_, copyErr := io.Copy(gzipWriter, input)
	closeGzipErr := gzipWriter.Close()
	closeOutputErr := output.Close()
	if copyErr != nil || closeGzipErr != nil || closeOutputErr != nil {
		_ = os.Remove(tempPath)
		if copyErr != nil {
			return fmt.Errorf("压缩日志失败：%w", copyErr)
		}
		if closeGzipErr != nil {
			return fmt.Errorf("关闭压缩日志失败：%w", closeGzipErr)
		}
		return fmt.Errorf("写入压缩日志失败：%w", closeOutputErr)
	}
	if err := os.Rename(tempPath, gzipPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("保存压缩日志失败：%w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("删除原始日志失败：%w", err)
	}
	return nil
}
