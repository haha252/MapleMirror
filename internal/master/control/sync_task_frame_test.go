package control

import (
	"strings"
	"testing"

	"mirror-server/internal/protocol"
)

func TestFitSyncTaskForControlFrameShrinksFallbackSources(t *testing.T) {
	token := strings.Repeat("x", 256*1024)
	task := protocol.SyncTask{
		TaskID:   "task-large",
		TaskType: "asset_download",
		Asset: protocol.SyncAsset{
			AssetID: "asset-1", FileName: "asset.bin", SizeBytes: 64 * 1024 * 1024,
		},
		FallbackSources: []protocol.SyncFallbackSource{
			{
				NodeID: "source-1", DownloadURL: "https://node.test/a", Token: token,
				Parts: []protocol.SyncFallbackPart{
					{RangeStart: 0, RangeEnd: 1, Token: token},
					{RangeStart: 2, RangeEnd: 3, Token: token},
				},
			},
			{NodeID: "source-2", DownloadURL: "https://node.test/b", Token: token},
		},
	}
	session := Session{NodeID: "node-1"}
	if syncTaskFitsControlFrame(task, session, "req-1") {
		t.Fatal("test task should start oversized")
	}
	fit := fitSyncTaskForControlFrame(task, session, "req-1")
	if !syncTaskFitsControlFrame(fit, session, "req-1") {
		t.Fatal("task should fit after fallback shrinking")
	}
	if len(fit.FallbackSources) >= len(task.FallbackSources) &&
		len(fit.FallbackSources[0].Parts) != 0 {
		t.Fatalf("fallback was not shrunk: %+v", fit.FallbackSources)
	}
}
