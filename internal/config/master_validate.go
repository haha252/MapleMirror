package config

import (
	"errors"
	"fmt"
	"net"
	"time"
)

func validateMaster(c Master) error {
	if !validListen(c.Server.PublicListen) {
		return errors.New("配置字段 server.public_listen 必须为合法监听地址")
	}
	if !validListen(c.Server.ManagementListen) {
		return errors.New("配置字段 server.management_listen 必须为合法监听地址")
	}
	if c.Server.ControlListen != "" && !validListen(c.Server.ControlListen) {
		return errors.New("配置字段 server.control_listen 必须为合法监听地址")
	}
	if c.Server.EnrollmentListen != "" && !validListen(c.Server.EnrollmentListen) {
		return errors.New("配置字段 server.enrollment_listen 必须为合法监听地址")
	}
	if c.Server.ControlWSListen != "" && !validListen(c.Server.ControlWSListen) {
		return errors.New("配置字段 server.control_ws_listen 必须为合法监听地址")
	}
	if c.Server.EnrollmentWSListen != "" && !validListen(c.Server.EnrollmentWSListen) {
		return errors.New("配置字段 server.enrollment_ws_listen 必须为合法监听地址")
	}
	if err := validateCatalog(c.Server); err != nil {
		return err
	}
	if err := validateLogging(c.Logging); err != nil {
		return err
	}
	for field, value := range masterDurations(c) {
		if err := validDuration(field, value); err != nil {
			return err
		}
	}
	heartbeatTimeout, _ := time.ParseDuration(c.Node.HeartbeatTimeout)
	heartbeatInterval, _ := time.ParseDuration(c.Node.HeartbeatInterval)
	if heartbeatInterval >= heartbeatTimeout {
		return errors.New("node.heartbeat_interval 必须小于 node.heartbeat_timeout")
	}
	publicProbeTimeout, _ := time.ParseDuration(c.Node.PublicProbeTimeout)
	publicProbeTTL, _ := time.ParseDuration(c.Node.PublicProbeTTL)
	if publicProbeTTL <= publicProbeTimeout {
		return errors.New("node.public_probe_ttl 必须大于 node.public_probe_timeout")
	}
	if c.Node.PublicProbeNetworkFailures <= 0 {
		return errors.New("node.public_probe_network_failures 必须大于零")
	}
	if err := validateDatabase(c.Database); err != nil {
		return err
	}
	if _, err := time.LoadLocation(c.Stats.Timezone); err != nil {
		return fmt.Errorf("统计时区 stats.timezone 无效：%w", err)
	}
	if err := validateArchive(c.Archive); err != nil {
		return err
	}
	if err := validateHistory(c.History); err != nil {
		return err
	}
	if err := validateScanSocks5(c.Scan.Socks5); err != nil {
		return err
	}
	if err := validateSEOIndexNow(c); err != nil {
		return err
	}
	if err := validatePoWSizeTiers(c.PoWSizeTiers); err != nil {
		return err
	}
	if err := validateVDF(c); err != nil {
		return err
	}
	if c.APIPoW.Algorithm != "sha256" {
		return errors.New("公开 API PoW 必须使用 sha256")
	}
	if c.DownloadToken.SigningKeyFile != "" {
		return errors.New("download_token.signing_key_file 已废弃，请改用 Ed25519 signing_private_key_file 与 verify_public_key_file")
	}
	if c.DownloadToken.SigningPrivateKeyFile == "" || c.DownloadToken.VerifyPublicKeyFile == "" {
		return errors.New("下载令牌 Ed25519 私钥和公钥文件不得为空")
	}
	if err := validateAdminWeb(c.Admin.Web); err != nil {
		return err
	}
	for _, cidr := range c.Proxy.TrustedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("配置包含无效 CIDR：%s", cidr)
		}
	}
	return nil
}
