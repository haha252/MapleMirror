package storage

import (
	"context"
	"log/slog"
)

type versionLogFunc func(context.Context, string, ...slog.Attr)

type openOptions struct {
	versionLogger versionLogFunc
}

type OpenOption func(*openOptions)

func WithVersionLogger(logger versionLogFunc) OpenOption {
	return func(opts *openOptions) {
		opts.versionLogger = logger
	}
}

func collectOpenOptions(options []OpenOption) openOptions {
	var opts openOptions
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return opts
}
