package indexnow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type persistedState struct {
	Key      string `json:"key"`
	Revision string `json:"revision"`
}

func readState(path string) persistedState {
	data, err := os.ReadFile(path)
	if err != nil {
		return persistedState{}
	}
	var state persistedState
	if json.Unmarshal(data, &state) != nil {
		return persistedState{}
	}
	state.Key = strings.TrimSpace(state.Key)
	state.Revision = strings.TrimSpace(state.Revision)
	return state
}

func writeState(path string, state persistedState) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建 IndexNow state 目录失败：%w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("编码 IndexNow state 失败：%w", err)
	}
	temporary, err := os.CreateTemp(directory, ".indexnow-state-*")
	if err != nil {
		return fmt.Errorf("创建 IndexNow state 临时文件失败：%w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("设置 IndexNow state 文件权限失败：%w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入 IndexNow state 失败：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("同步 IndexNow state 失败：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 IndexNow state 临时文件失败：%w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("原子写入 IndexNow state 失败：%w", err)
	}
	return nil
}
