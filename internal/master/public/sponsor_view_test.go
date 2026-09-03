package public

import (
	"strings"
	"testing"
)

func TestSponsorsCardRendersExpandableMonthlyGroup(t *testing.T) {
	page := buildSponsorPage([]Sponsor{
		{Name: "展开者", Date: "2026-04-03", Amount: "¥1", Method: "wechat"},
		{Name: "展开者", Date: "2026-04-20", Amount: "¥2", Method: "alipay"},
	}, 1, 10)
	body := sponsorsCard(page)

	for _, expected := range []string{
		`<details class="sponsor-group">`,
		`<summary class="sponsor-row">`,
		`本月 2 次`,
		`data-i18n="about.sponsorCount" data-i18n-params='{"count":2}'>2 次</span>`,
		`<article class="sponsor-donation">`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	latest := strings.Index(body, "2026-04-20")
	oldest := strings.LastIndex(body, "2026-04-03")
	if latest < 0 || oldest < 0 || latest >= oldest {
		t.Fatalf("expanded donations are not latest first: %s", body)
	}
}

func TestSponsorsCardDoesNotRenderFeaturedSectionWithoutPinned(t *testing.T) {
	page := buildSponsorPage([]Sponsor{{Name: "普通", Date: "2026-01-01", Amount: "¥1"}}, 1, 10)
	body := sponsorsCard(page)

	if strings.Contains(body, "特别感谢") || strings.Contains(body, "sponsor-featured") {
		t.Fatalf("unexpected featured section: %s", body)
	}
	if strings.Contains(body, "<details") {
		t.Fatalf("single donation should not be expandable: %s", body)
	}
}

func TestSponsorsCardRendersPinnedSectionAndPager(t *testing.T) {
	sponsors := []Sponsor{{Name: "置顶", Date: "2026-07-01", Amount: "¥1", Pinned: true}}
	for index := 0; index < 11; index++ {
		sponsors = append(sponsors, Sponsor{Name: string(rune('A' + index)), Date: "2026-06-01", Amount: "¥1"})
	}
	body := sponsorsCard(buildSponsorPage(sponsors, 1, 10))

	for _, expected := range []string{
		`id="sponsor-featured-title" data-i18n="about.sponsorFeatured">特别感谢</h3>`,
		`aria-label="赞助记录分页"`,
		`data-i18n="about.previous" aria-disabled="true">上一页</span>`,
		`href="?sponsor_page=2#sponsors">下一页</a>`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
}

func TestSponsorPageNumbersAreCompact(t *testing.T) {
	got := sponsorPageNumbers(5, 10)
	want := []int{1, 0, 4, 5, 6, 0, 10}
	if len(got) != len(want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("pages = %v, want %v", got, want)
		}
	}
}
