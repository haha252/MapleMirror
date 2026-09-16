package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestResetNodeIdentityForReEnrollmentPreservesAssetsAndPartials(t *testing.T) {
	root := t.TempDir()
	assetDir := filepath.Join(root, "assets")
	tempDir := filepath.Join(root, "tmp")
	if err := os.MkdirAll(filepath.Join(tempDir, "swarm-partials"), 0o700); err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(assetDir, "p", "v", "a.bin")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	partialPath := filepath.Join(tempDir, "swarm-partials", "a.part")
	if err := os.WriteFile(partialPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenNode(filepath.Join(root, "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO local_assets(asset_id,relative_path,digest_sha256,size_bytes,verified_at,state)
		VALUES('asset-1','p/v/a.bin','sha256:aa',5,?,'verified')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO swarm_partials(asset_id,manifest_id,partial_path,asset_size,piece_size,piece_count,verified_bitmap,created_at,updated_at,last_access_at)
		VALUES('asset-1','manifest-1',?,7,1048576,1,x'01',?,?,?)`, partialPath, now, now, now); err != nil {
		t.Fatal(err)
	}
	cert := filepath.Join(root, "node.crt")
	if err := os.WriteFile(cert, []byte("old identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Node{Storage: config.NodeStorage{Directory: assetDir, TempDirectory: tempDir}, TLS: config.TLS{CertFile: cert}}
	if err := resetNodeIdentityForReEnrollment(cfg, db, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(assetPath); err != nil {
		t.Fatalf("asset removed during re-enrollment: %v", err)
	}
	if _, err := os.Stat(partialPath); err != nil {
		t.Fatalf("managed partial removed during re-enrollment: %v", err)
	}
	if _, err := os.Stat(cert); !os.IsNotExist(err) {
		t.Fatalf("old identity certificate was not removed: %v", err)
	}
	var assets, partials int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_assets WHERE asset_id='asset-1'`).Scan(&assets); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM swarm_partials WHERE asset_id='asset-1'`).Scan(&partials); err != nil {
		t.Fatal(err)
	}
	if assets != 1 || partials != 1 {
		t.Fatalf("preserved db state assets=%d partials=%d", assets, partials)
	}
}
