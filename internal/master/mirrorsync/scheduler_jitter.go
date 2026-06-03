package mirrorsync

import (
	"hash/fnv"
	"time"
)

func stableProjectJitter(projectID string, interval time.Duration) time.Duration {
	window := 30 * time.Second
	if interval/10 < window {
		window = interval / 10
	}
	if window <= 0 {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(projectID))
	return time.Duration(uint64(hash.Sum32()) % uint64(window))
}
