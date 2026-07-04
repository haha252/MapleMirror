package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"mirror-server/internal/config"
)

type Logger struct {
	component string
	console   *slog.Logger
	file      *slog.Logger
	writer    *dailyWriter
}

func New(component string, cfg config.Logging, location *time.Location, console io.Writer) (*Logger, error) {
	if console == nil {
		console = os.Stdout
	}
	maxFileSizeMB := cfg.MaxFileSizeMB
	if maxFileSizeMB <= 0 {
		maxFileSizeMB = config.DefaultLogMaxFileSizeMB
	}
	writer, err := newDailyWriter(cfg.Directory, component, cfg.RetentionDays, location,
		maxFileSizeMB*1024*1024)
	if err != nil {
		return nil, err
	}
	return &Logger{
		component: component,
		console:   slog.New(newConsoleHandler(console, selectedLevel(cfg.ConsoleLevel), location)).With("component", component),
		file:      slog.New(slog.NewJSONHandler(writer, handlerOptions(cfg.FileLevel))).With("component", component),
		writer:    writer,
	}, nil
}

func handlerOptions(level string) *slog.HandlerOptions {
	return &slog.HandlerOptions{Level: selectedLevel(level)}
}

func (l *Logger) Close() error {
	return l.writer.Close()
}

func (l *Logger) Debug(ctx context.Context, message string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelDebug, message, attrs...)
}

func (l *Logger) Info(ctx context.Context, message string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelInfo, message, attrs...)
}

func (l *Logger) Warn(ctx context.Context, message string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelWarn, message, attrs...)
}

func (l *Logger) Error(ctx context.Context, message string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelError, message, attrs...)
}

func (l *Logger) log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	l.console.LogAttrs(ctx, level, message, attrs...)
	l.file.LogAttrs(ctx, level, message, attrs...)
}

func (l *Logger) ConfigWarning(field, value string) {
	if replacement, ok := config.DeprecatedWarningMessage(value); ok {
		l.Warn(context.Background(), "配置字段已废弃，请迁移到新字段",
			slog.String("field", field), slog.String("replacement", replacement))
		return
	}
	l.Warn(context.Background(), "配置字段缺失，已使用默认值",
		slog.String("field", field), slog.String("value", value))
}

func StartupError(component string, err error) {
	fmt.Fprintf(os.Stderr, "%s启动失败：%v\n", component, err)
}
