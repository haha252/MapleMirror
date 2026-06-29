package control

import "testing"

func assertTrafficStateCounters(t *testing.T, repo Repository) {
	t.Helper()
	var sent, started int64
	err := repo.DB.QueryRow(`SELECT sent_bytes, transfer_started_count
		FROM public_stat_totals WHERE id = 'global'`).Scan(&sent, &started)
	if err != nil || sent != 5 || started != 1 {
		t.Fatalf("流量状态累计不符合预期：sent=%d started=%d err=%v", sent, started, err)
	}
	err = repo.DB.QueryRow(`SELECT sent_bytes, transfer_started_count
		FROM daily_public_stats`).Scan(&sent, &started)
	if err != nil || sent != 5 || started != 1 {
		t.Fatalf("流量日状态不符合预期：sent=%d started=%d err=%v", sent, started, err)
	}
	err = repo.DB.QueryRow(`SELECT sent_bytes, transfer_started_count
		FROM asset_stat_totals WHERE asset_id = 'asset-1'`).Scan(&sent, &started)
	if err != nil || sent != 5 || started != 1 {
		t.Fatalf("资源流量状态不符合预期：sent=%d started=%d err=%v", sent, started, err)
	}
	err = repo.DB.QueryRow(`SELECT sent_bytes FROM node_traffic_totals
		WHERE node_id = 'node-1'`).Scan(&sent)
	if err != nil || sent != 5 {
		t.Fatalf("节点流量状态不符合预期：sent=%d err=%v", sent, err)
	}
}
