package logging

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

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
