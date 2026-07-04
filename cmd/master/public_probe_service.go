package main

import (
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	mastercontrol "mirror-server/internal/master/control"
)

func publicProbeService(cfg config.Master, repo mastercontrol.Repository,
	logger *logging.Logger) *mastercontrol.PublicProbeService {
	if cfg.Node.PublicProbeEnabled == nil || !*cfg.Node.PublicProbeEnabled {
		return nil
	}
	interval, _ := time.ParseDuration(cfg.Node.PublicProbeInterval)
	timeout, _ := time.ParseDuration(cfg.Node.PublicProbeTimeout)
	ttl, _ := time.ParseDuration(cfg.Node.PublicProbeTTL)
	return &mastercontrol.PublicProbeService{Repo: repo, Logger: logger,
		Config: mastercontrol.PublicProbeConfig{Enabled: true,
			Interval: interval, Timeout: timeout, TTL: ttl,
			NetworkFailures: cfg.Node.PublicProbeNetworkFailures}}
}
