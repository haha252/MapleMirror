package syncer

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"
)

const DefaultSwarmPartialTTL = 7 * 24 * time.Hour

func CleanSwarmPartials(db *sql.DB, storageDir, tempDir string, ttl time.Duration) (int, error) {
	if db == nil {
		return 0, nil
	}
	if ttl <= 0 {
		ttl = DefaultSwarmPartialTTL
	}
	root := filepath.Join(effectiveTempDir(storageDir, tempDir), "swarm-partials")
	if err := ensureManagedSwarmRoot(storageDir, root); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().Add(-ttl)
	rows, err := db.Query(`SELECT asset_id,manifest_id,partial_path,last_access_at FROM swarm_partials`)
	if err != nil {
		return 0, err
	}
	type row struct{ asset, manifest, path, accessed string }
	var items []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.asset, &item.manifest, &item.path, &item.accessed); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	referenced := map[string]bool{}
	removed := 0
	for _, item := range items {
		clean, safe := managedSwarmPath(root, item.path)
		if safe {
			referenced[clean] = true
		}
		accessed, parseErr := time.Parse(time.RFC3339Nano, item.accessed)
		_, statErr := os.Stat(clean)
		stale := parseErr != nil || accessed.Before(cutoff) || statErr != nil
		if !stale {
			continue
		}
		if safe {
			if err := os.Remove(clean); err != nil && !os.IsNotExist(err) {
				return removed, err
			}
			delete(referenced, clean)
		}
		if _, err := db.Exec(`DELETE FROM swarm_partials WHERE asset_id=? AND manifest_id=?`, item.asset, item.manifest); err != nil {
			return removed, err
		}
		removed++
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return removed, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if referenced[path] {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func ensureManagedSwarmRoot(storageDir, root string) error {
	return ensureSafeTempDirectory(storageDir, filepath.Dir(root))
}

func managedSwarmPath(root, path string) (string, bool) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", false
	}
	pathAbs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return pathAbs, false
	}
	if len(rel) >= 3 && rel[:3] == ".."+string(os.PathSeparator) {
		return pathAbs, false
	}
	return pathAbs, true
}

func (e Executor) removeOtherSwarmPartials(assetID, keepManifest string) error {
	if e.DB == nil {
		return nil
	}
	root := filepath.Join(effectiveTempDir(e.Storage, e.TempDir), "swarm-partials")
	rows, err := e.DB.Query(`SELECT manifest_id,partial_path FROM swarm_partials WHERE asset_id=? AND manifest_id!=?`, assetID, keepManifest)
	if err != nil {
		return err
	}
	var items [][2]string
	for rows.Next() {
		var manifest, path string
		if err := rows.Scan(&manifest, &path); err != nil {
			rows.Close()
			return err
		}
		items = append(items, [2]string{manifest, path})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if path, safe := managedSwarmPath(root, item[1]); safe {
			_ = os.Remove(path)
		}
		if _, err := e.DB.Exec(`DELETE FROM swarm_partials WHERE asset_id=? AND manifest_id=?`, assetID, item[0]); err != nil {
			return err
		}
		if e.Swarm != nil {
			e.Swarm.RemovePartial(assetID, item[0])
		}
	}
	return nil
}
