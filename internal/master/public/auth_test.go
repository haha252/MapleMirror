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

	auth, debug, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.AssetID != "asset-1" || auth.Claims.NodeID != "node-1" {
		t.Fatalf("授权绑定错误：%+v", auth.Claims)
	}
	if auth.Claims.ProjectID != "p1" || auth.Claims.Architecture != "amd64" || auth.Claims.System != "" {
		t.Fatalf("授权令牌资产元数据错误：%+v", auth.Claims)
	}
	if debug.ProjectID != "p1" || debug.Architecture != "amd64" || debug.System != "" {
		t.Fatalf("授权日志调试元数据错误：%+v", debug)
	}
	if debug.NodeName != "节点一" {
		t.Fatalf("授权日志节点名称错误：%+v", debug)
	}
	if auth.Claims.MaxBytes != 24 {
		t.Fatalf("授权最大字节数应按默认 2 倍资产大小计算：%d", auth.Claims.MaxBytes)
	}
	if auth.Claims.RangeConcurrencyLimit != 32 || debug.RangeLimit != 32 {
		t.Fatalf("授权 Range 并发默认值应为 32：claims=%d debug=%d",
			auth.Claims.RangeConcurrencyLimit, debug.RangeLimit)
	}
	if debug.DownloadURL != "https://node-1.example.com/p1/v1/a.zip" {
		t.Fatalf("下载地址返回错误：%q", debug.DownloadURL)
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

	var authCount, webAuthCount, apiAuthCount int
	err = db.QueryRow(`SELECT authorization_count, web_authorization_count, api_authorization_count FROM daily_project_stats
		WHERE project_id = 'p1'`).Scan(&authCount, &webAuthCount, &apiAuthCount)
	if err != nil || authCount != 1 || webAuthCount != 0 || apiAuthCount != 1 {
		t.Fatalf("下载授权次数未按 API 来源入账：total=%d web=%d api=%d err=%v",
			authCount, webAuthCount, apiAuthCount, err)
	}
	err = db.QueryRow(`SELECT authorization_count, web_authorization_count, api_authorization_count FROM daily_asset_stats
		WHERE asset_id = 'asset-1'`).Scan(&authCount, &webAuthCount, &apiAuthCount)
	if err != nil || authCount != 1 || webAuthCount != 0 || apiAuthCount != 1 {
		t.Fatalf("资源下载授权次数未按 API 来源入账：total=%d web=%d api=%d err=%v",
			authCount, webAuthCount, apiAuthCount, err)
	}
	assertAuthorizationStateCounters(t, db)
	var reserved int64
	err = db.QueryRow(`SELECT address_reserved_bytes FROM traffic_reservations
		WHERE authorization_id = ?`, auth.Claims.AuthorizationID).Scan(&reserved)
	if err != nil || reserved != 24 {
		t.Fatalf("流量预留应按授权最大字节数计算：reserved=%d err=%v", reserved, err)
	}
	var rangeLimit int
	var sourceKind string
	err = db.QueryRow(`SELECT range_limit, source_kind FROM download_authorizations
		WHERE id = ?`, auth.Claims.AuthorizationID).Scan(&rangeLimit, &sourceKind)
	if err != nil || rangeLimit != 32 || sourceKind != "api" {
		t.Fatalf("授权落库字段错误：limit=%d source=%q err=%v", rangeLimit, sourceKind, err)
	}
}

func TestIssueAuthorizationUsesConfiguredRangeConcurrencyLimit(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db, RangeLimit: 16}

	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, debug, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.RangeConcurrencyLimit != 16 || debug.RangeLimit != 16 {
		t.Fatalf("授权未使用配置的 Range 并发限制：claims=%d debug=%d",
			auth.Claims.RangeConcurrencyLimit, debug.RangeLimit)
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
	_, _, err = store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
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

	if _, _, err := store.IssueAuthorization(context.Background(), challenge1, testTokenLifetime(time.Minute), "req-2"); err != nil {
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
	if _, _, err := store.IssueAuthorization(context.Background(), challenge2, testTokenLifetime(time.Minute), "req-4"); err != nil {
		t.Fatalf("白名单客户端重复授权不应触发请求额度不足：%v", err)
	}

	var tokens int64
	err = db.QueryRow(`SELECT tokens_microunits FROM quota_buckets
		WHERE scope_kind = 'ipv4_32' AND scope_key = '127.0.0.1/32'`).Scan(&tokens)
	if err != nil || tokens != 1_000_000 {
		t.Fatalf("白名单客户端请求额度应保持满桶：tokens=%d err=%v", tokens, err)
	}
}

func TestIssueAuthorizationBypassesTrafficLimitForLoopback(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	_, _ = db.Exec(`UPDATE assets SET size_bytes = ? WHERE id = 'asset-1'`, int64(1<<30))
	_, _ = db.Exec(`UPDATE node_inventory SET size_bytes = ? WHERE asset_id = 'asset-1'`, int64(1<<30))
	store := Store{DB: db, Quota: newQuotaPolicy(config.Quota{
		RequestBuckets: config.RequestBuckets{
			IPv432:  config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv424:  config.Bucket{Capacity: 10, FullRefill: "48h"},
			IPv6128: config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv664:  config.Bucket{Capacity: 10, FullRefill: "48h"},
		},
		DailyTraffic: config.DailyTraffic{
			IPv432: "1 GiB", IPv424: "1 GiB", IPv6128: "1 GiB", IPv664: "1 GiB",
		},
	})}

	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "127.0.0.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2"); err != nil {
		t.Fatalf("白名单客户端不应触发每日流量额度不足：%v", err)
	}

	var reserved int64
	var status string
	err = db.QueryRow(`SELECT address_reserved_bytes, status FROM traffic_reservations`).Scan(&reserved, &status)
	if err != nil || reserved != int64(2<<30) || status != "exempt" {
		t.Fatalf("白名单流量预留应保留字节并标记豁免：reserved=%d status=%q err=%v", reserved, status, err)
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

func TestCreateChallengeAppliesMemoryRateLimit(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db, Challenges: newChallengeMemory(config.ChallengeLimits{
		BucketCapacity: challengeBucketCapacity, BucketFullRefill: "10m",
		MaxOutstandingExact: 100, MaxOutstandingTotal: 1000,
	})}
	for i := 0; i < challengeBucketCapacity; i++ {
		if _, err := store.CreateChallenge(context.Background(), "api_pow",
			"asset-1", "192.0.2.1/32", 4, time.Minute, "req"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req")
	if err != errChallengeQuota {
		t.Fatalf("挑战创建限流未生效：%v", err)
	}
}
