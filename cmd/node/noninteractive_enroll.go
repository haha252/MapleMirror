package main

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	nodecontrol "mirror-server/internal/node/control"
)

func nonInteractiveEnroll(cfg config.Node, identity nodecontrol.IdentityStore) error {
	if cfg.Node.Name == "" {
		return errors.New("非交互首次登记需要预先配置 node.name")
	}
	if cfg.Pairing.CodeFile == "" {
		return errors.New("非交互首次登记需要 pairing.code_file")
	}
	if cfg.Master.EnrollmentWSAddress == "" && cfg.Master.EnrollmentAddress == "" {
		return errors.New("非交互首次登记缺少主节点登记地址")
	}
	if _, err := identity.PairingCode(cfg.Pairing.CodeFile); err != nil {
		return fmt.Errorf("非交互首次登记无法读取 pairing code: %w", err)
	}
	tlsCfg, err := controltls.NodeClient(cfg.TLS.CAFile, "", "", cfg.TLS.ServerName)
	if err != nil {
		return fmt.Errorf("非交互首次登记需要预置可信 master CA: %w", err)
	}
	addressHost := ""
	if cfg.Master.EnrollmentAddress != "" {
		address, err := url.Parse(cfg.Master.EnrollmentAddress)
		if err != nil {
			return err
		}
		addressHost = address.Host
	}
	enroller := nodecontrol.Enroller{
		NodeName: cfg.Node.Name, Address: addressHost, WSAddress: cfg.Master.EnrollmentWSAddress,
		TLSConfig: tlsCfg, CodeFile: cfg.Pairing.CodeFile, CredentialFile: cfg.Pairing.CredentialFile,
		TokenPublicKeyFile: cfg.Download.VerifyPublicKeyFile, Identity: identity,
	}
	return enroller.RunUntilComplete(15 * time.Minute)
}
