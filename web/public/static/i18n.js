(function () {
  "use strict";

  var storageKey = "mirror-locale";
  var locales = window.MirrorI18nLocales || {};
  var order = Object.keys(locales);
  var defaultLocale = locales["zh-CN"] ? "zh-CN" : (order[0] || "zh-CN");
  var listeners = [];
  var locale = defaultLocale;
  var errors = {"INVALID_REQUEST":"error.invalid","CATALOG_CHANGED":"catalog.changed","CHANGELOG_CHANGED":"changelog.changed","PUBLIC_INTERNAL_ERROR":"error.generic","ASSET_NOT_FOUND":"error.projectNotFound","DOWNLOAD_TOKEN_INVALID":"error.tokenInvalid","CLIENT_BLOCKED":"blocked.message","CLIENT_RATE_LIMITED":"error.rateLimited","PUBLIC_RESOURCE_RATE_LIMITED":"error.rateLimited","WEB_PROTOCOL_RETIRED":"error.protocolRetired","API_VERSION_RETIRED":"error.apiRetired","INVALID_CLIENT_SOURCE":"error.invalidSource","CHALLENGE_RATE_LIMITED":"error.challengeRateLimited","CHALLENGE_CAPACITY_REACHED":"error.challengeBusy","VDF_BUSY":"error.challengeBusy","NO_ROUTABLE_NODE":"error.noNode","REQUEST_QUOTA_EXHAUSTED":"error.requestQuota","TRAFFIC_LIMIT_EXCEEDED":"error.trafficLimit","CHALLENGE_IN_PROGRESS":"error.challengeInProgress","CHALLENGE_REQUIRED":"error.challengeRequired","CHALLENGE_FAILED":"error.challengeFailed"};
  var categories = {
    "web": "source.web", "api": "source.api",
    "github_releases": "source.githubReleases", "github-release": "source.githubReleases",
    "github_archive": "source.githubArchive", "github-archive": "source.githubArchive",
    "github": "source.github", "gitee": "source.gitee", "direct": "source.direct",
    "peer": "source.peer", "local": "source.local", "unknown": "source.unknown"
  };

  function canonical(value) {
    var raw = String(value || "").toLowerCase();
    for (var i = 0; i < order.length; i++) {
      if (order[i].toLowerCase() === raw) return order[i];
    }
    var base = raw.split("-")[0];
    for (var j = 0; j < order.length; j++) {
      if (order[j].toLowerCase().split("-")[0] === base) return order[j];
    }
    return "";
  }

  function browserLocale() {
    var values = [];
    if (navigator.languages && navigator.languages.length) values = values.concat(navigator.languages);
    if (navigator.language) values.push(navigator.language);
    for (var i = 0; i < values.length; i++) {
      var found = canonical(values[i]);
      if (found) return found;
    }
    return defaultLocale;
  }

  function savedMode() {
    try { return localStorage.getItem(storageKey) || ""; }
    catch (_) { return ""; }
  }

  function interpolate(value, params) {
    return String(value).replace(/\{([a-zA-Z0-9_]+)\}/g, function (_, key) {
      return params && params[key] != null ? String(params[key]) : "";
    });
  }

  function t(key, params) {
    var selected = locales[locale] || locales[defaultLocale] || {messages: {}};
    var fallback = locales[defaultLocale] || {messages: {}};
    var value = selected.messages && selected.messages[key];
    if (value == null) value = fallback.messages && fallback.messages[key];
    return value == null ? key : interpolate(value, params);
  }

  function translate(value) {
    if (value == null) return "";
    var selected = locales[locale] || {};
    var key = selected.legacy && selected.legacy[String(value)];
    return key ? t(key) : String(value);
  }

  function errorMessage(error, fallbackKey) {
    error = error || {};
    if (errors[String(error.code || "")]) return t(errors[String(error.code)]);
    var selected = locales[locale] || {};
    var key = error.message && selected.legacy && selected.legacy[error.message];
    if (key) return t(key);
    return error.message || (fallbackKey ? t(fallbackKey) : t("error.generic"));
  }

  function sourceLabel(value) {
    var raw = String(value == null ? "" : value).trim();
    var key = categories[raw.toLowerCase()];
    return key ? t(key) : (raw || t("source.unknown"));
  }

  function formatNumber(value) {
    return new Intl.NumberFormat(locale).format(Number(value) || 0);
  }

  function formatDate(value) {
    if (!value) return "";
    var parsed = new Date(value);
    if (isNaN(parsed.getTime())) return value;
    return new Intl.DateTimeFormat(locale, {
      year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit"
    }).format(parsed);
  }

  function setRootLocale() {
    document.documentElement.lang = locale;
    document.documentElement.setAttribute("data-locale", locale);
  }

  function applyPageTitles(root) {
    var classes = document.body && document.body.className.split(/\s+/);
    var page = classes && classes.filter(function (name) { return name.indexOf("page-") === 0; })[0];
    if (!page) return;
    var nodes = (root || document).querySelectorAll("[data-i18n-page-title]");
    for (var i = 0; i < nodes.length; i++) {
      var node = nodes[i];
      var original = node.getAttribute("data-i18n-original") || node.textContent;
      node.setAttribute("data-i18n-original", original);
      var projectNode = document.querySelector(".project-main h2");
      var projectName = page === "page-project" && projectNode ? projectNode.textContent : "";
      if (page === "page-project" && projectName) {
        node.textContent = projectName;
        continue;
      }
      var key = page === "page-project" && projectName ? "page.projectTitle" : "page.title." + page;
      var translated = t(key, {title: projectName || original});
      node.textContent = translated === key ? original : translated;
    }
    var subtitles = (root || document).querySelectorAll("[data-i18n-page-subtitle]");
    for (var j = 0; j < subtitles.length; j++) {
      var subtitle = subtitles[j];
      var originalSubtitle = subtitle.getAttribute("data-i18n-original") || subtitle.textContent;
      subtitle.setAttribute("data-i18n-original", originalSubtitle);
      var value = t("page.subtitle." + page);
      subtitle.textContent = value.indexOf("page.subtitle.") === 0 ? originalSubtitle : value;
    }
    var title = document.querySelector("title");
    if (title) {
      var originalTitle = title.getAttribute("data-i18n-original") || title.textContent;
      title.setAttribute("data-i18n-original", originalTitle);
      var projectTitleNode = document.querySelector(".project-main h2");
      var projectName = page === "page-project" && projectTitleNode ? projectTitleNode.textContent : "";
      var titleKey = page === "page-project" && projectName ?
        "page.projectTitle" : "page.title." + page;
      var titleValue = t(titleKey, {
        title: projectName || originalTitle
      });
      title.textContent = titleValue === titleKey ? originalTitle : titleValue;
      document.title = title.textContent;
    }
  }

  function updateLanguageMenu() {
    var button = document.getElementById("language-toggle");
    var menu = document.getElementById("language-menu");
    if (button) {
      button.setAttribute("aria-label", t("language.select"));
      button.setAttribute("title", t("language.select"));
    }
    if (!menu) return;
    var active = savedMode() || "auto";
    var choices = menu.querySelectorAll("[data-locale-mode]");
    for (var i = 0; i < choices.length; i++) {
      var selected = choices[i].getAttribute("data-locale-mode") === active;
      choices[i].setAttribute("aria-pressed", selected ? "true" : "false");
      choices[i].classList.toggle("is-active", selected);
    }
  }

  function apply(root) {
    root = root || document;
    setRootLocale();
    var nodes = root.querySelectorAll ? root.querySelectorAll("[data-i18n]") : [];
    for (var i = 0; i < nodes.length; i++) nodes[i].textContent = t(nodes[i].getAttribute("data-i18n"));
    var attrs = ["title", "aria-label", "placeholder", "alt"];
    var all = root.querySelectorAll ? root.querySelectorAll("*") : [];
    for (var j = 0; j < all.length; j++) {
      for (var k = 0; k < attrs.length; k++) {
        var name = attrs[k];
        var key = all[j].getAttribute("data-i18n-" + name);
        if (key) all[j].setAttribute(name, t(key));
      }
    }
    applyPageTitles(root);
    updateLanguageMenu();
  }

  function buildLanguageMenu() {
    var menu = document.getElementById("language-menu");
    var button = document.getElementById("language-toggle");
    if (!menu || !button) return;
    menu.innerHTML = "";
    var choices = [["auto", "language.auto"]];
    order.forEach(function (name) { choices.push([name, "language.name." + name]); });
    choices.forEach(function (item) {
      var choice = document.createElement("button");
      choice.type = "button";
      choice.className = "language-choice";
      choice.setAttribute("data-locale-mode", item[0]);
      if (item[0] === "auto") choice.setAttribute("data-i18n", item[1]);
      choice.setAttribute("aria-pressed", "false");
      choice.textContent = item[0] === "auto" ? t(item[1]) : (locales[item[0]].label || item[0]);
      choice.addEventListener("click", function () {
        setLocale(item[0]);
        menu.hidden = true;
        button.setAttribute("aria-expanded", "false");
      });
      menu.appendChild(choice);
    });
    updateLanguageMenu();
    button.setAttribute("aria-expanded", "false");
    button.addEventListener("click", function (event) {
      event.stopPropagation();
      menu.hidden = !menu.hidden;
      button.setAttribute("aria-expanded", menu.hidden ? "false" : "true");
    });
    document.addEventListener("click", function (event) {
      if (!menu.hidden && !menu.contains(event.target) && event.target !== button) {
        menu.hidden = true;
        button.setAttribute("aria-expanded", "false");
      }
    });
  }

  function setLocale(mode) {
    var next = mode === "auto" ? browserLocale() : canonical(mode);
    if (!next) return;
    try {
      if (mode === "auto") localStorage.removeItem(storageKey);
      else localStorage.setItem(storageKey, next);
    } catch (_) {}
    locale = next;
    apply(document);
    listeners.slice().forEach(function (listener) { listener(locale); });
  }

  function onChange(listener) {
    if (typeof listener !== "function") return function () {};
    listeners.push(listener);
    return function () {
      var index = listeners.indexOf(listener);
      if (index >= 0) listeners.splice(index, 1);
    };
  }

  locale = canonical(savedMode()) || browserLocale();
  window.MirrorI18n = {
    apply: apply, errorMessage: errorMessage, formatDate: formatDate, formatNumber: formatNumber,
    getLocale: function () { return locale; }, onChange: onChange, setLocale: setLocale,
    sourceLabel: sourceLabel, t: t, translate: translate
  };
  setRootLocale();
  document.addEventListener("DOMContentLoaded", function () {
    apply(document);
    buildLanguageMenu();
    apply(document);
  });
})();
