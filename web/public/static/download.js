(function () {
  const statusBox = document.getElementById("download-status");
  const results = document.querySelector(".catalog-results");
  const projectsContainer = document.getElementById("project-cards");
  const suggestionsContainer = document.getElementById("suggested-project-cards");
  const suggestionsSection = document.getElementById("catalog-suggestions");
  const suggestionsTitle = document.getElementById("catalog-suggestions-title");
  const cardTemplate = document.getElementById("project-card-template");
  const projectSentinel = document.getElementById("project-load-more");
  const suggestionSentinel = document.getElementById("suggested-project-load-more");
  if (!statusBox || !results || !projectsContainer || !suggestionsContainer ||
      !suggestionsSection || !suggestionsTitle || !cardTemplate || !projectSentinel ||
      !suggestionSentinel || !window.DownloadCardRenderer || !window.DownloadFilters ||
      !window.DownloadLazyLoader) return;

  const configuredRows = Number.parseInt(results.dataset.catalogBatchRows, 10);
  const batchRows = Number.isInteger(configuredRows) && configuredRows > 0 ?
    configuredRows : 4;
  const configuredRemaining = Number.parseInt(
    results.dataset.catalogPrefetchRemainingRows, 10
  );
  const defaultRemainingRows = batchRows > 1 ? 1 : 0;
  const remainingRows = Number.isInteger(configuredRemaining) &&
    configuredRemaining >= 0 && configuredRemaining < batchRows ?
    configuredRemaining : defaultRemainingRows;
  let debounceTimer = 0;
  let initialController = null;
  let requestSequence = 0;
  const filters = window.DownloadFilters.create({onChange: scheduleLoad});
  const sections = {
    projects: createSection("projects", projectsContainer, projectSentinel),
    suggested_projects: createSection(
      "suggested_projects", suggestionsContainer, suggestionSentinel
    )
  };
  const lazyLoader = window.DownloadLazyLoader.create(batchRows, remainingRows, loadMore);

  function createSection(kind, container, sentinel) {
    const section = {
      kind: kind, container: container, sentinel: sentinel,
      button: sentinel.querySelector("button"), message: sentinel.querySelector("span"),
      cursor: "", loading: false, failed: false, controller: null,
      trigger: null, batchCards: []
    };
    section.button.addEventListener("click", function () { loadMore(section); });
    return section;
  }

  function setStatus(message, level, retry) {
    statusBox.replaceChildren();
    statusBox.className = "status " + (level || "muted");
    statusBox.hidden = !message;
    if (!message) return;
    statusBox.appendChild(document.createTextNode(message));
    if (retry) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = "重试";
      button.addEventListener("click", loadCatalog);
      statusBox.append(document.createTextNode(" "), button);
    }
  }

  function scheduleLoad() {
    window.clearTimeout(debounceTimer);
    debounceTimer = window.setTimeout(loadCatalog, 300);
  }

  function catalogURL(cursor, pageSize) {
    const params = new URLSearchParams();
    const search = filters.search();
    if (search) params.set("q", search.toLowerCase());
    filters.selectedFilters().forEach((value) => params.append("filter", value));
    params.set("page_size", String(pageSize));
    if (cursor) params.set("cursor", cursor);
    return "/api/public/v1/catalog?" + params.toString();
  }

  async function loadCatalog() {
    window.clearTimeout(debounceTimer);
    const sequence = ++requestSequence;
    resetRequests();
    projectsContainer.replaceChildren();
    suggestionsContainer.replaceChildren();
    suggestionsSection.hidden = true;
    setStatus("正在更新项目列表...", "muted");
    initialController = new AbortController();
    try {
      const catalog = await requestCatalog("", initialController.signal,
        lazyLoader.batchSize(projectsContainer));
      if (sequence !== requestSequence) return;
      filters.setGroups(catalog.filter_groups);
      renderInitial(catalog);
    } catch (error) {
      if (error && error.name === "AbortError") return;
      if (sequence !== requestSequence) return;
      if (error.status === 400 && filters.clearSelections()) {
        loadCatalog();
        return;
      }
      showInitialError(error);
    }
  }

  async function requestCatalog(cursor, signal, pageSize) {
    const response = await fetch(catalogURL(cursor, pageSize), {
      cache: "default", signal: signal
    });
    if (!response.ok) throw await responseError(response);
    return response.json();
  }

  async function responseError(response) {
    const error = new Error("catalog request failed");
    error.status = response.status;
    error.retryAfter = response.headers.get("Retry-After") || "";
    try {
      const body = await response.json();
      error.code = body.code || "";
    } catch (_) {
      error.code = "";
    }
    return error;
  }

  function renderInitial(catalog) {
    const projects = arrayValue(catalog.projects);
    const suggestions = arrayValue(catalog.suggested_projects);
    const options = cardOptions();
    const projectCards = renderCards(projectsContainer, projects, options, false);
    const suggestionCards = renderCards(suggestionsContainer, suggestions, options, false);
    setSectionCursor(sections.projects, catalog.next_projects_cursor, projectCards);
    setSectionCursor(
      sections.suggested_projects, catalog.next_suggested_projects_cursor, suggestionCards
    );
    suggestionsTitle.textContent = projects.length ?
      "您可能还在找：" : "没有严格匹配的项，但你可能在找：";
    suggestionsSection.hidden = suggestions.length === 0 &&
      !sections.suggested_projects.cursor;
    if (projects.length || suggestions.length) {
      setStatus("", "muted");
    } else if (filters.search() || filters.selectedFilters().length) {
      setStatus("没有找到符合当前搜索和筛选条件的项目。", "muted");
    } else {
      setStatus("暂无可展示项目。", "muted");
    }
  }

  async function loadMore(section) {
    if (section.loading || !section.cursor) return;
    const sequence = requestSequence;
    const cursor = section.cursor;
    section.loading = true;
    section.failed = false;
    section.controller = new AbortController();
    updateSentinel(section, "正在加载更多...");
    try {
      const catalog = await requestCatalog(cursor, section.controller.signal,
        lazyLoader.batchSize(section.container));
      if (sequence !== requestSequence) return;
      const projects = section.kind === "projects" ?
        arrayValue(catalog.projects) : arrayValue(catalog.suggested_projects);
      const cards = renderCards(section.container, projects, cardOptions(), true);
      const next = section.kind === "projects" ?
        catalog.next_projects_cursor : catalog.next_suggested_projects_cursor;
      section.loading = false;
      setSectionCursor(section, next, cards);
    } catch (error) {
      if (error && error.name === "AbortError") return;
      if (sequence !== requestSequence) return;
      section.loading = false;
      if (error.status === 409 && error.code === "CATALOG_CHANGED") {
        loadCatalog();
        return;
      }
      section.failed = true;
      const wait = error.status === 429 && error.retryAfter ?
        "，请在 " + error.retryAfter + " 秒后重试" : "";
      updateSentinel(section, "加载更多失败" + wait + "。");
    }
  }

  function cardOptions() {
    return {
      template: cardTemplate, selectedTags: filters.selectedTags(),
      tagLabels: filters.tagLabels(), search: filters.search(), status: setStatus
    };
  }

  function renderCards(container, projects, options, append) {
    const fragment = document.createDocumentFragment();
    const cards = [];
    projects.forEach((project) => {
      const card = window.DownloadCardRenderer.create(project, options);
      cards.push(card);
      fragment.appendChild(card);
    });
    if (append) container.appendChild(fragment);
    else container.replaceChildren(fragment);
    return cards;
  }

  function setSectionCursor(section, cursor, cards) {
    section.cursor = typeof cursor === "string" ? cursor : "";
    section.loading = false;
    section.failed = false;
    section.batchCards = cards || [];
    updateSentinel(section, "");
  }

  function updateSentinel(section, message) {
    lazyLoader.unwatch(section);
    const active = Boolean(section.cursor || section.loading || section.failed);
    section.sentinel.hidden = !active;
    section.message.textContent = message || "";
    section.button.textContent = section.failed ? "重试加载" :
      (section.kind === "projects" ? "加载更多项目" : "加载更多建议");
    section.button.hidden = lazyLoader.supported && !section.failed;
    section.button.disabled = section.loading;
    lazyLoader.watch(section);
  }

  function resetRequests() {
    if (initialController) initialController.abort();
    Object.values(sections).forEach((section) => {
      if (section.controller) section.controller.abort();
      section.cursor = "";
      section.loading = false;
      section.failed = false;
      section.batchCards = [];
      updateSentinel(section, "");
    });
  }

  function showInitialError(error) {
    if (error.status === 429) {
      const wait = error.retryAfter ? "，请在 " + error.retryAfter + " 秒后重试" : "";
      setStatus("搜索或筛选请求过于频繁" + wait + "。", "warn", true);
    } else {
      setStatus("项目列表加载失败，请稍后重试。", "warn", true);
    }
  }

  function arrayValue(value) {
    return Array.isArray(value) ? value : [];
  }

  loadCatalog();
})();
