package control

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2TaskWaitReportsReasonWithoutRepeatedLogs(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerBootstrapTask(t, repo, session.NodeID)
	var console bytes.Buffer
	logger, err := logging.New("master", config.Logging{Directory: t.TempDir(), ConsoleLevel: "info", FileLevel: "info"}, time.UTC, &console)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	repo.Logger = logger
	for i := 0; i < 2; i++ {
		if _, ok, err := repo.nextV2SyncTask(context.Background(), session.NodeID); err != nil || ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	}
	if strings.Count(console.String(), "no_verified_peer") != 1 {
		t.Fatalf("unexpected logs: %s", console.String())
	}
	repo.runtime().SetPeerBootstrap(session.NodeID, false)
	if _, _, err := repo.nextV2SyncTask(context.Background(), session.NodeID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(console.String(), "peer_bootstrap_not_supported") {
		t.Fatalf("missing compatibility wait: %s", console.String())
	}
	repo.runtime().MarkV2Status(session.NodeID, runtimeV2Status{Status: protocolv2.NodeStatus{SyncTaskSlotsAvailable: 0}, ReportedAt: time.Now()})
	if _, _, err := repo.nextV2SyncTaskForDispatch(context.Background(), session.NodeID, nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(console.String(), "slots_full") {
		t.Fatalf("missing capacity wait: %s", console.String())
	}
}
