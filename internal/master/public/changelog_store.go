package public

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

const changelogReloadInterval = 2 * time.Second

type changelogEntry struct {
	Level           string
	Title           string
	OccurredAt      time.Time
	DescriptionHTML string
	SearchText      string
	SourcePath      string
}

type changelogSnapshot struct {
	Generation uint64
	Entries    []changelogEntry
}

type changelogStore struct {
	mu       sync.RWMutex
	root     string
	observed string
	snapshot *changelogSnapshot
	logger   *logging.Logger
	stop     chan struct{}
	done     chan struct{}
}

func newChangelogStore(root string, logger *logging.Logger) *changelogStore {
	store := &changelogStore{
		root: strings.TrimSpace(root), logger: logger,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	store.observed = changelogFingerprint(store.root)
	store.snapshot = &changelogSnapshot{Generation: 1}
	if events, err := config.LoadChangelog(store.root); err != nil {
		store.logFailure("更新日志初始加载失败，暂时使用空记录", err)
	} else {
		store.snapshot.Entries = buildChangelogEntries(events)
	}
	go store.reloadLoop()
	return store
}

func (s *changelogStore) current() *changelogSnapshot {
	if s == nil {
		return &changelogSnapshot{Generation: 1}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot == nil {
		return &changelogSnapshot{Generation: 1}
	}
	return s.snapshot
}

func (s *changelogStore) reloadLoop() {
	ticker := time.NewTicker(changelogReloadInterval)
	defer ticker.Stop()
	defer close(s.done)
	for {
		select {
		case <-ticker.C:
			s.reloadIfChanged()
		case <-s.stop:
			return
		}
	}
}

func (s *changelogStore) reloadIfChanged() bool {
	fingerprint := changelogFingerprint(s.root)
	s.mu.Lock()
	if fingerprint == s.observed {
		s.mu.Unlock()
		return false
	}
	s.observed = fingerprint
	s.mu.Unlock()
	events, err := config.LoadChangelog(s.root)
	if err != nil {
		s.logFailure("更新日志热重载失败，沿用上一份有效记录", err)
		return false
	}
	entries := buildChangelogEntries(events)
	s.mu.Lock()
	generation := s.snapshot.Generation + 1
	s.snapshot = &changelogSnapshot{Generation: generation, Entries: entries}
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.Info(context.Background(), "更新日志已热重载",
			slog.Uint64("generation", generation), slog.Int("events", len(entries)))
	}
	return true
}

func (s *changelogStore) close() {
	if s == nil || s.stop == nil {
		return
	}
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
}

func (s *changelogStore) logFailure(message string, err error) {
	if s.logger != nil {
		s.logger.Warn(context.Background(), message, slog.String("error", err.Error()))
	}
}

func buildChangelogEntries(events []config.ChangelogEvent) []changelogEntry {
	entries := make([]changelogEntry, 0, len(events))
	for _, event := range events {
		description := renderChangelogDescription(event.Description)
		entries = append(entries, changelogEntry{
			Level: event.Level, Title: event.Title, OccurredAt: event.OccurredAt,
			DescriptionHTML: description.HTML,
			SearchText:      strings.ToLower(event.Title + " " + description.Text),
			SourcePath:      event.SourcePath,
		})
	}
	return entries
}

func changelogFingerprint(root string) string {
	root = strings.TrimSpace(root)
	var state strings.Builder
	months, err := os.ReadDir(root)
	if err != nil {
		state.WriteString("root-error:")
		state.WriteString(err.Error())
		return fmt.Sprintf("%x", sha256.Sum256([]byte(state.String())))
	}
	sort.Slice(months, func(i, j int) bool { return months[i].Name() < months[j].Name() })
	for _, month := range months {
		if !month.IsDir() || !changelogMonthDirectory(month.Name()) {
			continue
		}
		files, readErr := os.ReadDir(filepath.Join(root, month.Name()))
		if readErr != nil {
			state.WriteString(month.Name() + "|error:" + readErr.Error() + "\n")
			continue
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
		for _, file := range files {
			if !file.Type().IsRegular() || !changelogSourceFile(file.Name()) {
				continue
			}
			path := filepath.Join(root, month.Name(), file.Name())
			info, statErr := file.Info()
			state.WriteString(filepath.ToSlash(path))
			if statErr != nil {
				state.WriteString("|error:" + statErr.Error())
			} else {
				state.WriteString(fmt.Sprintf("|%d|%d|%s", info.ModTime().UnixNano(),
					info.Size(), info.Mode().String()))
			}
			state.WriteByte('\n')
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(state.String())))
}

func changelogMonthDirectory(name string) bool {
	parsed, err := time.Parse("2006-01", name)
	return err == nil && parsed.Format("2006-01") == name
}

func changelogSourceFile(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".yaml" || extension == ".yml"
}
