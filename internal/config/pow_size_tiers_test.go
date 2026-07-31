package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyFixedPoWDifficultyMigratesToSizeTiers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := strings.Replace(string(MasterExample), "pow_size_tiers:", "legacy_pow_size_tiers:", 1)
	legacy = strings.Replace(legacy, "# 公开 API PoW 挑战配置。\n",
		"altcha:\n  difficulty: 24\n  challenge_ttl: \"2m\"\n\n# 公开 API PoW 挑战配置。\n", 1)
	legacy = strings.Replace(legacy, "  algorithm: \"sha256\"\n",
		"  algorithm: \"sha256\"\n  leading_zero_bits: 24\n", 1)
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	var deprecated []string
	cfg, err := LoadMaster(path, func(field, value string) {
		if replacement, ok := DeprecatedWarningMessage(value); ok {
			deprecated = append(deprecated, field+"=>"+replacement)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PoWSizeTiers) != len(defaultPoWSizeTiers) {
		t.Fatalf("旧固定难度未迁移为默认分档：%+v", cfg.PoWSizeTiers)
	}
	if !containsString(deprecated, "altcha.difficulty=>pow_size_tiers") ||
		!containsString(deprecated, "api_pow.leading_zero_bits=>pow_size_tiers") {
		t.Fatalf("旧固定难度缺少弃用警告：%+v", deprecated)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Contains(text, "legacy_pow_size_tiers") ||
		strings.Contains(text, "leading_zero_bits:") ||
		strings.Contains(text, "altcha:\n  difficulty:") {
		t.Fatalf("旧固定难度字段未从配置删除：%s", text)
	}
	if !strings.Contains(text, "min_size: 500 MiB") ||
		!strings.Contains(text, "difficulty: 26") {
		t.Fatalf("新大小分档未写回配置：%s", text)
	}
}

func TestPoWSizeTierValidation(t *testing.T) {
	tests := []struct {
		name  string
		tiers []PoWSizeTier
		want  string
	}{
		{name: "empty", want: "至少需要一个分档"},
		{name: "first not zero", tiers: []PoWSizeTier{
			{MinSize: "1 B", Difficulty: 20}}, want: "第一档"},
		{name: "invalid unit", tiers: []PoWSizeTier{
			{MinSize: "0 MB", Difficulty: 20}}, want: "B、KiB、MiB 或 GiB"},
		{name: "unordered", tiers: []PoWSizeTier{
			{MinSize: "0 B", Difficulty: 20},
			{MinSize: "5 MiB", Difficulty: 22},
			{MinSize: "1 MiB", Difficulty: 23}}, want: "严格递增"},
		{name: "duplicate", tiers: []PoWSizeTier{
			{MinSize: "0 B", Difficulty: 20},
			{MinSize: "0 B", Difficulty: 22}}, want: "严格递增"},
		{name: "difficulty", tiers: []PoWSizeTier{
			{MinSize: "0 B", Difficulty: 0}}, want: "difficulty 必须大于零"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePoWSizeTiers(tc.tiers)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("校验错误=%v，期望包含 %q", err, tc.want)
			}
		})
	}
}
