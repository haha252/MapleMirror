package mirrorsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type PublicChangeNotifier interface {
	NotifyPaths(context.Context, []string)
}

type publicFingerprint struct {
	Enabled bool
	Hash    string
}

var publicIndexNowPaths = []string{"/", "/about", "/api-docs", "/stats", "/changelog"}

func (s Store) publicFingerprints(ctx context.Context) map[string]publicFingerprint {
	if s.DB == nil {
		return nil
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
		return nil
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
			return nil
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
		return nil
	}
	result := make(map[string]publicFingerprint, len(builders))
	for id, builder := range builders {
		sum := sha256.Sum256([]byte(builder.String()))
		result[id] = publicFingerprint{Enabled: enabled[id], Hash: hex.EncodeToString(sum[:])}
	}
	return result
}

func (s Scanner) publicFingerprints(ctx context.Context) map[string]publicFingerprint {
	if s.Notifier == nil {
		return nil
	}
	return s.Store.publicFingerprints(ctx)
}

func (s Scanner) notifyPublicChanges(ctx context.Context, before map[string]publicFingerprint) {
	if s.Notifier == nil || before == nil {
		return
	}
	after := s.Store.publicFingerprints(ctx)
	if after == nil {
		if s.Logger != nil {
			s.Logger.Warn(ctx, "公开页面指纹读取失败，跳过 IndexNow 通知")
		}
		return
	}
	s.Notifier.NotifyPaths(ctx, fullPublicPaths(before, after))
}

func fullPublicPaths(before, after map[string]publicFingerprint) []string {
	paths := make(map[string]struct{}, len(publicIndexNowPaths)+len(after))
	for _, path := range publicIndexNowPaths {
		paths[path] = struct{}{}
	}
	for id, fingerprint := range after {
		if fingerprint.Enabled {
			paths["/"+url.PathEscape(id)+"/"] = struct{}{}
		}
	}
	for _, path := range changedPublicPaths(before, after) {
		paths[path] = struct{}{}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func changedPublicPaths(before, after map[string]publicFingerprint) []string {
	ids := map[string]struct{}{}
	for id := range before {
		ids[id] = struct{}{}
	}
	for id := range after {
		ids[id] = struct{}{}
	}
	paths := map[string]struct{}{}
	for id := range ids {
		previous, previousOK := before[id]
		current, currentOK := after[id]
		if previousOK && currentOK && previous == current {
			continue
		}
		if !((previousOK && previous.Enabled) || (currentOK && current.Enabled)) {
			continue
		}
		paths["/"] = struct{}{}
		paths["/"+url.PathEscape(id)+"/"] = struct{}{}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}
