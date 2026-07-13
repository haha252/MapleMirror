package public

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const sponsorPageSize = 10

type sponsorGroup struct {
	Name      string
	Date      string
	Amount    string
	Pinned    bool
	Methods   []string
	Donations []Sponsor
}

type sponsorPage struct {
	TotalDonations int
	Pinned         []sponsorGroup
	Entries        []sponsorGroup
	Page           int
	PageCount      int
}

func buildSponsorPage(sponsors []Sponsor, requestedPage, pageSize int) sponsorPage {
	if pageSize <= 0 {
		pageSize = sponsorPageSize
	}
	groups := foldSponsors(sponsors)
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Date > groups[j].Date })

	pinned := make([]sponsorGroup, 0)
	regular := make([]sponsorGroup, 0, len(groups))
	for _, group := range groups {
		if group.Pinned {
			pinned = append(pinned, group)
		} else {
			regular = append(regular, group)
		}
	}

	pageCount := (len(regular) + pageSize - 1) / pageSize
	if pageCount == 0 {
		pageCount = 1
	}
	if requestedPage < 1 {
		requestedPage = 1
	}
	if requestedPage > pageCount {
		requestedPage = pageCount
	}
	start := (requestedPage - 1) * pageSize
	end := min(start+pageSize, len(regular))

	return sponsorPage{
		TotalDonations: len(sponsors),
		Pinned:         pinned,
		Entries:        regular[start:end],
		Page:           requestedPage,
		PageCount:      pageCount,
	}
}

func foldSponsors(sponsors []Sponsor) []sponsorGroup {
	groups := make([]sponsorGroup, 0, len(sponsors))
	indexes := make(map[string]int)
	for index, sponsor := range sponsors {
		key, foldable := sponsorFoldKey(sponsor)
		if !foldable {
			key = "record:" + strconv.Itoa(index)
		}
		groupIndex, exists := indexes[key]
		if !exists {
			groupIndex = len(groups)
			indexes[key] = groupIndex
			groups = append(groups, sponsorGroup{})
		}
		groups[groupIndex].Donations = append(groups[groupIndex].Donations, sponsor)
	}
	for index := range groups {
		finishSponsorGroup(&groups[index])
	}
	return groups
}

func sponsorFoldKey(sponsor Sponsor) (string, bool) {
	date, err := time.Parse("2006-01-02", strings.TrimSpace(sponsor.Date))
	if err != nil {
		return "", false
	}
	if id := strings.TrimSpace(sponsor.SponsorID); id != "" {
		return "id:" + id + ":" + date.Format("2006-01"), true
	}
	name := strings.TrimSpace(sponsor.Name)
	if name == "" || strings.EqualFold(name, "<none>") {
		return "", false
	}
	return "name:" + name + ":" + date.Format("2006-01"), true
}

func finishSponsorGroup(group *sponsorGroup) {
	sort.SliceStable(group.Donations, func(i, j int) bool {
		return group.Donations[i].Date > group.Donations[j].Date
	})
	latest := group.Donations[0]
	group.Name = strings.TrimSpace(latest.Name)
	group.Date = strings.TrimSpace(latest.Date)
	group.Amount = combinedSponsorAmount(group.Donations)
	seenMethods := make(map[string]bool)
	for _, donation := range group.Donations {
		group.Pinned = group.Pinned || donation.Pinned
		method := normalizedSponsorMethod(donation.Method)
		if method != "" && !seenMethods[method] {
			seenMethods[method] = true
			group.Methods = append(group.Methods, method)
		}
	}
}

func normalizedSponsorMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "alipay":
		return "alipay"
	case "wechat", "weixin":
		return "wechat"
	default:
		return ""
	}
}

func combinedSponsorAmount(donations []Sponsor) string {
	if len(donations) == 1 {
		return donations[0].Amount
	}
	var total int64
	for _, donation := range donations {
		amount, ok := sponsorAmountCents(donation.Amount)
		if !ok || amount > maxInt64-total {
			return joinedSponsorAmounts(donations)
		}
		total += amount
	}
	if total%100 == 0 {
		return "¥" + strconv.FormatInt(total/100, 10)
	}
	return "¥" + strconv.FormatInt(total/100, 10) + "." + twoDigits(total%100)
}

const maxInt64 = int64(1<<63 - 1)

func sponsorAmountCents(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "¥")
	value = strings.TrimPrefix(value, "￥")
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", "")
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || !decimalDigits(parts[0]) {
		return 0, false
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, false
	}
	fraction := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 || !decimalDigits(parts[1]) {
			return 0, false
		}
		fraction, _ = strconv.ParseInt(parts[1], 10, 64)
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	if whole > (maxInt64-fraction)/100 {
		return 0, false
	}
	return whole*100 + fraction, true
}

func decimalDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func twoDigits(value int64) string {
	if value < 10 {
		return "0" + strconv.FormatInt(value, 10)
	}
	return strconv.FormatInt(value, 10)
}

func joinedSponsorAmounts(donations []Sponsor) string {
	amounts := make([]string, 0, len(donations))
	for _, donation := range donations {
		if amount := strings.TrimSpace(donation.Amount); amount != "" {
			amounts = append(amounts, amount)
		}
	}
	return strings.Join(amounts, " + ")
}

func requestedSponsorPage(value string) int {
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 {
		return 1
	}
	return page
}
