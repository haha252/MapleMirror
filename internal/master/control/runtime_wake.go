package control

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
		}
	}
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
