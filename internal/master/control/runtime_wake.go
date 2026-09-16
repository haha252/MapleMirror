package control

import "time"

func (s *RuntimeStore) NotifySyncTasks(nodeIDs ...string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncTaskWake == nil {
		s.syncTaskWake = map[string]int{}
	}
	for _, nodeID := range nodeIDs {
		if nodeID == "" {
			continue
		}
		if s.nodeSessionActiveLocked(nodeID) {
			s.syncTaskWake[nodeID]++
			if ch := s.syncTaskSignals[nodeID]; ch != nil {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (s *RuntimeStore) SyncTaskWakeChannel(nodeID string) <-chan struct{} {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncTaskSignals == nil {
		s.syncTaskSignals = map[string]chan struct{}{}
	}
	ch := s.syncTaskSignals[nodeID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.syncTaskSignals[nodeID] = ch
	}
	if s.syncTaskWake[nodeID] > 0 {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return ch
}

func (s *RuntimeStore) ConsumeSyncTaskWake(nodeID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncTaskWake[nodeID] <= 0 {
		return false
	}
	delete(s.syncTaskWake, nodeID)
	return true
}

func (s *RuntimeStore) nodeSessionActiveLocked(nodeID string) bool {
	for _, item := range s.sessions {
		if item.NodeID == nodeID {
			return true
		}
	}
	return false
}

func (r Repository) scheduleSyncTaskRetryWake(nodeID, retryAfter string) {
	if nodeID == "" || retryAfter == "" {
		return
	}
	wakeAt, err := time.Parse(time.RFC3339Nano, retryAfter)
	if err != nil {
		return
	}
	r.scheduleSyncTaskRetryWakeAt(nodeID, wakeAt)
}

func (r Repository) scheduleSyncTaskRetryWakeAt(nodeID string, wakeAt time.Time) {
	if nodeID == "" {
		return
	}
	delay := time.Until(wakeAt)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		r.runtime().NotifySyncTasks(nodeID)
	})
}
