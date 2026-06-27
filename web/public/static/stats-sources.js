(function () {
  function sourceParts(item) {
    let total = Number(item && item.downloads || 0);
    let web = Number(item && item.web_downloads || 0);
    let api = Number(item && item.api_downloads || 0);
    if (!web && !api && total) web = total;
    if (!total) total = web + api;
    if (web + api !== total) web = Math.max(0, total - api);
    return {total: total, web: web, api: api};
  }

  function compactTrend(trend) {
    if (!trend || !trend.s) return [];
    const start = new Date(trend.s + "T00:00:00Z");
    return (trend.v || []).map((views, i) => {
      const day = new Date(start.getTime() + i * 86400000).toISOString().slice(0, 10);
      const downloads = (trend.d || [])[i] || 0;
      const web = (trend.wd || [])[i] || 0;
      const api = (trend.ad || [])[i] || 0;
      return normalizePoint(day, views, downloads, web, api, (trend.b || [])[i] || 0);
    });
  }

  function todayPoint(point) {
    if (!Array.isArray(point) || !point[0]) return null;
    return normalizePoint(point[0], point[1] || 0, point[2] || 0,
      point[4] || 0, point[5] || 0, point[3] || 0);
  }

  function normalizePoint(day, views, downloads, web, api, sentBytes) {
    return {day: day, views: views, downloads: downloads,
      web_downloads: (!web && !api && downloads) ? downloads : web,
      api_downloads: api, sent_bytes: sentBytes};
  }

  window.MirrorStatsSources = {
    sourceParts: sourceParts,
    compactTrend: compactTrend,
    todayPoint: todayPoint
  };
})();
