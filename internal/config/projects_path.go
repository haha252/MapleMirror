package config

import (
	"errors"
	"path/filepath"
	"strings"
)

func resolveProjectIconPath(baseDir, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	trimmed := strings.TrimSpace(value)
	clean := filepath.Clean(trimmed)
	if filepath.IsAbs(clean) || isWindowsAbsolutePath(trimmed) {
		return "", errors.New("项目 icon_path 必须使用相对路径")
	}
	if clean == "." || clean == "" {
		return "", errors.New("项目 icon_path 不能为空路径")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("项目 icon_path 不得越级访问配置目录")
	}
	switch strings.ToLower(filepath.Ext(clean)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
	default:
		return "", errors.New("项目 icon_path 只允许 png、jpg、jpeg、gif、webp、svg 图片")
	}
	return filepath.Join(baseDir, clean), nil
}

func isWindowsAbsolutePath(value string) bool {
	if len(value) >= 2 && value[1] == ':' {
		drive := value[0]
		if ('A' <= drive && drive <= 'Z') || ('a' <= drive && drive <= 'z') {
			return true
		}
	}
	return strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`)
}
