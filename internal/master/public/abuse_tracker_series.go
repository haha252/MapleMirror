package public

import (
	"net/netip"
	"strings"
	"time"
)

type thresholdResult struct {
	level      abuseLevel
	retryAfter time.Duration
}

func thresholdLevel(burst, rolling, elevatedBurst, elevatedRolling, severeBurst, severeRolling, rejectBurst, rejectRolling int64) thresholdResult {
	switch {
	case burst >= rejectBurst || rolling >= rejectRolling:
		return thresholdResult{level: abuseLevelReject, retryAfter: 10 * time.Minute}
	case burst >= severeBurst || rolling >= severeRolling:
		return thresholdResult{level: abuseLevelSevere}
	case burst >= elevatedBurst || rolling >= elevatedRolling:
		return thresholdResult{level: abuseLevelElevated}
	default:
		return thresholdResult{}
	}
}

func abuseKeys(kind, clientPrefix string) (string, string, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(clientPrefix))
	if err != nil {
		return "", "", false
	}
	k := strings.TrimSpace(kind)
	if k == "" {
		k = "web"
	}
	exact := k + "|exact|" + prefix.String()
	addr := prefix.Addr()
	if addr.Is4() {
		return exact, k + "|network|" + netip.PrefixFrom(addr, 24).Masked().String(), true
	}
	return exact, k + "|network|" + netip.PrefixFrom(addr, 64).Masked().String(), true
}

func (s *abuseSeries) add(now time.Time, weight int64) {
	s.events = append(s.events, abuseEvent{at: now, weight: weight})
	s.lastUsed = now
}

func (s *abuseSeries) prune(now time.Time, window time.Duration) {
	if s == nil {
		return
	}
	if window <= 0 {
		window = 10 * time.Minute
	}
	cutoff := now.Add(-window)
	idx := 0
	for idx < len(s.events) && s.events[idx].at.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		s.events = append([]abuseEvent(nil), s.events[idx:]...)
	}
}

func (s *abuseSeries) counts(now time.Time, burstWindow, rollingWindow time.Duration) (int64, int64) {
	if s == nil {
		return 0, 0
	}
	s.prune(now, rollingWindow)
	burstCutoff := now.Add(-burstWindow)
	var burst, rolling int64
	for _, event := range s.events {
		rolling += event.weight
		if event.at.After(burstCutoff) || event.at.Equal(burstCutoff) {
			burst += event.weight
		}
	}
	return burst, rolling
}
