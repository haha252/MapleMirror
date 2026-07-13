package public

import (
	"fmt"
	"testing"
)

func TestBuildSponsorPageFoldsSameSponsorMonthByLatestDonation(t *testing.T) {
	page := buildSponsorPage([]Sponsor{
		{Name: "同一人", Date: "2026-01-03", Amount: "¥1.25", Method: "wechat"},
		{Name: " 同一人 ", Date: "2026-01-21", Amount: "￥2.75", Method: "alipay"},
		{Name: "较新的人", Date: "2026-02-01", Amount: "¥5", Method: "wechat"},
	}, 1, 10)

	if page.TotalDonations != 3 || len(page.Entries) != 2 {
		t.Fatalf("page = %+v, want 3 donations folded into 2 entries", page)
	}
	if page.Entries[0].Name != "较新的人" {
		t.Fatalf("first entry = %q, want latest group", page.Entries[0].Name)
	}
	group := page.Entries[1]
	if group.Name != "同一人" || group.Date != "2026-01-21" || group.Amount != "¥4" {
		t.Fatalf("folded group = %+v", group)
	}
	if len(group.Donations) != 2 || group.Donations[0].Date != "2026-01-21" {
		t.Fatalf("donations are not latest first: %+v", group.Donations)
	}
	if len(group.Methods) != 2 || group.Methods[0] != "alipay" || group.Methods[1] != "wechat" {
		t.Fatalf("methods = %v, want latest unique methods", group.Methods)
	}
}

func TestFoldSponsorsKeepsMonthsAndAnonymousRecordsSeparate(t *testing.T) {
	groups := foldSponsors([]Sponsor{
		{Name: "同一人", Date: "2026-01-01"},
		{Name: "同一人", Date: "2026-02-01"},
		{Name: "<None>", Date: "2026-01-02"},
		{Name: "<none>", Date: "2026-01-03"},
		{Name: "", Date: "2026-01-04"},
		{Name: "同一人", Date: "日期不合法"},
	})

	if len(groups) != 6 {
		t.Fatalf("group count = %d, want 6 separate groups", len(groups))
	}
}

func TestFoldSponsorsUsesOptionalSponsorID(t *testing.T) {
	groups := foldSponsors([]Sponsor{
		{SponsorID: "stable-id", Name: "旧昵称", Date: "2026-03-01", Amount: "¥1"},
		{SponsorID: "stable-id", Name: "新昵称", Date: "2026-03-20", Amount: "¥2"},
	})

	if len(groups) != 1 || groups[0].Name != "新昵称" || groups[0].Amount != "¥3" {
		t.Fatalf("groups = %+v, want one group using latest nickname", groups)
	}
}

func TestBuildSponsorPageKeepsPinnedGroupsOutsidePagination(t *testing.T) {
	sponsors := []Sponsor{
		{Name: "置顶者", Date: "2026-06-01", Amount: "¥1"},
		{Name: "置顶者", Date: "2026-06-02", Amount: "¥2", Pinned: true},
	}
	for index := 1; index <= 11; index++ {
		sponsors = append(sponsors, Sponsor{
			Name:   fmt.Sprintf("普通-%02d", index),
			Date:   fmt.Sprintf("2026-05-%02d", index),
			Amount: "¥1",
		})
	}

	first := buildSponsorPage(sponsors, 1, 10)
	second := buildSponsorPage(sponsors, 2, 10)
	beyondLast := buildSponsorPage(sponsors, 99, 10)
	if len(first.Pinned) != 1 || len(first.Pinned[0].Donations) != 2 {
		t.Fatalf("pinned groups = %+v", first.Pinned)
	}
	if first.PageCount != 2 || len(first.Entries) != 10 {
		t.Fatalf("first page = %+v", first)
	}
	if len(second.Pinned) != 1 || len(second.Entries) != 1 || second.Page != 2 {
		t.Fatalf("second page = %+v", second)
	}
	if beyondLast.Page != 2 || len(beyondLast.Entries) != 1 {
		t.Fatalf("out-of-range page was not clamped: %+v", beyondLast)
	}
}

func TestCombinedSponsorAmountFallsBackWithoutGuessing(t *testing.T) {
	donations := []Sponsor{{Amount: "礼物一份"}, {Amount: "¥2"}}
	if got := combinedSponsorAmount(donations); got != "礼物一份 + ¥2" {
		t.Fatalf("combined amount = %q", got)
	}
}
