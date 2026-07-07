(function () {
  var a = window.admin;
  if (!a || a.page() !== "security") return;
  var blockPage = 1, trafficPage = 1, auditPage = 1, currentAuth = "";

  function pager(el, page, total, size, cb) {
    var pages = Math.max(1, Math.ceil((total || 0) / size));
    el.innerHTML = '<button class="admin-secondary" type="button" data-page-prev>上一页</button>' +
      '<span>第 ' + page + ' / ' + pages + ' 页，共 ' + (total || 0) + ' 条</span>' +
      '<button class="admin-secondary" type="button" data-page-next>下一页</button>';
    el.querySelector("[data-page-prev]").disabled = page <= 1;
    el.querySelector("[data-page-next]").disabled = page >= pages;
    el.querySelector("[data-page-prev]").onclick = function () { cb(page - 1); };
    el.querySelector("[data-page-next]").onclick = function () { cb(page + 1); };
  }

  function loadSecurity(page) {
    blockPage = page || blockPage;
    return a.api("/admin/api/security/blocks?page=" + blockPage + "&page_size=20").then(function (data) {
      var body = document.getElementById("blocks-body");
      var rows = data.blocks || [];
      if (!rows.length) {
        body.innerHTML = '<tr><td colspan="8" class="muted">暂无封禁记录</td></tr>';
      } else {
        body.innerHTML = rows.map(function (item) {
          return '<tr><td><input type="checkbox" data-block-select data-kind="' + a.esc(item.kind) + '" data-key="' + a.esc(item.key) + '"></td><td>' +
            a.esc(item.kind === "admin" ? "管理登录" : "公开下载") + "</td><td>" +
            a.esc(item.display_ip || item.masked_ip || item.key) + "</td><td>" + a.esc(item.reason || item.source || "") +
            "</td><td>" + a.esc(String(item.attempts_after_block || 0) + " / L" + String(item.escalation_level || 0)) +
            "</td><td>" + a.esc(item.punishment_active ? "惩罚验证" : "普通封禁") +
            "</td><td>" + a.esc(item.expires_at || "") +
            '</td><td><button class="admin-secondary" data-block-kind="' + a.esc(item.kind) +
            '" data-block-key="' + a.esc(item.key) + '">解除</button></td></tr>';
        }).join("");
      }
      document.getElementById("blocks-check-all").checked = false;
      pager(document.getElementById("blocks-pager"), data.pagination.page, data.pagination.total, data.pagination.page_size, loadSecurity);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function createBlock() {
    var payload = {
      kind: document.getElementById("block-kind").value,
      key: document.getElementById("block-key").value.trim(),
      reason: document.getElementById("block-reason").value.trim(),
      duration: document.getElementById("block-duration").value.trim()
    };
    a.confirmAction("添加封禁", "确认添加该封禁记录？", function () {
      a.api("/admin/api/security/blocks", { method: "POST", body: JSON.stringify(payload) })
        .then(function (data) { a.setStatus(data.message || "封禁已添加"); loadSecurity(1); loadAudit(auditPage); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  function selectedBlocks() {
    return Array.prototype.slice.call(document.querySelectorAll("[data-block-select]:checked")).map(function (el) {
      return { kind: el.getAttribute("data-kind"), key: el.getAttribute("data-key") };
    });
  }

  function deleteBlocks(items) {
    if (!items.length) return a.setStatus("请先选择封禁记录。");
    a.confirmAction("解除封禁", "确认解除选中的 " + items.length + " 条封禁记录？", function () {
      a.api("/admin/api/security/blocks", { method: "DELETE", body: JSON.stringify({ items: items }) })
        .then(function (data) { a.setStatus(data.message || "封禁已解除"); loadSecurity(blockPage); loadAudit(auditPage); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  function loadAudit(page) {
    auditPage = page || auditPage;
    return a.api("/admin/api/security/audit-events?page=" + auditPage + "&page_size=20").then(function (data) {
      var body = document.getElementById("audit-body");
      var rows = data.events || [];
      body.innerHTML = rows.length ? rows.map(function (item) {
        return "<tr><td>" + a.esc(item.operation) + "</td><td>" +
          a.esc(item.target_type + "/" + item.target_id) + "</td><td>" +
          a.badge(item.result) + "</td><td>" + a.esc(item.created_at) + "</td></tr>";
      }).join("") : '<tr><td colspan="4" class="muted">暂无审计事件</td></tr>';
      pager(document.getElementById("audit-pager"), data.pagination.page, data.pagination.total, data.pagination.page_size, loadAudit);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function queryAuthorization() {
    currentAuth = document.getElementById("authorization-id").value.trim();
    if (!currentAuth) return a.setStatus("authorization_id 不能为空。");
    a.api("/admin/api/authorizations/" + encodeURIComponent(currentAuth)).then(function (data) {
      a.setStatus("授权 " + data.authorization_id + "：节点 " + data.node_id + "，已入账 " + a.bytes(data.sent_bytes));
      loadTraffic(1);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function clearAuthorization() {
    currentAuth = "";
    document.getElementById("authorization-id").value = "";
    loadTraffic(1);
  }

  function loadTraffic(page) {
    trafficPage = page || trafficPage;
    var query = currentAuth ? "?authorization_id=" + encodeURIComponent(currentAuth) + "&" : "?";
    a.text("traffic-scope", currentAuth ? "授权 " + currentAuth : "最近事件");
    a.api("/admin/api/traffic/events" + query + "page=" + trafficPage + "&page_size=20").then(function (data) {
      var rows = data.events || [];
      var box = document.getElementById("traffic-events");
      box.innerHTML = '<table><thead><tr><th>授权</th><th>节点</th><th>序号</th><th>字节</th><th>状态</th><th>入账</th></tr></thead><tbody>' +
        (rows.length ? 
        rows.map(function (item) {
          return "<tr><td>" + a.esc(item.authorization_id) + "</td><td>" + a.esc(item.node_id) + "</td><td>" + a.esc(item.event_sequence) +
            "</td><td>" + a.esc(a.bytes(item.sent_bytes)) + "</td><td>" + a.badge(item.status) +
            "</td><td>" + a.esc(item.accounted_at || "未入账") + "</td></tr>";
        }).join("") : '<tr><td colspan="6" class="muted">暂无流量事件</td></tr>') + "</tbody></table>";
      pager(document.getElementById("traffic-pager"), data.pagination.page, data.pagination.total, data.pagination.page_size, loadTraffic);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  document.addEventListener("click", function (event) {
    var block = event.target.closest("[data-block-kind]");
    if (block) deleteBlocks([{ kind: block.getAttribute("data-block-kind"), key: block.getAttribute("data-block-key") }]);
  });
  document.getElementById("blocks-check-all").addEventListener("change", function (event) {
    document.querySelectorAll("[data-block-select]").forEach(function (el) { el.checked = event.target.checked; });
  });
  document.getElementById("blocks-delete-selected").addEventListener("click", function () { deleteBlocks(selectedBlocks()); });
  document.getElementById("block-create").addEventListener("click", createBlock);
  document.getElementById("security-refresh").addEventListener("click", function () { loadSecurity(blockPage); loadAudit(auditPage); });
  document.getElementById("authorization-query").addEventListener("click", queryAuthorization);
  document.getElementById("authorization-clear").addEventListener("click", clearAuthorization);
  document.getElementById("authorization-id").addEventListener("keydown", function (event) {
    if (event.key === "Enter") queryAuthorization();
  });
  loadSecurity(1);
  loadAudit(1);
  loadTraffic(1);
  a.autoRefresh(function () { loadSecurity(blockPage); loadAudit(auditPage); }, 30000);
  a.autoRefresh(function () { loadTraffic(trafficPage); }, 15000);
})();
