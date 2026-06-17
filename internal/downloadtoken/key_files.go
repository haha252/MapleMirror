package downloadtoken

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func readPrivate(path string) (ed25519.PrivateKey, error) {
	block, err := readPEM(path)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("下载令牌私钥格式无效：%w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("下载令牌私钥不是 Ed25519")
	}
	return priv, nil
}

func readPublic(path string) (ed25519.PublicKey, error) {
	block, err := readPEM(path)
	if err != nil {
		return nil, err
	}
	return parsePublicBlock(block)
}

func parsePublicPEM(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("下载令牌密钥 PEM 内容无效")
	}
	return parsePublicBlock(block)
}

func parsePublicBlock(block *pem.Block) (ed25519.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("下载令牌公钥格式无效：%w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("下载令牌公钥不是 Ed25519")
	}
	return pub, nil
}

func readPEM(path string) (*pem.Block, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("下载令牌密钥 PEM 内容无效")
	}
	return block, nil
}

func writePEM(path, typ string, der []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
