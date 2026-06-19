package mirrorsync

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"mirror-server/internal/config"

	"go.starlark.net/starlark"
)

const starlarkMaxExecutionSteps = 100000
const starlarkExecutionTimeout = 2 * time.Second

type starlarkClassifier struct {
	filename string
	globals  starlark.StringDict
}

func loadStarlarkClassifier(project config.Project) (*starlarkClassifier, error) {
	script := project.AssetPipeline.Classify.Script
	source := strings.TrimSpace(script.Inline)
	filename := "<asset-classifier>"
	if strings.TrimSpace(script.Path) != "" {
		filename = project.ResolvedClassifyScriptPath
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("读取 Starlark 分类脚本失败：%w", err)
		}
		source = string(data)
	}
	if source == "" {
		return nil, nil
	}
	thread, stop := starlarkThread()
	defer stop()
	globals, err := starlark.ExecFile(thread, filename, source, starlarkPredeclared())
	if err != nil {
		return nil, fmt.Errorf("编译 Starlark 分类脚本失败：%w", err)
	}
	if _, ok := globals["classify"].(starlark.Callable); !ok {
		return nil, fmt.Errorf("Starlark 分类脚本必须定义 classify(asset, release, project, batch)")
	}
	return &starlarkClassifier{filename: filename, globals: globals}, nil
}

func (c *starlarkClassifier) Classify(asset ResourceCandidate, release ResourceVersion,
	project config.Project, batch []ResourceCandidate) (config.AssetClassification, error) {
	fn := c.globals["classify"]
	thread, stop := starlarkThread()
	defer stop()
	result, err := starlark.Call(thread, fn, starlark.Tuple{
		starlarkAsset(asset),
		starlarkRelease(release),
		starlarkProject(project),
		starlarkBatch(batch),
	}, nil)
	if err != nil {
		return config.AssetClassification{}, fmt.Errorf("执行 Starlark 分类脚本失败：%w", err)
	}
	if result == starlark.None {
		return config.AssetClassification{}, nil
	}
	dict, ok := result.(*starlark.Dict)
	if !ok {
		return config.AssetClassification{}, fmt.Errorf("Starlark classify 必须返回 dict 或 None")
	}
	return classificationFromStarlarkDict(dict)
}

func starlarkThread() (*starlark.Thread, func()) {
	thread := &starlark.Thread{Name: "asset-classifier"}
	thread.SetMaxExecutionSteps(starlarkMaxExecutionSteps)
	timer := time.AfterFunc(starlarkExecutionTimeout, func() {
		thread.Cancel("asset classifier timed out")
	})
	return thread, func() { timer.Stop() }
}

func starlarkPredeclared() starlark.StringDict {
	return starlark.StringDict{
		"regex_match":      starlark.NewBuiltin("regex_match", starlarkRegexMatch),
		"regex_find":       starlark.NewBuiltin("regex_find", starlarkRegexFind),
		"lower":            starlark.NewBuiltin("lower", starlarkLower),
		"contains":         starlark.NewBuiltin("contains", starlarkContains),
		"normalize_system": starlark.NewBuiltin("normalize_system", starlarkNormalizeSystem),
		"normalize_arch":   starlark.NewBuiltin("normalize_arch", starlarkNormalizeArch),
	}
}

func starlarkAsset(asset ResourceCandidate) *starlark.Dict {
	return stringDict(map[string]starlark.Value{
		"file_name":        starlark.String(asset.FileName),
		"name":             starlark.String(asset.FileName),
		"size_bytes":       starlark.MakeInt64(asset.SizeBytes),
		"digest":           starlark.String(asset.Digest),
		"download_url":     starlark.String(asset.DownloadURL),
		"source_type":      starlark.String(asset.SourceType),
		"source_asset_key": starlark.String(asset.SourceAssetKey),
	})
}

func starlarkRelease(release ResourceVersion) *starlark.Dict {
	return stringDict(map[string]starlark.Value{
		"version":            starlark.String(release.Version),
		"source_type":        starlark.String(release.SourceType),
		"source_release_key": starlark.String(release.SourceReleaseKey),
		"prerelease":         starlark.Bool(release.Prerelease),
	})
}

func starlarkProject(project config.Project) *starlark.Dict {
	return stringDict(map[string]starlark.Value{
		"id":         starlark.String(project.ID),
		"name":       starlark.String(project.Name),
		"repository": starlark.String(project.Repository),
	})
}

func starlarkBatch(batch []ResourceCandidate) *starlark.List {
	values := make([]starlark.Value, 0, len(batch))
	for _, asset := range batch {
		values = append(values, starlarkAsset(asset))
	}
	return starlark.NewList(values)
}

func stringDict(values map[string]starlark.Value) *starlark.Dict {
	dict := starlark.NewDict(len(values))
	for key, value := range values {
		_ = dict.SetKey(starlark.String(key), value)
	}
	return dict
}

func starlarkRegexMatch(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple,
	kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, value string
	if err := starlark.UnpackArgs("regex_match", args, kwargs, "pattern", &pattern, "value", &value); err != nil {
		return nil, err
	}
	matched, err := regexp.MatchString(pattern, value)
	return starlark.Bool(matched), err
}

func starlarkRegexFind(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple,
	kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, value string
	if err := starlark.UnpackArgs("regex_find", args, kwargs, "pattern", &pattern, "value", &value); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	matches := re.FindStringSubmatch(value)
	if len(matches) == 0 {
		return starlark.None, nil
	}
	return starlark.String(matches[len(matches)-1]), nil
}
