(function () {
  var a = window.admin, w = window.adminWorkspace;
  if (!a || !w || a.page() !== "security") return;
  var ip = "", page = 1, selected = "", rows = [], generation = 0, busy = false, queued = false, loadedAt = 0;
  function status(item) {
    if (item.status === "active") return "下载中";
    if (item.status === "expired_first_connection") return "未使用已过期";
    if (item.status === "expired_idle") return Number(item.sent_bytes) > 0 ? "下载结束" : "空闲超时";
    if (item.status === "expired_max_duration") return "达到最长时长";
    return Number(item.sent_bytes) > 0 ? "已产生流量" : "等待下载";
  }
  function render() {
    w.list("download-history-list", rows, function (r) { return r.authorization_id; }, function (r) {
      return '<span><strong>' + a.esc(r.file_name || "未知文件") + '</strong><span class="sub">' + a.esc((r.project_name || r.project_id || "未知项目") + " · " + (r.version || "未知版本")) + '</span></span><span>' + a.badge(status(r)) + '</span><span class="ws-row-meta"><strong>' + a.esc(a.bytes(r.sent_bytes)) + '</strong><span class="sub">' + a.esc(r.issued_at || "—") + '</span></span>';
    }, selected);
    if (!document.getElementById("security-downloads").hidden) show();
  }
  function show() {
    var r = rows.find(function (r) { return r.authorization_id === selected; });
    if (!r) return w.detail("下载记录", "", '<div class="ws-empty">在左侧选择记录</div><p class="ws-note">每条记录代表一次下载令牌签发；Range 与重试流量合并到同一条记录。查询不包含未签发令牌的请求。</p>');
    w.detail("下载记录", "", w.title(r.file_name || "未知文件", a.badge(status(r))) + w.section("授权与实际下载", w.kv([
      ["实际发送", a.bytes(r.sent_bytes)], ["项目", r.project_name || r.project_id], ["版本", r.version], ["平台", [r.system, r.architecture].filter(Boolean).join(" / ") || "未标注"],
      ["下载节点", r.node_name || r.node_id], ["签发时间", r.issued_at], ["首次传输", r.first_transfer_at || "未开始"], ["最后传输", r.last_transfer_at || "暂无"], ["授权到期", r.expires_at],
      ["签发来源", r.source_kind === "api" ? "公开 API" : "网页下载"], ["状态原因", r.status_reason || "暂无"]])) + '<details class="ws-section"><summary>技术信息</summary>' +
      a.compactKv({"Authorization ID": r.authorization_id, "Asset ID": r.asset_id, "Project ID": r.project_id, "Node ID": r.node_id, "Request ID": r.request_id}, "detail-plain") + '</details>');
  }
  function load(force) {
    if (!ip || !force && Date.now() - loadedAt < 15000) return Promise.resolve();
    if (busy) { if (force) queued = true; return Promise.resolve(); }
    busy = true; var token = generation;
    return w.read("/admin/api/security/download-history?ip=" + encodeURIComponent(ip) + "&page=" + page + "&page_size=20").then(function (data) {
      if (token !== generation) return;
      loadedAt = Date.now(); rows = data.downloads || []; var s = data.summary || {};
      w.html("download-history-summary", '<span class="muted">' + a.esc(data.ip || ip) + ' · 最近 ' + a.esc(data.retention_days || 7) + ' 天 · 签发 ' + a.esc(s.token_count || 0) + ' 次 · 下载 ' + a.esc(s.transfer_count || 0) + ' 次 · ' + a.esc(a.bytes(s.sent_bytes)) + '</span>');
      w.pager("download-history-pager", data.pagination, function (next) { page = next; generation++; load(true).catch(report); });
      render(); w.updated("security-updated");
    }).then(function () { finish(); }, function (err) { finish(); if (token === generation) throw err; });
  }
  function finish() { busy = false; if (queued) { queued = false; load(true).catch(report); } }
  function report(err) { a.setStatus(err.message); }
  function query() {
    var value = document.getElementById("download-history-ip").value.trim();
    if (!value) return a.setStatus("请输入要查询的 IP。");
    ip = value; page = 1; selected = ""; rows = []; generation++; render(); load(true).catch(report);
  }
  function clear() {
    ip = ""; selected = ""; rows = []; page = 1; generation++; document.getElementById("download-history-ip").value = "";
    w.html("download-history-summary", ""); w.html("download-history-pager", ""); render();
    w.html("download-history-list", '<div class="ws-empty">输入单个 IPv4 或 IPv6 地址查询下载记录</div>');
  }
  document.getElementById("download-history-query").onclick = query;
  document.getElementById("download-history-clear").onclick = clear;
  document.getElementById("download-history-ip").onkeydown = function (event) { if (event.key === "Enter") query(); };
  document.getElementById("download-history-list").onclick = function (event) {
    var row = event.target.closest("[data-ws-key]"); if (!row) return; selected = row.getAttribute("data-ws-key"); render(); w.open();
  };
  window.adminDownloadHistory = {refresh: load, show: show};
  clear();
})();
