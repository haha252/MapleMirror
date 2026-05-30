package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"time"

	"mirror-server/internal/bootstrap"
	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/logging"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/node/syncer"
)

type controlSupervisor struct {
	cfg      config.Node
	db       *sql.DB
	logger   *logging.Logger
	address  string
	executor syncer.Executor
}

func (s controlSupervisor) run() {
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
		time.Sleep(interval)
	}
}

func (s controlSupervisor) buildClient(interval time.Duration) (*nodecontrol.Client, error) {
	tlsCfg, err := controltls.NodeClient(s.cfg.TLS.CAFile, s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile, s.cfg.TLS.ServerName)
	if err != nil {
		return nil, err
	}
	store := nodecontrol.IdentityStore{DB: s.db}
	nodeID, err := store.NodeID()
	if err != nil {
		return nil, err
	}
	return &nodecontrol.Client{
		NodeID: nodeID, Address: s.address, TLSConfig: tlsCfg,
		HeartbeatInterval: interval, Logger: s.logger, Executor: s.executor, DB: s.db,
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
	if err := bootstrap.WritePairingCode(s.cfg.Pairing.CodeFile, code); err != nil {
		return err
	}
	_ = os.Remove(s.cfg.Pairing.CredentialFile)
	enroller := nodecontrol.Enroller{
		NodeName: s.cfg.Node.Name,
		Address:  "",
		TLSConfig: nil,
		CodeFile: s.cfg.Pairing.CodeFile,
		CredentialFile: s.cfg.Pairing.CredentialFile,
		TokenPublicKeyFile: s.cfg.Download.VerifyPublicKeyFile,
		Identity: nodecontrol.IdentityStore{DB: s.db, CertFile: s.cfg.TLS.CertFile,
			KeyFile: s.cfg.TLS.KeyFile, CAFile: s.cfg.TLS.CAFile},
	}
	if s.cfg.Master.EnrollmentAddress == "" {
		return errors.New("主节点登记地址为空，无法重新登记")
	}
	parsed, err := url.Parse(s.cfg.Master.EnrollmentAddress)
	if err != nil {
		return err
	}
	enroller.Address = parsed.Host
	tlsCfg, err := controltls.NodeClient(s.cfg.TLS.CAFile, "", "", s.cfg.TLS.ServerName)
	if err != nil {
		return err
	}
	enroller.TLSConfig = tlsCfg
	if s.logger != nil {
		s.logger.Info(context.Background(), "节点开始重新登记",
			slog.String("node_id", client.NodeID),
			slog.String("master", enroller.Address))
	}
	return enroller.RunUntilComplete(15 * time.Minute)
}
