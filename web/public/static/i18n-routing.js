(function (root) {
  "use strict";

  function localizedPath(next) {
    var page = document.documentElement;
    if (!page) return "";
    return page.getAttribute(next === "en" ? "data-locale-en-path" : "data-locale-zh-path") || "";
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
    navigateToLocale: navigateToLocale,
    syncSavedLocale: syncSavedLocale
  };
})(window);
