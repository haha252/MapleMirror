package public

import (
	"database/sql"
	"time"
)

const routingPressureMaxAge = 2 * time.Minute

func (s Store) selectRoutableAsset(candidates []routableAssetInfo) (routableAssetInfo, error) {
	if len(candidates) == 0 {
		return routableAssetInfo{}, sql.ErrNoRows
	}
	best := candidates[0]
	bestScore := s.routingScore(best)
	for _, item := range candidates[1:] {
		score := s.routingScore(item)
		if betterRoutableCandidate(item, score, best, bestScore) {
			best = item
			bestScore = score
		}
	}
	return best, nil
}

func (s Store) routingScore(item routableAssetInfo) float64 {
	score := float64(item.Priority)
	if s.Runtime == nil {
		return score
	}
	pressure := s.Runtime.LatestRoutingPressure(item.NodeID, routingPressureMaxAge)
	if !pressure.Valid {
		return score
	}
	ratio := pressure.PressureRatio
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 2 {
		ratio = 2
	}
	active := pressure.ActiveDownloads
	if active < 0 {
		active = 0
	}
	if active > 100 {
		active = 100
	}
	return score - ratio*50 - float64(active)*0.1
}

func betterRoutableCandidate(candidate routableAssetInfo, candidateScore float64,
	current routableAssetInfo, currentScore float64) bool {
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	if candidate.Priority != current.Priority {
		return candidate.Priority > current.Priority
	}
	candidateHeartbeat := parseHeartbeatTime(candidate.LastHeartbeat)
	currentHeartbeat := parseHeartbeatTime(current.LastHeartbeat)
	if !candidateHeartbeat.Equal(currentHeartbeat) {
		return candidateHeartbeat.After(currentHeartbeat)
	}
	return candidate.NodeID < current.NodeID
}

func parseHeartbeatTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
