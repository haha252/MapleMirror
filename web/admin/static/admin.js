(function () {
  var status = document.getElementById("admin-status");

  function setStatus(text) {
    if (!status) return;
    status.textContent = text || "";
    status.hidden = !text;
  }

  function esc(value) {
    return String(value == null ? "" : value)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function text(id, value) {
    var el = document.getElementById(id);
    if (el) el.textContent = value == null ? "" : value;
  }

  function bytes(value) {
    var units = ["B", "KiB", "MiB", "GiB", "TiB"];
    var n = Number(value) || 0;
    var index = 0;
    while (n >= 1024 && index < units.length - 1) {
      n = n / 1024;
      index++;
    }
    return (index === 0 ? String(n) : n.toFixed(1)) + " " + units[index];
  }

  function badge(value) {
    return '<span class="admin-badge">' + esc(value || "未知") + "</span>";
  }

  function api(path, options) {
    options = options || {};
    options.credentials = "same-origin";
    if (options.body && !options.headers) {
      options.headers = { "Content-Type": "application/json" };
    }
    return fetch(path, options).then(function (res) {
      return res.json().then(function (body) {
        if (!res.ok) throw new Error(body.message || "操作失败");
        return body;
      });
    });
  }

  function confirmAction(title, body, run) {
    var modal = document.getElementById("admin-modal");
    var ok = document.getElementById("admin-modal-confirm");
    var cancel = document.getElementById("admin-modal-cancel");
    if (!modal || !ok || !cancel) return run();
    document.getElementById("admin-modal-title").textContent = title;
    document.getElementById("admin-modal-body").textContent = body;
    modal.hidden = false;
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

  function kv(data) {
    return Object.keys(data || {}).map(function (key) {
      return '<div class="detail-row"><span>' + esc(key) + '</span><strong>' +
        esc(data[key]) + "</strong></div>";
    }).join("");
  }

  function connectionLabel(value) {
    var state = String(value || "").toLowerCase();
    if (state === "online" || state === "syncing" || state === "ready") return "在线";
    if (state === "offline") return "离线";
    if (state === "disabled") return "已禁用";
    return value || "未知";
  }

  function currentPage() {
    return document.body.getAttribute("data-admin-page") || "overview";
  }

  document.querySelectorAll("[data-nav]").forEach(function (link) {
    var page = currentPage() === "project-edit" ? "projects" : currentPage();
    if (link.getAttribute("data-nav") === page) {
      link.setAttribute("aria-current", "page");
    } else {
      link.removeAttribute("aria-current");
    }
  });

  window.admin = {
    api: api,
    badge: badge,
    bytes: bytes,
    confirmAction: confirmAction,
    connectionLabel: connectionLabel,
    esc: esc,
    kv: kv,
    page: currentPage,
    setStatus: setStatus,
    text: text
  };
})();
