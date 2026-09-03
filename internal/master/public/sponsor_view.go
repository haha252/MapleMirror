package public

import (
	"sort"
	"strconv"
	"strings"
)

func sponsorsCard(page sponsorPage) string {
	count := strconv.Itoa(page.TotalDonations)
	body := `<section id="sponsors" class="panel-card sponsor-card"><div class="sponsor-card__head"><div class="about-card__title">` + aboutIcon("heart") + `<h2 data-i18n="about.sponsors">赞助者列表</h2></div><span data-i18n="about.sponsorCount" data-i18n-params='{"count":` + count + `}'>` + num(int64(page.TotalDonations)) + ` 次</span></div>`
	if page.TotalDonations == 0 {
		return body + `<p class="muted empty" data-i18n="about.sponsorEmpty">暂无赞助者记录</p></section>`
	}
	if len(page.Pinned) > 0 {
		body += `<section class="sponsor-featured" aria-labelledby="sponsor-featured-title"><h3 id="sponsor-featured-title" data-i18n="about.sponsorFeatured">特别感谢</h3><div class="sponsor-list sponsor-list--featured">`
		body += sponsorGroupRows(page.Pinned)
		body += `</div></section>`
	}
	if len(page.Entries) > 0 {
		body += `<div class="sponsor-list" data-i18n-aria-label="about.sponsorRecords" aria-label="赞助记录">` + sponsorGroupRows(page.Entries) + `</div>`
	}
	body += sponsorPager(page)
	return body + `</section>`
}

func sponsorGroupRows(groups []sponsorGroup) string {
	var body strings.Builder
	for _, group := range groups {
		body.WriteString(sponsorGroupRow(group))
	}
	return body.String()
}

func sponsorGroupRow(group sponsorGroup) string {
	row := sponsorGroupSummary(group)
	if len(group.Donations) == 1 {
		return `<article class="sponsor-row">` + row + `</article>`
	}
	return `<details class="sponsor-group"><summary class="sponsor-row">` + row + `</summary><div class="sponsor-group__items">` + sponsorDonationRows(group.Donations) + `</div></details>`
}

func sponsorGroupSummary(group sponsorGroup) string {
	initial := strings.TrimSpace(group.Name)
	if initial == "" {
		initial = "赞"
	}
	badges := sponsorMethodBadges(group.Methods)
	if group.Pinned {
		badges += `<span class="sponsor-badge sponsor-badge--pinned" data-i18n="about.sponsorPinned">置顶</span>`
	}
	if len(group.Donations) > 1 {
		count := strconv.Itoa(len(group.Donations))
		badges += `<span class="sponsor-badge sponsor-badge--count" data-i18n="about.sponsorMonthCount" data-i18n-params='{"count":` + count + `}'>本月 ` + count + ` 次</span>`
	}
	return `<span class="sponsor-avatar">` + esc(firstRune(initial)) + `</span><span class="sponsor-row__main"><strong>` + esc(group.Name) + `</strong>` + badges + `<time datetime="` + esc(group.Date) + `">` + esc(group.Date) + `</time></span><b>` + esc(group.Amount) + `</b>`
}

func sponsorDonationRows(donations []Sponsor) string {
	var body strings.Builder
	for _, donation := range donations {
		badges := sponsorBadge(donation.Method)
		if donation.Pinned {
			badges += `<span class="sponsor-badge sponsor-badge--pinned" data-i18n="about.sponsorPinned">置顶</span>`
		}
		body.WriteString(`<article class="sponsor-donation"><time datetime="` + esc(donation.Date) + `">` + esc(donation.Date) + `</time><span>` + badges + `</span><b>` + esc(donation.Amount) + `</b></article>`)
	}
	return body.String()
}

func sponsorPager(page sponsorPage) string {
	if page.PageCount <= 1 {
		return ""
	}
	body := `<nav class="sponsor-pager" data-i18n-aria-label="about.sponsorPager" aria-label="赞助记录分页">`
	body += sponsorPagerControl("上一页", "about.previous", page.Page-1, page.Page == 1, "prev")
	body += `<span class="sponsor-pager__pages">`
	for _, number := range sponsorPageNumbers(page.Page, page.PageCount) {
		if number == 0 {
			body += `<span class="sponsor-pager__ellipsis" aria-hidden="true">...</span>`
		} else if number == page.Page {
			body += `<span class="sponsor-pager__page is-current" aria-current="page">` + strconv.Itoa(number) + `</span>`
		} else {
			body += `<a class="sponsor-pager__page" href="?sponsor_page=` + strconv.Itoa(number) + `#sponsors">` + strconv.Itoa(number) + `</a>`
		}
	}
	body += `</span>`
	body += sponsorPagerControl("下一页", "about.next", page.Page+1, page.Page == page.PageCount, "next")
	return body + `</nav>`
}

func sponsorPagerControl(label, i18nKey string, page int, disabled bool, relation string) string {
	if disabled {
		return `<span class="sponsor-pager__control is-disabled" data-i18n="` + i18nKey + `" aria-disabled="true">` + label + `</span>`
	}
	return `<a class="sponsor-pager__control" data-i18n="` + i18nKey + `" rel="` + relation + `" href="?sponsor_page=` + strconv.Itoa(page) + `#sponsors">` + label + `</a>`
}

func sponsorPageNumbers(current, total int) []int {
	wanted := map[int]bool{1: true, total: true, current - 1: true, current: true, current + 1: true}
	pages := make([]int, 0, 7)
	for page := range wanted {
		if page >= 1 && page <= total {
			pages = append(pages, page)
		}
	}
	sort.Ints(pages)
	result := make([]int, 0, len(pages)+2)
	for index, page := range pages {
		if index > 0 && page-pages[index-1] > 1 {
			result = append(result, 0)
		}
		result = append(result, page)
	}
	return result
}

func sponsorMethodBadges(methods []string) string {
	var badges strings.Builder
	for _, method := range methods {
		badges.WriteString(sponsorBadge(method))
	}
	return badges.String()
}

func sponsorBadge(method string) string {
	switch normalizedSponsorMethod(method) {
	case "alipay":
		return `<span class="sponsor-badge sponsor-badge--alipay">Alipay</span>`
	case "wechat":
		return `<span class="sponsor-badge sponsor-badge--wechat">WeChat</span>`
	default:
		return ""
	}
}

func firstRune(value string) string {
	for _, char := range value {
		return string(char)
	}
	return "赞"
}
