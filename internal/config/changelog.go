package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	ChangelogTimeLayout = "2006-01-02 15:04"
	changelogMaxBytes   = 64 * 1024
)

var changelogMonthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

type ChangelogEvent struct {
	Level       string    `yaml:"level"`
	Title       string    `yaml:"title"`
	Time        string    `yaml:"time"`
	Description string    `yaml:"description"`
	OccurredAt  time.Time `yaml:"-"`
	SourcePath  string    `yaml:"-"`
}

func LoadChangelog(root string) ([]ChangelogEvent, error) {
	months, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取更新日志目录失败：%w", err)
	}
	var events []ChangelogEvent
	for _, month := range months {
		if !month.IsDir() || !changelogMonthPattern.MatchString(month.Name()) {
			continue
		}
		loaded, err := loadChangelogMonth(root, month.Name())
		if err != nil {
			return nil, err
		}
		events = append(events, loaded...)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].SourcePath < events[j].SourcePath
		}
		return events[i].OccurredAt.After(events[j].OccurredAt)
	})
	return events, nil
}

func loadChangelogMonth(root, month string) ([]ChangelogEvent, error) {
	dir := filepath.Join(root, month)
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取更新日志月份目录 %s 失败：%w", month, err)
	}
	var events []ChangelogEvent
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name()))
		if !file.Type().IsRegular() || ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, file.Name())
		event, err := loadChangelogFile(path, filepath.Join(month, file.Name()), month)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func loadChangelogFile(path, relativePath, month string) (ChangelogEvent, error) {
	var event ChangelogEvent
	data, err := os.ReadFile(path)
	if err != nil {
		return event, fmt.Errorf("读取更新日志 %s 失败：%w", relativePath, err)
	}
	if len(data) > changelogMaxBytes {
		return event, fmt.Errorf("更新日志 %s 超过 64 KiB", relativePath)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&event); err != nil {
		return event, fmt.Errorf("解析更新日志 %s 失败：%w", relativePath, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return event, fmt.Errorf("更新日志 %s 只能包含一个 YAML 文档", relativePath)
	}
	event.Level = strings.TrimSpace(event.Level)
	event.Title = strings.TrimSpace(event.Title)
	event.Time = strings.TrimSpace(event.Time)
	event.Description = strings.TrimSpace(event.Description)
	event.SourcePath = filepath.ToSlash(relativePath)
	if err := validateChangelogEvent(&event, month); err != nil {
		return event, fmt.Errorf("更新日志 %s 无效：%w", relativePath, err)
	}
	return event, nil
}

func validateChangelogEvent(event *ChangelogEvent, month string) error {
	if ChangelogLevelRank(event.Level) < 0 {
		return errors.New("level 必须为 info、notice、warn 或 critical")
	}
	if event.Title == "" {
		return errors.New("title 不能为空")
	}
	if utf8.RuneCountInString(event.Title) > 200 {
		return errors.New("title 不能超过 200 个字符")
	}
	occurredAt, err := time.ParseInLocation(ChangelogTimeLayout, event.Time, shanghaiLocation())
	if err != nil {
		return fmt.Errorf("time 必须使用 YYYY-MM-DD HH:mm 格式：%w", err)
	}
	if occurredAt.Format("2006-01") != month {
		return fmt.Errorf("time 所属月份必须与目录 %s 一致", month)
	}
	event.OccurredAt = occurredAt
	return nil
}

func ChangelogLevelRank(level string) int {
	switch strings.TrimSpace(level) {
	case "info":
		return 0
	case "notice":
		return 1
	case "warn":
		return 2
	case "critical":
		return 3
	default:
		return -1
	}
}

func shanghaiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("BJT", 8*60*60)
	}
	return location
}
