package swarmstate

import (
	"sync"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

type Partial struct {
	AssetID    string
	ManifestID string
	Path       string
	Bitset     []byte
	Trusted    []byte // runtime-only: persisted verified pieces revalidated this process
	Revision   uint64
}

type peerFailure struct {
	failures int
	retryAt  time.Time
}

type Registry struct {
	mu            sync.RWMutex
	manifests     map[string]protocolv2.SwarmManifest
	partials      map[string]Partial
	sources       map[string][]protocolv2.SwarmSource // manifest id -> latest source snapshot
	sourceUpdated map[string]time.Time
	peerFailures  map[string]peerFailure
	wake          func()
	wakeScheduled bool
}

func New() *Registry {
	return &Registry{manifests: map[string]protocolv2.SwarmManifest{}, partials: map[string]Partial{},
		sources: map[string][]protocolv2.SwarmSource{}, sourceUpdated: map[string]time.Time{}, peerFailures: map[string]peerFailure{}}
}
func key(asset, manifest string) string { return asset + "\x00" + manifest }
func cloneManifest(m protocolv2.SwarmManifest) protocolv2.SwarmManifest {
	m.PieceHashes = append([]byte(nil), m.PieceHashes...)
	return m
}
func cloneSources(in []protocolv2.SwarmSource) []protocolv2.SwarmSource {
	out := make([]protocolv2.SwarmSource, len(in))
	for i, s := range in {
		out[i] = s
		out[i].Availability = append([]byte(nil), s.Availability...)
	}
	return out
}
func (r *Registry) SetManifest(m protocolv2.SwarmManifest) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.manifests[m.ManifestID] = cloneManifest(m)
	r.mu.Unlock()
}
func (r *Registry) Manifest(id string) (protocolv2.SwarmManifest, bool) {
	if r == nil {
		return protocolv2.SwarmManifest{}, false
	}
	r.mu.RLock()
	m, ok := r.manifests[id]
	r.mu.RUnlock()
	return cloneManifest(m), ok
}
func (r *Registry) SetSources(manifestID string, s []protocolv2.SwarmSource) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.sources[manifestID] = cloneSources(s)
	r.sourceUpdated[manifestID] = time.Now()
	r.mu.Unlock()
}
func (r *Registry) Sources(manifestID string) []protocolv2.SwarmSource {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	s := cloneSources(r.sources[manifestID])
	r.mu.RUnlock()
	return s
}
func (r *Registry) SetPartial(p Partial) {
	if r == nil {
		return
	}
	p.Bitset = append([]byte(nil), p.Bitset...)
	p.Trusted = append([]byte(nil), p.Trusted...)
	r.mu.Lock()
	r.partials[key(p.AssetID, p.ManifestID)] = p
	r.mu.Unlock()
}
func (r *Registry) Partial(asset, manifest string) (Partial, bool) {
	if r == nil {
		return Partial{}, false
	}
	r.mu.RLock()
	p, ok := r.partials[key(asset, manifest)]
	r.mu.RUnlock()
	p.Bitset = append([]byte(nil), p.Bitset...)
	p.Trusted = append([]byte(nil), p.Trusted...)
	return p, ok
}
func (r *Registry) UpdateBitset(asset, manifest string, bits []byte) {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.partials[key(asset, manifest)]
	p.AssetID = asset
	p.ManifestID = manifest
	p.Bitset = append([]byte(nil), bits...)
	if len(p.Trusted) != len(bits) {
		p.Trusted = make([]byte, len(bits))
	}
	r.partials[key(asset, manifest)] = p
	r.mu.Unlock()
}
func (r *Registry) MarkTrusted(asset, manifest string, index int, trusted bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.partials[key(asset, manifest)]
	if index >= 0 && index/8 < len(p.Trusted) {
		if trusted {
			p.Trusted[index/8] |= 1 << uint(index%8)
		} else {
			p.Trusted[index/8] &^= 1 << uint(index%8)
		}
	}
	r.partials[key(asset, manifest)] = p
	r.mu.Unlock()
}
func (r *Registry) RemovePartial(asset, manifest string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.partials, key(asset, manifest))
	r.mu.Unlock()
}

func (r *Registry) SetWake(fn func()) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.wake = fn
	r.mu.Unlock()
}
func (r *Registry) MarkPiece(asset, manifest string, index int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.partials[key(asset, manifest)]
	if index >= 0 && index/8 < len(p.Bitset) {
		p.Bitset[index/8] |= 1 << uint(index%8)
		p.Trusted[index/8] |= 1 << uint(index%8)
		p.Revision++
	}
	r.partials[key(asset, manifest)] = p
	r.scheduleWakeLocked()
	r.mu.Unlock()
}
func (r *Registry) ClearPiece(asset, manifest string, index int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.partials[key(asset, manifest)]
	if index >= 0 && index/8 < len(p.Bitset) {
		p.Bitset[index/8] &^= 1 << uint(index%8)
		p.Trusted[index/8] &^= 1 << uint(index%8)
		p.Revision++
	}
	r.partials[key(asset, manifest)] = p
	r.scheduleWakeLocked()
	r.mu.Unlock()
}
func (r *Registry) Availabilities() []Partial {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Partial, 0, len(r.partials))
	for _, p := range r.partials {
		p.Bitset = append([]byte(nil), p.Bitset...)
		p.Trusted = append([]byte(nil), p.Trusted...)
		out = append(out, p)
	}
	return out
}

func (r *Registry) scheduleWakeLocked() {
	if r.wake == nil || r.wakeScheduled {
		return
	}
	r.wakeScheduled = true
	time.AfterFunc(100*time.Millisecond, func() {
		r.mu.Lock()
		r.wakeScheduled = false
		wake := r.wake
		r.mu.Unlock()
		if wake != nil {
			wake()
		}
	})
}
