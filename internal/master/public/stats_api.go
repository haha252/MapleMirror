package public

import "net/http"

type statsSnapshot struct {
	Today string `json:"today"`
	HTML  struct {
		Metrics string `json:"metrics"`
		Ranks   string `json:"ranks"`
		Nodes   string `json:"nodes"`
	} `json:"html"`
	Trend []DailyTrend `json:"trend"`
}

func (s Server) statsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	stats, err := s.Store.StatsDashboard(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "统计数据读取失败")
		return
	}
	nodes, err := s.Store.Nodes(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "节点状态读取失败")
		return
	}
	var out statsSnapshot
	out.Today = stats.Today
	out.HTML.Metrics = metricsGrid(stats)
	out.HTML.Ranks = rankList(stats.Resources)
	out.HTML.Nodes = nodesTable(nodes)
	out.Trend = stats.Trend
	writeOK(w, r, http.StatusOK, "查询成功", out)
}
