package public

import (
	"context"
	"testing"
	"time"
)

func TestIssueAuthorizationCreatesDownloadHistorySnapshot(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := store.IssueAuthorization(context.Background(), challenge,
		testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	var prefix, fileName, projectName, nodeName, source, status string
	err = db.QueryRow(`SELECT client_prefix_key, file_name, project_name, node_name,
		source_kind, status FROM download_history WHERE authorization_id = ?`,
		auth.Claims.AuthorizationID).Scan(&prefix, &fileName, &projectName, &nodeName,
		&source, &status)
	if err != nil || prefix != "192.0.2.1/32" || fileName != "a.zip" ||
		projectName != "项目一" || nodeName != "节点一" || source != "api" ||
		status != "issued" {
		t.Fatalf("history snapshot prefix=%q file=%q project=%q node=%q source=%q status=%q err=%v",
			prefix, fileName, projectName, nodeName, source, status, err)
	}
}
