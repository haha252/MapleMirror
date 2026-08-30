(function () {
  var a = window.admin;
  if (!a || a.page() !== "security") return;
  var blockPage = 1, auditPage = 1, trafficPage = 1, currentAuth = "", trafficLoaded = false;

  function pager(el, page, total, size, cb) {
    var pages = Math.max(1, Math.ceil((total || 0) / size));
    el.innerHTML = '<button class="admin-secondary" data-page-prev>上一页</button>' +
      '<span>第 ' + page + ' / ' + pages + ' 页，共 ' + (total || 0) + ' 条</span>' +
      '<button class="admin-secondary" data-page-next>下一页</button>';
    el.querySelector("[data-page-prev]").disabled = page <= 1;
    el.querySelector("[data-page-next]").disabled = page >= pages;
    el.querySelector("[data-page-prev]").onclick = function () { cb(page - 1); };
    el.querySelector("[data-page-next]").onclick = function () { cb(page + 1); };
  }

  function renderSummary(data) {
    var attention = Number(data.attention_count || 0);
    var title = attention ? "有 " + attention + " 项需要关注" : "当前状态正常";
    var note = attention ? "下方只列出真正需要处理的安全状态。" : "没有发现需要人工处理的安全异常。";
    document.getElementById("security-summary").innerHTML =
      '<div class="security-summary__status">' + a.badge(attention ? "需要关注" : "正常") +
      '<strong>' + a.esc(title) + '</strong><span class="muted">' + a.esc(note) + '</span></div>' +
      '<div class="security-summary__facts">' +
      fact("活跃封禁", data.active_blocks || 0) + fact("登录异常来源", data.login_failure_sources || 0) +
      fact("惩罚验证", data.punishment_active || 0) + fact("有效管理会话", data.active_sessions || 0) +
      fact("24h 管理操作失败", data.failed_admin_actions_24h || 0) + '</div>';
    renderAttention(data);
  }

  function fact(label, value) {
    return '<span class="security-summary__fact">' + a.esc(label) + '<strong>' + a.esc(value) + '</strong></span>';
  }

  function renderAttention(data) {
    var list = document.getElementById("security-attention");
    var items = [];
    (data.login_warnings || []).forEach(function (item) {
      var remain = Number(item.remaining_before_block || 0);
      items.push('<article class="admin-record"><div class="admin-record__summary">' +
        '<div class="admin-record__identity"><strong>管理登录连续失败</strong><span class="sub">' +
        a.esc(item.masked_ip || "未知来源") + ' · 最近 ' + a.esc(item.last_failed_at || "暂无") +
        '</span></div><div class="admin-record__state">' + a.badge("需要关注") +
        '<span class="admin-record__meta">已失败 ' + a.esc(item.failed_count || 0) +
        ' 次' + (remain ? ' · 再 ' + a.esc(remain) + ' 次将封禁' : ' · 已达到封禁阈值') +
        '</span></div></div></article>');
    });
    if (Number(data.punishment_active || 0) > 0) {
      items.push('<article class="admin-record"><div class="admin-record__summary">' +
        '<div class="admin-record__identity"><strong>下载来源正在执行惩罚验证</strong>' +
        '<span class="sub">这是已经触发防滥用升级的来源，不是普通访问。</span></div>' +
        '<div class="admin-record__state">' + a.badge("需要关注") +
        '<span class="admin-record__meta">' + a.esc(data.punishment_active) + ' 个来源</span></div></div></article>');
    }
    list.innerHTML = items.length ? items.join("") :
      '<div class="security-attention-empty">暂无需要处理的问题。正常封禁和普通管理活动不会被当作异常。</div>';
  }

  function loadSummary() {
    return a.api("/admin/api/security/summary").then(renderSummary)
      .catch(function (err) { a.setStatus(err.message); });
  }

  function blockReason(item) {
    if (item.reason === "admin_login_failed") return "登录失败自动封禁";
    return item.reason || (item.source === "manual" ? "手动封禁" : item.source) || "未填写原因";
  }

  function blockCard(item) {
    var type = item.kind === "admin" ? "管理登录" : "公开下载";
    var status = item.punishment_active ? "惩罚验证中" :
      (Number(item.escalation_level || 0) > 0 ? "升级 L" + item.escalation_level : "普通封禁");
    return '<article class="admin-record"><div class="admin-record__summary">' +
      '<div class="admin-record__identity"><div class="security-record-select"><input type="checkbox" data-block-select data-kind="' +
      a.esc(item.kind) + '" data-key="' + a.esc(item.key) + '"><strong>' +
      a.esc(item.display_ip || item.masked_ip || item.key) + '</strong></div><span class="sub">' +
      a.esc(type + " · " + blockReason(item)) + '</span></div><div class="admin-record__state">' +
      a.badge(status) + '<span class="admin-record__meta">到期 ' + a.esc(item.expires_at || "未知") +
      '</span></div><div class="admin-record__actions"><button class="admin-secondary" data-block-detail>查看详情</button>' +
      '<button class="admin-secondary" data-block-kind="' + a.esc(item.kind) + '" data-block-key="' +
      a.esc(item.key) + '">解除</button></div></div><div class="admin-record__detail" data-block-detail-box hidden>' +
      '<div class="admin-record__detail-grid">' +
      detail("封禁时间", item.blocked_at || "—") + detail("最后尝试", item.last_attempt_at || "暂无") +
      detail("封禁后尝试", item.attempts_after_block || 0) + detail("升级等级", "L" + (item.escalation_level || 0)) +
      '</div><details class="admin-tech-details"><summary>显示技术信息</summary>' +
      a.compactKv({"类型": item.kind, "匹配键": item.key, "来源": item.source || ""}, "detail-plain") +
      '</details></div></article>';
  }

  function detail(label, value) {
    return '<div class="admin-record__detail-item"><span>' + a.esc(label) +
      '</span><strong>' + a.esc(value) + '</strong></div>';
  }

  function loadBlocks(page) {
    blockPage = page || blockPage;
    return a.api("/admin/api/security/blocks?page=" + blockPage + "&page_size=20").then(function (data) {
      var rows = data.blocks || [];
      document.getElementById("blocks-list").innerHTML = rows.length ? rows.map(blockCard).join("") :
        '<div class="admin-record muted">暂无活跃封禁</div>';
      document.getElementById("blocks-check-all").checked = false;
      pager(document.getElementById("blocks-pager"), data.pagination.page, data.pagination.total,
        data.pagination.page_size, loadBlocks);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function createBlock() {
    var payload = {
      kind: document.getElementById("block-kind").value,
      key: document.getElementById("block-key").value.trim(),
      reason: document.getElementById("block-reason").value.trim(),
      duration: document.getElementById("block-duration").value.trim()
    };
    a.confirmAction("添加封禁", "确认添加这条封禁？", function () {
      a.api("/admin/api/security/blocks", {method: "POST", body: JSON.stringify(payload)})
        .then(function (data) { a.setStatus(data.message || "封禁已添加"); refreshSecurity(); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  function selectedBlocks() {
    return Array.prototype.slice.call(document.querySelectorAll("[data-block-select]:checked")).map(function (el) {
      return {kind: el.getAttribute("data-kind"), key: el.getAttribute("data-key")};
    });
  }

  function deleteBlocks(items) {
    if (!items.length) return a.setStatus("请先选择封禁记录。");
    a.confirmAction("解除封禁", "确认解除 " + items.length + " 条封禁记录？", function () {
      a.api("/admin/api/security/blocks", {method: "DELETE", body: JSON.stringify({items: items})})
        .then(function (data) { a.setStatus(data.message || "封禁已解除"); refreshSecurity(); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  function auditCard(item) {
    var name = item.target_name || item.target_id || "系统";
    return '<article class="admin-record"><div class="admin-record__summary">' +
      '<div class="admin-record__identity"><strong>' + a.esc(a.auditOperationLabel(item.operation)) +
      '</strong><span class="sub">' + a.esc(name) + '</span></div><div class="admin-record__state">' +
      a.badge(a.resultLabel(item.result)) + '<span class="admin-record__meta">' +
      a.esc(item.created_at || "") + '</span></div><div class="admin-record__actions">' +
      '<button class="admin-secondary" data-audit-detail>查看详情</button></div></div>' +
      '<div class="admin-record__detail" data-audit-detail-box hidden><p class="admin-record__message">' +
      a.esc(item.details_summary || "没有补充说明") + '</p><details class="admin-tech-details"><summary>显示技术信息</summary>' +
      a.compactKv({"操作": item.operation, "目标类型": item.target_type, "目标 ID": item.target_id,
        "Request ID": item.request_id || ""}, "detail-plain") + '</details></div></article>';
  }

  function loadAudit(page) {
    auditPage = page || auditPage;
    return a.api("/admin/api/security/audit-events?page=" + auditPage + "&page_size=10").then(function (data) {
      var rows = data.events || [];
      document.getElementById("audit-list").innerHTML = rows.length ? rows.map(auditCard).join("") :
        '<div class="admin-record muted">暂无管理活动</div>';
      pager(document.getElementById("audit-pager"), data.pagination.page, data.pagination.total,
        data.pagination.page_size, loadAudit);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function loadTraffic(page) {
    trafficPage = page || trafficPage;
    var query = currentAuth ? "?authorization_id=" + encodeURIComponent(currentAuth) + "&" : "?";
    a.text("traffic-scope", currentAuth ? "正在查看指定授权" : "最近事件");
    trafficLoaded = true;
    return a.api("/admin/api/traffic/events" + query + "page=" + trafficPage + "&page_size=20").then(function (data) {
      var rows = data.events || [];
      document.getElementById("traffic-events").innerHTML =
        '<table class="admin-responsive-table"><thead><tr><th>授权</th><th>节点</th><th>序号</th><th>字节</th><th>状态</th><th>入账</th></tr></thead><tbody>' +
        (rows.length ? rows.map(function (item) {
          return '<tr><td data-label="授权">' + a.esc(item.authorization_id) + '</td><td data-label="节点">' +
            a.esc(item.node_id) + '</td><td data-label="序号">' + a.esc(item.event_sequence) +
            '</td><td data-label="字节">' + a.esc(a.bytes(item.sent_bytes)) + '</td><td data-label="状态">' +
            a.badge(item.status) + '</td><td data-label="入账">' + a.esc(item.accounted_at || "未入账") + '</td></tr>';
        }).join("") : '<tr><td data-label="" colspan="6" class="muted">暂无流量事件</td></tr>') + '</tbody></table>';
      pager(document.getElementById("traffic-pager"), data.pagination.page, data.pagination.total,
        data.pagination.page_size, loadTraffic);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function queryAuthorization() {
    currentAuth = document.getElementById("authorization-id").value.trim();
    if (!currentAuth) return a.setStatus("请输入授权 ID。");
    loadTraffic(1);
  }

  function refreshSecurity() {
    loadSummary(); loadBlocks(blockPage); loadAudit(auditPage);
    if (trafficLoaded) loadTraffic(trafficPage);
  }

  document.addEventListener("click", function (event) {
    var block = event.target.closest("[data-block-kind]");
    if (block) return deleteBlocks([{kind: block.getAttribute("data-block-kind"), key: block.getAttribute("data-block-key")}]);
    var detailButton = event.target.closest("[data-block-detail], [data-audit-detail]");
    if (detailButton) {
      var card = detailButton.closest(".admin-record");
      var box = card.querySelector("[data-block-detail-box], [data-audit-detail-box]");
      box.hidden = !box.hidden;
      detailButton.textContent = box.hidden ? "查看详情" : "收起";
    }
  });
  document.getElementById("blocks-check-all").addEventListener("change", function (event) {
    document.querySelectorAll("[data-block-select]").forEach(function (el) { el.checked = event.target.checked; });
  });
  document.getElementById("blocks-delete-selected").addEventListener("click", function () { deleteBlocks(selectedBlocks()); });
  document.getElementById("block-create").addEventListener("click", createBlock);
  document.getElementById("security-refresh").addEventListener("click", refreshSecurity);
  document.getElementById("authorization-query").addEventListener("click", queryAuthorization);
  document.getElementById("authorization-clear").addEventListener("click", function () {
    currentAuth = ""; document.getElementById("authorization-id").value = ""; loadTraffic(1);
  });
  document.getElementById("authorization-id").addEventListener("keydown", function (event) {
    if (event.key === "Enter") queryAuthorization();
  });
  document.querySelector(".security-advanced").addEventListener("toggle", function () {
    if (this.open && !trafficLoaded) loadTraffic(1);
  });
  loadSummary(); loadBlocks(1); loadAudit(1);
  a.autoRefresh(function () { loadSummary(); loadBlocks(blockPage); loadAudit(auditPage); }, 30000);
})();
