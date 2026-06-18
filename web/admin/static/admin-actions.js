(function () {
  function status(text) {
    if (window.adminSetStatus) window.adminSetStatus(text);
  }

  function api(path, options) {
    options = options || {};
    options.credentials = "same-origin";
    if (options.body && !options.headers) options.headers = { "Content-Type": "application/json" };
    return fetch(path, options).then(function (res) {
      return res.json().then(function (body) {
        if (!res.ok) throw new Error(body.message || "操作失败");
        return body;
      });
    });
  }

  function confirmAction(title, body, run) {
    var modal = document.getElementById("admin-modal");
    document.getElementById("admin-modal-title").textContent = title;
    document.getElementById("admin-modal-body").textContent = body;
    modal.hidden = false;
    var ok = document.getElementById("admin-modal-confirm");
    var cancel = document.getElementById("admin-modal-cancel");
    function close() {
      modal.hidden = true;
      ok.onclick = null;
      cancel.onclick = null;
    }
    cancel.onclick = close;
    ok.onclick = function () {
      close();
      run();
    };
  }

  function loadPairing() {
    return api("/admin/api/pairing-requests").then(function (data) {
      var body = document.getElementById("pairing-body");
      if (!body) return;
      body.innerHTML = (data.requests || []).map(function (item) {
        return "<tr><td>" + esc(item.PublicName || item.public_name || "") + "</td><td>" +
          esc(item.Fingerprint || item.fingerprint || "") + "</td><td>" +
          esc(item.ExpiresAt || item.expires_at || "") + '</td><td><div class="admin-actions">' +
          '<button class="admin-secondary" data-pairing-action="approve" data-name="' + esc(item.PublicName || item.public_name || "") +
          '" data-fingerprint="' + esc(item.Fingerprint || item.fingerprint || "") + '" data-id="' + esc(item.ID || item.id) + '">批准</button>' +
          '<button class="admin-secondary" data-pairing-action="reject" data-id="' + esc(item.ID || item.id) + '">拒绝</button></div></td></tr>';
      }).join("");
    });
  }

  function loadSecurity() {
    return api("/admin/api/security/blocks").then(function (data) {
      var body = document.getElementById("blocks-body");
      if (!body) return;
      var rows = [];
      (data.admin_blocks || []).forEach(function (item) { rows.push(["admin", item]); });
      (data.client_blocks || []).forEach(function (item) { rows.push(["client", item]); });
      body.innerHTML = rows.map(function (row) {
        var kind = row[0], item = row[1];
        return "<tr><td>" + esc(item.display_ip || item.masked_ip || item.key) + "</td><td>" +
          esc(item.reason || "") + "</td><td>" + esc(item.expires_at || "") +
          '</td><td><button class="admin-secondary" data-block-kind="' + kind +
          '" data-block-key="' + esc(item.key) + '">解除</button></td></tr>';
      }).join("");
    });
  }

  function esc(value) {
    return String(value == null ? "" : value).replace(/&/g, "&amp;")
      .replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function refreshAll() {
    loadPairing().catch(function (err) { status(err.message); });
    loadSecurity().catch(function (err) { status(err.message); });
  }
  window.adminLoadActions = refreshAll;

  document.addEventListener("click", function (event) {
    var nodeButton = event.target.closest("[data-node-action]");
    if (nodeButton) return nodeAction(nodeButton);
    var pairButton = event.target.closest("[data-pairing-action]");
    if (pairButton) return pairingAction(pairButton);
    var blockButton = event.target.closest("[data-block-kind]");
    if (blockButton) return unblockAction(blockButton);
  });

  function nodeAction(button) {
    var node = button.getAttribute("data-node");
    var action = button.getAttribute("data-node-action");
    if (action === "sync-status") {
      api("/admin/api/nodes/" + encodeURIComponent(node) + "/sync-status").then(function (data) {
        status("同步诊断：" + JSON.stringify(data));
      }).catch(function (err) { status(err.message); });
      return;
    }
    confirmAction("节点操作", "确认对节点 " + node + " 执行 " + action + "？", function () {
      api("/admin/api/nodes/" + encodeURIComponent(node) + "/" + action, { method: "POST", body: "{}" })
        .then(function (data) { status(data.message || "操作已完成"); location.reload(); })
        .catch(function (err) { status(err.message); });
    });
  }

  function pairingAction(button) {
    var id = button.getAttribute("data-id");
    var action = button.getAttribute("data-pairing-action");
    confirmAction("登记请求", "确认" + (action === "approve" ? "批准" : "拒绝") + "该登记请求？", function () {
      var payload = action === "approve" ? {
        confirmed_public_name: button.getAttribute("data-name"),
        confirmed_public_key_fingerprint: button.getAttribute("data-fingerprint")
      } : {};
      api("/admin/api/pairing-requests/" + encodeURIComponent(id) + "/" + action, {
        method: "POST", body: JSON.stringify(payload)
      }).then(function (data) { status(data.message || "操作已完成"); refreshAll(); })
        .catch(function (err) { status(err.message); });
    });
  }

  function unblockAction(button) {
    var kind = button.getAttribute("data-block-kind");
    var key = button.getAttribute("data-block-key");
    confirmAction("解除封禁", "确认解除该封禁记录？", function () {
      api("/admin/api/security/blocks/" + kind + "/" + encodeURIComponent(key), { method: "DELETE" })
        .then(function (data) { status(data.message || "封禁已解除"); refreshAll(); })
        .catch(function (err) { status(err.message); });
    });
  }

  document.getElementById("pairing-create").addEventListener("click", function () {
    confirmAction("创建配对码", "确认创建一个 5 分钟有效的一次性配对码？", function () {
      api("/admin/api/pairing-codes", { method: "POST", body: JSON.stringify({ ttl_seconds: 300 }) })
        .then(function (data) {
          var box = document.getElementById("pairing-code");
          box.hidden = false;
          box.textContent = "配对码：" + data.pairing_code + "，过期时间：" + data.expires_at;
        }).catch(function (err) { status(err.message); });
    });
  });

  document.getElementById("scan-all").addEventListener("click", function () {
    confirmAction("触发扫描", "确认触发所有启用项目扫描？", function () {
      api("/admin/api/sync/scans", { method: "POST", body: JSON.stringify({}) })
        .then(function (data) { status(data.message || "扫描已创建"); })
        .catch(function (err) { status(err.message); });
    });
  });
  document.getElementById("security-refresh").addEventListener("click", refreshAll);
})();
