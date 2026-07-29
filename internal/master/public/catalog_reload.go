package public

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

const catalogReloadInterval = 2 * time.Second

func newCatalogIndex(projects config.Projects, filters config.Filters,
	projectsPath, filtersPath string, cache *catalogResultCache,
	logger *logging.Logger) *catalogIndex {
	index := &catalogIndex{
		snapshot:     newCatalogSnapshot(projects, filters, 1),
		projectsPath: projectsPath, filtersPath: filtersPath,
		cache: cache, logger: logger, stop: make(chan struct{}), done: make(chan struct{}),
	}
	index.observed = catalogSourceFingerprint(projectsPath, filtersPath, projects.ProjectFiles)
	go index.reloadLoop()
	return index
}

func (i *catalogIndex) reloadLoop() {
	ticker := time.NewTicker(catalogReloadInterval)
	defer ticker.Stop()
	defer close(i.done)
	for {
		select {
		case <-ticker.C:
			i.reloadIfChanged()
		case <-i.stop:
			return
		}
	}
}

func (i *catalogIndex) reloadIfChanged() bool {
	current := i.current()
	nextFingerprint := catalogSourceFingerprint(
		i.projectsPath, i.filtersPath, current.ProjectFiles)
	i.mu.Lock()
	if nextFingerprint == i.observed {
		i.mu.Unlock()
		return false
	}
	i.observed = nextFingerprint
	i.mu.Unlock()

	projects, err := config.LoadProjects(i.projectsPath, nil)
	if err != nil {
		i.logReloadFailure("项目标签索引热重载失败，沿用上一次有效配置", err)
		return false
	}
	filters, err := config.LoadFilters(i.filtersPath, nil)
	if err != nil {
		i.logReloadFailure("筛选器配置热重载失败，沿用上一次有效配置", err)
		return false
	}
	next := newCatalogSnapshot(projects, filters, current.Generation+1)
	i.mu.Lock()
	i.snapshot = next
	i.observed = catalogSourceFingerprint(i.projectsPath, i.filtersPath, next.ProjectFiles)
	i.mu.Unlock()
	if i.cache != nil {
		i.cache.setMaxBytes(filters.CacheBytes)
		i.cache.clear()
	}
	if i.logger != nil {
		i.logger.Info(context.Background(), "首页搜索与筛选索引已热重载",
			slog.Uint64("generation", next.Generation),
			slog.Int("projects", len(next.Projects)),
			slog.Int("selectors", len(next.Filters.Selectors)))
	}
	return true
}

func (i *catalogIndex) logReloadFailure(message string, err error) {
	if i.logger != nil {
		i.logger.Warn(context.Background(), message, slog.String("error", err.Error()))
	}
}

func (i *catalogIndex) close() {
	if i == nil {
		return
	}
	select {
	case <-i.stop:
	default:
		close(i.stop)
	}
	<-i.done
}

func catalogSourceFingerprint(projectsPath, filtersPath string, projectFiles []string) string {
	paths := []string{projectsPath, filtersPath}
	refs, err := config.ResolveProjectFileReferences(projectsPath, projectFiles)
	if err != nil {
		paths = append(paths, "resolve-error:"+err.Error())
	} else {
		for _, ref := range refs {
			paths = append(paths, ref.Path)
		}
	}
	sort.Strings(paths)
	var state strings.Builder
	for _, path := range paths {
		state.WriteString(path)
		info, statErr := os.Stat(path)
		if statErr != nil {
			state.WriteString("|error:")
			state.WriteString(statErr.Error())
		} else {
			state.WriteString(fmt.Sprintf("|%d|%d", info.ModTime().UnixNano(), info.Size()))
		}
		state.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(state.String()))
	return fmt.Sprintf("%x", sum[:])
}
