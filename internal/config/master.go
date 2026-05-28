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
	Scan          Scan           `yaml:"scan"`
	ALTCHA        ALTCHA         `yaml:"altcha"`
	APIPoW        APIPoW         `yaml:"api_pow"`
	DownloadToken DownloadToken  `yaml:"download_token"`
	Node          NodeControl    `yaml:"node"`
	Admin         Administration `yaml:"admin"`
}

type MasterServer struct {
	PublicListen     string `yaml:"public_listen"`
	ManagementListen string `yaml:"management_listen"`
	ControlListen    string `yaml:"control_listen"`
	EnrollmentListen string `yaml:"enrollment_listen"`
}
type Database struct {
	Path        string `yaml:"path"`
	BusyTimeout string `yaml:"busy_timeout"`
	WAL         *bool  `yaml:"wal"`
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
type Scan struct {
	Interval       string `yaml:"interval"`
	GitHubTokenEnv string `yaml:"github_token_env"`
}
type ALTCHA struct {
	WorkloadProfile string `yaml:"workload_profile"`
	ChallengeTTL    string `yaml:"challenge_ttl"`
}
type APIPoW struct {
	Algorithm       string `yaml:"algorithm"`
	LeadingZeroBits int    `yaml:"leading_zero_bits"`
	ChallengeTTL    string `yaml:"challenge_ttl"`
}
type DownloadToken struct {
	TTL            string `yaml:"ttl"`
	SigningKeyFile string `yaml:"signing_key_file"`
}
type NodeControl struct {
	HeartbeatTimeout  string `yaml:"heartbeat_timeout"`
	HeartbeatInterval string `yaml:"heartbeat_interval"`
	EnrollmentTimeout string `yaml:"enrollment_timeout"`
	PairingCodeTTL    string `yaml:"pairing_code_ttl"`
	TLS               TLS    `yaml:"tls"`
}
type TLS struct {
	CAFile            string `yaml:"ca_file"`
	CertFile          string `yaml:"cert_file"`
	KeyFile           string `yaml:"key_file"`
	ClientCAFile      string `yaml:"client_ca_file"`
	SigningCACertFile string `yaml:"signing_ca_cert_file"`
	SigningCAKeyFile  string `yaml:"signing_ca_key_file"`
	ServerName        string `yaml:"server_name"`
}
type Administration struct {
	AllowedCIDRs        []string `yaml:"allowed_cidrs"`
	TokenEnv            string   `yaml:"token_env"`
	TokenMinBytes       int      `yaml:"token_min_bytes"`
	HighRiskRequireMTLS *bool    `yaml:"high_risk_require_mtls"`
	TLS                 AdminTLS `yaml:"tls"`
}
type AdminTLS struct {
	CertFile     string `yaml:"cert_file"`
	KeyFile      string `yaml:"key_file"`
	ClientCAFile string `yaml:"client_ca_file"`
}

func LoadMaster(path string, warn WarnFunc) (Master, error) {
	var c Master
	if err := readYAML(path, &c, MasterExample); err != nil {
		return c, err
	}
	applyMasterDefaults(&c, warn)
	return c, validateMaster(c)
}

func applyMasterDefaults(c *Master, warn WarnFunc) {
	applyLoggingDefaults(&c.Logging, "logs/master", warn)
	setString(&c.Server.ManagementListen, "127.0.0.1:9080", "server.management_listen", warn)
	setString(&c.Database.Path, "data/master.db", "database.path", warn)
	setString(&c.Database.BusyTimeout, "5s", "database.busy_timeout", warn)
	if c.Database.WAL == nil {
		value := true
		c.Database.WAL = &value
		warnDefault(warn, "database.wal", "true")
	}
	setString(&c.RequestID.ResponseHeader, "X-Request-ID", "request_id.response_header", warn)
	setString(&c.RequestID.ParentHeader, "X-Request-ID", "request_id.parent_header", warn)
	setString(&c.Stats.Timezone, "Asia/Shanghai", "stats.timezone", warn)
	setString(&c.Scan.Interval, "15m", "scan.interval", warn)
	setString(&c.ALTCHA.WorkloadProfile, "medium", "altcha.workload_profile", warn)
	setString(&c.ALTCHA.ChallengeTTL, "2m", "altcha.challenge_ttl", warn)
	setString(&c.APIPoW.Algorithm, "sha256", "api_pow.algorithm", warn)
	if c.APIPoW.LeadingZeroBits == 0 {
		c.APIPoW.LeadingZeroBits = 23
		warnDefault(warn, "api_pow.leading_zero_bits", "23")
	}
	setString(&c.APIPoW.ChallengeTTL, "2m", "api_pow.challenge_ttl", warn)
	setString(&c.DownloadToken.TTL, "15m", "download_token.ttl", warn)
	setString(&c.Node.HeartbeatTimeout, "30s", "node.heartbeat_timeout", warn)
	setString(&c.Node.HeartbeatInterval, "10s", "node.heartbeat_interval", warn)
	setString(&c.Node.EnrollmentTimeout, "10m", "node.enrollment_timeout", warn)
	setString(&c.Node.PairingCodeTTL, "5m", "node.pairing_code_ttl", warn)
	if c.Admin.TokenMinBytes == 0 {
		c.Admin.TokenMinBytes = 32
		warnDefault(warn, "admin.token_min_bytes", "32")
	}
	if c.Admin.HighRiskRequireMTLS == nil {
		value := true
		c.Admin.HighRiskRequireMTLS = &value
		warnDefault(warn, "admin.high_risk_require_mtls", "true")
	}
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
	if !loopbackListen(c.Server.ManagementListen) {
		return errors.New("管理监听 server.management_listen 在首版必须限制于回环地址")
	}
	if c.Server.ControlListen != "" && !validListen(c.Server.ControlListen) {
		return errors.New("配置字段 server.control_listen 必须为合法监听地址")
	}
	if c.Server.EnrollmentListen != "" && !validListen(c.Server.EnrollmentListen) {
		return errors.New("配置字段 server.enrollment_listen 必须为合法监听地址")
	}
	if err := validateLogging(c.Logging); err != nil {
		return err
	}
	for field, value := range map[string]string{"database.busy_timeout": c.Database.BusyTimeout, "scan.interval": c.Scan.Interval, "altcha.challenge_ttl": c.ALTCHA.ChallengeTTL, "api_pow.challenge_ttl": c.APIPoW.ChallengeTTL, "download_token.ttl": c.DownloadToken.TTL, "node.heartbeat_timeout": c.Node.HeartbeatTimeout, "node.heartbeat_interval": c.Node.HeartbeatInterval, "node.enrollment_timeout": c.Node.EnrollmentTimeout, "node.pairing_code_ttl": c.Node.PairingCodeTTL} {
		if err := validDuration(field, value); err != nil {
			return err
		}
	}
	heartbeatTimeout, _ := time.ParseDuration(c.Node.HeartbeatTimeout)
	heartbeatInterval, _ := time.ParseDuration(c.Node.HeartbeatInterval)
	if heartbeatInterval >= heartbeatTimeout {
		return errors.New("node.heartbeat_interval 必须小于 node.heartbeat_timeout")
	}
	if _, err := time.LoadLocation(c.Stats.Timezone); err != nil {
		return fmt.Errorf("统计时区 stats.timezone 无效：%w", err)
	}
	if c.ALTCHA.WorkloadProfile != "medium" {
		return errors.New("网页 ALTCHA 默认工作量档位必须为 medium（中等）")
	}
	if c.APIPoW.Algorithm != "sha256" || c.APIPoW.LeadingZeroBits <= 0 {
		return errors.New("公开 API PoW 必须使用 sha256 且前导零位数大于零")
	}
	if c.DownloadToken.SigningKeyFile == "" {
		return errors.New("下载令牌签名密钥文件 download_token.signing_key_file 不得为空")
	}
	if c.Admin.TokenEnv == "" || c.Admin.HighRiskRequireMTLS == nil || !*c.Admin.HighRiskRequireMTLS {
		return errors.New("管理 API 必须配置令牌环境变量且高风险操作强制 mTLS")
	}
	if c.Admin.TokenMinBytes < 32 {
		return errors.New("管理令牌最小字节数不得低于 32")
	}
	if len(c.Admin.AllowedCIDRs) == 0 {
		return errors.New("管理 API 必须配置管理网络 CIDR")
	}
	for _, cidr := range append(c.Proxy.TrustedCIDRs, c.Admin.AllowedCIDRs...) {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("配置包含无效 CIDR：%s", cidr)
		}
	}
	return nil
}
