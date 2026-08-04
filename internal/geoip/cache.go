package geoip

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

func (m *Manager) loadCache() {
	if m.cachePath == "" {
		return
	}
	data, err := os.ReadFile(m.cachePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			m.warn("读取国家 IP 数据库缓存失败", slog.String("path", m.cachePath), slog.String("error", err.Error()))
		}
		return
	}
	var doc cacheDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		m.warn("国家 IP 数据库缓存格式无效", slog.String("path", m.cachePath), slog.String("error", err.Error()))
		return
	}
	if doc.Version != cacheDocumentVersion {
		m.warn("国家 IP 数据库缓存版本无效", slog.String("path", m.cachePath),
			slog.Int("version", doc.Version))
		return
	}
	_, snap, err := buildSnapshot(doc.Prefixes)
	if err != nil {
		m.warn("国家 IP 数据库缓存校验失败", slog.String("path", m.cachePath), slog.String("error", err.Error()))
		return
	}
	snap.updatedAt = doc.UpdatedAt
	m.current.Store(&snap)
	m.info("已加载国家 IP 数据库缓存", slog.String("path", m.cachePath),
		slog.String("updated_at", snap.updatedAt), slog.Int("prefixes", len(doc.Prefixes)))
}

func (m *Manager) writeCache(doc cacheDocument) error {
	if m.cachePath == "" {
		return nil
	}
	dir := filepath.Dir(m.cachePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".country-ip-cache-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, m.cachePath)
}
