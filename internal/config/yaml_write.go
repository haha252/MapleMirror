package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func writeRepairedYAML(path string, data []byte, repaired bool) error {
	if !repaired {
		return nil
	}
	if err := replaceConfigFile(path, data); err != nil {
		return fmt.Errorf("补齐配置文件失败：%w", err)
	}
	return nil
}

func replaceConfigFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("写入配置临时文件失败：%w", err)
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入配置临时文件失败：%w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("刷新配置临时文件失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭配置临时文件失败：%w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("替换配置文件失败：%w", err)
	}
	keep = true
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
