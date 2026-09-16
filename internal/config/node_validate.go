package config

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"mirror-server/internal/downloadurl"
)

func validateNode(c *Node) error {
	if c.Node.Name == "" {
		return errors.New("节点配置 node.name 不得为空")
	}
	if !validListen(c.Server.Listen) {
		return errors.New("节点配置 server.listen 必须为合法监听地址")
	}
	if _, ok := downloadurl.NormalizeBase(c.Server.PublicDownloadBaseURL); !ok {
		return errors.New("节点配置 server.public_download_base_url 必须为 HTTPS 公网基址；HTTP 仅允许本机回环地址，且不得包含路径、查询或片段")
	}
	if c.Master.ControlAddress == "" && c.Master.ControlWSAddress == "" {
		return errors.New("节点配置 master.control_address 与 master.control_ws_address 至少填写一个")
	}
	if c.Master.ControlAddress != "" {
		address, err := url.Parse(c.Master.ControlAddress)
		if err != nil || address.Scheme != "https" || address.Host == "" {
			return errors.New("节点配置 master.control_address 必须为 HTTPS 地址")
		}
	}
	if c.Master.EnrollmentAddress != "" {
		enrollment, err := url.Parse(c.Master.EnrollmentAddress)
		if err != nil || enrollment.Scheme != "https" || enrollment.Host == "" {
			return errors.New("节点配置 master.enrollment_address 必须为 HTTPS 地址")
		}
	}
	if c.Master.ControlWSAddress != "" {
		controlWS, err := url.Parse(c.Master.ControlWSAddress)
		if err != nil || controlWS.Scheme != "wss" || controlWS.Host == "" {
			return errors.New("节点配置 master.control_ws_address 必须为 WSS 地址")
		}
	}
	if c.Master.EnrollmentWSAddress != "" {
		enrollment, err := url.Parse(c.Master.EnrollmentWSAddress)
		if err != nil || enrollment.Scheme != "wss" || enrollment.Host == "" {
			return errors.New("节点配置 master.enrollment_ws_address 必须为 WSS 地址")
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
	if c.Bandwidth.Target == "" {
		return errors.New("节点目标带宽不得为空")
	}
	target, err := ParseBandwidthBPS("bandwidth.target", c.Bandwidth.Target, false)
	if err != nil {
		return err
	}
	c.Bandwidth.TargetBPS = target
	minimum, err := ParseBandwidthBPS("bandwidth.minimum", c.Bandwidth.Minimum, true)
	if err != nil {
		return err
	}
	if minimum > target {
		return errors.New("节点保底带宽不得大于目标带宽")
	}
	c.Bandwidth.MinimumBPS = minimum
	if c.Sync.MaxWorkers <= 0 {
		return errors.New("同步线程数必须大于零")
	}
	if c.Sync.MaxMirrorProjects < 0 {
		return errors.New("节点最大镜像项目数不得小于零")
	}
	if c.Sync.PeerFallbackMaxConcurrent <= 0 {
		return errors.New("节点间复制全局并发数必须大于零")
	}
	if c.Sync.PeerFallbackWorkers <= 0 {
		return errors.New("节点间复制分片并发数必须大于零")
	}
	limit, err := ParseBandwidthBPS("sync.bandwidth_limit", c.Sync.BandwidthLimit, true)
	if err != nil {
		return err
	}
	c.Sync.BandwidthLimitBPS = limit
	if strings.EqualFold(strings.TrimSpace(c.Sync.SwarmUploadLimit), "auto") {
		c.Sync.SwarmUploadLimitBPS = target / 4
		if c.Sync.SwarmUploadLimitBPS < 1 {
			c.Sync.SwarmUploadLimitBPS = 1
		}
	} else {
		swarmLimit, err := ParseBandwidthBPS("sync.swarm_upload_limit", c.Sync.SwarmUploadLimit, true)
		if err != nil {
			return err
		}
		c.Sync.SwarmUploadLimitBPS = swarmLimit
	}
	minSize, err := ParseBytes("sync.peer_fallback_min_size", c.Sync.PeerFallbackMinSize, true)
	if err != nil {
		return err
	}
	c.Sync.PeerFallbackMinSizeBytes = minSize
	return validateLogging(c.Logging)
}
