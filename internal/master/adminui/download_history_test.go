package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDownloadHistoryAPIQueriesExactIPAndSummarizes(t *testing.T) {
	server, db := newTestServer(t)
	server.downloadHistoryRetentionDays = 7
	now := time.Now().UTC()
	insertHistory := func(id, prefix, issued string, sent int64) {
		mustExecAdminUI(t, db, `INSERT INTO download_history
			(authorization_id, client_prefix_key, source_kind, project_id, project_name,
			asset_id, file_name, version, system, architecture, node_id, node_name,
			issued_at, expires_at, first_transfer_at, last_transfer_at, sent_bytes,
			status, status_reason, request_id, updated_at)
			VALUES (?, ?, 'web', 'p1', '项目一', ?, ?, 'v1', 'windows', 'amd64',
			'node-1', '节点一', ?, ?, ?, ?, ?, 'expired_idle', 'expired_idle', ?, ?)`,
			id, prefix, "asset-"+id, id+".zip", issued, now.Add(time.Hour).Format(time.RFC3339Nano),
			issued, issued, sent, "req-"+id, issued)
	}
	insertHistory("new", "192.0.2.10/32", now.Add(-time.Hour).Format(time.RFC3339Nano), 128)
	insertHistory("old", "192.0.2.10/32", now.Add(-2*time.Hour).Format(time.RFC3339Nano), 64)
	insertHistory("other", "192.0.2.11/32", now.Add(-time.Hour).Format(time.RFC3339Nano), 999)

	req := httptest.NewRequest(http.MethodGet,
		"/admin/api/security/download-history?ip=192.0.2.10&page=1&page_size=20", nil)
	rec := httptest.NewRecorder()
	server.downloadHistoryAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		IP      string `json:"ip"`
		Summary struct {
			TokenCount    int   `json:"token_count"`
			TransferCount int   `json:"transfer_count"`
			SentBytes     int64 `json:"sent_bytes"`
		} `json:"summary"`
		Downloads  []map[string]any `json:"downloads"`
		Pagination pagination       `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.IP != "192.0.2.10" || body.Summary.TokenCount != 2 ||
		body.Summary.TransferCount != 2 || body.Summary.SentBytes != 192 {
		t.Fatalf("unexpected summary: %+v", body)
	}
	if body.Pagination.Total != 2 || len(body.Downloads) != 2 ||
		body.Downloads[0]["authorization_id"] != "new" {
		t.Fatalf("unexpected rows: %+v", body)
	}
}

func TestDownloadHistoryAPIRejectsCIDRAndInvalidIP(t *testing.T) {
	server, _ := newTestServer(t)
	for _, value := range []string{"192.0.2.0/24", "not-an-ip", ""} {
		req := httptest.NewRequest(http.MethodGet,
			"/admin/api/security/download-history?ip="+value, nil)
		rec := httptest.NewRecorder()
		server.downloadHistoryAPI(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("ip=%q status=%d body=%s", value, rec.Code, rec.Body.String())
		}
	}
}
