(function () {
  var a = window.admin, w = window.adminWorkspace, history = window.adminDownloadHistory;
  if (!a || !w || a.page() !== "security") return;
  var view = "blocks", blockPage = 1, auditPage = 1, query = "", selected = "", blocks = [], audits = [], generation = 0;
  var summaryAt = 0, blocksAt = 0, auditAt = 0, manual = false, summary = {};
  function key(r) { return view === "blocks" ? r.kind + ":" + r.key : String(r.id || r.event_id || r.request_id + r.operation + r.created_at); }
  function reason(r) { var labels = {admin_login_failed: "登录失败自动封禁", request_quota_exhausted: "请求次数额度耗尽", traffic_limit_exceeded: "每日流量额度耗尽", static_blocklist: "静态封禁名单"}; return String(r.reason || "").split(";").map(function (value) { value = value.trim(); return labels[value] || value; }).join("；") || (r.source === "manual" ? "手动封禁" : r.source) || "未填写原因"; }
  function render() {
    if (view === "downloads") return history.show();
    if (view === "blocks") {
      w.list("blocks-list", blocks, key, function (r) {
        return '<input type="checkbox" data-block-select data-kind="' + a.esc(r.kind) + '" data-key="' + a.esc(r.key) + '" aria-label="选择 ' + a.esc(r.display_ip || r.key) + '"><span><strong>' + a.esc(r.display_ip || r.masked_ip || r.key) + '</strong><span class="sub">' + a.esc((r.kind === "admin" ? "管理登录" : "公开下载") + " · " + reason(r)) + '</span></span><span>' + a.badge(r.punishment_active ? "惩罚验证中" : "封禁中") + '<span class="sub">到期 ' + a.esc(r.expires_at || "未知") + '</span></span>';
      }, selected, true);
      updateChecks();
    } else {
      var q = document.getElementById("audit-search").value.toLowerCase();
      w.list("audit-list", audits.filter(function (r) { return [a.auditOperationLabel(r.operation), r.target_name, r.target_id, a.resultLabel(r.result), r.details_summary].join(" ").toLowerCase().indexOf(q) >= 0; }), key, function (r) {
        return '<span><strong>' + a.esc(a.auditOperationLabel(r.operation)) + '</strong><span class="sub">' + a.esc(r.target_name || r.target_id || "系统") + '</span></span><span>' + a.badge(a.resultLabel(r.result)) + '</span><span class="ws-row-meta">' + a.esc(r.created_at || "—") + '</span>';
      }, selected);
    }
    if (!manual) show();
  }
  function updateChecks() {
    var inputs = Array.from(document.querySelectorAll("[data-block-select]")), all = document.getElementById("blocks-check-all");
    all.checked = inputs.length > 0 && inputs.every(function (el) { return el.checked; });
    all.indeterminate = !all.checked && inputs.some(function (el) { return el.checked; });
  }
  function show() {
    var r = (view === "blocks" ? blocks : audits).find(function (r) { return key(r) === selected; });
    if (!r) { selected = ""; return w.detail(view === "blocks" ? "封禁详情" : "审计详情", "", '<div class="ws-empty">在左侧选择记录</div>'); }
    if (view === "blocks") {
      w.detail("封禁详情", '<button class="admin-secondary" id="block-delete-one">解除封禁</button>', w.title(r.display_ip || r.key, a.badge(r.kind === "admin" ? "管理登录" : "公开下载")) +
        w.section("原因与范围", w.kv([["原因", reason(r)], ["匹配范围", r.key], ["来源", r.source || "—"], ["封禁时间", r.blocked_at], ["到期时间", r.expires_at]])) +
        w.section("封禁后访问", w.kv([["最后尝试", r.last_attempt_at || "暂无"], ["尝试次数", r.attempts_after_block || 0], ["升级等级", "L" + (r.escalation_level || 0)], ["惩罚验证", r.punishment_active ? "执行中" : "未触发"]])) + '<p class="ws-note">封禁后尝试次数最多延迟约 30 秒。请求次数额度与每日流量额度分别计算。</p>');
    } else {
      w.detail("审计详情", "", w.title(a.auditOperationLabel(r.operation), a.badge(a.resultLabel(r.result))) + w.section("操作记录", w.kv([["目标", r.target_name || r.target_id || "系统"], ["时间", r.created_at], ["结果", a.resultLabel(r.result)], ["说明", r.details_summary || "没有补充说明"]])) +
        '<details class="ws-section"><summary>技术信息</summary>' + a.compactKv({"操作": r.operation, "目标类型": r.target_type, "目标 ID": r.target_id, "Request ID": r.request_id}, "detail-plain") + '</details>');
    }
  }
  function load() {
    var token = generation, requests = [];
    if (Date.now() - summaryAt >= 30000) requests.push(w.read("/admin/api/security/summary").then(function (data) {
      summaryAt = Date.now(); summary = data; w.metrics("security-summary", [["活跃封禁", data.active_blocks || 0], ["登录异常来源", data.login_failure_sources || 0], ["惩罚验证", data.punishment_active || 0], ["管理会话", data.active_sessions || 0], ["24h 操作失败", data.failed_admin_actions_24h || 0]]);
      var warningMetric = document.getElementById("security-summary").children[1];
      warningMetric.setAttribute("role", "button"); warningMetric.tabIndex = 0; warningMetric.title = "查看登录异常来源";
    }));
    if (view === "blocks" && Date.now() - blocksAt >= 10000) requests.push(w.read("/admin/api/security/blocks?page=" + blockPage + "&page_size=20&q=" + encodeURIComponent(query)).then(function (data) {
      if (token !== generation) return; blocksAt = Date.now(); blocks = data.blocks || [];
      w.pager("blocks-pager", data.pagination, function (next) { blockPage = next; invalidate(); }); render(); w.updated("security-updated");
    }));
    if (view === "audit" && Date.now() - auditAt >= 30000) requests.push(w.read("/admin/api/security/audit-events?page=" + auditPage + "&page_size=10").then(function (data) {
      if (token !== generation) return; auditAt = Date.now(); audits = data.events || [];
      w.pager("audit-pager", data.pagination, function (next) { auditPage = next; invalidate(); }); render(); w.updated("security-updated");
    }));
    if (view === "downloads") requests.push(history.refresh());
    return Promise.all(requests);
  }
  var refresh = w.poll(load, 1000);
  function invalidate() { generation++; blocksAt = 0; auditAt = 0; refresh(); }
  function deleteBlocks(items) {
    if (!items.length) return a.setStatus("请先选择封禁记录。");
    a.confirmAction("解除封禁", "确认解除 " + items.length + " 条封禁记录？", function () {
      a.api("/admin/api/security/blocks", {method: "DELETE", body: JSON.stringify({items: items})}).then(function (data) { a.setStatus(data.message || "封禁已解除"); summaryAt = 0; invalidate(); }).catch(function (err) { a.setStatus(err.message); });
    });
  }
  function createForm() {
    manual = true; selected = "";
    w.detail("添加手动封禁", '<button id="block-create" class="admin-primary">确认添加</button><button id="block-create-cancel" class="admin-secondary">取消</button>', '<div class="ws-manual-form">' +
      '<label class="admin-field"><span>封禁类型</span><select id="block-kind"><option value="client">公开下载客户端</option><option value="admin">管理登录来源</option></select></label>' +
      '<label class="admin-field"><span>IP / 前缀</span><input id="block-key" placeholder="单个 IP；下载客户端支持 IPv4 /24"></label><label class="admin-field"><span>原因</span><input id="block-reason" placeholder="例如：持续异常请求"></label><label class="admin-field"><span>时长</span><input id="block-duration" value="168h"></label></div>');
    w.open();
  }
  document.getElementById("workspace-refresh-now").onclick = function () { summaryAt = 0; invalidate(); if (view === "downloads") history.refresh(true).catch(function (err) { a.setStatus(err.message); }); };
  document.getElementById("blocks-search-button").onclick = function () { query = document.getElementById("blocks-search").value.trim(); blockPage = 1; selected = ""; a.text("blocks-search-scope", query ? "搜索 “" + query + "”" : "全部活跃封禁"); invalidate(); };
  document.getElementById("blocks-search-clear").onclick = function () { document.getElementById("blocks-search").value = ""; document.getElementById("blocks-search-button").click(); };
  document.getElementById("blocks-search").onkeydown = function (event) { if (event.key === "Enter") document.getElementById("blocks-search-button").click(); };
  document.getElementById("audit-search").oninput = render;
  document.getElementById("blocks-check-all").onchange = function () { var checked = this.checked; document.querySelectorAll("[data-block-select]").forEach(function (el) { el.checked = checked; }); updateChecks(); };
  document.getElementById("blocks-list").onchange = updateChecks;
  document.getElementById("blocks-delete-selected").onclick = function () { deleteBlocks(Array.from(document.querySelectorAll("[data-block-select]:checked")).map(function (el) { return {kind: el.getAttribute("data-kind"), key: el.getAttribute("data-key")}; })); };
  document.getElementById("block-create-open").onclick = createForm;
  document.getElementById("blocks-list").onkeydown = function (event) { if (event.target.hasAttribute("data-ws-key") && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); event.target.click(); } };
  document.addEventListener("click", function (event) {
    var tab = event.target.closest("[data-security-view]");
    if (tab) {
      view = tab.getAttribute("data-security-view"); w.setRefreshLabel(view === "blocks" ? "封禁刷新 · 10 秒" : view === "audit" ? "审计刷新 · 30 秒" : "下载记录 · 15 秒"); manual = false; selected = ""; generation++;
      document.querySelectorAll("[data-security-view]").forEach(function (t) { var active = t === tab; t.setAttribute("aria-selected", String(active)); document.getElementById("security-" + t.getAttribute("data-security-view")).hidden = !active; });
      a.text("security-list-title", view === "blocks" ? "当前封禁" : view === "audit" ? "最近管理活动" : "IP 内容查询"); render(); refresh();
    }
    var row = event.target.closest("#blocks-list [data-ws-key],#audit-list [data-ws-key]");
    if (row && !event.target.closest("input")) { manual = false; selected = row.getAttribute("data-ws-key"); render(); w.open(); }
    if (event.target.id === "block-delete-one") { var r = blocks.find(function (r) { return key(r) === selected; }); if (r) deleteBlocks([{kind: r.kind, key: r.key}]); }
    if (event.target.id === "block-create-cancel") { manual = false; show(); }
    if (event.target.id === "block-create") {
      var payload = {}; ["kind", "key", "reason", "duration"].forEach(function (name) { payload[name] = document.getElementById("block-" + name).value.trim(); });
      a.confirmAction("添加封禁", "确认添加这条封禁？", function () {
        a.api("/admin/api/security/blocks", {method: "POST", body: JSON.stringify(payload)}).then(function (data) { manual = false; show(); a.setStatus(data.message || "封禁已添加"); summaryAt = 0; invalidate(); }).catch(function (err) { a.setStatus(err.message); });
      });
    }
  });
  w.setRefreshLabel("封禁刷新 · 10 秒");
  document.getElementById("security-summary").onclick = function (event) {
    var metric = event.target.closest("article");
    if (!metric || metric !== this.children[1]) return;
    manual = true;
    var warnings = summary.login_warnings || [];
    w.detail("登录异常来源", "", warnings.length ? warnings.map(function (r) { return w.section(r.masked_ip || "未知来源", w.kv([["连续失败", r.failed_count || 0], ["最近失败", r.last_failed_at || "—"], ["距封禁阈值", r.remaining_before_block || 0]])); }).join("") : '<p class="ws-note">当前没有待处理的连续登录失败来源。</p>');
    w.open();
  };
  document.getElementById("security-summary").onkeydown = function (event) { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); event.target.click(); } };
  render();
})();
