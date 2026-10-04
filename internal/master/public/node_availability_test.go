package public

import (
	"context"
	"testing"
)

func TestDownloadReadyNodesMatchesPublicDownloadState(t *testing.T) {
	cases := []struct {
		name, change string
		want         bool
	}{
		{"partially synced", `UPDATE nodes SET routing_ready = 0`, true},
		{"offline", `UPDATE nodes SET state = 'offline'`, false},
		{"disabled", `UPDATE nodes SET state = 'disabled'`, false},
		{"no heartbeat", `UPDATE nodes SET last_heartbeat_at = ''`, false},
		{"no download url", `UPDATE nodes SET public_download_base_url = ''`, false},
		{"probe blocked", `UPDATE nodes SET public_probe_network_failures = 5`, false},
		{"probe below threshold", `UPDATE nodes SET public_probe_network_failures = 4`, true},
		{"wrong digest", `UPDATE node_inventory SET local_digest_sha256 = 'bad'`, false},
		{"wrong size", `UPDATE node_inventory SET size_bytes = 13`, false},
		{"target removed", `UPDATE target_inventory SET desired_state = 'remove'`, false},
		{"project disabled", `UPDATE projects SET enabled = 0`, false},
		{"release deselected", `UPDATE releases SET selected = 0`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openMaster(t)
			seedRoutableAsset(t, db)
			mustExec(t, db, tc.change)
			store := Store{DB: db, PublicProbeNetworkFailures: 5}
			got, err := store.DownloadReadyNodes(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got["node-1"] != tc.want {
				t.Fatalf("availability=%v want %v", got, tc.want)
			}
			nodes, err := store.Nodes(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range nodes {
				if got[node.NodeID] != node.DownloadReady {
					t.Fatalf("admin availability differs from public node state: %+v", node)
				}
			}
		})
	}
}
