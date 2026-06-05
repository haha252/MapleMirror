package syncer

import (
	"context"
	"sync"
)

const maxConcurrentPeerFallbacks = 3

var peerFallbackSlots = make(chan struct{}, maxConcurrentPeerFallbacks)

func acquirePeerFallback(ctx context.Context) error {
	select {
	case peerFallbackSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releasePeerFallback() {
	<-peerFallbackSlots
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
