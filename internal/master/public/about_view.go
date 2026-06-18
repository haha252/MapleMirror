package public

import (
	"encoding/json"
	"errors"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	sponsorFileName        = "sponsor.json"
	sponsorExampleFileName = "sponsor.example.json"
	legacySponsorFileName  = "sponsors.json"
)

type Sponsor struct {
	Name   string `json:"name"`
	Date   string `json:"date"`
	Amount string `json:"amount"`
	Pinned bool   `json:"pinned"`
	Method string `json:"method"`
}

func loadSponsors() []Sponsor {
	return loadSponsorsFromFiles(sponsorFileCandidates())
}

func loadSponsorsFromFiles(paths []string) []Sponsor {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil
		}
		return parseSponsors(data)
	}
	return nil
}

func parseSponsors(data []byte) []Sponsor {
	var sponsors []Sponsor
	if err := json.Unmarshal(data, &sponsors); err != nil {
		return nil
	}
	sort.SliceStable(sponsors, func(i, j int) bool {
		if sponsors[i].Pinned != sponsors[j].Pinned {
			return sponsors[i].Pinned
		}
		return sponsors[i].Date > sponsors[j].Date
	})
	return sponsors
}

func sponsorFileCandidates() []string {
	paths := make([]string, 0, 6)
	seen := make(map[string]bool)
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	addDir := func(dir string) {
		add(filepath.Join(dir, sponsorFileName))
		add(filepath.Join(dir, legacySponsorFileName))
		add(filepath.Join(dir, ".config", sponsorFileName))
	}
	if exe, err := os.Executable(); err == nil {
		addDir(filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		addDir(cwd)
	}
	if dir, err := findRepoResource("configs"); err == nil {
		add(filepath.Join(dir, legacySponsorFileName))
		add(filepath.Join(dir, sponsorFileName))
		add(filepath.Join(dir, sponsorExampleFileName))
	}
	return paths
}

const mirrorDescription = "枫源镜像 是一个公益镜像服务，面向 Github Release 设计。我们致力于为所有用户提供高速且稳定的下载服务，获取到软件的最新版本。"

func aboutBody(sponsors []Sponsor) template.HTML {
	body := `<section class="about-stack">`
	body += aboutCard("info", "项目简介", mirrorDescription)
	body += `<section class="panel-card about-card"><div class="about-card__title">` + aboutIcon("heart") + `<h2>赞助支持</h2></div><p>您的支持将会<strong>全部用于</strong> 枫源镜像 的服务器、带宽、域名等支出。</p><div class="donate-grid"><article><h3>微信</h3><img src="/static/public/wechat.png" alt="微信赞助二维码"></article><article><h3>支付宝</h3><img src="/static/public/alipay.png" alt="支付宝赞助二维码"></article></div></section>`
	body += sponsorsCard(sponsors)
	body += `<section class="about-section"><h2>致谢</h2><div class="thanks-grid"><article class="panel-card thanks-card"><h3>页面设计</h3><p>本站的页面设计大量参考了<a href="https://miawa.cn/" rel="noopener noreferrer" target="_blank"><strong>柠枺镜像</strong></a>的现代化设计。</p></article></div></section>`
	body += `<section class="panel-card about-card"><h2>贡献</h2><p>想为我们贡献服务器节点？亦或者是发现了安全漏洞向我们报告？我们随时欢迎！邮箱：<a href="mailto:frostlynx@qq.com">frostlynx@qq.com</a></p></section>`
	body += `</section>`
	return template.HTML(body)
}

func aboutCard(icon, title, text string) string {
	return `<section class="panel-card about-card"><div class="about-card__title">` + aboutIcon(icon) + `<h2>` + esc(title) + `</h2></div><p>` + text + `</p></section>`
}

func sponsorsCard(sponsors []Sponsor) string {
	body := `<section class="panel-card sponsor-card"><div class="sponsor-card__head"><div class="about-card__title">` + aboutIcon("heart") + `<h2>赞助者列表</h2></div><span>` + num(int64(len(sponsors))) + ` 位</span></div><div class="sponsor-list">`
	if len(sponsors) == 0 {
		body += `<p class="muted empty">暂无赞助者记录</p>`
	}
	for _, sponsor := range sponsors {
		body += sponsorRow(sponsor)
	}
	return body + `</div></section>`
}

func sponsorRow(s Sponsor) string {
	initial := strings.TrimSpace(s.Name)
	if initial == "" {
		initial = "赞"
	}
	badges := sponsorBadge(s.Method)
	if s.Pinned {
		badges += `<span class="sponsor-badge sponsor-badge--pinned">置顶</span>`
	}
	return `<article class="sponsor-row"><span class="sponsor-avatar">` + esc(firstRune(initial)) + `</span><div><strong>` + esc(s.Name) + `</strong>` + badges + `<time>` + esc(s.Date) + `</time></div><b>` + esc(s.Amount) + `</b></article>`
}

func firstRune(value string) string {
	for _, r := range value {
		return string(r)
	}
	return "赞"
}

func sponsorBadge(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "alipay":
		return `<span class="sponsor-badge sponsor-badge--alipay">Alipay</span>`
	case "wechat", "weixin":
		return `<span class="sponsor-badge sponsor-badge--wechat">WeChat</span>`
	default:
		return ""
	}
}

func aboutIcon(name string) string {
	if name == "heart" {
		return `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20.8 4.6a5.4 5.4 0 0 0-7.6 0L12 5.8l-1.2-1.2a5.4 5.4 0 0 0-7.6 7.6L12 21l8.8-8.8a5.4 5.4 0 0 0 0-7.6Z"/></svg>`
	}
	return `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 17v-6m0-4h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"/></svg>`
}
