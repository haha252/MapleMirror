package config

import (
	_ "embed"
	"path/filepath"
)

//go:embed templates/config.example.yaml
var MasterExample []byte

//go:embed templates/projects.example.yaml
var ProjectsExample []byte

//go:embed templates/projects/example.yaml
var ProjectExample []byte

//go:embed templates/projects.repair.yaml
var ProjectsRepairExample []byte

//go:embed templates/quota.example.yaml
var QuotaExample []byte

//go:embed templates/quota.repair.yaml
var QuotaRepairExample []byte

//go:embed templates/notices.example.yaml
var NoticesExample []byte

//go:embed templates/filters.example.yaml
var FiltersExample []byte

//go:embed templates/node.example.yaml
var NodeExample []byte

func WriteExamples(directory string) error {
	for name, data := range map[string][]byte{
		"config.example.yaml":   MasterExample,
		"projects.example.yaml": ProjectsExample,
		"projects/example.yaml": ProjectExample,
		"quota.example.yaml":    QuotaExample,
		"notices.example.yaml":  NoticesExample,
		"filters.example.yaml":  FiltersExample,
		"node.example.yaml":     NodeExample,
	} {
		if err := writeExample(filepath.Join(directory, name), data); err != nil {
			return err
		}
	}
	return nil
}
