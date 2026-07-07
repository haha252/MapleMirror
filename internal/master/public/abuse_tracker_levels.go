package public

import "time"

func (t *abuseTracker) levelFor(decision abuseDecision) (abuseLevel, int, time.Duration) {
	exact := thresholdLevel(
		decision.ExactBurst, decision.ExactRoll,
		int64(t.cfg.Challenge.Exact.ElevatedBurst), int64(t.cfg.Challenge.Exact.ElevatedRolling),
		int64(t.cfg.Challenge.Exact.SevereBurst), int64(t.cfg.Challenge.Exact.SevereRolling),
		int64(t.cfg.Challenge.Exact.RejectBurst), int64(t.cfg.Challenge.Exact.RejectRolling),
	)
	network := thresholdLevel(
		decision.NetBurst, decision.NetRoll,
		int64(t.cfg.Challenge.Network.ElevatedBurst), int64(t.cfg.Challenge.Network.ElevatedRolling),
		int64(t.cfg.Challenge.Network.SevereBurst), int64(t.cfg.Challenge.Network.SevereRolling),
		int64(t.cfg.Challenge.Network.RejectBurst), int64(t.cfg.Challenge.Network.RejectRolling),
	)
	if network.level > exact.level {
		exact = network
	}
	switch exact.level {
	case abuseLevelReject:
		return exact.level, 0, exact.retryAfter
	case abuseLevelSevere:
		return exact.level, t.cfg.Challenge.SevereBits, 0
	case abuseLevelElevated:
		return exact.level, t.cfg.Challenge.ElevatedBits, 0
	default:
		return abuseLevelNormal, 0, 0
	}
}
