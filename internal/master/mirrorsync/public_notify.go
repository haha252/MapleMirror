package mirrorsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"

	"mirror-server/internal/config"
	"mirror-server/internal/indexnow"
	"mirror-server/internal/publiclocale"
)

type PublicChangeNotifier interface {
	NotifyChanges(context.Context, []indexnow.PageChange)
}

type ImmediatePublicChangeNotifier interface {
	NotifySnapshotNow(context.Context, indexnow.Snapshot) int
}

type publicFingerprint struct {
	Enabled bool
	Hash    string
}

var publicIndexNowPaths = []string{"/", "/about", "/api-docs", "/stats", "/changelog"}

func (s Store) publicFingerprints(ctx context.Context) (map[string]publicFingerprint, error) {
	if s.DB == nil {
		return nil, errors.New("公开页面数据库未配置")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.name, p.repository,
		COALESCE(p.description, ''), COALESCE(p.homepage_url, ''), p.enabled,
		COALESCE(r.id, ''), COALESCE(r.tag_name, ''), COALESCE(r.prerelease, 0),
		COALESCE(r.published_at, ''), COALESCE(a.file_name, ''),
		COALESCE(a.architecture, ''), COALESCE(a.system, ''),
		COALESCE(a.variant, ''), COALESCE(a.display_label, ''),
		COALESCE(a.priority, 0), COALESCE(a.size_bytes, 0),
		COALESCE(a.digest_sha256, ''), COALESCE(a.labels_json, '')
		FROM projects p
		LEFT JOIN releases r ON r.project_id = p.id AND r.selected = 1
		LEFT JOIN assets a ON a.release_id = r.id AND a.service_state = 'candidate'
		ORDER BY p.id, r.published_at DESC, r.id, a.file_name, a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	builders := map[string]*strings.Builder{}
	enabled := map[string]bool{}
	for rows.Next() {
		var id, name, repository, description, homepage string
		var releaseID, tag, published, fileName, architecture, system string
		var variant, displayLabel, digest, labels string
		var projectEnabled, prerelease, priority int
		var size int64
		if err := rows.Scan(&id, &name, &repository, &description, &homepage, &projectEnabled,
			&releaseID, &tag, &prerelease, &published, &fileName, &architecture, &system,
			&variant, &displayLabel, &priority, &size, &digest, &labels); err != nil {
			return nil, err
		}
		builder := builders[id]
		if builder == nil {
			builder = &strings.Builder{}
			builders[id] = builder
			enabled[id] = projectEnabled != 0
		}
		fmt.Fprintf(builder, "project|%s|%s|%s|%s|%s|%t|release|%s|%s|%d|%s|asset|%s|%s|%s|%s|%s|%d|%d|%s|%s\n",
			id, name, repository, description, homepage, projectEnabled != 0,
			releaseID, tag, prerelease, published, fileName, architecture, system,
			variant, displayLabel, priority, size, digest, labels)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make(map[string]publicFingerprint, len(builders))
	for id, builder := range builders {
		sum := sha256.Sum256([]byte(builder.String()))
		result[id] = publicFingerprint{Enabled: enabled[id], Hash: hex.EncodeToString(sum[:])}
	}
	return result, nil
}

func (s Scanner) publicSnapshot(ctx context.Context, projects config.Projects) (indexnow.Snapshot, error) {
	return s.publicSnapshotWithConfig(ctx, projects, true)
}

func (s Scanner) storedPublicSnapshot(ctx context.Context, projects config.Projects) (indexnow.Snapshot, error) {
	return s.publicSnapshotWithConfig(ctx, projects, false)
}

func (s Scanner) publicSnapshotWithConfig(ctx context.Context, projects config.Projects, applyConfig bool) (indexnow.Snapshot, error) {
	fingerprints, err := s.Store.publicFingerprints(ctx)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]struct{}, len(projects.Projects))
	for _, project := range projects.Projects {
		configured[project.ID] = struct{}{}
		if current, ok := fingerprints[project.ID]; ok {
			if applyConfig {
				current.Enabled = project.Enabled
			}
			fingerprints[project.ID] = current
		} else {
			fingerprints[project.ID] = publicFingerprint{Enabled: project.Enabled,
				Hash: configProjectFingerprint(project)}
		}
	}
	if applyConfig && len(projects.Projects) > 0 {
		for id, current := range fingerprints {
			if _, ok := configured[id]; !ok {
				current.Enabled = false
				fingerprints[id] = current
			}
		}
	}

	localeCount := len(publiclocale.All())
	snapshot := make(indexnow.Snapshot, (len(publicIndexNowPaths)+len(fingerprints))*localeCount)
	for _, path := range publicIndexNowPaths {
		addLocalizedSnapshotPages(snapshot, path, staticFingerprint(s.SEORevision, path))
	}
	ids := make([]string, 0, len(fingerprints))
	for id, fingerprint := range fingerprints {
		if fingerprint.Enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var homepage strings.Builder
	fmt.Fprintf(&homepage, "revision|%s\n", s.SEORevision)
	for _, id := range ids {
		fingerprint := fingerprints[id].Hash
		fmt.Fprintf(&homepage, "%s|%s\n", id, fingerprint)
		path := "/" + url.PathEscape(id) + "/"
		addLocalizedSnapshotPages(snapshot, path, "sha256:"+fingerprint)
	}
	addLocalizedSnapshotPages(snapshot, "/", digestFingerprint(homepage.String()))
	return snapshot, nil
}

func addLocalizedSnapshotPages(snapshot indexnow.Snapshot, path, fingerprint string) {
	for _, localizedPath := range publiclocale.Paths(path) {
		snapshot[localizedPath] = indexnow.Page{
			Path: localizedPath, Fingerprint: fingerprint, Present: true,
		}
	}
}

func configProjectFingerprint(project config.Project) string {
	return digestFingerprint("config|" + projectHash(project))
}

func staticFingerprint(revision, path string) string {
	return digestFingerprint("static|" + strings.TrimSpace(revision) + "|" + path)
}

func digestFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s Scanner) PublicSnapshot(ctx context.Context, projects config.Projects) (indexnow.Snapshot, error) {
	return s.publicSnapshot(ctx, projects)
}

func publicPageChanges(before, after indexnow.Snapshot) []indexnow.PageChange {
	paths := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changes := make([]indexnow.PageChange, 0, len(ordered))
	for _, path := range ordered {
		previous, previousOK := before[path]
		current, currentOK := after[path]
		if previousOK && currentOK && previous.Fingerprint == current.Fingerprint && previous.Present == current.Present {
			continue
		}
		if currentOK && current.Present {
			current.Path = path
			changes = append(changes, current)
			continue
		}
		if previousOK {
			changes = append(changes, indexnow.PageChange{Path: path,
				Fingerprint: digestFingerprint("deleted|" + previous.Fingerprint), Present: false})
		}
	}
	return changes
}

func (s Scanner) notifyPublicChanges(ctx context.Context, before indexnow.Snapshot, projects config.Projects) {
	if s.Notifier == nil || before == nil {
		return
	}
	after, err := s.publicSnapshot(ctx, projects)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(ctx, "公开页面快照读取失败，跳过 IndexNow 通知", slog.String("error", err.Error()))
		}
		return
	}
	changes := publicPageChanges(before, after)
	if len(changes) > 0 {
		s.Notifier.NotifyChanges(ctx, changes)
	}
}
