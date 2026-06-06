package control

import "time"

type runtimeInventoryBatch struct {
	Revision   uint64
	ReportedAt string
}

func (s *RuntimeStore) InventoryBatchTime(nodeID string, revision uint64, now time.Time) string {
	if s == nil {
		return now.Format(time.RFC3339Nano)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inventoryBatches == nil {
		s.inventoryBatches = map[string]runtimeInventoryBatch{}
	}
	batch, ok := s.inventoryBatches[nodeID]
	if !ok || batch.Revision != revision {
		batch = runtimeInventoryBatch{
			Revision:   revision,
			ReportedAt: now.Format(time.RFC3339Nano),
		}
		s.inventoryBatches[nodeID] = batch
	}
	return batch.ReportedAt
}

func (s *RuntimeStore) FinishInventoryBatch(nodeID string, revision uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if batch, ok := s.inventoryBatches[nodeID]; ok && batch.Revision == revision {
		delete(s.inventoryBatches, nodeID)
	}
}
