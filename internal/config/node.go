package config

import (
	"errors"
	"net/url"
)

type Node struct {
	Node      NodeIdentity `yaml:"node"`
	Server    NodeServer   `yaml:"server"`
	Master    NodeMaster   `yaml:"master"`
	Storage   NodeStorage  `yaml:"storage"`
	Bandwidth Bandwidth    `yaml:"bandwidth"`
	Sync      Sync         `yaml:"sync"`
	Logging   Logging      `yaml:"logging"`
	Pairing   Pairing      `yaml:"pairing"`
	TLS       TLS          `yaml:"tls"`
}

type NodeIdentity struct {
	Name   string `yaml:"name"`
	IDFile string `yaml:"id_file"`
}
type NodeServer struct {
	Listen string `yaml:"listen"`
}
type NodeMaster struct {
	ControlAddress string `yaml:"control_address"`
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
type Pairing struct {
	CredentialFile string `yaml:"credential_file"`
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
	address, err := url.Parse(c.Master.ControlAddress)
	if err != nil || address.Scheme != "https" || address.Host == "" {
		return errors.New("节点配置 master.control_address 必须为 HTTPS 地址")
	}
	if c.Storage.Directory == "" || c.Storage.TempDirectory == "" || c.Storage.StateDB == "" {
		return errors.New("节点存储目录和本地状态库路径不得为空")
	}
	if c.Bandwidth.Target == "" || c.Sync.MaxWorkers <= 0 {
		return errors.New("节点目标带宽不得为空且同步线程数必须大于零")
	}
	return validateLogging(c.Logging)
}
