package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
)

func TestPushV2SourcesDoesNotHoldRowsAcrossNestedDBQuery(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO asset_piece_manifests
		(id,asset_id,asset_size,asset_sha256,piece_layout_version,piece_size,piece_count,piece_hash_blob,status,created_by_node_id,created_at,updated_at)
		VALUES ('manifest-1','asset-1',10,
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,1048576,1,zeroblob(32),'authoritative',?,'now','now')`, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,task_type,asset_id,state,request_id,created_at,updated_at,attempt_id,lease_expires_at)
		VALUES ('task-1',?,'asset_download','asset-1','running','req','now','now','attempt-1',?)`,
		session.NodeID, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))

	server := &V2Server{Repo: repo}
	queue := controlv2.NewQueue(8, 1<<20)
	defer queue.Close()
	server.queues.Store(session.NodeID, queue)

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	server.pushV2SourcesForAsset(ctx, "asset-1")
	if err := ctx.Err(); err != nil {
		t.Fatalf("pushV2SourcesForAsset exhausted context; likely nested DB query while rows held: %v", err)
	}
}
