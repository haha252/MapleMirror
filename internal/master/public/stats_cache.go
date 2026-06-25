package public

import (
	"context"
	"sync"
	"time"
)

const (
	statsFastCacheTTL    = 5 * time.Second
	statsDetailsCacheTTL = 30 * time.Second
)

type statsCache struct {
	fastEntry    cachedStatsValue[statsFastSnapshot]
	detailsEntry cachedStatsValue[statsDetailsSnapshot]
}

type cachedStatsValue[T any] struct {
	mu        sync.Mutex
	expiresAt time.Time
	value     T
	hasValue  bool
}

func (s Server) statsCache() *statsCache {
	if s.StatsCache != nil {
		return s.StatsCache
	}
	return &statsCache{}
}

func (c *statsCache) fast(ctx context.Context, store Store) (statsFastSnapshot, error) {
	return c.fastEntry.get(ctx, statsFastCacheTTL, store.StatsRealtime)
}

func (c *statsCache) details(ctx context.Context, store Store) (statsDetailsSnapshot, error) {
	return c.detailsEntry.get(ctx, statsDetailsCacheTTL, func(ctx context.Context) (statsDetailsSnapshot, error) {
		stats, err := store.StatsDashboard(ctx)
		if err != nil {
			return statsDetailsSnapshot{}, err
		}
		nodes, err := store.Nodes(ctx)
		if err != nil {
			return statsDetailsSnapshot{}, err
		}
		return compactStatsDetails(stats, nodes), nil
	})
}

func (c *cachedStatsValue[T]) get(ctx context.Context, ttl time.Duration,
	load func(context.Context) (T, error)) (T, error) {
	now := timeNow()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasValue && now.Before(c.expiresAt) {
		return c.value, nil
	}
	value, err := load(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	c.value = value
	c.hasValue = true
	c.expiresAt = now.Add(ttl)
	return value, nil
}
