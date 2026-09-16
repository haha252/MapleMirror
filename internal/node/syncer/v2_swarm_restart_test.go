package syncer

import (
	"crypto/sha256"
	"testing"

	"mirror-server/internal/node/swarmstate"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func TestOpenSwarmPartialRestoresPersistedBitmapAfterProcessRestart(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	content := []byte("abcdef")
	pieceHash := sha256.Sum256(content)
	whole := sha256.Sum256(content)
	manifest := protocolv2.SwarmManifest{
		AssetID: "asset-restart", AssetSize: int64(len(content)),
		AssetSHA256: "sha256:" + fmtHex(whole[:]), PieceLayoutVersion: swarm.LayoutVersion,
		PieceSize: swarm.MinPieceSize, PieceCount: 1, PieceHashAlgorithm: "sha256", PieceHashes: pieceHash[:],
	}
	manifest.ManifestID = swarm.ManifestID(manifest.AssetID, manifest.AssetSHA256, manifest.AssetSize, manifest.PieceSize, manifest.PieceHashes)
	task := protocolv2.SyncTask{TaskID: "task-restart", AttemptID: "attempt-1", TaskType: "asset_download",
		Asset: protocolv2.SyncAsset{AssetID: manifest.AssetID, SizeBytes: manifest.AssetSize, DigestSHA256: manifest.AssetSHA256}, Manifest: &manifest}

	firstRegistry := swarmstate.New()
	first := Executor{DB: db, Storage: storageDir, TempDir: tempDir, Swarm: firstRegistry}
	file, bits, err := first.openSwarmPartial(task, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(content, 0); err != nil {
		file.Close()
		t.Fatal(err)
	}
	swarm.Set(bits, 0)
	if err := first.persistPartialBitmap(manifest.AssetID, manifest.ManifestID, bits); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()

	// A new registry represents a fresh Node process. The bitmap is restored,
	// while Trusted stays empty so the first serve must lazy-rehash the piece.
	secondRegistry := swarmstate.New()
	second := Executor{DB: db, Storage: storageDir, TempDir: tempDir, Swarm: secondRegistry}
	file, restored, err := second.openSwarmPartial(task, manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if !swarm.Has(restored, 0) {
		t.Fatal("persisted verified piece was not restored")
	}
	partial, ok := secondRegistry.Partial(manifest.AssetID, manifest.ManifestID)
	if !ok || !swarm.Has(partial.Bitset, 0) {
		t.Fatal("fresh process registry did not restore partial availability")
	}
	if swarm.Has(partial.Trusted, 0) {
		t.Fatal("restored piece must require lazy revalidation in the new process")
	}
}

func fmtHex(data []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = digits[b>>4]
		out[i*2+1] = digits[b&0x0f]
	}
	return string(out)
}
