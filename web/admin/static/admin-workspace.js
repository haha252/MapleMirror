(function () {
  var a = window.admin;
  if (!a) return;
  if (document.getElementById("workspace-editor")) {
    document.body.setAttribute("data-workspace", "true");
    var editorOffset = function () { document.documentElement.style.setProperty("--workspace-top", Math.ceil(document.querySelector(".site-header").getBoundingClientRect().bottom + 14) + "px"); };
    if (window.ResizeObserver) new ResizeObserver(editorOffset).observe(document.querySelector(".site-header"));
    window.addEventListener("resize", editorOffset); editorOffset(); return;
  }
  if (!document.getElementById("workspace-detail-panel")) return;
  document.body.setAttribute("data-workspace", "true");
  var panel = document.getElementById("workspace-detail-panel"), backdrop = document.getElementById("workspace-backdrop");
  var media = window.matchMedia("(max-width: 760px)"), origin, blocked = [], paused = false, refreshLabel = "状态刷新 · 1 秒";
  function html(id, value) {
    var el = document.getElementById(id);
    if (!el || el._wsMarkup === value) return;
    var opened = Array.from(el.querySelectorAll("details")).map(function (d) { return d.open; });
    el.innerHTML = value; el._wsMarkup = value;
    el.querySelectorAll("details").forEach(function (d, i) { d.open = !!opened[i]; });
  }
  function list(id, rows, key, markup, selected, checkboxes) {
    var el = document.getElementById(id), keep = Object.create(null);
    el.querySelectorAll("[data-ws-key]").forEach(function (row) { keep[row.getAttribute("data-ws-key")] = row; });
    el.querySelectorAll(".ws-empty").forEach(function (p) { p.remove(); });
    rows.forEach(function (item) {
      var value = String(key(item)), row = keep[value];
      if (!row) { row = document.createElement(checkboxes ? "div" : "button"); if (checkboxes) { row.tabIndex = 0; row.setAttribute("role", "button"); } else row.type = "button"; row.className = checkboxes ? "ws-row ws-block-row" : "ws-row"; row.setAttribute("data-ws-key", value); el.appendChild(row); }
      var content = markup(item);
      if (row._wsMarkup !== content) { var checked = row.querySelector("input:checked"); row.innerHTML = content; if (checked && row.querySelector("input")) row.querySelector("input").checked = true; row._wsMarkup = content; }
      row.setAttribute("aria-pressed", String(value === selected)); delete keep[value];
    });
    Object.keys(keep).forEach(function (k) { keep[k].remove(); });
    if (!rows.length) el.innerHTML = '<div class="ws-empty">没有匹配的记录</div>';
  }
  function metrics(id, rows) {
    html(id, rows.map(function (r) { return '<article class="panel-card ws-metric"><span>' + a.esc(r[0]) + '</span><strong>' + a.esc(r[1]) + '</strong></article>'; }).join(""));
  }
  function pager(id, data, change) {
    var el = document.getElementById(id), pages = Math.max(1, Math.ceil(data.total / data.page_size));
    html(id, '<button class="admin-secondary" data-prev' + (data.page <= 1 ? ' disabled' : '') + '>上一页</button><span>第 ' + data.page + ' / ' + pages + ' 页 · 共 ' + data.total + ' 条</span><button class="admin-secondary" data-next' + (data.page >= pages ? ' disabled' : '') + '>下一页</button>');
    el.querySelector('[data-prev]').onclick = function () { change(data.page - 1); };
    el.querySelector('[data-next]').onclick = function () { change(data.page + 1); };
  }
  function section(title, body) { return '<section class="ws-section"><h3>' + a.esc(title) + '</h3>' + body + '</section>'; }
  function kv(rows) { return '<dl class="ws-kv">' + rows.map(function (row) { return '<dt>' + a.esc(row[0]) + '</dt><dd>' + a.esc(row[1] == null ? "—" : row[1]) + '</dd>'; }).join("") + '</dl>'; }
  function title(name, meta) { return '<h2>' + a.esc(name) + '</h2><div class="ws-detail-meta">' + (meta || "") + '</div>'; }
  function detail(name, actions, content) {
    a.text("workspace-detail-title", name || "详情"); html("workspace-detail-actions", actions || "");
    html("workspace-detail-body", content || '<div class="ws-empty">在左侧选择记录</div>');
  }
  function open() {
    if (!media.matches || panel.classList.contains("is-open")) return;
    origin = document.activeElement;
    panel.classList.add("is-open"); backdrop.classList.add("is-open");
    panel.setAttribute("role", "dialog"); panel.setAttribute("aria-modal", "true");
    blocked = Array.from(document.querySelectorAll(".site-header,.page-title-row,.ws-metrics,.ws-left"));
    blocked.forEach(function (el) {
      el.setAttribute("aria-hidden", "true"); el.inert = true;
      el.querySelectorAll("a,button,input,select,textarea,summary,[tabindex]").forEach(function (item) {
        item._wsSaved = true; item._wsTab = item.getAttribute("tabindex"); item.setAttribute("tabindex", "-1");
      });
    });
    document.getElementById("workspace-detail-close").focus();
  }
  function close() {
    panel.classList.remove("is-open"); backdrop.classList.remove("is-open");
    panel.removeAttribute("role"); panel.removeAttribute("aria-modal");
    blocked.forEach(function (el) {
      el.removeAttribute("aria-hidden"); el.inert = false;
      el.querySelectorAll("[tabindex]").forEach(function (item) {
        if (!item._wsSaved) return; item._wsSaved = false;
        if (item._wsTab == null) item.removeAttribute("tabindex"); else item.setAttribute("tabindex", item._wsTab);
      });
    }); blocked = [];
    if (origin && origin.isConnected) origin.focus(); origin = null;
  }
  function offset() {
    var header = document.querySelector(".site-header");
    document.documentElement.style.setProperty("--workspace-top", Math.ceil(header.getBoundingClientRect().bottom + 14) + "px");
  }
  function read(path) {
    var controller = window.AbortController ? new AbortController() : null;
    var timer = controller && window.setTimeout(function () { controller.abort(); }, 10000);
    return a.api(path, controller ? {signal: controller.signal} : {}).then(function (data) {
      window.clearTimeout(timer); return data;
    }, function (err) { window.clearTimeout(timer); throw err; });
  }
  function poll(fn, interval) {
    var busy = false, failures = 0, retry = 0;
    function run(force) {
      if (busy || (!force && (paused || document.hidden || Date.now() < retry))) return;
      busy = true;
      Promise.resolve().then(fn).then(function () {
        failures = 0; retry = 0;
      }, function (err) {
        failures++; retry = Date.now() + Math.min(30000, Math.pow(2, failures - 1) * 1000);
        a.setStatus(err.message || "读取失败，请重试");
      }).then(function () { busy = false; });
    }
    run(true); var timer = window.setInterval(function () { run(false); }, interval || 1000);
    document.addEventListener("visibilitychange", function () { if (!document.hidden) run(false); });
    window.addEventListener("pagehide", function () { clearInterval(timer); });
    window.addEventListener("pageshow", function (event) { if (event.persisted) { timer = window.setInterval(function () { run(false); }, interval || 1000); run(false); } });
    return function () { run(true); };
  }
  function updated(id) { a.text(id, "最近更新 " + new Date().toLocaleTimeString("zh-CN", {hour12: false})); }
  document.getElementById("workspace-detail-close").onclick = close; backdrop.onclick = close;
  function topDialog() {
    var confirm = document.getElementById("admin-modal"), info = document.getElementById("admin-info-modal");
    return !confirm.hidden ? confirm : !info.hidden ? info : null;
  }
  var modalOrigin, activeDialog;
  new MutationObserver(function () {
    var dialog = topDialog();
    if (dialog && dialog !== activeDialog) { modalOrigin = document.activeElement; activeDialog = dialog; dialog.querySelector("button").focus(); }
    else if (!dialog && activeDialog) { activeDialog = null; if (modalOrigin && modalOrigin.isConnected) modalOrigin.focus(); }
  }).observe(document.body, {attributes: true, attributeFilter: ["hidden"], subtree: true});
  document.addEventListener("focusin", function (event) {
    var scope = topDialog() || (panel.classList.contains("is-open") ? panel : null);
    if (scope && !scope.contains(event.target)) scope.querySelector("button").focus();
  });
  document.addEventListener("keydown", function (event) {
    var dialog = topDialog(), scope = dialog || (panel.classList.contains("is-open") ? panel : null);
    if (!scope) return;
    if (event.key === "Escape") {
      event.preventDefault();
      if (dialog) dialog.querySelector("#admin-modal-cancel,#admin-info-close").click(); else close();
    }
    if (event.key !== "Tab") return;
    var buttons = Array.from(scope.querySelectorAll("a,button,input,select,textarea,summary")).filter(function (el) { return !el.disabled && el.offsetParent; });
    var first = buttons[0], last = buttons[buttons.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  });
  document.getElementById("workspace-refresh-pause").onclick = function () {
    paused = !paused; this.textContent = paused ? "继续" : "暂停";
    this.setAttribute("aria-pressed", String(paused)); a.text("workspace-refresh-state", paused ? "刷新已暂停" : refreshLabel);
  };
  if (window.ResizeObserver) new ResizeObserver(offset).observe(document.querySelector(".site-header"));
  window.addEventListener("resize", function () { offset(); if (!media.matches) close(); }); offset();
  window.adminWorkspace = {setRefreshLabel: function (label) { refreshLabel = label; if (!paused) a.text("workspace-refresh-state", label); }, html: html, list: list, metrics: metrics, pager: pager, section: section, kv: kv, title: title, detail: detail, open: open, close: close, read: read, poll: poll, updated: updated};
})();
