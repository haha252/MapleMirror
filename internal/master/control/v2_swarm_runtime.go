package control

import (
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

type runtimeSwarmAvailability struct {
	ManifestID string
	Revision   uint64
	Bitset     []byte
	UpdatedAt  time.Time
}

func (s *RuntimeStore) UpdateSwarmAvailability(nodeID string, a protocolv2.SwarmAvailability) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	if node.SwarmAvailability == nil {
		node.SwarmAvailability = map[string]runtimeSwarmAvailability{}
	}
	bits := append([]byte(nil), a.Bitset...)
	node.SwarmAvailability[a.AssetID] = runtimeSwarmAvailability{ManifestID: a.ManifestID, Revision: a.Revision, Bitset: bits, UpdatedAt: time.Now()}
	s.latest[nodeID] = node
}
func (s *RuntimeStore) swarmAvailability(assetID, manifestID, target string) map[string][]byte {
	out := map[string][]byte{}
	if s == nil {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for nodeID, node := range s.latest {
		if nodeID == target {
			continue
		}
		a, ok := node.SwarmAvailability[assetID]
		if ok && a.ManifestID == manifestID {
			out[nodeID] = append([]byte(nil), a.Bitset...)
		}
	}
	return out
}

func (r Repository) markV2SeedComplete(nodeID string, m protocolv2.SwarmManifest) {
	bits := make([]byte, swarm.BitsetBytes(m.PieceCount))
	for i := 0; i < m.PieceCount; i++ {
		swarm.Set(bits, i)
	}
	r.runtime().UpdateSwarmAvailability(nodeID, protocolv2.SwarmAvailability{
		AssetID: m.AssetID, ManifestID: m.ManifestID, Revision: 1, Bitset: bits,
	})
}

func (s *RuntimeStore) ClearSwarmAsset(assetID string) {
	if s == nil || assetID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for nodeID, node := range s.latest {
		if node.SwarmAvailability == nil {
			continue
		}
		delete(node.SwarmAvailability, assetID)
		s.latest[nodeID] = node
	}
}
