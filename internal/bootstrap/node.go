package bootstrap

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/config"
)

type NodeAnswers struct {
	Name                string
	ControlAddress      string
	EnrollmentAddress   string
	ControlWSAddress    string
	EnrollmentWSAddress string
	ServerName          string
	PairingCode         string
	TLSConfig           *tls.Config
}

func NodeFirstRun(cfg config.Node) (NodeAnswers, error) {
	if !Interactive() {
		return NodeAnswers{}, fmt.Errorf("首次启动缺少节点身份，请在交互式终端运行下载节点完成初始化")
	}
	c := NewConsole()
	name, err := c.Ask("节点名称", cfg.Node.Name)
	if err != nil {
		return NodeAnswers{}, err
	}
	answers := NodeAnswers{Name: name}
	useV2 := cfg.Master.ControlWSAddress != "" || cfg.Master.EnrollmentWSAddress != ""
	var enrollmentEndpoint string
	if useV2 {
		answers.ControlWSAddress, err = c.Ask("主节点 control.v2 WSS 地址", cfg.Master.ControlWSAddress)
		if err != nil {
			return NodeAnswers{}, err
		}
		answers.EnrollmentWSAddress, err = c.Ask("主节点 enrollment.v2 WSS 地址", cfg.Master.EnrollmentWSAddress)
		if err != nil {
			return NodeAnswers{}, err
		}
		enrollmentEndpoint = answers.EnrollmentWSAddress
	} else {
		answers.ControlAddress, err = c.Ask("主节点 legacy 控制地址", cfg.Master.ControlAddress)
		if err != nil {
			return NodeAnswers{}, err
		}
		answers.EnrollmentAddress, err = c.Ask("主节点 legacy 登记地址", cfg.Master.EnrollmentAddress)
		if err != nil {
			return NodeAnswers{}, err
		}
		enrollmentEndpoint = answers.EnrollmentAddress
	}
	answers.ServerName, err = c.Ask("TLS 服务端名称", cfg.TLS.ServerName)
	if err != nil {
		return NodeAnswers{}, err
	}
	answers.PairingCode, err = c.Ask("一次性配对码", "")
	if err != nil {
		return NodeAnswers{}, err
	}
	answers.TLSConfig, err = confirmEnrollmentCert(c, enrollmentEndpoint, answers.ServerName, cfg.TLS.CAFile)
	if err != nil {
		return NodeAnswers{}, err
	}
	return answers, nil
}
func WritePairingCode(path, code string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(code), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func confirmEnrollmentCert(c Console, rawURL, serverName, caFile string) (*tls.Config, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", parsed.Host,
		&tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	state := conn.ConnectionState()
	_ = conn.Close()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("主节点未返回 TLS 证书")
	}
	cert := state.PeerCertificates[0]
	sum := sha256.Sum256(cert.Raw)
	fp := "sha256:" + hex.EncodeToString(sum[:])
	c.Println("主节点登记入口 TLS 指纹：", fp)
	if !c.Confirm("确认信任该主节点证书并继续登记") {
		return nil, fmt.Errorf("已取消节点登记")
	}
	if err := writeCert(caFile, cert); err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		ServerName: serverName, InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("主节点未返回 TLS 证书")
			}
			got := sha256.Sum256(rawCerts[0])
			if got != sum {
				return fmt.Errorf("主节点 TLS 证书指纹变化")
			}
			return nil
		}}, nil
}

func writeCert(path string, cert *x509.Certificate) error {
	return writePEM(path, "CERTIFICATE", cert.Raw, 0o644)
}

func PublicKeyPEM(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return "", fmt.Errorf("下载令牌公钥格式无效")
	}
	return string(data), nil
}
