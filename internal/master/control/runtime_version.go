package control

import "strings"

const maxSoftwareVersionLength = 128

func normalizedSoftwareVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) > maxSoftwareVersionLength {
		value = value[:maxSoftwareVersionLength]
	}
	return value
}

func (s *RuntimeStore) SetSoftwareVersion(nodeID, version string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.SoftwareVersion = version
	s.latest[nodeID] = node
}

func (s *RuntimeStore) SoftwareVersion(nodeID string) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest[nodeID].SoftwareVersion
}
