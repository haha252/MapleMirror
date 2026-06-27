package public

import (
	"fmt"
	"html/template"
	"math"
	"time"
)

func fillSeries(start, end string, values map[string]int64) []int64 {
	days := daysBetween(start, end)
	out := make([]int64, 0, len(days))
	for _, day := range days {
		out = append(out, values[day])
	}
	return out
}

func combineTrends(start, end string, views, downloads, webDownloads, apiDownloads, bytes map[string]int64) []DailyTrend {
	days := daysBetween(start, end)
	out := make([]DailyTrend, 0, len(days))
	for _, day := range days {
		out = append(out, DailyTrend{
			Day: day, Views: views[day], Downloads: downloads[day],
			WebDownloads: webDownloads[day], APIDownloads: apiDownloads[day],
			SentBytes: bytes[day],
		})
	}
	return out
}

func daysBetween(start, end string) []string {
	first, err := time.Parse("2006-01-02", start)
	if err != nil {
		return []string{}
	}
	last, err := time.Parse("2006-01-02", end)
	if err != nil {
		return []string{}
	}
	var out []string
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		out = append(out, day.Format("2006-01-02"))
	}
	return out
}

func trendLabel(recent, previous int64) string {
	if previous == 0 {
		if recent == 0 {
			return "0%"
		}
		return "+100%"
	}
	change := (float64(recent-previous) / float64(previous)) * 100
	sign := "+"
	if change < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, change)
}

func sparkline(values []int64) template.HTML {
	if len(values) == 0 {
		return ""
	}
	max := int64(1)
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	points := make([]byte, 0, len(values)*8)
	for i, value := range values {
		x := float64(i) * 100 / math.Max(float64(len(values)-1), 1)
		y := 28 - (float64(value)*24/float64(max) + 2)
		points = append(points, []byte(fmt.Sprintf("%.1f,%.1f ", x, y))...)
	}
	return template.HTML(`<svg class="sparkline" viewBox="0 0 100 30" aria-hidden="true"><polyline points="` +
		template.HTMLEscapeString(string(points)) + `" /></svg>`)
}
