package public

import (
	"database/sql"
	"time"

	"mirror-server/internal/geoip"
)

const routingPressureMaxAge = 2 * time.Minute
const routingRegionBonus = 20

func (s Store) selectRoutableAsset(candidates []routableAssetInfo, requestRegion geoip.Region) (routableAssetInfo, error) {
	if len(candidates) == 0 {
		return routableAssetInfo{}, sql.ErrNoRows
	}
	best := candidates[0]
	bestEffectivePriority := effectiveRoutingPriority(best, requestRegion)
	bestScore := s.routingScore(best, requestRegion)
	for _, item := range candidates[1:] {
		effectivePriority := effectiveRoutingPriority(item, requestRegion)
		score := s.routingScore(item, requestRegion)
		if betterRoutableCandidate(item, effectivePriority, score,
			best, bestEffectivePriority, bestScore) {
			best = item
			bestEffectivePriority = effectivePriority
			bestScore = score
		}
	}
	return best, nil
}

func effectiveRoutingPriority(item routableAssetInfo, requestRegion geoip.Region) int {
	priority := item.Priority
	if requestRegion != geoip.RegionUnknown && item.Region == requestRegion {
		priority += routingRegionBonus
	}
	return priority
}

func (s Store) routingScore(item routableAssetInfo, requestRegion geoip.Region) float64 {
	score := float64(effectiveRoutingPriority(item, requestRegion))
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

func betterRoutableCandidate(candidate routableAssetInfo, candidateEffectivePriority int,
	candidateScore float64, current routableAssetInfo, currentEffectivePriority int,
	currentScore float64) bool {
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	if candidateEffectivePriority != currentEffectivePriority {
		return candidateEffectivePriority > currentEffectivePriority
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
