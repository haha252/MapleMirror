package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
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
	Name string `yaml:"name"`
}
type NodeServer struct {
	Listen                string `yaml:"listen"`
	PublicDownloadBaseURL string `yaml:"public_download_base_url"`
}
type NodeMaster struct {
	ControlAddress      string `yaml:"control_address"`
	EnrollmentAddress   string `yaml:"enrollment_address"`
	ControlWSAddress    string `yaml:"control_ws_address"`
	EnrollmentWSAddress string `yaml:"enrollment_ws_address"`
}
type NodeStorage struct {
	Directory     string `yaml:"directory"`
	TempDirectory string `yaml:"temp_directory"`
	StateDB       string `yaml:"state_db"`
}
type Bandwidth struct {
	Target     string `yaml:"target"`
	Minimum    string `yaml:"minimum"`
	TargetBPS  int64  `yaml:"-"`
	MinimumBPS int64  `yaml:"-"`
}
type Sync struct {
	MaxWorkers                int    `yaml:"max_workers"`
	MaxMirrorProjects         int    `yaml:"max_mirror_projects"`
	ForcePeerDownload         bool   `yaml:"force_peer_download"`
	BandwidthLimit            string `yaml:"bandwidth_limit"`
	BandwidthLimitBPS         int64  `yaml:"-"`
	SwarmUploadLimit          string `yaml:"swarm_upload_limit"`
	SwarmUploadLimitBPS       int64  `yaml:"-"`
	PeerFallbackMaxConcurrent int    `yaml:"peer_fallback_max_concurrent"`
	PeerFallbackWorkers       int    `yaml:"peer_fallback_workers"`
	PeerFallbackMinSize       string `yaml:"peer_fallback_min_size"`
	PeerFallbackMinSizeBytes  int64  `yaml:"-"`
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
	legacyIDFile := false
	data, repaired, err := readYAMLWithRepair(path, &c, NodeExample, NodeExample, func(doc *yaml.Node) bool {
		changed, found := migrateNodeIDFile(doc)
		legacyIDFile = legacyIDFile || found
		return changed
	})
	if err != nil {
		return c, err
	}
	if legacyIDFile {
		warnDeprecated(warn, "node.id_file", "storage.state_db 和 tls 证书材料")
	}
	applyNodeDefaults(&c, warn)
	if err := validateNode(&c); err != nil {
		return c, err
	}
	return c, writeRepairedYAML(path, data, repaired)
}

func SaveNodeFirstRun(path string, c Node) error {
	if err := validateNode(&c); err != nil {
		return err
	}
	return updateYAMLScalars(path, map[string]string{
		"node.name":                    c.Node.Name,
		"master.control_address":       c.Master.ControlAddress,
		"master.enrollment_address":    c.Master.EnrollmentAddress,
		"master.control_ws_address":    c.Master.ControlWSAddress,
		"master.enrollment_ws_address": c.Master.EnrollmentWSAddress,
		"tls.server_name":              c.TLS.ServerName,
	})
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
	setString(&c.Bandwidth.Minimum, "5 MiB/s", "bandwidth.minimum", warn)
	setString(&c.Sync.BandwidthLimit, "0", "sync.bandwidth_limit", warn)
	setString(&c.Sync.SwarmUploadLimit, "auto", "sync.swarm_upload_limit", warn)
	if c.Sync.PeerFallbackMaxConcurrent == 0 {
		c.Sync.PeerFallbackMaxConcurrent = 3
		warnDefault(warn, "sync.peer_fallback_max_concurrent", "3")
	}
	if c.Sync.PeerFallbackWorkers == 0 {
		c.Sync.PeerFallbackWorkers = 8
		warnDefault(warn, "sync.peer_fallback_workers", "8")
	}
	setString(&c.Sync.PeerFallbackMinSize, "32 MiB", "sync.peer_fallback_min_size", warn)
}

func migrateNodeIDFile(doc *yaml.Node) (bool, bool) {
	root := yamlRoot(doc)
	node := findYAMLMapValue(root, "node")
	if node == nil || node.Kind != yaml.MappingNode {
		return false, false
	}
	found := yamlMapIndex(node, "id_file") >= 0
	return yamlRemoveMapKey(node, "id_file"), found
}

func ParseBandwidthBPS(field, value string, allowZero bool) (int64, error) {
	bps, err := parseBandwidthBPS(value, allowZero)
	if err != nil {
		return 0, fmt.Errorf("配置字段 %s 必须使用 0、MiB/s、MB/s 或 Mbps 格式", field)
	}
	return bps, nil
}
