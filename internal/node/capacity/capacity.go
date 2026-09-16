package capacity

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const DefaultSafetyReserveBytes int64 = 256 << 20

var ErrInsufficientSpace = errors.New("insufficient filesystem capacity")

var capacitySamplePath = samplePath

type Sample struct {
	AvailableBytes int64
	TotalBytes     int64
	Valid          bool
	filesystemID   string
}

type Snapshot struct {
	Asset           Sample
	Partial         Sample
	ReservedAsset   int64
	ReservedPartial int64
}

type Manager struct {
	AssetPath   string
	PartialPath string
	SafetyBytes int64

	mu           sync.Mutex
	reservedByFS map[string]int64
}

func NewManager(assetPath, partialPath string) *Manager {
	return &Manager{AssetPath: assetPath, PartialPath: partialPath,
		SafetyBytes: DefaultSafetyReserveBytes, reservedByFS: make(map[string]int64)}
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	asset := capacitySamplePath(m.AssetPath)
	partial := capacitySamplePath(m.PartialPath)
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{Asset: asset, Partial: partial,
		ReservedAsset: m.reservedByFS[asset.filesystemID], ReservedPartial: m.reservedByFS[partial.filesystemID]}
}

func (m *Manager) ReserveDownload(size int64) (func(), error) {
	if m == nil || size <= 0 {
		return func() {}, nil
	}
	asset := capacitySamplePath(m.AssetPath)
	partial := capacitySamplePath(m.PartialPath)
	if !asset.Valid || !partial.Valid {
		return nil, ErrInsufficientSpace
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	need := map[string]int64{}
	need[partial.filesystemID] += size
	if asset.filesystemID != partial.filesystemID {
		need[asset.filesystemID] += size
	}
	available := map[string]int64{asset.filesystemID: asset.AvailableBytes, partial.filesystemID: partial.AvailableBytes}
	for fs, bytes := range need {
		if available[fs]-m.reservedByFS[fs]-m.safety(available[fs]) < bytes {
			return nil, ErrInsufficientSpace
		}
	}
	for fs, bytes := range need {
		m.reservedByFS[fs] += bytes
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			for fs, bytes := range need {
				m.reservedByFS[fs] -= bytes
				if m.reservedByFS[fs] <= 0 {
					delete(m.reservedByFS, fs)
				}
			}
		})
	}, nil
}

func (m *Manager) safety(available int64) int64 {
	if m.SafetyBytes < 0 {
		return 0
	}
	return m.SafetyBytes
}

func nearestExisting(path string) string {
	path = filepath.Clean(path)
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
