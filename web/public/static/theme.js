(function () {
  const root = document.documentElement;
  const key = "mirror-theme";
  const media = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;

  function effectiveTheme() {
    const saved = window.localStorage ? localStorage.getItem(key) : "";
    if (saved === "light" || saved === "dark") return saved;
    return media && media.matches ? "dark" : "light";
  }

  function applyTheme(theme) {
    root.setAttribute("data-theme", theme);
    const toggle = document.getElementById("theme-toggle");
    if (toggle) toggle.textContent = theme === "dark" ? "浅色模式" : "深色模式";
  }

  applyTheme(effectiveTheme());

  document.addEventListener("DOMContentLoaded", function () {
    const toggle = document.getElementById("theme-toggle");
    if (!toggle) return;
    toggle.addEventListener("click", function () {
      const next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
      if (window.localStorage) localStorage.setItem(key, next);
      applyTheme(next);
    });
  });

  if (media && media.addEventListener) {
    media.addEventListener("change", function () {
      if (window.localStorage && localStorage.getItem(key)) return;
      applyTheme(effectiveTheme());
    });
  }
})();
