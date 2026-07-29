package public

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogCacheExpiresAndEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newCatalogResultCache(250)
	defer cache.close()
	now := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	cache.put("first", catalogPayload{Body: []byte("one"), ETag: "1"})
	cache.put("second", catalogPayload{Body: []byte("two"), ETag: "2"})
	if _, ok := cache.get("first"); !ok {
		t.Fatal("最近使用项应存在")
	}
	cache.put("third", catalogPayload{Body: []byte("three"), ETag: "3"})
	if _, ok := cache.get("second"); ok {
		t.Fatal("最久未使用项应被淘汰")
	}
	now = now.Add(catalogCacheTTL)
	if _, ok := cache.get("first"); ok {
		t.Fatal("到期项不应命中")
	}
}

func TestCatalogCacheSkipsOversizedAndDisabledEntries(t *testing.T) {
	cache := newCatalogResultCache(100)
	defer cache.close()
	cache.put("large", catalogPayload{Body: make([]byte, 101)})
	if len(cache.entries) != 0 {
		t.Fatal("超大响应不应缓存")
	}
	cache.setMaxBytes(0)
	cache.put("small", catalogPayload{Body: []byte("x")})
	if len(cache.entries) != 0 {
		t.Fatal("0 B 应关闭结果缓存")
	}
}

func TestCatalogCacheCoalescesConcurrentLoads(t *testing.T) {
	cache := newCatalogResultCache(1024)
	defer cache.close()
	var loads atomic.Int32
	start := make(chan struct{})
	release := make(chan struct{})
	load := func(context.Context) (catalogPayload, error) {
		loads.Add(1)
		close(start)
		<-release
		return catalogPayload{Body: []byte("body"), ETag: "tag"}, nil
	}
	var wg sync.WaitGroup
	results := make(chan catalogPayload, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload, err := cache.getOrLoad(context.Background(), "same", load)
			if err != nil {
				t.Error(err)
			}
			results <- payload
		}()
	}
	<-start
	close(release)
	wg.Wait()
	close(results)
	if loads.Load() != 1 {
		t.Fatalf("相同条件并发加载次数=%d want 1", loads.Load())
	}
	for result := range results {
		if string(result.Body) != "body" {
			t.Fatalf("并发结果错误：%+v", result)
		}
	}
}

func TestCatalogCacheClearAndCleanupReleaseMemory(t *testing.T) {
	cache := newCatalogResultCache(1024)
	defer cache.close()
	now := time.Now()
	cache.now = func() time.Time { return now }
	cache.put("one", catalogPayload{Body: []byte("body")})
	now = now.Add(catalogCacheTTL)
	cache.cleanup(now)
	if cache.used != 0 || len(cache.entries) != 0 {
		t.Fatalf("过期清理未释放容量：used=%d entries=%d", cache.used, len(cache.entries))
	}
	cache.put("two", catalogPayload{Body: []byte("body")})
	cache.clear()
	if cache.used != 0 || cache.lru.Len() != 0 {
		t.Fatalf("清空缓存未释放容量：used=%d lru=%d", cache.used, cache.lru.Len())
	}
}

func TestCatalogCacheClearDoesNotRestoreInflightOldGeneration(t *testing.T) {
	cache := newCatalogResultCache(1024)
	defer cache.close()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cache.getOrLoad(context.Background(), "old",
			func(context.Context) (catalogPayload, error) {
				close(started)
				<-release
				return catalogPayload{Body: []byte("old")}, nil
			})
	}()
	<-started
	cache.clear()
	close(release)
	<-done
	if _, ok := cache.get("old"); ok {
		t.Fatal("索引换代后旧的并发加载结果不应重新进入缓存")
	}
}
