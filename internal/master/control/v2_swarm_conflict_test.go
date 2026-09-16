package control

import (
	"context"
	"testing"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func testV2Manifest(assetID, digest string, size int64, fill byte) protocolv2.SwarmManifest {
	pieceSize, pieceCount, _ := swarm.ChoosePieceSize(size)
	hashes := make([]byte, pieceCount*32)
	for i := range hashes {
		hashes[i] = fill
	}
	m := protocolv2.SwarmManifest{AssetID: assetID, AssetSize: size, AssetSHA256: digest,
		PieceLayoutVersion: swarm.LayoutVersion, PieceSize: pieceSize, PieceCount: pieceCount,
		PieceHashAlgorithm: "sha256", PieceHashes: hashes}
	m.ManifestID = swarm.ManifestID(m.AssetID, m.AssetSHA256, m.AssetSize, m.PieceSize, m.PieceHashes)
	return m
}

func TestV2ManifestConflictFreezesPartialSwarmAndRetriesWholeFile(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	lease := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at,lease_expires_at,attempt_id)
		VALUES('task-conflict',?,'asset-1','asset_download','running','req','now','now',?,'attempt-old')`, session.NodeID, lease)

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	m1 := testV2Manifest("asset-1", digest, 10, 1)
	m2 := testV2Manifest("asset-1", digest, 10, 2)
	ack, err := repo.AcceptV2Manifest(context.Background(), session.NodeID, m1)
	if err != nil || ack.Status != "accepted" {
		t.Fatalf("first manifest ack=%+v err=%v", ack, err)
	}
	ack, err = repo.AcceptV2Manifest(context.Background(), session.NodeID, m2)
	if err != nil || ack.Status != "conflict" {
		t.Fatalf("conflicting manifest ack=%+v err=%v", ack, err)
	}
	if len(repo.runtime().swarmAvailability("asset-1", m1.ManifestID, "")) == 0 {
		t.Fatal("seed availability should exist before freeze")
	}

	server := &V2Server{Repo: repo}
	if err := server.freezeV2SwarmAsset(context.Background(), "asset-1"); err != nil {
		t.Fatal(err)
	}
	if len(repo.runtime().swarmAvailability("asset-1", m1.ManifestID, "")) != 0 {
		t.Fatal("manifest conflict did not clear runtime availability")
	}
	var state, attempt string
	if err := repo.DB.QueryRow(`SELECT state,COALESCE(attempt_id,'') FROM node_tasks WHERE id='task-conflict'`).Scan(&state, &attempt); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempt != "" {
		t.Fatalf("task state=%q attempt=%q", state, attempt)
	}

	task, ok, err := repo.nextV2SyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		t.Fatalf("whole retry missing task=%+v ok=%v err=%v", task, ok, err)
	}
	if !task.SwarmDisabled || task.Bootstrap || task.Manifest != nil {
		t.Fatalf("conflict must force whole-file mode: %+v", task)
	}
}

func TestV2ManifestPersistsAcrossMasterRuntimeRestart(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	manifest := testV2Manifest("asset-1", digest, 10, 7)
	ack, err := repo.AcceptV2Manifest(context.Background(), session.NodeID, manifest)
	if err != nil || ack.Status != "accepted" {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}

	restarted := repo
	restarted.Runtime = NewRuntimeStore()
	got, ok, err := restarted.LoadV2Manifest(context.Background(), "asset-1")
	if err != nil || !ok {
		t.Fatalf("manifest not restored ok=%v err=%v", ok, err)
	}
	if got.ManifestID != manifest.ManifestID || string(got.PieceHashes) != string(manifest.PieceHashes) {
		t.Fatalf("restored manifest mismatch got=%+v want=%+v", got, manifest)
	}
}

func TestV2ManifestRejectsAuthoritativeAssetBindingMismatch(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	wrongDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	manifest := testV2Manifest("asset-1", wrongDigest, 10, 9)
	ack, err := repo.AcceptV2Manifest(context.Background(), session.NodeID, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != "rejected" {
		t.Fatalf("binding mismatch status=%q", ack.Status)
	}
}

func TestV2ManifestRejectsPieceCountAboveProtocolLimit(t *testing.T) {
	manifest := protocolv2.SwarmManifest{
		AssetID: "asset", ManifestID: "invalid", AssetSize: 1,
		AssetSHA256:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PieceLayoutVersion: swarm.LayoutVersion, PieceSize: swarm.MinPieceSize,
		PieceCount: int(swarm.MaxPieces) + 1, PieceHashAlgorithm: "sha256",
	}
	if err := validateManifestStructure(manifest); err == nil {
		t.Fatal("manifest above MaxPieces should be rejected")
	}
}
