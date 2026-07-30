package config

import (
	"errors"
	"fmt"
	"net"
	"time"
)

type Master struct {
	Server        MasterServer   `yaml:"server"`
	Database      Database       `yaml:"database"`
	Logging       Logging        `yaml:"logging"`
	RequestID     RequestID      `yaml:"request_id"`
	Proxy         Proxy          `yaml:"proxy"`
	Stats         Stats          `yaml:"stats"`
	Archive       Archive        `yaml:"archive"`
	Scan          Scan           `yaml:"scan"`
	PoWSizeTiers  []PoWSizeTier  `yaml:"pow_size_tiers"`
	ALTCHA        ALTCHA         `yaml:"altcha"`
	APIPoW        APIPoW         `yaml:"api_pow"`
	DownloadToken DownloadToken  `yaml:"download_token"`
	Node          NodeControl    `yaml:"node"`
	Admin         Administration `yaml:"admin"`
}

type MasterServer struct {
	PublicListen                 string `yaml:"public_listen"`
	ManagementListen             string `yaml:"management_listen"`
	ControlListen                string `yaml:"control_listen"`
	EnrollmentListen             string `yaml:"enrollment_listen"`
	CatalogBatchRows             *int   `yaml:"catalog_batch_rows"`
	CatalogPrefetchRemainingRows *int   `yaml:"catalog_prefetch_remaining_rows"`
}
type RequestID struct {
	ResponseHeader string `yaml:"response_header"`
	ParentHeader   string `yaml:"parent_header"`
}
type Proxy struct {
	TrustedCIDRs []string `yaml:"trusted_cidrs"`
}
type Stats struct {
	Timezone string `yaml:"timezone"`
}
type Archive struct {
	Enabled *bool  `yaml:"enabled"`
	Root    string `yaml:"root"`
}
type Scan struct {
	Interval       string      `yaml:"interval"`
	GitHubTokenEnv string      `yaml:"github_token_env"`
	GitHubTimeout  string      `yaml:"github_timeout"`
	Socks5         Socks5Proxy `yaml:"socks5"`
}
type Socks5Proxy struct {
	Enabled  bool   `yaml:"enabled"`
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}
type ALTCHA struct {
	ChallengeTTL string `yaml:"challenge_ttl"`
}
type APIPoW struct {
	Algorithm    string `yaml:"algorithm"`
	ChallengeTTL string `yaml:"challenge_ttl"`
}
type DownloadToken struct {
	FirstConnectionTimeout string `yaml:"first_connection_timeout"`
	IdleTimeout            string `yaml:"idle_timeout"`
	MaxDuration            string `yaml:"max_duration"`
	SigningPrivateKeyFile  string `yaml:"signing_private_key_file"`
	VerifyPublicKeyFile    string `yaml:"verify_public_key_file"`
	SigningKeyFile         string `yaml:"signing_key_file"`
}
type NodeControl struct {
	HeartbeatTimeout           string `yaml:"heartbeat_timeout"`
	HeartbeatOfflineGrace      string `yaml:"heartbeat_offline_grace"`
	HeartbeatInterval          string `yaml:"heartbeat_interval"`
	PublicProbeEnabled         *bool  `yaml:"public_probe_enabled"`
	PublicProbeInterval        string `yaml:"public_probe_interval"`
	PublicProbeTimeout         string `yaml:"public_probe_timeout"`
	PublicProbeTTL             string `yaml:"public_probe_ttl"`
	PublicProbeNetworkFailures int    `yaml:"public_probe_network_failures"`
	EnrollmentTimeout          string `yaml:"enrollment_timeout"`
	PairingCodeTTL             string `yaml:"pairing_code_ttl"`
	TLS                        TLS    `yaml:"tls"`
}
type TLS struct {
	CAFile            string `yaml:"ca_file"`
	CAKeyFile         string `yaml:"ca_key_file"`
	CertFile          string `yaml:"cert_file"`
	KeyFile           string `yaml:"key_file"`
	ClientCAFile      string `yaml:"client_ca_file"`
	SigningCACertFile string `yaml:"signing_ca_cert_file"`
	SigningCAKeyFile  string `yaml:"signing_ca_key_file"`
	ServerName        string `yaml:"server_name"`
}

func LoadMaster(path string, warn WarnFunc) (Master, error) {
	var c Master
	data, repaired, legacyALTCHA, legacyAPI, err := readMasterYAML(path, &c)
	if err != nil {
		return c, err
	}
	if legacyALTCHA {
		warnDeprecated(warn, "altcha.difficulty", "pow_size_tiers")
	}
	if legacyAPI {
		warnDeprecated(warn, "api_pow.leading_zero_bits", "pow_size_tiers")
	}
	applyMasterDefaults(&c, warn)
	if err := validateMaster(c); err != nil {
		return c, err
	}
	return c, writeRepairedYAML(path, data, repaired)
}

func applyMasterDefaults(c *Master, warn WarnFunc) {
	applyLoggingDefaults(&c.Logging, "logs/master", warn)
	setString(&c.Server.ManagementListen, "127.0.0.1:9080", "server.management_listen", warn)
	applyCatalogDefaults(&c.Server, warn)
	applyDatabaseDefaults(c, warn)
	setString(&c.RequestID.ResponseHeader, "X-Request-ID", "request_id.response_header", warn)
	setString(&c.RequestID.ParentHeader, "X-Request-ID", "request_id.parent_header", warn)
	setString(&c.Stats.Timezone, "Asia/Shanghai", "stats.timezone", warn)
	applyArchiveDefaults(c, warn)
	setString(&c.Scan.Interval, "15m", "scan.interval", warn)
	setString(&c.Scan.GitHubTimeout, "2m", "scan.github_timeout", warn)
	setString(&c.ALTCHA.ChallengeTTL, "2m", "altcha.challenge_ttl", warn)
	setString(&c.APIPoW.Algorithm, "sha256", "api_pow.algorithm", warn)
	setString(&c.APIPoW.ChallengeTTL, "2m", "api_pow.challenge_ttl", warn)
	setString(&c.DownloadToken.FirstConnectionTimeout, "20s", "download_token.first_connection_timeout", warn)
	setString(&c.DownloadToken.IdleTimeout, "120s", "download_token.idle_timeout", warn)
	setString(&c.DownloadToken.MaxDuration, "30m", "download_token.max_duration", warn)
	setString(&c.DownloadToken.SigningPrivateKeyFile, "secrets/download-token-ed25519.key", "download_token.signing_private_key_file", warn)
	setString(&c.DownloadToken.VerifyPublicKeyFile, "secrets/download-token-ed25519.pub", "download_token.verify_public_key_file", warn)
	setString(&c.Node.HeartbeatTimeout, "90s", "node.heartbeat_timeout", warn)
	setString(&c.Node.HeartbeatOfflineGrace, "5m", "node.heartbeat_offline_grace", warn)
	setString(&c.Node.HeartbeatInterval, "15s", "node.heartbeat_interval", warn)
	if c.Node.PublicProbeEnabled == nil {
		value := true
		c.Node.PublicProbeEnabled = &value
		warnDefault(warn, "node.public_probe_enabled", "true")
	}
	setString(&c.Node.PublicProbeInterval, "60s", "node.public_probe_interval", warn)
	setString(&c.Node.PublicProbeTimeout, "10s", "node.public_probe_timeout", warn)
	setString(&c.Node.PublicProbeTTL, "30s", "node.public_probe_ttl", warn)
	if c.Node.PublicProbeNetworkFailures == 0 {
		c.Node.PublicProbeNetworkFailures = 5
		warnDefault(warn, "node.public_probe_network_failures", "5")
	}
	setString(&c.Node.EnrollmentTimeout, "10m", "node.enrollment_timeout", warn)
	setString(&c.Node.PairingCodeTTL, "5m", "node.pairing_code_ttl", warn)
	setString(&c.Node.TLS.CAFile, "secrets/master-ca.pem", "node.tls.ca_file", warn)
	setString(&c.Node.TLS.CAKeyFile, "secrets/master-ca.key", "node.tls.ca_key_file", warn)
	setString(&c.Node.TLS.CertFile, "secrets/master-control.crt", "node.tls.cert_file", warn)
	setString(&c.Node.TLS.KeyFile, "secrets/master-control.key", "node.tls.key_file", warn)
	setString(&c.Node.TLS.ClientCAFile, "secrets/node-signing-ca.pem", "node.tls.client_ca_file", warn)
	setString(&c.Node.TLS.SigningCACertFile, "secrets/node-signing-ca.pem", "node.tls.signing_ca_cert_file", warn)
	setString(&c.Node.TLS.SigningCAKeyFile, "secrets/node-signing-ca.key", "node.tls.signing_ca_key_file", warn)
	applyAdminWebDefaults(c, warn)
	setString(&c.Admin.TLS.CertFile, "secrets/admin-web.crt", "admin.tls.cert_file", warn)
	setString(&c.Admin.TLS.KeyFile, "secrets/admin-web.key", "admin.tls.key_file", warn)
}

func setString(value *string, fallback, field string, warn WarnFunc) {
	if *value == "" {
		*value = fallback
		warnDefault(warn, field, fallback)
	}
}

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
	if err := validateScanSocks5(c.Scan.Socks5); err != nil {
		return err
	}
	if err := validatePoWSizeTiers(c.PoWSizeTiers); err != nil {
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
