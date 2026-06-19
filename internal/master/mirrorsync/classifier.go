package mirrorsync

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"mirror-server/internal/config"
)

type assetClassification struct {
	Accepted             bool
	RejectReason         string
	Architecture         string
	System               string
	Variant              string
	DisplayLabel         string
	Priority             int
	Labels               []string
	ClassificationReason string
}

type assetClassifier struct {
	project        config.Project
	architectureRE *regexp.Regexp
	systemRE       *regexp.Regexp
	ruleRegexes    []*regexp.Regexp
	usesArch       bool
	usesSystem     bool
	script         *starlarkClassifier
}

func newAssetClassifier(project config.Project) (assetClassifier, error) {
	archRE, err := compileArchitectureRegex(project)
	if err != nil {
		return assetClassifier{}, err
	}
	systemRE, err := compileSystemRegex(project)
	if err != nil {
		return assetClassifier{}, err
	}
	out := assetClassifier{
		project:        project,
		architectureRE: archRE,
		systemRE:       systemRE,
		usesArch:       project.PipelineUsesArchitecture(),
		usesSystem:     project.PipelineUsesSystem(),
	}
	for _, rule := range project.AssetPipeline.Classify.Rules {
		if strings.TrimSpace(rule.Match.Regex) == "" {
			out.ruleRegexes = append(out.ruleRegexes, nil)
			continue
		}
		re, err := regexp.Compile(rule.Match.Regex)
		if err != nil {
			return assetClassifier{}, err
		}
		out.ruleRegexes = append(out.ruleRegexes, re)
	}
	script, err := loadStarlarkClassifier(project)
	if err != nil {
		return assetClassifier{}, err
	}
	out.script = script
	return out, nil
}

func (c assetClassifier) Classify(asset ResourceCandidate, release ResourceVersion, batch []ResourceCandidate) (assetClassification, error) {
	out := assetClassification{
		Accepted:     true,
		Architecture: assetArchitecture(asset.FileName, c.architectureRE),
		System:       assetSystem(asset.FileName, c.systemRE),
	}
	reasons := []string{}
	for i, rule := range c.project.AssetPipeline.Classify.Rules {
		matched, err := c.ruleMatches(rule.Match, i, asset.FileName)
		if err != nil {
			return assetClassification{}, err
		}
		if !matched {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("rule:%d", i+1))
		if rule.Assign.Accept != nil && !*rule.Assign.Accept {
			out.Accepted = false
			out.RejectReason = strings.TrimSpace(rule.Assign.RejectReason)
			if out.RejectReason == "" {
				out.RejectReason = "asset classify reject"
			}
			break
		}
		applyClassificationAssign(&out, rule.Assign)
	}
	out.ClassificationReason = strings.Join(reasons, ",")
	if c.script != nil {
		scriptOut, err := c.script.Classify(asset, release, c.project, batch)
		if err != nil {
			return assetClassification{}, err
		}
		mergeScriptClassification(&out, scriptOut)
	}
	if c.usesArch && strings.TrimSpace(out.Architecture) == "" {
		out.Architecture = "None"
	}
	if c.usesSystem && strings.TrimSpace(out.System) == "" {
		out.System = "None"
	}
	return out, nil
}

func (c assetClassifier) ruleMatches(match config.AssetClassifyMatch, index int, name string) (bool, error) {
	if match.Exact != "" {
		return name == match.Exact, nil
	}
	if match.Glob != "" {
		return path.Match(match.Glob, name)
	}
	if match.Regex != "" {
		return c.ruleRegexes[index].MatchString(name), nil
	}
	return false, nil
}

func applyClassificationAssign(out *assetClassification, assign config.AssetClassification) {
	if assign.System != "" {
		if value, ok := config.NormalizeAssetSystem(assign.System); ok {
			out.System = value
		}
	}
	if assign.Architecture != "" {
		if value, ok := config.NormalizeAssetArchitecture(assign.Architecture); ok {
			out.Architecture = value
		}
	}
	if assign.Variant != "" {
		out.Variant = strings.TrimSpace(assign.Variant)
	}
	if assign.DisplayLabel != "" {
		out.DisplayLabel = strings.TrimSpace(assign.DisplayLabel)
	}
	if assign.Priority != nil {
		out.Priority = *assign.Priority
	}
	if len(assign.Labels) > 0 {
		out.Labels = append([]string(nil), assign.Labels...)
	}
}
