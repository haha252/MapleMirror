package public

import (
	"strings"
	"unicode"
)

func fuzzyCatalogDistance(query string, project catalogProject) (int, bool) {
	queryRunes := []rune(query)
	if len(queryRunes) < 2 {
		return 0, false
	}
	limit := fuzzyDistanceLimit(len(queryRunes))
	best := limit + 1
	for _, term := range project.SearchTerms {
		for _, candidate := range fuzzyTerms(term) {
			distance := damerauLevenshtein(queryRunes, []rune(candidate), limit)
			if distance < best {
				best = distance
			}
		}
	}
	return best, best <= limit
}

func fuzzyDistanceLimit(length int) int {
	switch {
	case length <= 4:
		return 1
	case length <= 8:
		return 2
	default:
		return 3
	}
}

func fuzzyTerms(value string) []string {
	terms := []string{value}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, field := range fields {
		if field != value {
			terms = appendSearchTerm(terms, field)
		}
	}
	return terms
}

func damerauLevenshtein(left, right []rune, limit int) int {
	if delta := len(left) - len(right); delta > limit || delta < -limit {
		return limit + 1
	}
	previousPrevious := make([]int, len(right)+1)
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current[0] = i
		rowMinimum := current[0]
		for j := 1; j <= len(right); j++ {
			cost := 0
			if left[i-1] != right[j-1] {
				cost = 1
			}
			current[j] = minCatalogInt(
				previous[j]+1,
				current[j-1]+1,
				previous[j-1]+cost,
			)
			if i > 1 && j > 1 && left[i-1] == right[j-2] && left[i-2] == right[j-1] {
				current[j] = minCatalogInt(current[j], previousPrevious[j-2]+1)
			}
			if current[j] < rowMinimum {
				rowMinimum = current[j]
			}
		}
		if rowMinimum > limit {
			return limit + 1
		}
		previousPrevious, previous, current = previous, current, previousPrevious
	}
	return previous[len(right)]
}

func minCatalogInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}
