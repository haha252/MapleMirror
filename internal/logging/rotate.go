package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	size            int64
	maxFileSize     int64
	maintenanceWake chan struct{}
	stop            chan struct{}
	done            chan struct{}
	closeOnce       sync.Once
	compress        func(string) error
	errorOutput     io.Writer
	retryDelay      time.Duration
}

func newDailyWriter(directory, component string, retention int, location *time.Location,
	maxFileSize int64) (*dailyWriter, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败：%w", err)
	}
	w := &dailyWriter{
		directory:       directory,
		component:       component,
		retention:       retention,
		location:        location,
		maxFileSize:     maxFileSize,
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
	if w.exceedsSizeLimit(len(data)) {
		if err := w.rotateBySize(date); err != nil {
			w.mu.Unlock()
			return 0, err
		}
		rotated = true
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
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
	w.size = 0
	return err
}

func (w *dailyWriter) rotate(date string) error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
		w.size = 0
	}
	path := filepath.Join(w.directory, w.component+"-"+date+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开日志文件失败：%w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("读取日志文件大小失败：%w", err)
	}
	w.file, w.date, w.size = file, date, info.Size()
	return nil
}

func (w *dailyWriter) exceedsSizeLimit(incoming int) bool {
	if w.maxFileSize <= 0 || w.size == 0 {
		return false
	}
	return w.size >= w.maxFileSize || int64(incoming) > w.maxFileSize-w.size
}

func (w *dailyWriter) rotateBySize(date string) error {
	if w.file == nil {
		return nil
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return fmt.Errorf("关闭已满日志文件失败：%w", err)
	}
	w.file = nil
	activePath := filepath.Join(w.directory, w.component+"-"+date+".log")
	segmentPath, err := w.nextSegmentPath(date)
	if err != nil {
		_ = w.rotate(date)
		return err
	}
	if err := os.Rename(activePath, segmentPath); err != nil {
		_ = w.rotate(date)
		return fmt.Errorf("封存已满日志文件失败：%w", err)
	}
	w.size = 0
	if err := w.rotate(date); err != nil {
		return err
	}
	return nil
}

func (w *dailyWriter) nextSegmentPath(date string) (string, error) {
	for sequence := 1; sequence <= 999_999; sequence++ {
		name := fmt.Sprintf("%s-%s.%03d.log", w.component, date, sequence)
		path := filepath.Join(w.directory, name)
		available, err := logSegmentAvailable(path)
		if err != nil {
			return "", err
		}
		if available {
			return path, nil
		}
	}
	return "", errors.New("当日日志分片数量超过上限")
}

func logSegmentAvailable(path string) (bool, error) {
	for _, candidate := range []string{path, path + ".gz", path + ".gz.tmp"} {
		if _, err := os.Stat(candidate); err == nil {
			return false, nil
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("检查日志分片失败：%w", err)
		}
	}
	return true, nil
}
