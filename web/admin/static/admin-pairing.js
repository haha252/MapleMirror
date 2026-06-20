(function () {
  var a = window.admin;
  if (!a) return;

  function showPairingToast(rows) {
    var toast = document.getElementById("pairing-toast");
    var body = document.getElementById("pairing-toast-body");
    if (!toast || !body) return;
    var key = pairingKey(rows || []);
    if (!rows || !rows.length || sessionStorage.getItem("pairing-toast-dismissed") === key) {
      toast.hidden = true;
      return;
    }
    toast.setAttribute("data-pairing-key", key);
    body.innerHTML = rows.map(function (item) {
      var name = item.PublicName || item.public_name || "";
      var fp = item.Fingerprint || item.fingerprint || "";
      var id = item.ID || item.id;
      return '<section class="pairing-toast__item"><div><strong>' + a.esc(name || "未命名节点") +
        '</strong><span>' + a.esc(fp || "无指纹") + '</span><small>过期：' +
        a.esc(item.ExpiresAt || item.expires_at || "") + '</small></div><div class="admin-actions">' +
        '<button class="admin-secondary" data-pairing-action="approve" data-name="' +
        a.esc(name) + '" data-fingerprint="' + a.esc(fp) + '" data-id="' + a.esc(id) +
        '">批准</button><button class="admin-secondary" data-pairing-action="reject" data-id="' +
        a.esc(id) + '">拒绝</button></div></section>';
    }).join("");
    toast.hidden = false;
  }

  function loadPairingToast() {
    return a.api("/admin/api/pairing-requests")
      .then(function (data) { showPairingToast(data.requests || []); })
      .catch(function () {});
  }

  function pairingAction(button) {
    var id = button.getAttribute("data-id");
    var action = button.getAttribute("data-pairing-action");
    a.confirmAction("登记请求", "确认" + (action === "approve" ? "批准" : "拒绝") + "该登记请求？", function () {
      var payload = action === "approve" ? {
        confirmed_public_name: button.getAttribute("data-name"),
        confirmed_public_key_fingerprint: button.getAttribute("data-fingerprint")
      } : {};
      a.api("/admin/api/pairing-requests/" + encodeURIComponent(id) + "/" + action, {
        method: "POST", body: JSON.stringify(payload)
      }).then(function (data) {
        a.setStatus(data.message || "操作已完成");
        sessionStorage.removeItem("pairing-toast-dismissed");
        loadPairingToast();
      }).catch(function (err) { a.setStatus(err.message); });
    });
  }

  document.addEventListener("click", function (event) {
    var pairButton = event.target.closest("[data-pairing-action]");
    if (pairButton) return pairingAction(pairButton);
    if (event.target.closest("#pairing-toast-close")) {
      var toast = document.getElementById("pairing-toast");
      if (toast) {
        sessionStorage.setItem("pairing-toast-dismissed", toast.getAttribute("data-pairing-key") || "");
        toast.hidden = true;
      }
    }
  });

  function pairingKey(rows) {
    return rows.map(function (item) { return item.ID || item.id || ""; }).sort().join(",");
  }

  a.loadPairingToast = loadPairingToast;
  loadPairingToast();
  a.autoRefresh(loadPairingToast, 20000);
})();
