package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func validateAbuseControl(c AbuseControl) error {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "off", "observe", "enforce":
	default:
		return errors.New("abuse_control.mode 必须是 off、observe 或 enforce")
	}
	if err := validDuration("quota.abuse_control.challenge.burst_window", c.Challenge.BurstWindow); err != nil {
		return err
	}
	if err := validDuration("quota.abuse_control.challenge.rolling_window", c.Challenge.RollingWindow); err != nil {
		return err
	}
	if c.Challenge.InvalidSolutionWeight <= 0 {
		return errors.New("abuse_control.challenge.invalid_solution_weight 必须大于零")
	}
	if c.Challenge.ElevatedBits <= 0 || c.Challenge.SevereBits <= 0 || c.Challenge.MaxBits <= 0 {
		return errors.New("abuse_control.challenge.bits 必须大于零")
	}
	if !(c.Challenge.ElevatedBits < c.Challenge.SevereBits && c.Challenge.SevereBits <= c.Challenge.MaxBits && c.Challenge.MaxBits <= 32) {
		return errors.New("abuse_control.challenge.bits 关系必须满足 elevated < severe <= max 且 max 不得超过 32")
	}
	if err := validateThresholdWindow("abuse_control.challenge.exact", c.Challenge.Exact); err != nil {
		return err
	}
	if err := validateThresholdWindow("abuse_control.challenge.network", c.Challenge.Network); err != nil {
		return err
	}
	if c.Challenge.Network.ElevatedBurst < c.Challenge.Exact.ElevatedBurst ||
		c.Challenge.Network.ElevatedRolling < c.Challenge.Exact.ElevatedRolling ||
		c.Challenge.Network.SevereBurst < c.Challenge.Exact.SevereBurst ||
		c.Challenge.Network.SevereRolling < c.Challenge.Exact.SevereRolling ||
		c.Challenge.Network.RejectBurst < c.Challenge.Exact.RejectBurst ||
		c.Challenge.Network.RejectRolling < c.Challenge.Exact.RejectRolling {
		return errors.New("abuse_control.challenge.network 不能低于 exact 对应阈值")
	}
	if err := validDuration("quota.abuse_control.blocked.flush_interval", c.Blocked.FlushInterval); err != nil {
		return err
	}
	if c.Blocked.FlushBatch <= 0 {
		return errors.New("abuse_control.blocked.flush_batch 必须大于零")
	}
	if err := validDuration("quota.abuse_control.blocked.cache_negative_ttl", c.Blocked.CacheNegative); err != nil {
		return err
	}
	if c.Blocked.Escalation.Level1Attempts <= 0 || c.Blocked.Escalation.Level2Attempts <= c.Blocked.Escalation.Level1Attempts ||
		c.Blocked.Escalation.Level3Attempts <= c.Blocked.Escalation.Level2Attempts {
		return errors.New("abuse_control.blocked.escalation.level_*_attempts 必须严格递增")
	}
	if err := validDuration("quota.abuse_control.blocked.escalation.level_1_duration", c.Blocked.Escalation.Level1Duration); err != nil {
		return err
	}
	if err := validDuration("quota.abuse_control.blocked.escalation.level_2_duration", c.Blocked.Escalation.Level2Duration); err != nil {
		return err
	}
	if err := validDuration("quota.abuse_control.blocked.escalation.level_3_duration", c.Blocked.Escalation.Level3Duration); err != nil {
		return err
	}
	d1, _ := time.ParseDuration(c.Blocked.Escalation.Level1Duration)
	d2, _ := time.ParseDuration(c.Blocked.Escalation.Level2Duration)
	d3, _ := time.ParseDuration(c.Blocked.Escalation.Level3Duration)
	if d1 <= 0 {
		return fmt.Errorf("abuse_control.blocked.escalation.level_1_duration 必须为正")
	}
	if d2 <= 0 {
		return fmt.Errorf("abuse_control.blocked.escalation.level_2_duration 必须为正")
	}
	if d3 <= 0 {
		return fmt.Errorf("abuse_control.blocked.escalation.level_3_duration 必须为正")
	}
	if !(d1 < d2 && d2 < d3) {
		return errors.New("abuse_control.blocked.escalation.level_*_duration 必须严格递增")
	}
	if !(c.Blocked.Escalation.Level1Attempts < c.Blocked.Escalation.Level2Attempts &&
		c.Blocked.Escalation.Level2Attempts < c.Blocked.Escalation.Level3Attempts) {
		return errors.New("abuse_control.blocked.escalation.level_*_attempts 必须严格递增")
	}
	if c.Punishment.Enabled != nil && c.Punishment.Difficulty <= 0 {
		return errors.New("abuse_control.punishment.difficulty 必须大于零")
	}
	if c.Punishment.Difficulty < 33 || c.Punishment.Difficulty > 255 {
		return errors.New("abuse_control.punishment.difficulty 必须在 33 到 255 之间")
	}
	if c.Punishment.TotalAttempts < c.Blocked.Escalation.Level1Attempts {
		return errors.New("abuse_control.punishment.total_attempts 不能低于第一级升级阈值")
	}
	if c.Punishment.BurstAttempts <= 0 || c.Punishment.RollingAttempts <= 0 {
		return errors.New("abuse_control.punishment.*_attempts 必须大于零")
	}
	if c.Punishment.WorkerLimit < 1 || c.Punishment.WorkerLimit > 32 {
		return errors.New("abuse_control.punishment.worker_limit 必须在 1 到 32 之间")
	}
	return nil
}

func validateThresholdWindow(prefix string, w AbuseThresholdWindow) error {
	if !(w.ElevatedBurst < w.SevereBurst && w.SevereBurst < w.RejectBurst) {
		return fmt.Errorf("%s 的 burst 阈值必须满足 elevated < severe < reject", prefix)
	}
	if !(w.ElevatedRolling < w.SevereRolling && w.SevereRolling < w.RejectRolling) {
		return fmt.Errorf("%s 的 rolling 阈值必须满足 elevated < severe < reject", prefix)
	}
	if w.ElevatedBurst <= 0 || w.ElevatedRolling <= 0 || w.SevereBurst <= 0 || w.SevereRolling <= 0 || w.RejectBurst <= 0 || w.RejectRolling <= 0 {
		return fmt.Errorf("%s 的阈值必须大于零", prefix)
	}
	return nil
}
