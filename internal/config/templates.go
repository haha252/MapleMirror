package config

import (
	_ "embed"
	"path/filepath"
)

//go:embed templates/config.example.yaml
var MasterExample []byte

//go:embed templates/projects.example.yaml
var ProjectsExample []byte

//go:embed templates/projects.repair.yaml
var ProjectsRepairExample []byte

//go:embed templates/quota.example.yaml
var QuotaExample []byte

//go:embed templates/quota.repair.yaml
var QuotaRepairExample []byte

//go:embed templates/node.example.yaml
var NodeExample []byte

func WriteExamples(directory string) error {
	for name, data := range map[string][]byte{
		"config.example.yaml":   MasterExample,
		"projects.example.yaml": ProjectsExample,
		"quota.example.yaml":    QuotaExample,
		"node.example.yaml":     NodeExample,
	} {
		if err := writeExample(filepath.Join(directory, name), data); err != nil {
			return err
		}
	}
	return nil
}
