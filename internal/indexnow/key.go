package indexnow

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,128}$`)

type KeyMaterial struct {
	Value   string
	Created bool
}

func LoadOrCreateKey(path string) (KeyMaterial, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return KeyMaterial{}, errors.New("IndexNow key 文件路径为空")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		key := strings.TrimSpace(string(data))
		if !validKey(key) {
			return KeyMaterial{}, fmt.Errorf("IndexNow key 文件内容不合法：%s", path)
		}
		return KeyMaterial{Value: key}, nil
	}
	if !os.IsNotExist(err) {
		return KeyMaterial{}, fmt.Errorf("读取 IndexNow key 文件失败：%w", err)
	}
	key, err := newKey()
	if err != nil {
		return KeyMaterial{}, err
	}
	if err := persistKey(path, key); err != nil {
		return KeyMaterial{}, err
	}
	return KeyMaterial{Value: key, Created: true}, nil
}

func validKey(key string) bool {
	return keyPattern.MatchString(key)
}

func newKey() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("生成 IndexNow key 失败：%w", err)
	}
	return hex.EncodeToString(data), nil
}

func persistKey(path, key string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建 IndexNow key 目录失败：%w", err)
	}
	temporary, err := os.CreateTemp(directory, ".indexnow-key-*")
	if err != nil {
		return fmt.Errorf("创建 IndexNow key 临时文件失败：%w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("设置 IndexNow key 文件权限失败：%w", err)
	}
	if _, err := temporary.WriteString(key); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入 IndexNow key 失败：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("同步 IndexNow key 失败：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 IndexNow key 临时文件失败：%w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("原子写入 IndexNow key 失败：%w", err)
	}
	return nil
}
