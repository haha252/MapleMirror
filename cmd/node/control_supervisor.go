package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"mirror-server/internal/bootstrap"
	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/node/activity"
	"mirror-server/internal/node/capacity"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/node/eventwake"
	nodeprobe "mirror-server/internal/node/probe"
	"mirror-server/internal/node/swarmstate"
	"mirror-server/internal/node/syncer"
)

type controlSupervisor struct {
	cfg       config.Node
	db        *sql.DB
	logger    *logging.Logger
	address   string
	wsURL     string
	version   string
	executor  syncer.Executor
	limiter   *nodecontrol.TaskLimiter
	bandwidth nodecontrol.BandwidthSampler
	capacity  *capacity.Manager
	activity  *activity.Counters
	eventWake *eventwake.Notifier
	swarm     *swarmstate.Registry
	probes    *nodeprobe.Store
	v2Runtime *nodecontrol.V2Runtime
}

func (s controlSupervisor) run() {
	if s.wsURL != "" {
		s.runV2()
		return
	}
	interval := 5 * time.Second
	for {
		client, err := s.buildClient(interval)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn(context.Background(), "节点控制客户端未就绪",
					slog.String("master", s.address),
					slog.String("error", err.Error()))
			}
			time.Sleep(5 * time.Second)
			continue
		}
		if s.logger != nil {
			s.logger.Debug(context.Background(), "节点控制轮询开始",
				slog.String("node_id", client.NodeID),
				slog.String("master", client.Address),
				slog.String("interval", interval.String()))
		}
		started := time.Now()
		nextInterval, err := client.RunOnce()
		if err != nil {
			if s.handleRecoverableError(client, err) {
				continue
			}
			if s.logger != nil {
				s.logger.Warn(context.Background(), "节点控制轮询失败",
					slog.String("node_id", client.NodeID),
					slog.String("master", client.Address),
					slog.String("error", err.Error()))
			}
		} else if nextInterval > 0 {
			interval = nextInterval
			if s.logger != nil {
				s.logger.Debug(context.Background(), "节点控制轮询完成",
					slog.String("node_id", client.NodeID),
					slog.String("master", client.Address),
					slog.String("next_interval", nextInterval.String()))
			}
		}
		time.Sleep(nextDelay(interval, time.Since(started)))
	}
}

func nextDelay(interval, elapsed time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	if elapsed >= interval {
		return 0
	}
	return interval - elapsed
}

func (s controlSupervisor) buildClient(interval time.Duration) (*nodecontrol.Client, error) {
	store := nodecontrol.IdentityStore{DB: s.db, CertFile: s.cfg.TLS.CertFile,
		KeyFile: s.cfg.TLS.KeyFile, CAFile: s.cfg.TLS.CAFile}
	tlsCfg, err := store.TLSConfig(s.cfg.TLS.ServerName, true)
	if err != nil {
		return nil, err
	}
	nodeID, err := store.NodeID()
	if err != nil {
		return nil, err
	}
	return &nodecontrol.Client{
		NodeID: nodeID, Address: s.address, PublicDownloadBaseURL: s.cfg.Server.PublicDownloadBaseURL,
		TargetBandwidthBPS: s.cfg.Bandwidth.TargetBPS,
		MaxMirrorProjects:  s.cfg.Sync.MaxMirrorProjects, ForcePeerDownload: s.cfg.Sync.ForcePeerDownload, TLSConfig: tlsCfg,
		SoftwareVersion: s.version,
		Storage:         s.cfg.Storage.Directory, HeartbeatInterval: interval,
		Logger: s.logger, Executor: s.executor, DB: s.db, TaskLimiter: s.limiter,
		TaskTimeout: syncer.DefaultHTTPClientTimeout,
		Bandwidth:   s.bandwidth, Capacity: s.capacity, Activity: s.activity, EventWake: s.eventWake,
		Swarm: s.swarm, ProbeStore: s.probes, V2Runtime: s.v2Runtime,
	}, nil
}

func (s controlSupervisor) handleRecoverableError(client *nodecontrol.Client, err error) bool {
	rejection, ok := err.(nodecontrol.RejectionError)
	if !ok || !rejection.Recoverable() {
		return false
	}
	if !bootstrap.Interactive() {
		if s.logger != nil {
			s.logger.Warn(context.Background(), "节点证书被主节点拒绝，但当前不是交互式终端，无法自动重登记",
				slog.String("node_id", client.NodeID),
				slog.String("master", client.Address),
				slog.String("code", rejection.Code),
				slog.String("error", rejection.Error()))
		}
		return false
	}
	if s.logger != nil {
		s.logger.Warn(context.Background(), "节点证书被主节点拒绝，准备重新登记",
			slog.String("node_id", client.NodeID),
			slog.String("master", client.Address),
			slog.String("code", rejection.Code),
			slog.String("error", rejection.Error()))
	}
	if err := s.reEnroll(client, rejection); err != nil {
		if s.logger != nil {
			s.logger.Warn(context.Background(), "节点重新登记失败",
				slog.String("node_id", client.NodeID),
				slog.String("master", client.Address),
				slog.String("error", err.Error()))
		}
		return false
	}
	return true
}

func (s controlSupervisor) reEnroll(client *nodecontrol.Client, rejection nodecontrol.RejectionError) error {
	console := bootstrap.NewConsole()
	console.Println("主节点拒绝了控制连接：", rejection.Error())
	code, err := console.Ask("请输入新的一次性配对码", "")
	if err != nil {
		return err
	}
	if code == "" {
		return errors.New("未输入新的配对码")
	}
	identity := nodecontrol.IdentityStore{DB: s.db, CertFile: s.cfg.TLS.CertFile,
		KeyFile: s.cfg.TLS.KeyFile, CAFile: s.cfg.TLS.CAFile}
	tlsCfg, err := identity.TLSConfig(s.cfg.TLS.ServerName, false)
	if err != nil {
		return err
	}
	if err := resetNodeIdentityForReEnrollment(s.cfg, s.db, s.logger); err != nil {
		return err
	}
	if err := identity.SavePairingCode(code); err != nil {
		return err
	}
	enroller := nodecontrol.Enroller{
		NodeName:           s.cfg.Node.Name,
		Address:            "",
		WSAddress:          s.cfg.Master.EnrollmentWSAddress,
		TLSConfig:          tlsCfg,
		CodeFile:           s.cfg.Pairing.CodeFile,
		CredentialFile:     s.cfg.Pairing.CredentialFile,
		TokenPublicKeyFile: s.cfg.Download.VerifyPublicKeyFile,
		Identity:           identity,
	}
	if s.cfg.Master.EnrollmentAddress == "" && s.cfg.Master.EnrollmentWSAddress == "" {
		return errors.New("主节点登记地址为空，无法重新登记")
	}
	if s.cfg.Master.EnrollmentAddress != "" {
		parsed, err := url.Parse(s.cfg.Master.EnrollmentAddress)
		if err != nil {
			return err
		}
		enroller.Address = parsed.Host
	}
	if s.logger != nil {
		nodeID := ""
		if client != nil {
			nodeID = client.NodeID
		}
		s.logger.Info(context.Background(), "节点开始重新登记",
			slog.String("node_id", nodeID),
			slog.String("master", enroller.Address))
	}
	return enroller.RunUntilComplete(15 * time.Minute)
}

func (s controlSupervisor) runV2() {
	interval := 5 * time.Second
	backoff := time.Second
	for {
		client, err := s.buildClient(interval)
		if err == nil {
			started := time.Now()
			next, runErr := client.RunV2(context.Background(), s.wsURL)
			if next > 0 {
				interval = next
			}
			err = runErr
			if time.Since(started) > 30*time.Second {
				backoff = time.Second
			}
		}
		if err != nil {
			var rejection nodecontrol.RejectionError
			if errors.As(err, &rejection) && rejection.Recoverable() && bootstrap.Interactive() {
				if reErr := s.reEnroll(nil, rejection); reErr == nil {
					backoff = time.Second
					continue
				}
			}
		}
		if err != nil && s.logger != nil {
			s.logger.Warn(context.Background(), "control.v2 WSS 连接中断",
				slog.String("master", s.wsURL), slog.String("error", err.Error()),
				slog.Duration("retry_in", backoff))
		}
		time.Sleep(backoff + time.Duration(time.Now().UnixNano()%int64(backoff/5+1)))
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}
