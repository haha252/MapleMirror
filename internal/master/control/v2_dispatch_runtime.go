package control

import (
	"sync"
	"time"
)

type runtimeV2Dispatch struct {
	PauseUntil      time.Time
	CapacityBarrier uint64
	WaitingStatus   bool
}

// Serialize domain claims and capacity feedback across the reader, timers and
// wake loop, including a connection takeover. Never hold RuntimeStore.mu here.
func (s *RuntimeStore) lockV2Tasks(nodeID string) func() {
	value, _ := s.v2TaskLocks.LoadOrStore(nodeID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *RuntimeStore) v2DispatchState(nodeID string) runtimeV2Dispatch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest[nodeID].V2Dispatch
}

func (s *RuntimeStore) setV2DispatchState(nodeID string, state runtimeV2Dispatch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.V2Dispatch = state
	s.latest[nodeID] = node
}
