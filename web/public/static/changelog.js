(function () {
  const desktopSearch = document.getElementById("changelog-search-desktop");
  const mobileSearch = document.getElementById("changelog-search-mobile");
  const searches = [desktopSearch, mobileSearch].filter(Boolean);
  const levels = Array.from(document.querySelectorAll('input[name="changelog-minimum-level"]'));
  const status = document.getElementById("changelog-status");
  const timeline = document.getElementById("changelog-timeline");
  const sentinel = document.getElementById("changelog-load-more");
  const button = sentinel && sentinel.querySelector("button");
  const filters = document.getElementById("changelog-filters");
  const filterButton = document.getElementById("changelog-filter-button");
  const filterCount = document.getElementById("changelog-filter-count");
  const filterClear = document.getElementById("changelog-filter-clear");
  const filterClose = document.getElementById("changelog-filter-close");
  const backdrop = document.getElementById("changelog-filter-backdrop");
  if (!searches.length || !levels.length || !status || !timeline || !sentinel || !button || !filters || !filterButton || !filterCount || !filterClear || !filterClose || !backdrop) return;
  const i18n = window.MirrorI18n;
  const text = (key, fallback, params) => i18n ? i18n.t(key, params) : fallback;
  let cursor = "";
  let loading = false;
  let loaded = 0;
  let sequence = 0;
  let controller = null;
  let debounce = 0;
  function searchValue() { return searches[0].value.trim(); }
  function localizeControls() {
    const clear = document.querySelector("#changelog-filter-clear span");
    const filter = document.querySelector("#changelog-filter-button > span:first-of-type");
    if (clear) clear.textContent = text("catalog.clear", "取消全部");
    if (filter) filter.textContent = text("catalog.filter", "筛选器");
    levels.forEach(function (input) {
      const label = input.parentElement && input.parentElement.querySelector("span");
      const key = {info: "changelog.all", notice: "changelog.notice", warn: "changelog.warn", critical: "changelog.critical"}[input.value];
      if (label && key) label.textContent = text(key, label.textContent);
    });
  }
  function levelValue() {
    const selected = levels.find(function (input) { return input.checked; });
    return selected ? selected.value : "info";
  }
  function requestURL(nextCursor) {
    const params = new URLSearchParams();
    params.set("minimum_level", levelValue());
    params.set("limit", "20");
    if (searchValue()) params.set("q", searchValue());
    if (nextCursor) params.set("cursor", nextCursor);
    return "/api/public/v1/changelog?" + params.toString();
  }
  async function request(nextCursor, signal) {
    const requestOptions = window.MirrorCompat.withAbortSignal({cache: "no-cache"}, signal);
    const response = await fetch(requestURL(nextCursor), requestOptions);
    let payload = null;
    try { payload = await response.json(); } catch (_) { payload = null; }
    if (!response.ok) {
      const error = new Error(payload && payload.message || "changelog request failed");
      error.status = response.status;
      error.code = payload && payload.code || "";
      throw error;
    }
    return payload && payload.data || {items: []};
  }
  async function loadInitial() {
    window.clearTimeout(debounce);
    const current = ++sequence;
    if (controller) controller.abort();
    controller = window.MirrorCompat.createAbortController();
    cursor = "";
    loaded = 0;
    timeline.replaceChildren();
    setStatus(text("changelog.loading", "正在加载更新记录..."));
    setButton(false);
    loading = true;
    try {
      const data = await request("", controller.signal);
      if (current !== sequence) return;
      appendItems(data.items);
      cursor = data.next_cursor || "";
      finishBatch();
    } catch (error) {
      if (error.name === "AbortError" || current !== sequence) return;
      showError(error, true);
    } finally {
      if (current === sequence) loading = false;
    }
  }
  async function loadMore() {
    if (loading || !cursor) return;
    const current = sequence;
    controller = window.MirrorCompat.createAbortController();
    loading = true;
    setStatus(text("catalog.loadingMore", "正在加载更多更新记录..."));
    setButton(false);
    try {
      const data = await request(cursor, controller.signal);
      if (current !== sequence) return;
      appendItems(data.items);
      cursor = data.next_cursor || "";
      finishBatch();
    } catch (error) {
      if (error.name === "AbortError" || current !== sequence) return;
      if (error.status === 409 && error.code === "CHANGELOG_CHANGED") {
        loadInitial();
        return;
      }
      showError(error, false);
    } finally {
      if (current === sequence) loading = false;
    }
  }
  function appendItems(items) {
    if (!Array.isArray(items)) return;
    const fragment = document.createDocumentFragment();
    items.forEach(function (item) {
      fragment.appendChild(createEntry(item));
      loaded++;
    });
    timeline.appendChild(fragment);
  }
  function createEntry(item) {
    const safeLevel = ["info", "notice", "warn", "critical"].includes(item.level) ? item.level : "info";
    const article = document.createElement("article");
    article.className = "changelog-entry changelog-entry--" + safeLevel;
    article.setAttribute("role", "listitem");
    const level = text("changelog.entryLevel." + safeLevel, safeLevel);
    article.setAttribute("aria-label", safeLevel + " 等级，" + (item.title || ""));
    article.setAttribute("aria-label", text("changelog.entryAria", safeLevel + " 等级，" + (item.title || ""), {level: level, title: item.title || ""}));
    const card = document.createElement("div");
    card.className = "changelog-entry__card panel-card";
    const head = document.createElement("div");
    head.className = "changelog-entry__head";
    const title = document.createElement("h2");
    title.textContent = item.title || "";
    const time = document.createElement("time");
    time.dateTime = item.occurred_at || "";
    time.textContent = displayBeijingTime(item.occurred_at);
    head.append(title, time);
    card.appendChild(head);
    if (item.description_html) {
      const description = document.createElement("div");
      description.className = "changelog-entry__description";
      description.innerHTML = item.description_html;
      card.appendChild(description);
    }
    article.appendChild(card);
    return article;
  }
  function displayBeijingTime(value) {
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) return value || "";
    const parts = new Intl.DateTimeFormat(i18n ? i18n.getLocale() : (document.documentElement.lang || undefined), {
      timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit",
      day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23"
    }).formatToParts(parsed);
    const values = {};
    parts.forEach(function (part) { values[part.type] = part.value; });
    return values.year + "-" + values.month + "-" + values.day +
      " " + values.hour + ":" + values.minute;
  }
  function finishBatch() {
    if (!loaded) {
      setStatus(searchValue() || levelValue() !== "info" ?
        text("changelog.noMatch", "没有找到符合当前条件的更新记录。") :
        text("changelog.empty", "暂无更新记录。"));
      setButton(false);
    } else if (cursor) {
      setStatus("");
      setButton(true, text("changelog.loadMore", "加载更多"));
    } else {
      setStatus(text("changelog.loadedAll", "已加载全部更新记录。"));
      setButton(false);
    }
  }
  function showError(error, initial) {
    setStatus(error.status === 429 ? text("changelog.rateLimited", "请求过于频繁，请稍后重试。") :
      (i18n ? i18n.errorMessage(error, "changelog.failed") : "更新记录读取失败，请重试。"));
    setButton(true, text("catalog.retry", "重试"), initial ? loadInitial : loadMore);
  }
  function setStatus(message) {
    status.textContent = message;
    status.hidden = !message;
  }
  function setButton(visible, label, action) {
    button.hidden = !visible;
    button.textContent = label || text("changelog.loadMore", "加载更多");
    button.onclick = action || loadMore;
  }
  function syncSearch(source) {
    searches.forEach(function (input) {
      if (input !== source) input.value = source.value;
    });
    window.clearTimeout(debounce);
    debounce = window.setTimeout(loadInitial, 300);
  }
  function updateFilterState() {
    const active = levelValue() !== "info";
    filterClear.disabled = !active;
    filterCount.hidden = !active;
    filterCount.textContent = active ? "1" : "";
    filterButton.setAttribute("aria-label", active ?
      text("changelog.filterCount", "筛选器，已选择 1 项", {count: 1}) :
      text("catalog.filter", "筛选器"));
  }
  function setFilterOpen(open) {
    filters.dataset.open = open ? "true" : "false";
    filterButton.setAttribute("aria-expanded", open ? "true" : "false");
    backdrop.hidden = !open;
    document.body.classList.toggle("catalog-filter-open", open);
  }
  searches.forEach(function (input) {
    input.addEventListener("input", function () { syncSearch(input); });
  });
  levels.forEach(function (input) {
    input.addEventListener("change", function () {
      updateFilterState();
      loadInitial();
    });
  });
  filterClear.addEventListener("click", function () {
    levels[0].checked = true;
    updateFilterState();
    loadInitial();
  });
  filterButton.addEventListener("click", function () {
    setFilterOpen(filters.dataset.open !== "true");
  });
  filterClose.addEventListener("click", function () { setFilterOpen(false); });
  backdrop.addEventListener("click", function () { setFilterOpen(false); });
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") setFilterOpen(false);
  });
  window.MirrorCompat.onMediaChange(window.matchMedia("(min-width: 1101px)"), function (event) {
    if (event.matches) setFilterOpen(false);
  });
  if ("IntersectionObserver" in window) {
    const scrollRoot = window.matchMedia("(min-width: 1101px)").matches ? document.querySelector(".changelog-content") : null;
    const observer = new IntersectionObserver(function (entries) {
      if (entries.some(function (entry) { return entry.isIntersecting; })) loadMore();
    }, {root: scrollRoot, rootMargin: "300px 0px"});
    observer.observe(sentinel);
  }
  if (i18n) i18n.onChange(function () {
    localizeControls();
    updateFilterState();
    loadInitial();
  });
  localizeControls();
  updateFilterState(); loadInitial();
})();
