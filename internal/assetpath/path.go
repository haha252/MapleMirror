package assetpath

import (
	"errors"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

var ErrInvalidPath = errors.New("资产路径不合法")

var windowsReservedNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

type Parts struct {
	ProjectID string
	Version   string
	FileName  string
}

func PublicPath(projectID, version, fileName string) string {
	return "/" + url.PathEscape(projectID) + "/" + url.PathEscape(version) + "/" + url.PathEscape(fileName)
}

func ParsePublicPath(value string) (Parts, error) {
	value = strings.TrimPrefix(value, "/")
	segments := strings.Split(value, "/")
	if len(segments) != 3 {
		return Parts{}, ErrInvalidPath
	}
	var out Parts
	values := []*string{&out.ProjectID, &out.Version, &out.FileName}
	for i, segment := range segments {
		if segment == "" {
			return Parts{}, ErrInvalidPath
		}
		decoded, err := url.PathUnescape(segment)
		if err != nil || strings.TrimSpace(decoded) == "" {
			return Parts{}, ErrInvalidPath
		}
		*values[i] = decoded
	}
	return out, nil
}

func SafeRelativePath(projectID, version, fileName string) string {
	return filepath.Join(safePart(projectID), safePart(version), safeFileName(fileName))
}

func safeFileName(value string) string {
	value = filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	if value == "." || value == string(filepath.Separator) {
		value = ""
	}
	return safePart(value)
}

func safePart(value string) string {
	value = path.Clean(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "." || value == ".." || strings.HasPrefix(value, "../") {
		value = ""
	}
	value = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		if r < 32 {
			return '_'
		}
		return r
	}, value)
	value = strings.TrimRight(value, ". ")
	if value == "" {
		return "asset"
	}
	if isWindowsReservedName(value) {
		return "asset-" + value
	}
	return value
}

func isWindowsReservedName(value string) bool {
	name := value
	if dot := strings.IndexByte(name, '.'); dot >= 0 {
		name = name[:dot]
	}
	_, reserved := windowsReservedNames[strings.ToUpper(name)]
	return reserved
}
