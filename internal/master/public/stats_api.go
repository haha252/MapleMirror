package public

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type statsFastSnapshot struct {
	Metrics         [3][3]int64 `json:"m"`
	DownloadSources [2][3]int64 `json:"ds"`
	Today           [6]any      `json:"p"`
}

type statsDetailsSnapshot struct {
	Ranks [][]any            `json:"r"`
	Nodes [][]any            `json:"n"`
	Trend statsTrendSnapshot `json:"t"`
}

type statsTrendSnapshot struct {
	Start        string  `json:"s"`
	Views        []int64 `json:"v"`
	Downloads    []int64 `json:"d"`
	WebDownloads []int64 `json:"wd"`
	APIDownloads []int64 `json:"ad"`
	Bytes        []int64 `json:"b"`
}

func (s Server) statsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	stats, err := s.statsCache().fast(r.Context(), s.Store)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "统计数据读取失败")
		return
	}
	writeStatsJSON(w, stats)
}

func (s Server) statsDetailsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	stats, err := s.statsCache().details(r.Context(), s.Store)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "统计数据读取失败")
		return
	}
	writeStatsJSON(w, stats)
}

func (s Store) StatsRealtime(ctx context.Context) (statsFastSnapshot, error) {
	today := statDay(timeNow(), s.Location)
	start, previousStart := dateOffset(today, -29), dateOffset(today, -59)
	var out statsFastSnapshot
	var views, downloads, traffic MetricStat
	if err := s.loadMetricSummaries(ctx, previousStart, start, today,
		&views, &downloads, &traffic); err != nil {
		return out, err
	}
	var webDownloads, apiDownloads MetricStat
	if err := s.loadDownloadSourceSummaries(ctx, previousStart, start, today,
		&webDownloads, &apiDownloads); err != nil {
		return out, err
	}
	for i, metric := range []MetricStat{views, downloads, traffic} {
		out.Metrics[i] = metricTuple(metric)
	}
	out.DownloadSources[0] = metricTuple(webDownloads)
	out.DownloadSources[1] = metricTuple(apiDownloads)
	point, err := s.DailyTrends(ctx, today, today)
	if err != nil {
		return out, err
	}
	out.Today[0] = today
	if len(point) > 0 {
		out.Today[1], out.Today[2], out.Today[3] = point[0].Views, point[0].Downloads, point[0].SentBytes
		out.Today[4], out.Today[5] = point[0].WebDownloads, point[0].APIDownloads
	}
	return out, nil
}

func compactStatsDetails(stats StatsDashboard, nodes []NodeSummary) statsDetailsSnapshot {
	var out statsDetailsSnapshot
	for _, item := range stats.Resources {
		out.Ranks = append(out.Ranks, []any{
			item.ProjectName, item.Version, item.Architecture, item.DownloadCount,
			item.WebDownloadCount, item.APIDownloadCount,
		})
	}
	for _, item := range nodes {
		ready := 0
		if item.DownloadReady {
			ready = 1
		}
		out.Nodes = append(out.Nodes, []any{
			item.PublicName, item.State, item.LastHeartbeat, ready,
			item.DownloadReadyReason, item.SLA24H, item.SLA7D,
			item.TotalSentBytes, item.PressureRatio,
		})
	}
	out.Trend = compactTrend(stats.Trend)
	return out
}

func compactTrend(trends []DailyTrend) statsTrendSnapshot {
	var out statsTrendSnapshot
	if len(trends) > 0 {
		out.Start = trends[0].Day
	}
	for _, item := range trends {
		out.Views = append(out.Views, item.Views)
		out.Downloads = append(out.Downloads, item.Downloads)
		out.WebDownloads = append(out.WebDownloads, item.WebDownloads)
		out.APIDownloads = append(out.APIDownloads, item.APIDownloads)
		out.Bytes = append(out.Bytes, item.SentBytes)
	}
	return out
}

func metricTuple(metric MetricStat) [3]int64 {
	return [3]int64{metric.Total, metric.Recent, metric.Previous}
}

func writeStatsJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func statsJSONCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Accept-Encoding")
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzip.NewWriter(w)
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, writer: gz}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w gzipResponseWriter) Write(data []byte) (int, error) {
	return w.writer.Write(data)
}
