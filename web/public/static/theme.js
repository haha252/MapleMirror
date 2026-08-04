(function () {
  const root = document.documentElement;
  const modeKey = "mirror-mode";
  const accentKey = "mirror-accent";
  const media = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
  const themes = [
    ["rose", "theme.rose", "#F5276C"],
    ["orange", "theme.orange", "#F54927"],
    ["maple", "theme.maple", "#FFA436"],
    ["sprout", "theme.sprout", "#D3F527"],
    ["cyan", "theme.cyan", "#27F5B0"],
    ["blue", "theme.blue", "#276CF5"],
    ["purple", "theme.purple", "#B027F5"]
  ];

  function text(key, fallback) {
    return window.MirrorI18n ? window.MirrorI18n.t(key) : fallback;
  }

  function updateBrand() {
    const primary = document.querySelector(".site-brand__primary");
    const secondary = document.querySelector(".site-brand__secondary");
    if (primary) primary.textContent = text("brand.primary", "枫源");
    if (secondary) secondary.textContent = text("brand.secondary", "镜像");
  }

  function saved(key) {
    try { return window.localStorage ? localStorage.getItem(key) : ""; }
    catch (_) { return ""; }
  }

  function save(key, value) {
    try { if (window.localStorage) localStorage.setItem(key, value); }
    catch (_) {}
  }

  function effectiveMode() {
    const mode = saved(modeKey);
    if (mode === "light" || mode === "dark") return mode;
    return media && media.matches ? "dark" : "light";
  }

  function applyMode(mode) {
    root.setAttribute("data-theme", mode);
    updateModeIcon();
  }

  function applyAccent(name) {
    const next = themes.some((item) => item[0] === name) ? name : "maple";
    root.setAttribute("data-accent", next);
    document.querySelectorAll(".palette-choice").forEach((button) => {
      button.classList.toggle("is-active", button.dataset.accent === next);
    });
  }

  function updateModeIcon() {
    const button = document.getElementById("mode-toggle");
    if (!button) return;
    const dark = root.getAttribute("data-theme") === "dark";
    button.innerHTML = dark
      ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden="true"><path d="M20 15.5A8.5 8.5 0 0 1 8.5 4a7 7 0 1 0 11.5 11.5Z"/></svg>'
      : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>';
    const label = dark ? text("theme.toLight", "切换到浅色模式") : text("theme.toDark", "切换到深色模式");
    button.setAttribute("aria-label", label);
    button.title = label;
  }

  function buildPalette() {
    const menu = document.getElementById("palette-menu");
    if (!menu) return;
    menu.innerHTML = "";
    themes.forEach(([name, key, color]) => {
      const label = text(key, key);
      const button = document.createElement("button");
      button.type = "button";
      button.className = "palette-choice";
      button.dataset.accent = name;
      button.title = label;
      button.setAttribute("aria-label", label);
      button.innerHTML = '<span style="background:' + color + '"></span><b>' + label + "</b>";
      button.addEventListener("click", function () {
        save(accentKey, name);
        applyAccent(name);
        menu.hidden = true;
      });
      menu.appendChild(button);
    });
    applyAccent(root.getAttribute("data-accent") || saved(accentKey) || "maple");
  }

  function startPageLoading() {
    const bar = document.getElementById("page-loading-bar");
    if (bar) bar.classList.add("is-loading");
  }

  applyMode(effectiveMode());
  applyAccent(saved(accentKey) || "maple");

  document.addEventListener("DOMContentLoaded", function () {
    updateBrand();
    buildPalette();
    const mode = document.getElementById("mode-toggle");
    const palette = document.getElementById("palette-toggle");
    const menu = document.getElementById("palette-menu");
    updateModeIcon();
    if (mode) mode.addEventListener("click", function () {
      const next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
      save(modeKey, next);
      applyMode(next);
    });
    if (palette && menu) palette.addEventListener("click", function () {
      menu.hidden = !menu.hidden;
    });
    if (window.MirrorI18n) window.MirrorI18n.onChange(function () {
      updateBrand();
      buildPalette();
      updateModeIcon();
    });
    document.addEventListener("click", function (event) {
      const link = event.target.closest && event.target.closest("a[href]");
      if (!link || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const url = new URL(link.href, window.location.href);
      if (url.origin !== window.location.origin || link.target || url.pathname === window.location.pathname && url.search === window.location.search) return;
      startPageLoading();
    });
    window.addEventListener("beforeunload", startPageLoading);
  });

  if (media) {
    window.MirrorCompat.onMediaChange(media, function () {
      if (!saved(modeKey)) applyMode(effectiveMode());
    });
  }
})();
