(function () {
  const statusBox = document.getElementById("download-status");
  const projectsContainer = document.getElementById("project-cards");
  const suggestionsContainer = document.getElementById("suggested-project-cards");
  const suggestionsSection = document.getElementById("catalog-suggestions");
  const suggestionsTitle = document.getElementById("catalog-suggestions-title");
  const cardTemplate = document.getElementById("project-card-template");
  if (!statusBox || !projectsContainer || !suggestionsContainer || !suggestionsSection ||
      !suggestionsTitle || !cardTemplate || !window.DownloadCardRenderer ||
      !window.DownloadFilters) return;

  let debounceTimer = 0;
  let requestController = null;
  let requestSequence = 0;
  const filters = window.DownloadFilters.create({onChange: scheduleLoad});

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

  function catalogURL() {
    const params = new URLSearchParams();
    const search = filters.search();
    if (search) params.set("q", search.toLowerCase());
    filters.selectedFilters().forEach((value) => params.append("filter", value));
    const query = params.toString();
    return "/api/public/v1/catalog" + (query ? "?" + query : "");
  }

  async function loadCatalog() {
    window.clearTimeout(debounceTimer);
    const sequence = ++requestSequence;
    if (requestController) requestController.abort();
    requestController = new AbortController();
    setStatus("正在更新项目列表...", "muted");
    try {
      const response = await fetch(catalogURL(), {
        cache: "default", signal: requestController.signal
      });
      if (!response.ok) throw await responseError(response);
      const catalog = await response.json();
      if (sequence !== requestSequence) return;
      filters.setGroups(catalog.filter_groups);
      renderCatalog(catalog);
    } catch (error) {
      if (error && error.name === "AbortError") return;
      if (sequence !== requestSequence) return;
      if (error.status === 400 && filters.clearSelections()) {
        loadCatalog();
        return;
      }
      if (error.status === 429) {
        const wait = error.retryAfter ? "，请在 " + error.retryAfter + " 秒后重试" : "";
        setStatus("搜索或筛选请求过于频繁" + wait + "。", "warn", true);
      } else {
        setStatus("项目列表加载失败，请稍后重试。", "warn", true);
      }
    }
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

  function renderCatalog(catalog) {
    const projects = Array.isArray(catalog.projects) ? catalog.projects : [];
    const suggestions = Array.isArray(catalog.suggested_projects) ?
      catalog.suggested_projects : [];
    const options = {
      template: cardTemplate,
      selectedTags: filters.selectedTags(),
      tagLabels: filters.tagLabels(),
      search: filters.search(),
      status: setStatus
    };
    renderCards(projectsContainer, projects, options);
    renderCards(suggestionsContainer, suggestions, options);
    suggestionsTitle.textContent = projects.length ?
      "您可能还在找：" : "没有严格匹配的项，但你可能在找：";
    suggestionsSection.hidden = suggestions.length === 0;
    if (projects.length || suggestions.length) {
      setStatus("", "muted");
    } else if (filters.search() || filters.selectedFilters().length) {
      setStatus("没有找到符合当前搜索和筛选条件的项目。", "muted");
    } else {
      setStatus("暂无可展示项目。", "muted");
    }
  }

  function renderCards(container, projects, options) {
    const fragment = document.createDocumentFragment();
    projects.forEach((project) => {
      fragment.appendChild(window.DownloadCardRenderer.create(project, options));
    });
    container.replaceChildren(fragment);
  }

  loadCatalog();
})();
