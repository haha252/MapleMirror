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

  function bandwidth(value) {
    return bytes(value) + "/s";
  }

  function nodePressure(node) {
    var pressure = node && node.pressure ? node.pressure : {};
    var actual = Number(pressure.actual_bandwidth_bps || 0);
    var target = Number(pressure.target_bandwidth_bps || node.target_bandwidth_bps || 0);
    var ratio = pressure.pressure_ratio;
    if (ratio == null && target > 0) ratio = actual / target;
    return {
      actual: actual,
      target: target,
      ratio: ratio == null ? null : Number(ratio)
    };
  }

  function bandwidthText(node) {
    var p = nodePressure(node || {});
    if (!p.target && !p.actual) return "暂无采样";
    return bandwidth(p.actual) + " / " + bandwidth(p.target);
  }

  function pressureText(node) {
    var ratio = nodePressure(node || {}).ratio;
    if (ratio == null || !isFinite(ratio)) return "暂无";
    return Math.round(ratio * 100) + "%";
  }

  function pressureMeter(node) {
    var ratio = nodePressure(node || {}).ratio;
    var pct = ratio == null || !isFinite(ratio) ? 0 : Math.max(0, Math.min(100, ratio * 100));
    var level = pct >= 90 ? "danger" : pct >= 70 ? "warn" : "ok";
    return '<div class="pressure-meter pressure-meter--' + level +
      '"><span style="width:' + pct.toFixed(0) + '%"></span><strong>' +
      esc(pressureText(node)) + "</strong></div>";
  }

  function badge(value) {
    var label = value || "未知";
    return '<span class="admin-badge ' + badgeClass(label) + '">' + esc(label) + "</span>";
  }

  function badgeClass(value) {
    var state = String(value || "").toLowerCase();
    if (/在线|就绪|启用|成功|正常|完成|同步中|扫描中|对账中|success|ready|online|syncing|succeeded/.test(state)) return "admin-badge--ok";
    if (/失败|异常|离线|拒绝|删除|failed|rejected|mismatch/.test(state)) return "admin-badge--bad";
    if (/等待|重试|关注|惩罚|未|暂无|pending|retry|unknown/.test(state)) return "admin-badge--warn";
    return "admin-badge--neutral";
  }

  function api(path, options) {
    options = options || {};
    options.credentials = "same-origin";
    options.headers = options.headers || {};
    if (options.body && !options.headers) {
      options.headers = { "Content-Type": "application/json" };
    }
    if (options.body && !options.headers["Content-Type"]) {
      options.headers["Content-Type"] = "application/json";
    }
    if (writeMethod(options.method)) {
      options.headers["X-CSRF-Token"] = csrfToken();
    }
    return fetch(path, options).then(function (res) {
      return res.json().then(function (body) {
        if (!res.ok) throw new Error(body.message || "操作失败");
        return body;
      });
    });
  }

  function csrfToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute("content") || "" : "";
  }

  function writeMethod(method) {
    method = String(method || "GET").toUpperCase();
    return method !== "GET" && method !== "HEAD" && method !== "OPTIONS";
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

  function infoDialog(title, body, copyText) {
    var modal = document.getElementById("admin-info-modal");
    var close = document.getElementById("admin-info-close");
    var copy = document.getElementById("admin-info-copy");
    var extra = document.getElementById("admin-info-extra");
    if (!modal || !close || !copy || !extra) return;
    document.getElementById("admin-info-title").textContent = title;
    document.getElementById("admin-info-body").textContent = body || "";
    extra.innerHTML = copyText ? '<code class="admin-copy-value">' + esc(copyText) + "</code>" : "";
    copy.hidden = !copyText;
    modal.hidden = false;
    close.onclick = function () { modal.hidden = true; };
    copy.onclick = function () {
      navigator.clipboard.writeText(copyText).then(function () {
        setStatus("已复制到剪贴板");
      }).catch(function () {
        setStatus("复制失败，请手动选择配对码");
      });
    };
  }

  function kv(data) {
    return Object.keys(data || {}).map(function (key) {
      return '<div class="detail-row"><span>' + esc(key) + '</span><strong>' +
        esc(data[key]) + "</strong></div>";
    }).join("");
  }

  function compactKv(data, className) {
    var classes = "detail-compact";
    if (className) classes += " " + className;
    return '<div class="' + classes + '">' + Object.keys(data || {}).map(function (key) {
      return '<div><span>' + esc(key) + '</span><strong>' +
        esc(data[key]) + "</strong></div>";
    }).join("") + "</div>";
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

  function autoRefresh(fn, interval) {
    var stopped = false;
    function run() {
      if (stopped || document.hidden) return;
      fn();
    }
    var timer = window.setInterval(run, interval || 15000);
    document.addEventListener("visibilitychange", run);
    return function () {
      stopped = true;
      window.clearInterval(timer);
    };
  }

  document.querySelectorAll("[data-nav]").forEach(function (link) {
    var page = currentPage() === "project-edit" ? "projects" : currentPage();
    page = page === "node-management" ? "nodes" : page;
    if (link.getAttribute("data-nav") === page) {
      link.setAttribute("aria-current", "page");
    } else {
      link.removeAttribute("aria-current");
    }
  });

  window.admin = {
    api: api,
    autoRefresh: autoRefresh,
    badge: badge,
    bandwidthText: bandwidthText,
    bytes: bytes,
    confirmAction: confirmAction,
    compactKv: compactKv,
    connectionLabel: connectionLabel,
    esc: esc,
    infoDialog: infoDialog,
    kv: kv,
    page: currentPage,
    pressureMeter: pressureMeter,
    pressureText: pressureText,
    setStatus: setStatus,
    text: text
  };
})();
