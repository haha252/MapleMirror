package mirrorsync

import (
	"fmt"
	"strings"

	"mirror-server/internal/config"

	"go.starlark.net/starlark"
)

func starlarkLower(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple,
	kwargs []starlark.Tuple) (starlark.Value, error) {
	var value string
	if err := starlark.UnpackArgs("lower", args, kwargs, "value", &value); err != nil {
		return nil, err
	}
	return starlark.String(strings.ToLower(value)), nil
}

func starlarkContains(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple,
	kwargs []starlark.Tuple) (starlark.Value, error) {
	var value, part string
	if err := starlark.UnpackArgs("contains", args, kwargs, "value", &value, "part", &part); err != nil {
		return nil, err
	}
	return starlark.Bool(strings.Contains(value, part)), nil
}

func starlarkNormalizeSystem(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple,
	kwargs []starlark.Tuple) (starlark.Value, error) {
	var value string
	if err := starlark.UnpackArgs("normalize_system", args, kwargs, "value", &value); err != nil {
		return nil, err
	}
	if out, ok := config.NormalizeAssetSystem(value); ok {
		return starlark.String(out), nil
	}
	return starlark.None, nil
}

func starlarkNormalizeArch(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple,
	kwargs []starlark.Tuple) (starlark.Value, error) {
	var value string
	if err := starlark.UnpackArgs("normalize_arch", args, kwargs, "value", &value); err != nil {
		return nil, err
	}
	if out, ok := config.NormalizeAssetArchitecture(value); ok {
		return starlark.String(out), nil
	}
	return starlark.None, nil
}

func classificationFromStarlarkDict(dict *starlark.Dict) (config.AssetClassification, error) {
	var out config.AssetClassification
	if accept, ok, err := dictBool(dict, "accept"); err != nil || ok {
		if err != nil {
			return out, err
		}
		out.Accept = &accept
	}
	var err error
	if out.RejectReason, err = dictString(dict, "reject_reason"); err != nil {
		return out, err
	}
	if out.System, err = dictString(dict, "system"); err != nil {
		return out, err
	}
	if out.Architecture, err = dictString(dict, "architecture"); err != nil {
		return out, err
	}
	if out.Variant, err = dictString(dict, "variant"); err != nil {
		return out, err
	}
	if out.DisplayLabel, err = dictString(dict, "display_label"); err != nil {
		return out, err
	}
	if priority, ok, err := dictInt(dict, "priority"); err != nil || ok {
		if err != nil {
			return out, err
		}
		out.Priority = &priority
	}
	labels, err := dictLabels(dict, "labels")
	if err != nil {
		return out, err
	}
	out.Labels = labels
	if err := validateScriptClassification(out); err != nil {
		return out, err
	}
	return out, nil
}

func mergeScriptClassification(out *assetClassification, assign config.AssetClassification) {
	if assign.Accept != nil && !*assign.Accept {
		out.Accepted = false
		out.RejectReason = strings.TrimSpace(assign.RejectReason)
		if out.RejectReason == "" {
			out.RejectReason = "asset starlark reject"
		}
		appendClassificationReason(out, "starlark")
		return
	}
	applyClassificationAssign(out, assign)
	appendClassificationReason(out, "starlark")
}

func appendClassificationReason(out *assetClassification, reason string) {
	if out.ClassificationReason == "" {
		out.ClassificationReason = reason
		return
	}
	out.ClassificationReason += "," + reason
}

func validateScriptClassification(assign config.AssetClassification) error {
	if assign.System != "" {
		if _, ok := config.NormalizeAssetSystem(assign.System); !ok {
			return fmt.Errorf("Starlark 返回了无效 system：%s", assign.System)
		}
	}
	if assign.Architecture != "" {
		if _, ok := config.NormalizeAssetArchitecture(assign.Architecture); !ok {
			return fmt.Errorf("Starlark 返回了无效 architecture：%s", assign.Architecture)
		}
	}
	return nil
}

func dictString(dict *starlark.Dict, key string) (string, error) {
	value, ok, err := dict.Get(starlark.String(key))
	if err != nil || !ok || value == starlark.None {
		return "", err
	}
	if text, ok := starlark.AsString(value); ok {
		return text, nil
	}
	return "", fmt.Errorf("Starlark 字段 %s 必须是 string", key)
}

func dictBool(dict *starlark.Dict, key string) (bool, bool, error) {
	value, ok, err := dict.Get(starlark.String(key))
	if err != nil || !ok || value == starlark.None {
		return false, false, err
	}
	b, ok := value.(starlark.Bool)
	if !ok {
		return false, false, fmt.Errorf("Starlark 字段 %s 必须是 bool", key)
	}
	return bool(b), true, nil
}

func dictInt(dict *starlark.Dict, key string) (int, bool, error) {
	value, ok, err := dict.Get(starlark.String(key))
	if err != nil || !ok || value == starlark.None {
		return 0, false, err
	}
	i, ok := value.(starlark.Int)
	if !ok {
		return 0, false, fmt.Errorf("Starlark 字段 %s 必须是 int", key)
	}
	n, ok := i.Int64()
	if !ok {
		return 0, false, fmt.Errorf("Starlark 字段 %s 超出 int64 范围", key)
	}
	return int(n), true, nil
}

func dictLabels(dict *starlark.Dict, key string) ([]string, error) {
	value, ok, err := dict.Get(starlark.String(key))
	if err != nil || !ok || value == starlark.None {
		return nil, err
	}
	if _, ok := value.(starlark.String); ok {
		return nil, fmt.Errorf("Starlark 字段 %s 必须是字符串列表", key)
	}
	iterable, ok := value.(starlark.Iterable)
	if !ok {
		return nil, fmt.Errorf("Starlark 字段 %s 必须是字符串列表", key)
	}
	var labels []string
	iter := iterable.Iterate()
	defer iter.Done()
	var item starlark.Value
	for iter.Next(&item) {
		label, ok := starlark.AsString(item)
		if !ok {
			return nil, fmt.Errorf("Starlark 字段 %s 必须是字符串列表", key)
		}
		labels = append(labels, label)
	}
	return labels, nil
}
