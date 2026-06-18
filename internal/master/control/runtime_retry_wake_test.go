package control

import (
	"testing"
	"time"
)

func TestScheduleSyncTaskRetryWakeNotifiesActiveSession(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().StartSession(session)

	repo.scheduleSyncTaskRetryWakeAt(session.NodeID, time.Now().Add(10*time.Millisecond))
	deadline := time.After(time.Second)
	for {
		if repo.runtime().ConsumeSyncTaskWake(session.NodeID) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("retry wake was not notified")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
