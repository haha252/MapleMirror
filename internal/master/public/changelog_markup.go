package public

import (
	"html/template"
	"net/url"
	"strings"
)

const changelogMarkupMaxDepth = 16

type changelogMarkup struct {
	HTML string
	Text string
}

func renderChangelogDescription(source string) changelogMarkup {
	return renderChangelogInline(source, 0)
}

func renderChangelogInline(source string, depth int) changelogMarkup {
	if depth >= changelogMarkupMaxDepth {
		return changelogLiteral(source)
	}
	var html, text strings.Builder
	for len(source) > 0 {
		if source[0] == '\n' {
			html.WriteString("<br>")
			text.WriteByte(' ')
			source = source[1:]
			continue
		}
		if rendered, rest, ok := renderChangelogLink(source, depth); ok {
			html.WriteString(rendered.HTML)
			text.WriteString(rendered.Text)
			source = rest
			continue
		}
		delimiter, open, close := changelogDelimiter(source)
		if delimiter != "" {
			if end := strings.Index(source[len(delimiter):], delimiter); end >= 0 {
				end += len(delimiter)
				inner := renderChangelogInline(source[len(delimiter):end], depth+1)
				html.WriteString(open)
				html.WriteString(inner.HTML)
				html.WriteString(close)
				text.WriteString(inner.Text)
				source = source[end+len(delimiter):]
				continue
			}
		}
		size := changelogLiteralPrefix(source)
		literal := changelogLiteral(source[:size])
		html.WriteString(literal.HTML)
		text.WriteString(literal.Text)
		source = source[size:]
	}
	return changelogMarkup{HTML: html.String(), Text: text.String()}
}

func renderChangelogLink(source string, depth int) (changelogMarkup, string, bool) {
	if !strings.HasPrefix(source, "[") {
		return changelogMarkup{}, source, false
	}
	middle := strings.Index(source, "](")
	if middle <= 1 {
		return changelogMarkup{}, source, false
	}
	end := strings.IndexByte(source[middle+2:], ')')
	if end < 0 {
		return changelogMarkup{}, source, false
	}
	end += middle + 2
	target := strings.TrimSpace(source[middle+2 : end])
	if !safeChangelogLink(target) {
		return changelogMarkup{}, source, false
	}
	label := renderChangelogInline(source[1:middle], depth+1)
	href := template.HTMLEscapeString(target)
	return changelogMarkup{
		HTML: `<a href="` + href + `" target="_blank" rel="noopener noreferrer">` +
			label.HTML + `</a>`,
		Text: label.Text,
	}, source[end+1:], true
}

func safeChangelogLink(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func changelogDelimiter(source string) (delimiter, open, close string) {
	switch {
	case strings.HasPrefix(source, "**"):
		return "**", "<strong>", "</strong>"
	case strings.HasPrefix(source, "~~"):
		return "~~", "<del>", "</del>"
	case strings.HasPrefix(source, "*"):
		return "*", "<em>", "</em>"
	default:
		return "", "", ""
	}
}

func changelogLiteralPrefix(source string) int {
	for index, r := range source {
		if index > 0 && (r == '\n' || r == '[' || r == '*' || r == '~') {
			return index
		}
		if index == 0 && (r == '\n' || r == '[' || r == '*' || r == '~') {
			return len(string(r))
		}
	}
	return len(source)
}

func changelogLiteral(source string) changelogMarkup {
	return changelogMarkup{
		HTML: template.HTMLEscapeString(source),
		Text: source,
	}
}
