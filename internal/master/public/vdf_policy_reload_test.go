package public

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestVDFPolicyReloaderAppliesValidChangesAndKeepsLastGoodPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeVDFReloadConfig(t, path, 8)
	cfg := testVDFReloadConfig()
	policy, err := newVDFPolicy([]config.VDFSizeTier{{MinSize: "0 B", Iterations: 8}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	service := &vdfService{policy: policy}
	reloader := newVDFPolicyReloader(service, path, cfg, nil)
	if reloader == nil {
		t.Fatal("VDF 策略重载器未创建")
	}
	defer reloader.close()

	if reloader.reloadIfChanged() {
		t.Fatal("未修改配置时不应触发重载")
	}
	writeVDFReloadConfig(t, path, 16)
	if !reloader.reloadIfChanged() {
		t.Fatal("有效配置修改未触发重载")
	}
	if iterations, _ := service.parameters(0, abuseLevelNormal); iterations != 16 {
		t.Fatalf("新策略迭代数=%d，want 16", iterations)
	}

	writeVDFReloadConfig(t, path, 0)
	if reloader.reloadIfChanged() {
		t.Fatal("非法配置不应触发策略替换")
	}
	if iterations, _ := service.parameters(0, abuseLevelNormal); iterations != 16 {
		t.Fatalf("非法配置覆盖了旧策略，迭代数=%d", iterations)
	}

	writeVDFReloadConfig(t, path, 32)
	if !reloader.reloadIfChanged() {
		t.Fatal("修复后的配置未触发重载")
	}
	if iterations, _ := service.parameters(0, abuseLevelNormal); iterations != 32 {
		t.Fatalf("修复后的策略迭代数=%d，want 32", iterations)
	}
	reloader.stateMu.Lock()
	generation := reloader.generation
	reloader.stateMu.Unlock()
	if generation != 3 {
		t.Fatalf("策略代数=%d，want 3", generation)
	}
}

func TestVDFPolicyReloadPreservesExistingChallenges(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := testVDFStore(t, db, config.ChallengeLimits{
		BucketCapacity: 30, BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 100,
	})
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeVDFReloadConfig(t, path, 8)
	reloader := newVDFPolicyReloader(store.VDF, path, testVDFReloadConfig(), nil)
	defer reloader.close()

	oldChallenge, err := store.CreateVDFChallenge(t.Context(), "web", "asset-1",
		"192.0.2.40/32", abuseLevelNormal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := decodeVDFInteger(oldChallenge.BaseEncoded)
	if err != nil {
		t.Fatal(err)
	}
	oldSolution, err := encodeVDFInteger(sequentialVDFSolution(base, oldChallenge.Modulus, oldChallenge.Iterations))
	if err != nil {
		t.Fatal(err)
	}

	writeVDFReloadConfig(t, path, 16)
	if !reloader.reloadIfChanged() {
		t.Fatal("有效配置修改未触发重载")
	}
	if oldChallenge.Iterations != 8 || !validVDFSolution(oldChallenge, oldSolution) {
		t.Fatal("策略重载影响了已有挑战")
	}
	nextChallenge, err := store.CreateVDFChallenge(t.Context(), "web", "asset-1",
		"192.0.2.41/32", abuseLevelNormal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if nextChallenge.Iterations != 16 {
		t.Fatalf("新挑战迭代数=%d，want 16", nextChallenge.Iterations)
	}
}

func TestVDFPolicySnapshotIsConcurrentSafe(t *testing.T) {
	cfg := testVDFReloadConfig()
	first, err := newVDFPolicy([]config.VDFSizeTier{{MinSize: "0 B", Iterations: 8}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newVDFPolicy([]config.VDFSizeTier{{MinSize: "0 B", Iterations: 16}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	service := &vdfService{policy: first}
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := 0; index < 10000; index++ {
				iterations, _ := service.parameters(int64(index), abuseLevelNormal)
				if iterations != 8 && iterations != 16 {
					t.Errorf("读取到非法迭代数=%d", iterations)
					return
				}
			}
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		for index := 0; index < 10000; index++ {
			if index%2 == 0 {
				service.setPolicy(first)
			} else {
				service.setPolicy(second)
			}
		}
	}()
	group.Wait()
}

func TestVDFPolicyReloaderCloseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeVDFReloadConfig(t, path, 8)
	policy, err := newVDFPolicy([]config.VDFSizeTier{{MinSize: "0 B", Iterations: 8}}, testVDFReloadConfig())
	if err != nil {
		t.Fatal(err)
	}
	reloader := newVDFPolicyReloader(&vdfService{policy: policy}, path, testVDFReloadConfig(), nil)
	reloader.close()
	reloader.close()
}

func testVDFReloadConfig() config.VDF {
	return config.VDF{ElevatedMultiplier: 2, SevereMultiplier: 4, MaxIterations: 100}
}

func writeVDFReloadConfig(t *testing.T, path string, iterations uint64) {
	t.Helper()
	text := "vdf_size_tiers:\n  - min_size: \"0 B\"\n    iterations: " +
		strconv.FormatUint(iterations, 10) + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
