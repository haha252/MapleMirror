package control

import (
	"database/sql"
	"sync"
	"time"
)

type RuntimeStore struct {
	mu       sync.RWMutex
	sessions map[string]runtimeSession
	latest   map[string]runtimeNode
}

type runtimeSession struct {
	NodeID        string
	CertificateID string
	RequestID     string
	ConnectedAt   string
	LastSequence  uint64
}

type runtimeNode struct {
	Heartbeat runtimeHeartbeat
	Inventory runtimeInventoryReport
	Pressure  runtimePressureReport
}

type runtimeHeartbeat struct {
	State           string
	PressureRatio   float64
	ActiveDownloads int64
	FreeBytes       int64
	TargetBandwidth int64
	ActualBandwidth int64
	ReportedAt      string
	Valid           bool
}

type runtimeInventoryReport struct {
	Revision  int
	Complete  bool
	ItemCount int
	Result    string
	RequestID string
	Reported  string
	Valid     bool
}

type runtimePressureReport struct {
	PressureRatio   float64
	ActiveDownloads int64
	FreeBytes       int64
	TargetBandwidth int64
	ActualBandwidth int64
	RequestID       string
	ReportedAt      string
	Valid           bool
}

func NewRuntimeStore() *RuntimeStore {
	return &RuntimeStore{
		sessions: map[string]runtimeSession{},
		latest:   map[string]runtimeNode{},
	}
}

func (s *RuntimeStore) StartSession(session Session) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, item := range s.sessions {
		if item.NodeID == session.NodeID {
			delete(s.sessions, id)
		}
	}
	s.sessions[session.ID] = runtimeSession{
		NodeID: session.NodeID, CertificateID: session.CertificateID,
		RequestID: session.RequestID, ConnectedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (s *RuntimeStore) CloseSession(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *RuntimeStore) CloseNodeSessions(nodeID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, item := range s.sessions {
		if item.NodeID == nodeID {
			delete(s.sessions, id)
		}
	}
}

func (s *RuntimeStore) CurrentSequence(session Session) (uint64, error) {
	if s == nil {
		return 0, ErrSessionUnavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.sessions[session.ID]
	if !ok || item.NodeID != session.NodeID {
		return 0, ErrSessionUnavailable
	}
	return item.LastSequence, nil
}

func (s *RuntimeStore) UpdateSequence(session Session, seq uint64) error {
	if s == nil {
		return ErrSessionUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[session.ID]
	if !ok || item.NodeID != session.NodeID {
		return ErrSessionUnavailable
	}
	item.LastSequence = seq
	s.sessions[session.ID] = item
	return nil
}

func (s *RuntimeStore) ActiveSession(nodeID string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.sessions {
		if item.NodeID == nodeID {
			return true
		}
	}
	return false
}

func (s *RuntimeStore) LatestInventoryState(nodeID string) (int, bool, bool) {
	if s == nil {
		return 0, false, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.latest[nodeID].Inventory
	return item.Revision, item.Complete, item.Valid
}

func (s *RuntimeStore) MarkHeartbeat(nodeID string, hb runtimeHeartbeat) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.Heartbeat = hb
	s.latest[nodeID] = node
}

func (s *RuntimeStore) MarkInventory(nodeID string, report runtimeInventoryReport) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.Inventory = report
	s.latest[nodeID] = node
}

func (s *RuntimeStore) MarkPressure(nodeID string, report runtimePressureReport) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.Pressure = report
	s.latest[nodeID] = node
}

func (s *RuntimeStore) LatestHeartbeat(nodeID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.latest[nodeID].Heartbeat
	if !item.Valid {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"state": item.State, "pressure_ratio": item.PressureRatio,
		"active_downloads": item.ActiveDownloads, "free_bytes": item.FreeBytes,
		"target_bandwidth_bps": item.TargetBandwidth,
		"actual_bandwidth_bps": item.ActualBandwidth,
		"reported_at":          item.ReportedAt}, nil
}

func (s *RuntimeStore) LatestInventoryReport(nodeID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.latest[nodeID].Inventory
	if !item.Valid {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"revision": item.Revision, "complete": item.Complete,
		"item_count": item.ItemCount, "result": item.Result, "request_id": item.RequestID,
		"reported_at": item.Reported}, nil
}

func (s *RuntimeStore) LatestPressureReport(nodeID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.latest[nodeID].Pressure
	if !item.Valid {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"pressure_ratio": item.PressureRatio,
		"active_downloads": item.ActiveDownloads, "free_bytes": item.FreeBytes,
		"target_bandwidth_bps": item.TargetBandwidth,
		"actual_bandwidth_bps": item.ActualBandwidth,
		"request_id":           item.RequestID, "reported_at": item.ReportedAt}, nil
}
