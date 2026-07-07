package config

func applyAbuseControlDefaults(c *AbuseControl, warn WarnFunc) {
	setString(&c.Mode, "enforce", "abuse_control.mode", warn)
	if c.Challenge.BurstWindow == "" {
		c.Challenge.BurstWindow = "1m"
		warnDefault(warn, "abuse_control.challenge.burst_window", "1m")
	}
	if c.Challenge.RollingWindow == "" {
		c.Challenge.RollingWindow = "10m"
		warnDefault(warn, "abuse_control.challenge.rolling_window", "10m")
	}
	if c.Challenge.InvalidSolutionWeight == 0 {
		c.Challenge.InvalidSolutionWeight = 4
		warnDefault(warn, "abuse_control.challenge.invalid_solution_weight", "4")
	}
	if c.Challenge.ElevatedBits == 0 {
		c.Challenge.ElevatedBits = 2
		warnDefault(warn, "abuse_control.challenge.elevated_bits", "2")
	}
	if c.Challenge.SevereBits == 0 {
		c.Challenge.SevereBits = 4
		warnDefault(warn, "abuse_control.challenge.severe_bits", "4")
	}
	if c.Challenge.MaxBits == 0 {
		c.Challenge.MaxBits = 28
		warnDefault(warn, "abuse_control.challenge.max_bits", "28")
	}
	if c.Challenge.Exact.ElevatedBurst == 0 {
		c.Challenge.Exact.ElevatedBurst = 6
		warnDefault(warn, "abuse_control.challenge.exact.elevated_burst", "6")
	}
	if c.Challenge.Exact.ElevatedRolling == 0 {
		c.Challenge.Exact.ElevatedRolling = 15
		warnDefault(warn, "abuse_control.challenge.exact.elevated_rolling", "15")
	}
	if c.Challenge.Exact.SevereBurst == 0 {
		c.Challenge.Exact.SevereBurst = 12
		warnDefault(warn, "abuse_control.challenge.exact.severe_burst", "12")
	}
	if c.Challenge.Exact.SevereRolling == 0 {
		c.Challenge.Exact.SevereRolling = 25
		warnDefault(warn, "abuse_control.challenge.exact.severe_rolling", "25")
	}
	if c.Challenge.Exact.RejectBurst == 0 {
		c.Challenge.Exact.RejectBurst = 20
		warnDefault(warn, "abuse_control.challenge.exact.reject_burst", "20")
	}
	if c.Challenge.Exact.RejectRolling == 0 {
		c.Challenge.Exact.RejectRolling = 30
		warnDefault(warn, "abuse_control.challenge.exact.reject_rolling", "30")
	}
	if c.Challenge.Network.ElevatedBurst == 0 {
		c.Challenge.Network.ElevatedBurst = 60
		warnDefault(warn, "abuse_control.challenge.network.elevated_burst", "60")
	}
	if c.Challenge.Network.ElevatedRolling == 0 {
		c.Challenge.Network.ElevatedRolling = 150
		warnDefault(warn, "abuse_control.challenge.network.elevated_rolling", "150")
	}
	if c.Challenge.Network.SevereBurst == 0 {
		c.Challenge.Network.SevereBurst = 120
		warnDefault(warn, "abuse_control.challenge.network.severe_burst", "120")
	}
	if c.Challenge.Network.SevereRolling == 0 {
		c.Challenge.Network.SevereRolling = 250
		warnDefault(warn, "abuse_control.challenge.network.severe_rolling", "250")
	}
	if c.Challenge.Network.RejectBurst == 0 {
		c.Challenge.Network.RejectBurst = 200
		warnDefault(warn, "abuse_control.challenge.network.reject_burst", "200")
	}
	if c.Challenge.Network.RejectRolling == 0 {
		c.Challenge.Network.RejectRolling = 300
		warnDefault(warn, "abuse_control.challenge.network.reject_rolling", "300")
	}
	if c.Blocked.FlushInterval == "" {
		c.Blocked.FlushInterval = "30s"
		warnDefault(warn, "abuse_control.blocked.flush_interval", "30s")
	}
	if c.Blocked.FlushBatch == 0 {
		c.Blocked.FlushBatch = 100
		warnDefault(warn, "abuse_control.blocked.flush_batch", "100")
	}
	if c.Blocked.CacheNegative == "" {
		c.Blocked.CacheNegative = "30s"
		warnDefault(warn, "abuse_control.blocked.cache_negative_ttl", "30s")
	}
	if c.Blocked.Escalation.Level1Attempts == 0 {
		c.Blocked.Escalation.Level1Attempts = 10
		warnDefault(warn, "abuse_control.blocked.escalation.level_1_attempts", "10")
	}
	if c.Blocked.Escalation.Level1Duration == "" {
		c.Blocked.Escalation.Level1Duration = "720h"
		warnDefault(warn, "abuse_control.blocked.escalation.level_1_duration", "720h")
	}
	if c.Blocked.Escalation.Level2Attempts == 0 {
		c.Blocked.Escalation.Level2Attempts = 50
		warnDefault(warn, "abuse_control.blocked.escalation.level_2_attempts", "50")
	}
	if c.Blocked.Escalation.Level2Duration == "" {
		c.Blocked.Escalation.Level2Duration = "2160h"
		warnDefault(warn, "abuse_control.blocked.escalation.level_2_duration", "2160h")
	}
	if c.Blocked.Escalation.Level3Attempts == 0 {
		c.Blocked.Escalation.Level3Attempts = 200
		warnDefault(warn, "abuse_control.blocked.escalation.level_3_attempts", "200")
	}
	if c.Blocked.Escalation.Level3Duration == "" {
		c.Blocked.Escalation.Level3Duration = "8760h"
		warnDefault(warn, "abuse_control.blocked.escalation.level_3_duration", "8760h")
	}
	if c.Punishment.Enabled == nil {
		value := true
		c.Punishment.Enabled = &value
		warnDefault(warn, "abuse_control.punishment.enabled", "true")
	}
	if c.Punishment.Difficulty == 0 {
		c.Punishment.Difficulty = 128
		warnDefault(warn, "abuse_control.punishment.difficulty", "128")
	}
	if c.Punishment.TotalAttempts == 0 {
		c.Punishment.TotalAttempts = 30
		warnDefault(warn, "abuse_control.punishment.total_attempts", "30")
	}
	if c.Punishment.BurstAttempts == 0 {
		c.Punishment.BurstAttempts = 20
		warnDefault(warn, "abuse_control.punishment.burst_attempts", "20")
	}
	if c.Punishment.RollingAttempts == 0 {
		c.Punishment.RollingAttempts = 30
		warnDefault(warn, "abuse_control.punishment.rolling_attempts", "30")
	}
	if c.Punishment.WorkerLimit == 0 {
		c.Punishment.WorkerLimit = 32
		warnDefault(warn, "abuse_control.punishment.worker_limit", "32")
	}
}
