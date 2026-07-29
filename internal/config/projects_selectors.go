package config

type AssetSelectorConfig struct {
	ArchitectureEnabled *bool `yaml:"architecture_enabled,omitempty" json:"ArchitectureEnabled,omitempty"`
	SystemEnabled       *bool `yaml:"system_enabled,omitempty" json:"SystemEnabled,omitempty"`
}

func (p Project) ArchitectureSelectorEnabled() bool {
	if enabled := p.AssetPipeline.Selectors.ArchitectureEnabled; enabled != nil {
		return *enabled
	}
	return p.PipelineUsesArchitecture()
}

func (p Project) SystemSelectorEnabled() bool {
	if enabled := p.AssetPipeline.Selectors.SystemEnabled; enabled != nil {
		return *enabled
	}
	return p.PipelineUsesSystem()
}
