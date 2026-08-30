(function () {
  var a = window.admin;
  if (!a || a.page() !== "security") return;
  var currentIP = "", currentPage = 1;

  function pager(el, page, total, size) {
    var pages = Math.max(1, Math.ceil((total || 0) / size));
    el.innerHTML = '<button class="admin-secondary" data-history-prev>上一页</button>' +
      '<span>第 ' + page + ' / ' + pages + ' 页，共 ' + (total || 0) + ' 条</span>' +
      '<button class="admin-secondary" data-history-next>下一页</button>';
    el.querySelector("[data-history-prev]").disabled = page <= 1;
    el.querySelector("[data-history-next]").disabled = page >= pages;
    el.querySelector("[data-history-prev]").onclick = function () { load(page - 1); };
    el.querySelector("[data-history-next]").onclick = function () { load(page + 1); };
  }

  function statusLabel(item) {
    var status = item.status || "issued", sent = Number(item.sent_bytes || 0);
    if (status === "active") return "下载中";
    if (status === "expired_first_connection") return "未使用已过期";
    if (status === "expired_idle") return sent > 0 ? "下载结束" : "空闲超时";
    if (status === "expired_max_duration") return "达到最长时长";
    return sent > 0 ? "已产生流量" : "等待下载";
  }

  function reasonLabel(reason) {
    if (reason === "expired_first_connection") return "首次连接超时";
    if (reason === "expired_idle") return "空闲超时";
    if (reason === "expired_max_duration") return "达到最长下载时长";
    return reason || "暂无";
  }

  function sourceLabel(source) {
    return source === "api" ? "公开 API" : "网页下载";
  }

  function platform(item) {
    return [item.system, item.architecture].filter(Boolean).join(" / ") || "未标注平台";
  }

  function detail(label, value) {
    return '<div class="admin-record__detail-item"><span>' + a.esc(label) +
      '</span><strong>' + a.esc(value || "—") + '</strong></div>';
  }

  function card(item) {
    var sent = Number(item.sent_bytes || 0);
    return '<article class="admin-record"><div class="admin-record__summary">' +
      '<div class="admin-record__identity"><strong>' + a.esc(item.file_name || "未知文件") +
      '</strong><span class="sub">' + a.esc((item.project_name || item.project_id || "未知项目") +
      " · " + (item.version || "未知版本") + " · " + platform(item)) + '</span></div>' +
      '<div class="admin-record__state">' + a.badge(statusLabel(item)) +
      '<span class="admin-record__meta">' + a.esc(a.bytes(sent)) + ' · ' +
      a.esc(item.issued_at || "未知时间") + '</span></div>' +
      '<div class="admin-record__actions"><button class="admin-secondary" data-history-detail>查看详情</button></div>' +
      '</div><div class="admin-record__detail" data-history-detail-box hidden>' +
      '<div class="admin-record__detail-grid">' +
      detail("实际发送", a.bytes(sent)) + detail("下载节点", item.node_name || item.node_id) +
      detail("签发时间", item.issued_at) + detail("首次传输", item.first_transfer_at || "未开始") +
      detail("最后传输", item.last_transfer_at || "暂无") + detail("授权到期", item.expires_at) +
      detail("签发来源", sourceLabel(item.source_kind)) + detail("状态原因", reasonLabel(item.status_reason)) +
      '</div><details class="admin-tech-details"><summary>显示技术信息</summary>' +
      a.compactKv({"Authorization ID": item.authorization_id, "Asset ID": item.asset_id,
        "Project ID": item.project_id, "Node ID": item.node_id, "Request ID": item.request_id}, "detail-plain") +
      '</details></div></article>';
  }

  function renderSummary(data) {
    var summary = data.summary || {};
    document.getElementById("download-history-summary").innerHTML =
      '<span class="security-summary__fact">查询 IP<strong>' + a.esc(data.ip || currentIP) + '</strong></span>' +
      '<span class="security-summary__fact">保留范围<strong>最近 ' + a.esc(data.retention_days || 7) + ' 天</strong></span>' +
      '<span class="security-summary__fact">令牌签发<strong>' + a.esc(summary.token_count || 0) + ' 次</strong></span>' +
      '<span class="security-summary__fact">产生下载<strong>' + a.esc(summary.transfer_count || 0) + ' 次</strong></span>' +
      '<span class="security-summary__fact">累计流量<strong>' + a.esc(a.bytes(summary.sent_bytes || 0)) + '</strong></span>';
  }

  function load(page) {
    if (!currentIP) return;
    currentPage = page || 1;
    var path = "/admin/api/security/download-history?ip=" + encodeURIComponent(currentIP) +
      "&page=" + currentPage + "&page_size=20";
    a.api(path).then(function (data) {
      renderSummary(data);
      var rows = data.downloads || [];
      document.getElementById("download-history-list").innerHTML = rows.length ?
        rows.map(card).join("") :
        '<div class="security-attention-empty">最近保留范围内没有这个 IP 的下载授权记录。</div>';
      pager(document.getElementById("download-history-pager"), data.pagination.page,
        data.pagination.total, data.pagination.page_size);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function query() {
    currentIP = document.getElementById("download-history-ip").value.trim();
    if (!currentIP) return a.setStatus("请输入要查询的 IP。");
    load(1);
  }

  function clear() {
    currentIP = ""; currentPage = 1;
    document.getElementById("download-history-ip").value = "";
    document.getElementById("download-history-summary").innerHTML = "";
    document.getElementById("download-history-list").innerHTML =
      '<div class="security-attention-empty">输入单个 IPv4 或 IPv6 地址后查询最近下载记录。</div>';
    document.getElementById("download-history-pager").innerHTML = "";
  }

  document.getElementById("download-history-query").onclick = query;
  document.getElementById("download-history-clear").onclick = clear;
  document.getElementById("download-history-ip").addEventListener("keydown", function (event) {
    if (event.key === "Enter") query();
  });
  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-history-detail]");
    if (!button) return;
    var box = button.closest(".admin-record").querySelector("[data-history-detail-box]");
    box.hidden = !box.hidden;
    button.textContent = box.hidden ? "查看详情" : "收起";
  });
  clear();
})();
