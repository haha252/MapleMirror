package public

import (
	"context"
	"reflect"
	"testing"
)

func TestDownloadReadyNodesExistsMatchesReplicaRouting(t *testing.T) {
	for _, mutation := range []string{
		"", `UPDATE nodes SET state='disabled'`, `UPDATE nodes SET state='offline'`,
		`UPDATE nodes SET last_heartbeat_at=NULL`, `UPDATE nodes SET public_download_base_url=''`,
		`UPDATE nodes SET public_probe_network_failures=3`,
		`UPDATE projects SET enabled=0`, `UPDATE releases SET selected=0`,
		`UPDATE assets SET service_state='pending'`, `UPDATE target_inventory SET desired_state='remove'`,
		`UPDATE node_inventory SET state='stale'`, `UPDATE node_inventory SET local_digest_sha256='wrong'`,
		`UPDATE node_inventory SET size_bytes=size_bytes+1`,
	} {
		t.Run(mutation, func(t *testing.T) {
			db := openMaster(t)
			seedRoutableAsset(t, db)
			if mutation != "" {
				mustExec(t, db, mutation)
			}
			store := Store{DB: db, PublicProbeNetworkFailures: 3}
			want := map[string]bool{}
			rows, err := db.Query(`SELECT DISTINCT n.id FROM assets a `+routableAssetReplicaSQL+`
				JOIN releases r ON r.id=a.release_id AND r.selected=1
				JOIN projects p ON p.id=r.project_id AND p.enabled=1 WHERE a.service_state='candidate'`, store.routableAssetReplicaArgs()...)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				want[id] = true
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			_ = rows.Close()
			got, err := store.DownloadReadyNodes(context.Background())
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("ready=%v want=%v err=%v", got, want, err)
			}
		})
	}
}
