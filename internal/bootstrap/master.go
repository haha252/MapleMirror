package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mirror-server/internal/config"
	"mirror-server/internal/downloadtoken"
)

func MasterFirstRun(cfg config.Master) error {
	if !Interactive() {
		return fmt.Errorf("首次启动缺少安全材料，请在交互式终端运行主节点完成初始化")
	}
	c := NewConsole()
	c.Println("检测到首次启动或安全材料缺失，将生成本机部署所需的密钥和证书。")
	if !c.Confirm("继续初始化主节点") {
		return fmt.Errorf("已取消主节点初始化")
	}
	if err := EnsureMasterMaterials(cfg); err != nil {
		return err
	}
	c.Println("主节点初始化完成。")
	c.Println("管理令牌已写入：", cfg.Admin.TokenFile)
	c.Println("请妥善保存该文件，调用管理 API 时使用 Authorization: Bearer <文件内容>。")
	return nil
}

func EnsureMasterMaterials(cfg config.Master) error {
	if err := EnsureAdminToken(cfg.Admin.TokenFile, cfg.Admin.TokenMinBytes); err != nil {
		return err
	}
	if err := downloadtoken.GenerateKeyFiles(cfg.DownloadToken.SigningPrivateKeyFile,
		cfg.DownloadToken.VerifyPublicKeyFile); err != nil {
		return err
	}
	if err := EnsureCA(cfg.Node.TLS.CAFile, "secrets/master-ca.key", "mirror-master-ca"); err != nil {
		return err
	}
	if err := EnsureServerCert(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile,
		cfg.Node.TLS.CAFile, "secrets/master-ca.key", "mirror-master",
		[]string{"127.0.0.1", "localhost"}); err != nil {
		return err
	}
	if err := EnsureCA(cfg.Node.TLS.SigningCACertFile, cfg.Node.TLS.SigningCAKeyFile,
		"mirror-node-signing-ca"); err != nil {
		return err
	}
	if err := EnsureCA(cfg.Admin.TLS.ClientCAFile, "secrets/admin-client-ca.key",
		"mirror-admin-client-ca"); err != nil {
		return err
	}
	if err := EnsureServerCert(cfg.Admin.TLS.CertFile, cfg.Admin.TLS.KeyFile,
		cfg.Node.TLS.CAFile, "secrets/master-ca.key", "mirror-admin",
		[]string{"127.0.0.1", "localhost"}); err != nil {
		return err
	}
	return nil
}

func MasterNeedsMaterials(cfg config.Master) bool {
	paths := []string{cfg.Admin.TokenFile, cfg.DownloadToken.SigningPrivateKeyFile, cfg.DownloadToken.VerifyPublicKeyFile,
		cfg.Node.TLS.CAFile, cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile,
		cfg.Node.TLS.SigningCACertFile, cfg.Node.TLS.SigningCAKeyFile,
		cfg.Admin.TLS.CertFile, cfg.Admin.TLS.KeyFile, cfg.Admin.TLS.ClientCAFile}
	for _, path := range paths {
		if !exists(path) {
			return true
		}
	}
	return false
}

func EnsureAdminToken(path string, minBytes int) error {
	if path == "" {
		return fmt.Errorf("管理令牌文件路径为空")
	}
	if minBytes < 32 {
		minBytes = 32
	}
	if exists(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(data))) < minBytes {
			return fmt.Errorf("管理令牌文件内容长度不足")
		}
		return nil
	}
	token, err := randomToken(minBytes)
	if err != nil {
		return err
	}
	return writePEMBytes(path, []byte(token+"\n"), 0o600)
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func CopyFileIfMissing(dst, src string) error {
	if exists(dst) {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writePEMBytes(dst, data, 0o644)
}

func writePEMBytes(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
