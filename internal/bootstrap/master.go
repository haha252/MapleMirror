package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

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
	c.Println("管理面板用户文件：", cfg.Admin.Web.UsersFile)
	c.Println("首次打开管理面板时会自动生成初始管理员用户。")
	return nil
}

func EnsureMasterMaterials(cfg config.Master) error {
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
	if err := EnsureServerCert(cfg.Admin.TLS.CertFile, cfg.Admin.TLS.KeyFile,
		cfg.Node.TLS.CAFile, "secrets/master-ca.key", "mirror-admin",
		[]string{"127.0.0.1", "localhost"}); err != nil {
		return err
	}
	return nil
}

func MasterNeedsMaterials(cfg config.Master) bool {
	paths := []string{cfg.DownloadToken.SigningPrivateKeyFile, cfg.DownloadToken.VerifyPublicKeyFile,
		cfg.Node.TLS.CAFile, cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile,
		cfg.Node.TLS.SigningCACertFile, cfg.Node.TLS.SigningCAKeyFile,
		cfg.Admin.TLS.CertFile, cfg.Admin.TLS.KeyFile}
	for _, path := range paths {
		if !exists(path) {
			return true
		}
	}
	return false
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
