package publiclocale

import "strings"

type Locale struct {
	ID         string
	PathPrefix string
	HrefLang   string
	Script     string
	Default    bool
}

var registry = []Locale{
	{
		ID:       "zh-CN",
		HrefLang: "zh-CN",
		Script:   "i18n/zh-CN.js",
		Default:  true,
	},
	{
		ID:         "en",
		PathPrefix: "en",
		HrefLang:   "en",
		Script:     "i18n/en.js",
	},
}

func All() []Locale {
	result := make([]Locale, len(registry))
	copy(result, registry)
	return result
}

func Default() Locale {
	for _, locale := range registry {
		if locale.Default {
			return locale
		}
	}
	return registry[0]
}

func Lookup(id string) (Locale, bool) {
	id = strings.TrimSpace(id)
	for _, locale := range registry {
		if locale.ID == id {
			return locale, true
		}
	}
	return Locale{}, false
}

func LocalizedPath(localeID, path string) string {
	if path == "" || !strings.HasPrefix(path, "/") {
		return path
	}
	locale, ok := Lookup(localeID)
	if !ok {
		locale = Default()
	}
	prefix := strings.Trim(strings.TrimSpace(locale.PathPrefix), "/")
	if locale.Default || prefix == "" {
		return path
	}
	if path == "/" {
		return "/" + prefix + "/"
	}
	return "/" + prefix + path
}

func Paths(path string) []string {
	locales := All()
	paths := make([]string, 0, len(locales))
	for _, locale := range locales {
		paths = append(paths, LocalizedPath(locale.ID, path))
	}
	return paths
}

func RouteMap(path string) map[string]string {
	locales := All()
	routes := make(map[string]string, len(locales))
	for _, locale := range locales {
		routes[locale.ID] = LocalizedPath(locale.ID, path)
	}
	return routes
}
