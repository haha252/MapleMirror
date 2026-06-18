package syncer

import (
	"context"
	"sync"
)

const defaultMaxConcurrentPeerFallbacks = 3

var peerFallbackSlots = make(chan struct{}, defaultMaxConcurrentPeerFallbacks)
var peerFallbackSlotPools = struct {
	sync.Mutex
	byLimit map[int]chan struct{}
}{byLimit: map[int]chan struct{}{}}

func (e Executor) acquirePeerFallback(ctx context.Context) (func(), error) {
	if e.PeerFallbackMaxConcurrent > 0 {
		return acquirePeerFallbackFrom(ctx, peerFallbackSlotsFor(e.PeerFallbackMaxConcurrent))
	}
	return acquirePeerFallbackFrom(ctx, peerFallbackSlots)
}

func peerFallbackSlotsFor(limit int) chan struct{} {
	if limit <= 0 {
		return peerFallbackSlots
	}
	peerFallbackSlotPools.Lock()
	defer peerFallbackSlotPools.Unlock()
	slots, ok := peerFallbackSlotPools.byLimit[limit]
	if !ok {
		slots = make(chan struct{}, limit)
		peerFallbackSlotPools.byLimit[limit] = slots
	}
	return slots
}

func acquirePeerFallbackFrom(ctx context.Context, slots chan struct{}) (func(), error) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type assetInflight struct {
	done chan struct{}
}

var assetDownloads = struct {
	sync.Mutex
	active map[string]*assetInflight
}{active: map[string]*assetInflight{}}

func beginAssetDownload(ctx context.Context, assetID string) (func(), error) {
	if assetID == "" {
		return func() {}, nil
	}
	for {
		assetDownloads.Lock()
		if current, ok := assetDownloads.active[assetID]; ok {
			wait := current.done
			assetDownloads.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		current := &assetInflight{done: make(chan struct{})}
		assetDownloads.active[assetID] = current
		assetDownloads.Unlock()
		return func() {
			assetDownloads.Lock()
			delete(assetDownloads.active, assetID)
			close(current.done)
			assetDownloads.Unlock()
		}, nil
	}
}
