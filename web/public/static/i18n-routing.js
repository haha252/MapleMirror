(function (root) {
  "use strict";

  function localeRoutes() {
    var page = document.documentElement;
    if (!page) return {};
    var raw = page.getAttribute("data-locale-routes") || "";
    if (!raw) return {};
    try {
      var parsed = JSON.parse(raw);
      return parsed && typeof parsed === "object" ? parsed : {};
    } catch (_) {
      return {};
    }
  }

  function localizedPath(next) {
    var routes = localeRoutes();
    return typeof routes[next] === "string" ? routes[next] : "";
  }

  function localizePath(path) {
    path = String(path || "");
    if (!path || path.charAt(0) !== "/") return path;
    var page = document.documentElement;
    var prefix = page ? String(page.getAttribute("data-locale-prefix") || "").replace(/^\/+|\/+$/g, "") : "";
    if (!prefix) return path;
    if (path === "/") return "/" + prefix + "/";
    return "/" + prefix + path;
  }

  function navigateToLocale(next, replace) {
    var target = localizedPath(next);
    if (!target || target === window.location.pathname) return false;
    var url = target + window.location.search + window.location.hash;
    if (replace) window.location.replace(url);
    else window.location.assign(url);
    return true;
  }

  function syncSavedLocale(saved, current, browserLocale, canonicalize) {
    if (!saved) return {redirected: false, locale: current};
    var preferred = saved === "auto" ? browserLocale() : canonicalize(saved);
    if (!preferred || preferred === current) return {redirected: false, locale: current};
    if (navigateToLocale(preferred, true)) return {redirected: true, locale: current};
    return {redirected: false, locale: preferred};
  }

  root.MirrorLocaleRouting = {
    localizePath: localizePath,
    navigateToLocale: navigateToLocale,
    syncSavedLocale: syncSavedLocale
  };
})(window);
