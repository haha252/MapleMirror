package accountingarchive

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Maintain 压缩已经关闭的按日 JSONL，并清理超过统一日志保留期的文件。
func Maintain(root string, retentionDays int, now time.Time) error {
	if root == "" || retentionDays <= 0 {
		return nil
	}
	today := now.Format("2006-01-02")
	cutoff := now.AddDate(0, 0, -retentionDays)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		compressed := strings.HasSuffix(name, ".jsonl.gz")
		dateText := strings.TrimSuffix(strings.TrimSuffix(name, ".gz"), ".jsonl")
		if len(dateText) != 10 || (!compressed && !strings.HasSuffix(name, ".jsonl")) {
			return nil
		}
		date, parseErr := time.ParseInLocation("2006-01-02", dateText, now.Location())
		if parseErr != nil {
			return nil
		}
		if date.Before(cutoff) {
			return os.Remove(path)
		}
		if !compressed && dateText != today {
			return compressArchive(path)
		}
		return nil
	})
}

func compressArchive(path string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	tmp := path + ".gz.tmp"
	output, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	gz, err := gzip.NewWriterLevel(output, gzip.BestSpeed)
	if err == nil {
		_, err = io.Copy(gz, input)
	}
	if closeErr := gz.Close(); err == nil {
		err = closeErr
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("压缩归档失败：%w", err)
	}
	if err := os.Rename(tmp, path+".gz"); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(path)
}
