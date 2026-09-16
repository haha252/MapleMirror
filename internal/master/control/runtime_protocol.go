package control

func (s *RuntimeStore) SetControlProtocol(nodeID, protocol string) {
	if s == nil || nodeID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.ControlProtocol = protocol
	s.latest[nodeID] = node
}

func (s *RuntimeStore) ControlProtocol(nodeID string) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest[nodeID].ControlProtocol
}

func (s *RuntimeStore) SetPeerOnly(nodeID string, peerOnly bool) {
	if s == nil || nodeID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.PeerOnly = peerOnly
	s.latest[nodeID] = node
}

func (s *RuntimeStore) PeerOnly(nodeID string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest[nodeID].PeerOnly
}
