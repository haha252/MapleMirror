package public

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

var errInvalidCatalogQuery = errors.New("目录查询条件无效")

type catalogSelection struct {
	Group  string
	Option string
}

type catalogQuery struct {
	Search     string
	Selections []catalogSelection
	CacheKey   string
}

type catalogQueryResult struct {
	Query        catalogQuery
	Snapshot     *catalogSnapshot
	Projects     []string
	Suggestions  []string
	SelectedTags map[string]bool
}

type catalogCandidate struct {
	Project    catalogProject
	TagMatches int
	SearchRank int
	Fuzzy      bool
	Distance   int
}

func (i *catalogIndex) resolve(values url.Values) (catalogQueryResult, error) {
	snapshot := i.current()
	query, err := parseCatalogQuery(values, snapshot)
	if err != nil {
		return catalogQueryResult{}, err
	}
	return matchCatalogQuery(snapshot, query), nil
}

func parseCatalogQuery(values url.Values, snapshot *catalogSnapshot) (catalogQuery, error) {
	query := catalogQuery{Search: strings.TrimSpace(values.Get("q"))}
	if utf8.RuneCountInString(query.Search) > 100 {
		return query, errInvalidCatalogQuery
	}
	seen := map[string]bool{}
	for _, raw := range values["filter"] {
		group, option, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok {
			return query, errInvalidCatalogQuery
		}
		group, option = strings.TrimSpace(group), strings.TrimSpace(option)
		options, groupOK := snapshot.OptionLabels[group]
		if _, optionOK := options[option]; !groupOK || !optionOK {
			return query, errInvalidCatalogQuery
		}
		key := group + ":" + option
		if !seen[key] {
			query.Selections = append(query.Selections, catalogSelection{Group: group, Option: option})
			seen[key] = true
		}
	}
	sort.Slice(query.Selections, func(a, b int) bool {
		left, right := query.Selections[a], query.Selections[b]
		if left.Group != right.Group {
			return left.Group < right.Group
		}
		return left.Option < right.Option
	})
	parts := []string{normalizeCatalogText(query.Search)}
	for _, selection := range query.Selections {
		parts = append(parts, selection.Group+":"+selection.Option)
	}
	query.CacheKey = strings.Join(parts, "\x00")
	return query, nil
}

func matchCatalogQuery(snapshot *catalogSnapshot, query catalogQuery) catalogQueryResult {
	result := catalogQueryResult{
		Query: query, Snapshot: snapshot, SelectedTags: map[string]bool{},
	}
	for _, selection := range query.Selections {
		result.SelectedTags[selection.Group+"\x00"+selection.Option] = true
	}
	direct := directCatalogCandidates(snapshot, query)
	if query.Search != "" && len(direct) == 0 {
		direct = fuzzyCatalogCandidates(snapshot, query)
	}
	var exact, suggested []catalogCandidate
	for _, candidate := range direct {
		candidate.TagMatches = selectedTagMatches(candidate.Project, query.Selections)
		if len(query.Selections) == 0 {
			if candidate.Fuzzy {
				suggested = append(suggested, candidate)
			} else {
				exact = append(exact, candidate)
			}
			continue
		}
		if candidate.TagMatches == len(query.Selections) && !candidate.Fuzzy {
			exact = append(exact, candidate)
		} else if candidate.TagMatches > 0 {
			suggested = append(suggested, candidate)
		}
	}
	sortCatalogCandidates(exact, false)
	sortCatalogCandidates(suggested, true)
	if len(suggested) > 3 && anyFuzzyCandidate(suggested) {
		suggested = suggested[:3]
	}
	result.Projects = candidateIDs(exact)
	result.Suggestions = candidateIDs(suggested)
	return result
}

func directCatalogCandidates(snapshot *catalogSnapshot, query catalogQuery) []catalogCandidate {
	if query.Search == "" {
		out := make([]catalogCandidate, 0, len(snapshot.Projects))
		for _, project := range snapshot.Projects {
			out = append(out, catalogCandidate{Project: project})
		}
		return out
	}
	search := normalizeCatalogText(query.Search)
	var out []catalogCandidate
	for _, project := range snapshot.Projects {
		if rank, ok := directCatalogSearchRank(project, search); ok {
			out = append(out, catalogCandidate{Project: project, SearchRank: rank})
		}
	}
	return out
}

func directCatalogSearchRank(project catalogProject, search string) (int, bool) {
	if project.NormalizedID == search {
		return 0, true
	}
	if project.Normalized == search {
		return 1, true
	}
	for _, term := range project.TagTerms {
		if term == search {
			return 2, true
		}
	}
	for _, term := range project.SearchTerms {
		if term == search {
			return 2, true
		}
	}
	for _, term := range project.TagTerms {
		if strings.Contains(term, search) {
			return 3, true
		}
	}
	if strings.Contains(project.Normalized, search) {
		return 4, true
	}
	return 0, false
}

func fuzzyCatalogCandidates(snapshot *catalogSnapshot, query catalogQuery) []catalogCandidate {
	search := normalizeCatalogText(query.Search)
	var out []catalogCandidate
	for _, project := range snapshot.Projects {
		if distance, ok := fuzzyCatalogDistance(search, project); ok {
			out = append(out, catalogCandidate{
				Project: project, SearchRank: 5, Fuzzy: true, Distance: distance,
			})
		}
	}
	sortCatalogCandidates(out, true)
	return out
}

func selectedTagMatches(project catalogProject, selections []catalogSelection) int {
	matches := 0
	for _, selection := range selections {
		for _, value := range project.Tags[selection.Group] {
			if strings.EqualFold(strings.TrimSpace(value), selection.Option) {
				matches++
				break
			}
		}
	}
	return matches
}

func sortCatalogCandidates(items []catalogCandidate, suggestions bool) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if suggestions && left.TagMatches != right.TagMatches {
			return left.TagMatches > right.TagMatches
		}
		if left.SearchRank != right.SearchRank {
			return left.SearchRank < right.SearchRank
		}
		if left.Fuzzy && right.Fuzzy && left.Distance != right.Distance {
			return left.Distance < right.Distance
		}
		if left.Project.Normalized != right.Project.Normalized {
			return left.Project.Normalized < right.Project.Normalized
		}
		return left.Project.NormalizedID < right.Project.NormalizedID
	})
}

func anyFuzzyCandidate(items []catalogCandidate) bool {
	for _, item := range items {
		if item.Fuzzy {
			return true
		}
	}
	return false
}

func candidateIDs(items []catalogCandidate) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Project.ID)
	}
	return out
}
