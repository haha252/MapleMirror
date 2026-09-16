package main

import (
	"os"
	"time"

	"mirror-server/internal/bootstrap"
	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/logging"
	mastercontrol "mirror-server/internal/master/control"
)

func startControlServices(cfg config.Master, repo mastercontrol.Repository, logger *logging.Logger) {
	timeout, _ := time.ParseDuration(cfg.Node.HeartbeatTimeout)
	grace, _ := time.ParseDuration(cfg.Node.HeartbeatOfflineGrace)
	interval, _ := time.ParseDuration(cfg.Node.HeartbeatInterval)
	probes := publicProbeService(cfg, repo, logger)
	if cfg.Server.ControlListen != "" && cfg.Node.TLS.CertFile != "" && cfg.Node.TLS.KeyFile != "" {
		tlsCfg, err := controltls.ControlServer(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile, cfg.Node.TLS.ClientCAFile)
		startTLSListener(cfg.Server.ControlListen, tlsCfg, err, logger, mastercontrol.ControlServer{
			Repo: repo, HeartbeatInterval: interval, HeartbeatTimeout: timeout, Logger: logger,
			PublicProbes: probes,
		}.Handle)
	}
	if cfg.Server.ControlWSListen != "" && cfg.Node.TLS.CertFile != "" && cfg.Node.TLS.KeyFile != "" {
		tlsCfg, err := controltls.ControlServer(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile, cfg.Node.TLS.ClientCAFile)
		v2 := &mastercontrol.V2Server{Repo: repo, StatusInterval: interval, HeartbeatTimeout: timeout, Logger: logger, PublicProbes: probes}
		startHTTPSTLSListener(cfg.Server.ControlWSListen, tlsCfg, err, logger, v2.Handler())
	}
	if cfg.Server.EnrollmentWSListen != "" && cfg.Node.TLS.CertFile != "" && cfg.Node.TLS.KeyFile != "" {
		tlsCfg, err := controltls.EnrollmentServer(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile, "")
		enrollTimeout, _ := time.ParseDuration(cfg.Node.EnrollmentTimeout)
		publicKey, keyErr := bootstrap.PublicKeyPEM(cfg.DownloadToken.VerifyPublicKeyFile)
		if keyErr != nil {
			err = keyErr
		}
		caData, caErr := os.ReadFile(cfg.Node.TLS.CAFile)
		if caErr != nil {
			err = caErr
		}
		enroll := mastercontrol.EnrollmentServer{Repo: repo, EnrollmentTimeout: enrollTimeout, DownloadTokenPublicKeyPEM: publicKey, MasterCAPEM: string(caData)}
		startHTTPSTLSListener(cfg.Server.EnrollmentWSListen, tlsCfg, err, logger, enroll.WebSocketHandler())
	}
	if cfg.Server.EnrollmentListen != "" && cfg.Node.TLS.CertFile != "" && cfg.Node.TLS.KeyFile != "" {
		tlsCfg, err := controltls.EnrollmentServer(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile, "")
		enrollTimeout, _ := time.ParseDuration(cfg.Node.EnrollmentTimeout)
		publicKey, keyErr := bootstrap.PublicKeyPEM(cfg.DownloadToken.VerifyPublicKeyFile)
		if keyErr != nil {
			err = keyErr
		}
		caData, caErr := os.ReadFile(cfg.Node.TLS.CAFile)
		if caErr != nil {
			err = caErr
		}
		startTLSListener(cfg.Server.EnrollmentListen, tlsCfg, err, logger, mastercontrol.EnrollmentServer{
			Repo: repo, EnrollmentTimeout: enrollTimeout, DownloadTokenPublicKeyPEM: publicKey,
			MasterCAPEM: string(caData),
		}.Handle)
	}
	startHeartbeatSweep(repo, timeout, grace, logger)
}
