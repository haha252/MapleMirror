(function () {
  const root = document.documentElement;
  const modeKey = "mirror-mode";
  const accentKey = "mirror-accent";
  const media = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
  const themes = [
    ["rose", "玫瑰红", "#F5276C"],
    ["orange", "活力橙", "#F54927"],
    ["maple", "枫叶黄", "#FFA436"],
    ["sprout", "嫩芽黄", "#D3F527"],
    ["cyan", "水玉青", "#27F5B0"],
    ["blue", "大海蓝", "#276CF5"],
    ["purple", "基佬紫", "#B027F5"]
  ];

  function saved(key) {
    return window.localStorage ? localStorage.getItem(key) : "";
  }

  function effectiveMode() {
    const mode = saved(modeKey);
    if (mode === "light" || mode === "dark") return mode;
    return media && media.matches ? "dark" : "light";
  }

  function applyMode(mode) {
    root.setAttribute("data-theme", mode);
  }

  function applyAccent(name) {
    root.setAttribute("data-accent", themes.some((item) => item[0] === name) ? name : "maple");
  }

  function buildPalette() {
    const menu = document.getElementById("palette-menu");
    if (!menu) return;
    menu.innerHTML = "";
    themes.forEach(([name, label, color]) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "palette-choice";
      button.dataset.accent = name;
      button.title = label;
      button.setAttribute("aria-label", label);
      button.innerHTML = '<span style="background:' + color + '"></span><b>' + label + "</b>";
      button.addEventListener("click", function () {
        if (window.localStorage) localStorage.setItem(accentKey, name);
        applyAccent(name);
        menu.hidden = true;
      });
      menu.appendChild(button);
    });
  }

  applyMode(effectiveMode());
  applyAccent(saved(accentKey) || "maple");

  document.addEventListener("DOMContentLoaded", function () {
    buildPalette();
    const mode = document.getElementById("mode-toggle");
    const palette = document.getElementById("palette-toggle");
    const menu = document.getElementById("palette-menu");
    if (mode) mode.addEventListener("click", function () {
      const next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
      if (window.localStorage) localStorage.setItem(modeKey, next);
      applyMode(next);
    });
    if (palette && menu) palette.addEventListener("click", function () {
      menu.hidden = !menu.hidden;
    });
  });

  if (media && media.addEventListener) {
    media.addEventListener("change", function () {
      if (!saved(modeKey)) applyMode(effectiveMode());
    });
  }
})();
