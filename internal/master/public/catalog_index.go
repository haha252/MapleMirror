package public

import (
	"sort"
	"strings"
	"sync"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

type catalogIndex struct {
	mu           sync.RWMutex
	snapshot     *catalogSnapshot
	projectsPath string
	filtersPath  string
	cache        *catalogResultCache
	logger       *logging.Logger
	stop         chan struct{}
	done         chan struct{}
	observed     string
}

type catalogSnapshot struct {
	Generation   uint64
	Filters      config.Filters
	Projects     []catalogProject
	ByID         map[string]catalogProject
	OptionLabels map[string]map[string]string
	ProjectFiles []string
}

type catalogProject struct {
	ID           string
	Name         string
	NormalizedID string
	Normalized   string
	Tags         map[string][]string
	TagTerms     []string
	SearchTerms  []string
	Assets       projectAssetConfig
}

func newCatalogSnapshot(projects config.Projects, filters config.Filters, generation uint64) *catalogSnapshot {
	snapshot := &catalogSnapshot{
		Generation: generation, Filters: filters, ByID: map[string]catalogProject{},
		OptionLabels: filterOptionLabels(filters), ProjectFiles: append([]string(nil), projects.ProjectFiles...),
	}
	for _, project := range projects.Projects {
		if !project.Enabled {
			continue
		}
		item := catalogProject{
			ID: project.ID, Name: project.Name,
			NormalizedID: normalizeCatalogText(project.ID),
			Normalized:   normalizeCatalogText(project.Name),
			Tags:         cloneProjectTags(project.Tags),
			Assets: projectAssetConfig{
				IconPath:                    project.ResolvedIconPath,
				ArchitectureSelectorEnabled: project.ArchitectureSelectorEnabled(),
				SystemSelectorEnabled:       project.SystemSelectorEnabled(),
				DefaultSelectionMode:        project.NormalizedDefaultSelectionMode(),
			},
		}
		item.SearchTerms = appendSearchTerm(item.SearchTerms, item.NormalizedID)
		item.SearchTerms = appendSearchTerm(item.SearchTerms, item.Normalized)
		for group, values := range item.Tags {
			for _, value := range values {
				normalized := normalizeCatalogText(value)
				item.TagTerms = appendSearchTerm(item.TagTerms, normalized)
				item.SearchTerms = appendSearchTerm(item.SearchTerms, normalized)
				if label := snapshot.optionLabel(group, value); label != "" {
					item.SearchTerms = appendSearchTerm(item.SearchTerms, normalizeCatalogText(label))
				}
			}
		}
		snapshot.Projects = append(snapshot.Projects, item)
		snapshot.ByID[item.ID] = item
	}
	sort.Slice(snapshot.Projects, func(i, j int) bool {
		left, right := snapshot.Projects[i], snapshot.Projects[j]
		if left.Normalized != right.Normalized {
			return left.Normalized < right.Normalized
		}
		return left.NormalizedID < right.NormalizedID
	})
	return snapshot
}

func filterOptionLabels(filters config.Filters) map[string]map[string]string {
	labels := make(map[string]map[string]string, len(filters.Selectors))
	for _, selector := range filters.Selectors {
		options := make(map[string]string, len(selector.Options))
		for _, option := range selector.Options {
			options[option.ID] = option.Name
		}
		labels[selector.ID] = options
	}
	return labels
}

func (s *catalogSnapshot) optionLabel(group, value string) string {
	options := s.OptionLabels[strings.TrimSpace(group)]
	for optionID, label := range options {
		if strings.EqualFold(optionID, strings.TrimSpace(value)) {
			return label
		}
	}
	return ""
}

func cloneProjectTags(tags map[string][]string) map[string][]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string][]string, len(tags))
	for group, values := range tags {
		cleanGroup := strings.TrimSpace(group)
		for _, value := range values {
			out[cleanGroup] = append(out[cleanGroup], strings.TrimSpace(value))
		}
	}
	return out
}

func appendSearchTerm(terms []string, value string) []string {
	if value == "" {
		return terms
	}
	for _, current := range terms {
		if current == value {
			return terms
		}
	}
	return append(terms, value)
}

func normalizeCatalogText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (i *catalogIndex) current() *catalogSnapshot {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.snapshot
}

func (i *catalogIndex) assets() map[string]projectAssetConfig {
	snapshot := i.current()
	out := make(map[string]projectAssetConfig, len(snapshot.Projects))
	for _, project := range snapshot.Projects {
		out[project.ID] = project.Assets
	}
	return out
}
