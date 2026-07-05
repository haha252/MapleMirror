package buildinfo

import (
	"log/slog"
	"runtime"
	"runtime/debug"
)

// Attributes returns build identity fields embedded by go build.
func Attributes(version string) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("version", version),
		slog.String("go_version", runtime.Version()),
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return attrs
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			attrs = append(attrs, slog.String("vcs_revision", setting.Value))
		case "vcs.time":
			attrs = append(attrs, slog.String("vcs_time", setting.Value))
		case "vcs.modified":
			attrs = append(attrs, slog.String("vcs_modified", setting.Value))
		}
	}
	return attrs
}
