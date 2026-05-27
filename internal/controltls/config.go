package controltls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func EnrollmentServer(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("加载登记 TLS 证书失败：%w", err)
	}
	return serverConfig(cert, clientCAFile, tls.NoClientCert)
}

func ControlServer(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if clientCAFile == "" {
		return nil, fmt.Errorf("控制通道客户端 CA 不得为空")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("加载控制 TLS 证书失败：%w", err)
	}
	return serverConfig(cert, clientCAFile, tls.RequireAndVerifyClientCert)
}

func AdminServer(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if clientCAFile == "" {
		return nil, fmt.Errorf("管理高风险 mTLS 客户端 CA 不得为空")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("加载管理 TLS 证书失败：%w", err)
	}
	return serverConfig(cert, clientCAFile, tls.VerifyClientCertIfGiven)
}

func NodeClient(caFile, certFile, keyFile, serverName string) (*tls.Config, error) {
	if serverName == "" {
		return nil, fmt.Errorf("节点 TLS 服务端名称不得为空")
	}
	roots, err := certPool(caFile)
	if err != nil {
		return nil, err
	}
	cfg := tls13()
	cfg.RootCAs = roots
	cfg.ServerName = serverName
	if certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("加载节点客户端证书失败：%w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func serverConfig(cert tls.Certificate, clientCAFile string, auth tls.ClientAuthType) (*tls.Config, error) {
	cfg := tls13()
	cfg.Certificates = []tls.Certificate{cert}
	cfg.ClientAuth = auth
	if clientCAFile != "" {
		pool, err := certPool(clientCAFile)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = pool
	}
	return cfg, nil
}

func tls13() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
}

func certPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 CA 证书失败：%w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("CA 证书内容无效")
	}
	return pool, nil
}
