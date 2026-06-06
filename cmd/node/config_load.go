package main

import (
	"errors"

	"mirror-server/internal/config"
)

func loadNodeConfig(path string, warn config.WarnFunc) (config.Node, bool, error) {
	cfg, err := config.LoadNode(path, warn)
	if errors.Is(err, config.ErrExampleCreated) {
		return config.Node{}, true, nil
	}
	return cfg, false, err
}
