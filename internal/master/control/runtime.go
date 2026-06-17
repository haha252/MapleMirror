package control

import (
	"sync"
	"time"
)

type RuntimeStore struct {
	mu               sync.RWMutex
	sessions         map[string]runtimeSession
	latest           map[string]runtimeNode
	inventoryBatches map[string]runtimeInventoryBatch
}

type runtimeSession struct {
	NodeID        string
	CertificateID string
	RequestID     string
	ConnectedAt   string
	LastSequence  uint64
}

type runtimeNode struct {
	Heartbeat              runtimeHeartbeat
	Inventory              runtimeInventoryReport
	Pressure               runtimePressureReport
	SyncTaskSlotsAvailable int
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
		sessions:         map[string]runtimeSession{},
		latest:           map[string]runtimeNode{},
		inventoryBatches: map[string]runtimeInventoryBatch{},
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
	delete(s.inventoryBatches, session.NodeID)
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

func (s *RuntimeStore) SetSyncTaskSlotsAvailable(nodeID string, slots int) {
	if s == nil {
		return
	}
	if slots < 0 {
		slots = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.SyncTaskSlotsAvailable = slots
	s.latest[nodeID] = node
}

func (s *RuntimeStore) SyncTaskSlotsAvailable(nodeID string) int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest[nodeID].SyncTaskSlotsAvailable
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
