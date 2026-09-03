package public

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"golang.org/x/net/html"

	"mirror-server/internal/publiclocale"
)

func loadPublicLocaleMessages(staticFS fs.FS) (map[string]map[string]string, error) {
	catalog := make(map[string]map[string]string)
	for _, locale := range publiclocale.All() {
		if strings.TrimSpace(locale.Script) == "" {
			return nil, fmt.Errorf("语言 %q 缺少翻译脚本", locale.ID)
		}
		data, err := fs.ReadFile(staticFS, strings.TrimPrefix(locale.Script, "/"))
		if err != nil {
			return nil, fmt.Errorf("读取语言 %q 翻译脚本: %w", locale.ID, err)
		}
		messages, err := extractLocaleMessages(data)
		if err != nil {
			return nil, fmt.Errorf("解析语言 %q 翻译脚本: %w", locale.ID, err)
		}
		catalog[locale.ID] = messages
	}
	return catalog, nil
}

func extractLocaleMessages(script []byte) (map[string]string, error) {
	const marker = "messages:"
	start := bytes.Index(script, []byte(marker))
	if start < 0 {
		return nil, fmt.Errorf("未找到 %s", marker)
	}
	open := bytes.IndexByte(script[start+len(marker):], '{')
	if open < 0 {
		return nil, fmt.Errorf("messages 对象缺少左花括号")
	}
	open += start + len(marker)
	end, ok := matchingJSONObjectEnd(script, open)
	if !ok {
		return nil, fmt.Errorf("messages 对象没有闭合")
	}
	var messages map[string]string
	if err := json.Unmarshal(script[open:end+1], &messages); err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages 为空")
	}
	return messages, nil
}

func matchingJSONObjectEnd(data []byte, open int) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(data); i++ {
		ch := data[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func localizeRenderedHTML(raw []byte, messages map[string]string) ([]byte, error) {
	if len(messages) == 0 {
		return raw, nil
	}
	document, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	localizeHTMLNode(document, messages)
	var out bytes.Buffer
	if err := html.Render(&out, document); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func localizeHTMLNode(node *html.Node, messages map[string]string) {
	if node.Type == html.ElementNode {
		params := i18nParams(node)
		if key := htmlAttr(node, "data-i18n"); key != "" {
			if message, ok := messages[key]; ok {
				setHTMLText(node, interpolateLocaleMessage(message, params))
			}
		}
		for _, item := range []struct {
			source string
			target string
		}{
			{"data-i18n-title", "title"},
			{"data-i18n-aria-label", "aria-label"},
			{"data-i18n-placeholder", "placeholder"},
			{"data-i18n-alt", "alt"},
		} {
			key := htmlAttr(node, item.source)
			if key == "" {
				continue
			}
			if message, ok := messages[key]; ok {
				setHTMLAttr(node, item.target, interpolateLocaleMessage(message, params))
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		localizeHTMLNode(child, messages)
	}
}

func i18nParams(node *html.Node) map[string]any {
	raw := htmlAttr(node, "data-i18n-params")
	if raw == "" {
		return nil
	}
	var params map[string]any
	if json.Unmarshal([]byte(raw), &params) != nil {
		return nil
	}
	return params
}

func interpolateLocaleMessage(message string, params map[string]any) string {
	for key, value := range params {
		message = strings.ReplaceAll(message, "{"+key+"}", fmt.Sprint(value))
	}
	return message
}

func htmlAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func setHTMLAttr(node *html.Node, name, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == name {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: name, Val: value})
}

func setHTMLText(node *html.Node, value string) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		node.RemoveChild(child)
		child = next
	}
	node.AppendChild(&html.Node{Type: html.TextNode, Data: value})
}
