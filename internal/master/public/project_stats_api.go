package public

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const projectStatsCacheCapacity = 256

type projectStatsCache struct {
	mu      sync.Mutex
	entries map[string]projectStatsCacheEntry
}

type projectStatsCacheEntry struct {
	touched time.Time
	value   *cachedStatsValue[projectStatsSnapshot]
}

func (c *projectStatsCache) get(ctx context.Context, store Store, projectID string) (projectStatsSnapshot, error) {
	now := timeNow()
	key := projectID + "\x00" + statDay(now, store.Location)
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]projectStatsCacheEntry)
	}
	for id, entry := range c.entries {
		if now.Sub(entry.touched) >= time.Minute {
			delete(c.entries, id)
		}
	}
	entry, ok := c.entries[key]
	if !ok {
		if len(c.entries) >= projectStatsCacheCapacity {
			var oldest string
			var oldestTime time.Time
			for id, item := range c.entries {
				if oldestTime.IsZero() || item.touched.Before(oldestTime) {
					oldest, oldestTime = id, item.touched
				}
			}
			delete(c.entries, oldest)
		}
		entry.value = &cachedStatsValue[projectStatsSnapshot]{}
	}
	entry.touched = now
	c.entries[key] = entry
	c.mu.Unlock()
	return entry.value.get(ctx, statsFastCacheTTL, func(ctx context.Context) (projectStatsSnapshot, error) {
		return store.projectStatsAt(ctx, projectID, now)
	})
}

func (s Server) projectResource(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/stats") {
		s.projectStatsAPI(w, r)
		return
	}
	s.projectAssets(w, r)
}

func (s Server) projectStatsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/public/v1/projects/")
	projectID, ok := strings.CutSuffix(path, "/stats")
	if !ok || strings.TrimSpace(projectID) == "" || strings.Contains(projectID, "/") {
		writeError(w, r, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在")
		return
	}
	// Recheck visibility even on cache hits so disabled projects stop being public immediately.
	readCtx, cancel := stableDatabaseReadContext(r.Context())
	defer cancel()
	var visible string
	err := s.Store.DB.QueryRowContext(readCtx, `SELECT id FROM projects WHERE id = ? AND enabled = 1`, projectID).Scan(&visible)
	if err != nil {
		s.writeProjectStatsError(w, r, err)
		return
	}
	var snapshot projectStatsSnapshot
	if s.ProjectStatsCache == nil {
		snapshot, err = s.Store.ProjectStats(readCtx, projectID)
	} else {
		snapshot, err = s.ProjectStatsCache.get(readCtx, s.Store, projectID)
	}
	if err != nil {
		s.writeProjectStatsError(w, r, err)
		return
	}
	writeOK(w, r, http.StatusOK, "查询成功", snapshot)
}

func (s Server) writeProjectStatsError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在")
	} else {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "项目统计读取失败")
	}
}
