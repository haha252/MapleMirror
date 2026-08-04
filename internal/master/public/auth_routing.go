package public

import (
	"context"
	"database/sql"
	"net/netip"
	"strings"

	"mirror-server/internal/geoip"
)

type routableAssetInfo struct {
	NodeID        string
	NodeName      string
	Priority      int
	Region        geoip.Region
	LastHeartbeat string
	ProjectID     string
	Version       string
	FileName      string
	DownloadURL   string
	System        string
	Architecture  string
	Multiplier    int64
	SizeBytes     int64
}

func (s Store) routableAssetTx(ctx context.Context, tx *sql.Tx, assetID string,
	requestRegion geoip.Region) (routableAssetInfo, error) {
	var out routableAssetInfo
	args := append(s.routableAssetReplicaArgs(), assetID)
	rows, err := tx.QueryContext(ctx, `SELECT n.id, n.public_name, n.download_priority,
		COALESCE(n.region, 'unknown'),
		COALESCE(n.last_heartbeat_at, ''), r.project_id,
		r.tag_name, a.file_name, n.public_download_base_url, COALESCE(NULLIF(p.download_multiplier, 0), 1),
		a.size_bytes, a.architecture, a.system FROM assets a
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id`+routableAssetReplicaSQL+`
		WHERE a.id = ? AND a.service_state = 'candidate'
		AND r.selected = 1 AND p.enabled = 1
		ORDER BY n.download_priority DESC, COALESCE(n.last_heartbeat_at, '') DESC, n.id`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	candidates := make([]routableAssetInfo, 0, 4)
	for rows.Next() {
		var item routableAssetInfo
		var downloadBaseURL string
		if err := rows.Scan(&item.NodeID, &item.NodeName, &item.Priority, &item.Region,
			&item.LastHeartbeat, &item.ProjectID,
			&item.Version, &item.FileName, &downloadBaseURL, &item.Multiplier,
			&item.SizeBytes, &item.Architecture, &item.System); err != nil {
			return out, err
		}
		item.DownloadURL, err = joinDownloadURL(downloadBaseURL, item.ProjectID, item.Version, item.FileName)
		if err == nil {
			candidates = append(candidates, item)
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return s.selectRoutableAsset(candidates, requestRegion)
}

func (s Store) clientRegion(clientPrefix string) geoip.Region {
	if s.RegionClassifier == nil {
		return geoip.RegionUnknown
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(clientPrefix))
	if err != nil || !prefix.IsValid() || prefix.Bits() != prefix.Addr().BitLen() {
		return geoip.RegionUnknown
	}
	return s.RegionClassifier.Classify(prefix.Addr())
}

func (s Store) rangeConcurrencyLimit() int {
	if s.RangeLimit <= 0 {
		return 32
	}
	return s.RangeLimit
}
