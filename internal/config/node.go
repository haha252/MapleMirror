package config

import (
	"errors"
	"net"
	"net/url"
)

type Node struct {
	Node      NodeIdentity `yaml:"node"`
	Server    NodeServer   `yaml:"server"`
	Master    NodeMaster   `yaml:"master"`
	Storage   NodeStorage  `yaml:"storage"`
	Bandwidth Bandwidth    `yaml:"bandwidth"`
	Sync      Sync         `yaml:"sync"`
	Download  NodeDownload `yaml:"download_token"`
	Proxy     Proxy        `yaml:"proxy"`
	Logging   Logging      `yaml:"logging"`
	Pairing   Pairing      `yaml:"pairing"`
	TLS       TLS          `yaml:"tls"`
}

type NodeIdentity struct {
	Name   string `yaml:"name"`
	IDFile string `yaml:"id_file"`
}
type NodeServer struct {
	Listen                string `yaml:"listen"`
	PublicDownloadBaseURL string `yaml:"public_download_base_url"`
}
type NodeMaster struct {
	ControlAddress    string `yaml:"control_address"`
	EnrollmentAddress string `yaml:"enrollment_address"`
}
type NodeStorage struct {
	Directory     string `yaml:"directory"`
	TempDirectory string `yaml:"temp_directory"`
	StateDB       string `yaml:"state_db"`
}
type Bandwidth struct {
	Target string `yaml:"target"`
}
type Sync struct {
	MaxWorkers     int    `yaml:"max_workers"`
	BandwidthLimit string `yaml:"bandwidth_limit"`
}
type NodeDownload struct {
	VerifyPublicKeyFile string `yaml:"verify_public_key_file"`
	SigningKeyFile      string `yaml:"signing_key_file"`
}
type Pairing struct {
	CredentialFile string `yaml:"credential_file"`
	CodeFile       string `yaml:"code_file"`
}

func LoadNode(path string, warn WarnFunc) (Node, error) {
	var c Node
	if err := readYAML(path, &c, NodeExample); err != nil {
		return c, err
	}
	applyNodeDefaults(&c, warn)
	return c, validateNode(c)
}

func applyNodeDefaults(c *Node, warn WarnFunc) {
	applyLoggingDefaults(&c.Logging, "logs/node", warn)
	setString(&c.Storage.Directory, "data/assets", "storage.directory", warn)
	setString(&c.Storage.TempDirectory, "data/tmp", "storage.temp_directory", warn)
	setString(&c.Storage.StateDB, "data/node-state.db", "storage.state_db", warn)
	setString(&c.Download.VerifyPublicKeyFile, "data/download-token-ed25519.pub", "download_token.verify_public_key_file", warn)
	setString(&c.Pairing.CredentialFile, "data/node-credential.json", "pairing.credential_file", warn)
	setString(&c.Pairing.CodeFile, "data/pairing-code", "pairing.code_file", warn)
	setString(&c.TLS.CAFile, "data/master-ca.pem", "tls.ca_file", warn)
	setString(&c.TLS.CertFile, "data/node.crt", "tls.cert_file", warn)
	setString(&c.TLS.KeyFile, "data/node.key", "tls.key_file", warn)
	if c.Sync.MaxWorkers == 0 {
		c.Sync.MaxWorkers = 2
		warnDefault(warn, "sync.max_workers", "2")
	}
}

func validateNode(c Node) error {
	if c.Node.Name == "" {
		return errors.New("节点配置 node.name 不得为空")
	}
	if !validListen(c.Server.Listen) {
		return errors.New("节点配置 server.listen 必须为合法监听地址")
	}
	baseURL, err := url.Parse(c.Server.PublicDownloadBaseURL)
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return errors.New("节点配置 server.public_download_base_url 必须为合法 HTTP 或 HTTPS 地址")
	}
	address, err := url.Parse(c.Master.ControlAddress)
	if err != nil || address.Scheme != "https" || address.Host == "" {
		return errors.New("节点配置 master.control_address 必须为 HTTPS 地址")
	}
	if c.Master.EnrollmentAddress != "" {
		enrollment, err := url.Parse(c.Master.EnrollmentAddress)
		if err != nil || enrollment.Scheme != "https" || enrollment.Host == "" {
			return errors.New("节点配置 master.enrollment_address 必须为 HTTPS 地址")
		}
	}
	if c.TLS.ServerName == "" {
		return errors.New("节点配置 tls.server_name 不得为空")
	}
	if c.Storage.Directory == "" || c.Storage.TempDirectory == "" || c.Storage.StateDB == "" {
		return errors.New("节点存储目录和本地状态库路径不得为空")
	}
	if c.Download.SigningKeyFile != "" {
		return errors.New("download_token.signing_key_file 已废弃，下载节点只应配置 verify_public_key_file")
	}
	if c.Download.VerifyPublicKeyFile == "" {
		return errors.New("节点配置 download_token.verify_public_key_file 不得为空")
	}
	for _, cidr := range c.Proxy.TrustedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return errors.New("节点配置 proxy.trusted_cidrs 包含无效 CIDR")
		}
	}
	if c.Bandwidth.Target == "" || c.Sync.MaxWorkers <= 0 {
		return errors.New("节点目标带宽不得为空且同步线程数必须大于零")
	}
	return validateLogging(c.Logging)
}
