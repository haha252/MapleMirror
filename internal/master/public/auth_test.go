package public

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestIssueAuthorizationConsumesChallengeAndBindsRoutableNode(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}

	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	nonce := solveNonce(challenge)
	if !validLeadingZeros(challenge, nonce) {
		t.Fatal("测试 nonce 未满足 PoW")
	}

	auth, _, err := store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.AssetID != "asset-1" || auth.Claims.NodeID != "node-1" {
		t.Fatalf("授权绑定错误：%+v", auth.Claims)
	}
	if _, err := store.LoadChallenge(context.Background(), challenge.ID); err == nil {
		t.Fatal("挑战提交后仍可重复使用")
	}

	var tokens int64
	err = db.QueryRow(`SELECT tokens_microunits FROM quota_buckets
		WHERE scope_kind = 'ipv4_32' AND scope_key = '192.0.2.1/32'`).Scan(&tokens)
	if err != nil || tokens != 119_000_000 {
		t.Fatalf("请求额度未按倍率扣减：tokens=%d err=%v", tokens, err)
	}

	var authCount int
	err = db.QueryRow(`SELECT authorization_count FROM daily_project_stats
		WHERE project_id = 'p1'`).Scan(&authCount)
	if err != nil || authCount != 1 {
		t.Fatalf("下载授权次数未入账：count=%d err=%v", authCount, err)
	}
}

func TestIssueAuthorizationRejectsRequestQuotaExhausted(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	_, _ = db.Exec(`UPDATE projects SET download_multiplier = 3 WHERE id = 'p1'`)
	store := Store{DB: db, Quota: newQuotaPolicy(config.Quota{
		RequestBuckets: config.RequestBuckets{
			IPv432:  config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv424:  config.Bucket{Capacity: 10, FullRefill: "48h"},
			IPv6128: config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv664:  config.Bucket{Capacity: 10, FullRefill: "48h"},
		},
		DailyTraffic: config.DailyTraffic{
			IPv432: "3 GiB", IPv424: "20 GiB", IPv6128: "3 GiB", IPv664: "20 GiB",
		},
	})}

	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != errRequestQuota {
		t.Fatalf("请求额度不足应拒绝授权：%v", err)
	}
}

func TestIssueAuthorizationBypassesRequestQuotaForLoopback(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db, Quota: newQuotaPolicy(config.Quota{
		RequestBuckets: config.RequestBuckets{
			IPv432:  config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv424:  config.Bucket{Capacity: 10, FullRefill: "48h"},
			IPv6128: config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv664:  config.Bucket{Capacity: 10, FullRefill: "48h"},
		},
		DailyTraffic: config.DailyTraffic{
			IPv432: "3 GiB", IPv424: "20 GiB", IPv6128: "3 GiB", IPv664: "20 GiB",
		},
	})}

	challenge1, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "127.0.0.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	nonce := solveNonce(challenge1)
	if !validLeadingZeros(challenge1, nonce) {
		t.Fatal("测试 nonce 未满足 PoW")
	}

	if _, _, err := store.IssueAuthorization(context.Background(), challenge1, time.Minute, "req-2"); err != nil {
		t.Fatalf("白名单客户端首次授权失败：%v", err)
	}
	challenge2, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "127.0.0.1/32", 4, time.Minute, "req-3")
	if err != nil {
		t.Fatal(err)
	}
	nonce = solveNonce(challenge2)
	if !validLeadingZeros(challenge2, nonce) {
		t.Fatal("第二个测试 nonce 未满足 PoW")
	}
	if _, _, err := store.IssueAuthorization(context.Background(), challenge2, time.Minute, "req-4"); err != nil {
		t.Fatalf("白名单客户端重复授权不应触发请求额度不足：%v", err)
	}

	var tokens int64
	err = db.QueryRow(`SELECT tokens_microunits FROM quota_buckets
		WHERE scope_kind = 'ipv4_32' AND scope_key = '127.0.0.1/32'`).Scan(&tokens)
	if err != nil || tokens != 1_000_000 {
		t.Fatalf("白名单客户端请求额度应保持满桶：tokens=%d err=%v", tokens, err)
	}
}

func TestCreateChallengeRejectsUnavailableAsset(t *testing.T) {
	db := openMaster(t)
	store := Store{DB: db}
	_, err := store.CreateChallenge(context.Background(), "api_pow",
		"missing", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err == nil {
		t.Fatal("不存在可路由副本时不应创建挑战")
	}
}
