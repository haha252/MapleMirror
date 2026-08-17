package indexnow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const stateVersion = 2

type pageState struct {
	Fingerprint string `json:"fingerprint"`
	SubmittedAt string `json:"submitted_at"`
}

type errorState struct {
	Fingerprint string `json:"fingerprint"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	RecordedAt  string `json:"recorded_at"`
}

type persistedState struct {
	Version  int                   `json:"version"`
	Key      string                `json:"key"`
	Revision string                `json:"revision,omitempty"`
	Pages    map[string]pageState  `json:"pages"`
	Errors   map[string]errorState `json:"errors,omitempty"`
}

func emptyState(key string) persistedState {
	return persistedState{Version: stateVersion, Key: key,
		Pages: make(map[string]pageState), Errors: make(map[string]errorState)}
}

func loadState(path string) (persistedState, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return persistedState{}, true, nil
	}
	if err != nil {
		return persistedState{}, true, fmt.Errorf("读取 IndexNow state 失败：%w", err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedState{}, true, fmt.Errorf("解析 IndexNow state 失败：%w", err)
	}
	state.Key = strings.TrimSpace(state.Key)
	state.Revision = strings.TrimSpace(state.Revision)
	if state.Version != stateVersion || state.Pages == nil {
		return state, true, nil
	}
	if state.Errors == nil {
		state.Errors = make(map[string]errorState)
	}
	return state, false, nil
}

// readState 保留给同包旧测试和迁移代码使用；调用方需要用 loadState 判断 v1。
func readState(path string) persistedState {
	state, _, err := loadState(path)
	if err != nil {
		return persistedState{}
	}
	return state
}

func cloneState(state persistedState) persistedState {
	copyState := state
	copyState.Pages = make(map[string]pageState, len(state.Pages))
	for path, page := range state.Pages {
		copyState.Pages[path] = page
	}
	copyState.Errors = make(map[string]errorState, len(state.Errors))
	for path, failure := range state.Errors {
		copyState.Errors[path] = failure
	}
	return copyState
}

func writeState(path string, state persistedState) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if state.Version == 0 {
		state.Version = stateVersion
	}
	if state.Pages == nil {
		state.Pages = make(map[string]pageState)
	}
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
