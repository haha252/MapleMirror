package swarmstate

import "time"

func (r *Registry) NeedsSourceRefresh(manifestID string, maxAge time.Duration) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	updated := r.sourceUpdated[manifestID]
	r.mu.RUnlock()
	return updated.IsZero() || time.Since(updated) >= maxAge
}

func peerKey(manifestID, nodeID string) string { return manifestID + "\x00" + nodeID }

func (r *Registry) SourceReady(manifestID, nodeID string) bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	failure := r.peerFailures[peerKey(manifestID, nodeID)]
	r.mu.RUnlock()
	return failure.retryAt.IsZero() || !time.Now().Before(failure.retryAt)
}

func (r *Registry) ReportSourceFailure(manifestID, nodeID string) {
	if r == nil || nodeID == "" {
		return
	}
	r.mu.Lock()
	key := peerKey(manifestID, nodeID)
	f := r.peerFailures[key]
	f.failures++
	shift := f.failures - 1
	if shift > 5 {
		shift = 5
	}
	backoff := time.Second * time.Duration(1<<shift)
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}
	f.retryAt = time.Now().Add(backoff)
	r.peerFailures[key] = f
	r.mu.Unlock()
}

func (r *Registry) ReportSourceSuccess(manifestID, nodeID string) {
	if r == nil || nodeID == "" {
		return
	}
	r.mu.Lock()
	delete(r.peerFailures, peerKey(manifestID, nodeID))
	r.mu.Unlock()
}
