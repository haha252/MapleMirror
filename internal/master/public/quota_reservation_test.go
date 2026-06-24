package public

import (
	"context"
	"testing"
	"time"
)

func TestIssueAuthorizationRejectsOutstandingTrafficReservationAtLimit(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	now := time.Now().UTC()
	seedTrafficReservation(t, db, now, "auth-old", now.Add(time.Minute), 0)

	store := Store{DB: db, Quota: tinyTrafficQuota(24)}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2"); err != errTrafficLimit {
		t.Fatalf("未结算预留占满额度时应拒绝新授权：%v", err)
	}
	assertPublicTableCount(t, db, "download_authorizations", 1)
}

func TestIssueAuthorizationNarrowsTokenByOutstandingTrafficReservation(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	now := time.Now().UTC()
	seedTrafficReservation(t, db, now, "auth-old", now.Add(time.Minute), 0)

	store := Store{DB: db, Quota: tinyTrafficQuota(36)}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, debug, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.MaxBytes != 24 || auth.Claims.TrafficLimitBytes != 12 {
		t.Fatalf("授权应保留资产上限并按未预留额度收窄流量上限：claims=%+v", auth.Claims)
	}
	if debug.TrafficRemainingBytes["ipv4_32"] != 0 {
		t.Fatalf("剩余额度应包含新旧预留：remaining=%d",
			debug.TrafficRemainingBytes["ipv4_32"])
	}
	var reserved int64
	err = db.QueryRow(`SELECT address_reserved_bytes FROM traffic_reservations
		WHERE authorization_id = ?`, auth.Claims.AuthorizationID).Scan(&reserved)
	if err != nil || reserved != 12 {
		t.Fatalf("新授权应只预留剩余额度：reserved=%d err=%v", reserved, err)
	}
}

func TestIssueAuthorizationRejectsAfterActualTrafficLimitReached(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	now := time.Now().UTC()
	seedActualTraffic(t, db, now, 24)

	store := Store{DB: db, Quota: tinyTrafficQuota(24)}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2"); err != errTrafficLimit {
		t.Fatalf("真实流量达到上限后应拒绝新授权：%v", err)
	}
}

func TestIssueAuthorizationCapsTokenByActualTrafficRemaining(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	now := time.Now().UTC()
	seedActualTraffic(t, db, now, 20)

	store := Store{DB: db, Quota: tinyTrafficQuota(30)}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.TrafficLimitBytes != 10 {
		t.Fatalf("令牌应按真实剩余额度收窄：limit=%d", auth.Claims.TrafficLimitBytes)
	}
}

func seedTrafficReservation(t *testing.T, db execDB, now time.Time, authID string, expires time.Time, settled int64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id)
		VALUES (?, 'asset-1', 'node-1', '192.0.2.1/32', ?, ?, 24, 4, 'issued', 'old-req')`,
		authID, now.Add(-2*time.Minute).Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES (?, ?, 24, 24, ?, 'active', ?,
		'ipv4_32', '192.0.2.1/32', 'ipv4_24', '192.0.2.0/24')`,
		authID, statDay(now, time.Local), settled, now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func seedActualTraffic(t *testing.T, db execDB, now time.Time, bytes int64) {
	t.Helper()
	for _, row := range []struct {
		kind string
		key  string
	}{
		{"ipv4_32", "192.0.2.1/32"},
		{"ipv4_24", "192.0.2.0/24"},
	} {
		_, err := db.Exec(`INSERT INTO daily_traffic_stats
			(stat_day, scope_kind, scope_key, sent_bytes, updated_at)
			VALUES (?, ?, ?, ?, ?)`,
			statDay(now, time.Local), row.kind, row.key, bytes, now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
}

func tinyTrafficQuota(bytes int64) quotaPolicy {
	return quotaPolicy{
		buckets: map[string]bucketRule{
			"ipv4_32":  {capacity: 120 * tokenUnit, refill: 48 * time.Hour},
			"ipv4_24":  {capacity: 600 * tokenUnit, refill: 48 * time.Hour},
			"ipv6_128": {capacity: 120 * tokenUnit, refill: 48 * time.Hour},
			"ipv6_64":  {capacity: 600 * tokenUnit, refill: 48 * time.Hour},
		},
		daily: map[string]int64{
			"ipv4_32": bytes, "ipv4_24": bytes, "ipv6_128": bytes, "ipv6_64": bytes,
		},
	}
}
