(function () {
  function create(options) {
    const groupsContainer = document.getElementById("catalog-filter-groups");
    const panel = document.getElementById("catalog-filters");
    const backdrop = document.getElementById("catalog-filter-backdrop");
    const openButton = document.getElementById("catalog-filter-button");
    const clearButton = document.getElementById("catalog-filter-clear");
    const closeButton = document.getElementById("catalog-filter-close");
    const count = document.getElementById("catalog-filter-count");
    const desktopSearch = document.getElementById("catalog-search-desktop");
    const mobileSearch = document.getElementById("catalog-search-mobile");
    const searches = [desktopSearch, mobileSearch].filter(Boolean);
    const selected = new Set();
    const labels = new Map();
    let groups = [];
    let open = false;
    let historyEntry = false;
    let previousFocus = null;
    const i18n = window.MirrorI18n;
    const text = (key, fallback, params) => i18n ? i18n.t(key, params) : fallback;

    searches.forEach((input) => input.addEventListener("input", () => {
      searches.forEach((other) => {
        if (other !== input && other.value !== input.value) other.value = input.value;
      });
      options.onChange();
    }));

    openButton.addEventListener("click", openPanel);
    clearButton.addEventListener("click", () => {
      if (clearSelections()) options.onChange();
    });
    closeButton.addEventListener("click", () => closePanel(true));
    backdrop.addEventListener("click", () => closePanel(true));
    window.addEventListener("popstate", () => {
      if (open) closePanel(false);
    });
    document.addEventListener("keydown", (event) => {
      if (!open) return;
      if (event.key === "Escape") {
        event.preventDefault();
        closePanel(true);
      } else if (event.key === "Tab") {
        trapFocus(event);
      }
    });
    const media = window.matchMedia("(min-width: 1101px)");
    window.MirrorCompat.onMediaChange(media, (event) => {
      if (event.matches && open) closePanel(true);
      syncPanelAccessibility(event.matches);
    });
    panel.dataset.open = "false";
    syncPanelAccessibility(media.matches);
    if (i18n) i18n.onChange(function () {
      if (!groups.length) renderGroups();
      updateCount();
    });

    function setGroups(nextGroups) {
      groups = Array.isArray(nextGroups) ? nextGroups : [];
      const valid = new Set();
      labels.clear();
      groups.forEach((group) => (group.options || []).forEach((option) => {
        const key = tagKey(group.id, option.id);
        valid.add(key);
        labels.set(key, option.name || option.id);
      }));
      let changed = false;
      Array.from(selected).forEach((key) => {
        if (!valid.has(key)) {
          selected.delete(key);
          changed = true;
        }
      });
      renderGroups();
      updateCount();
      if (changed) options.onChange();
    }

    function renderGroups() {
      groupsContainer.replaceChildren();
      openButton.hidden = groups.length === 0;
      if (!groups.length) {
        const empty = document.createElement("p");
        empty.className = "muted";
        empty.textContent = text("catalog.noFilters", "暂无可用筛选器。");
        groupsContainer.appendChild(empty);
        return;
      }
      groups.forEach((group) => {
        const details = document.createElement("details");
        details.className = "catalog-filter-group";
        details.open = true;
        const summary = document.createElement("summary");
        const title = document.createElement("span");
        title.textContent = group.name || group.id;
        summary.append(title, chevronIcon());
        const optionList = document.createElement("div");
        optionList.className = "catalog-filter-options";
        (group.options || []).forEach((option) => {
          optionList.appendChild(filterOption(group, option));
        });
        details.append(summary, optionList);
        groupsContainer.appendChild(details);
      });
    }

    function filterOption(group, option) {
      const label = document.createElement("label");
      label.className = "catalog-filter-option";
      const input = document.createElement("input");
      const key = tagKey(group.id, option.id);
      input.type = "checkbox";
      input.checked = selected.has(key);
      input.dataset.filterGroup = group.id;
      input.dataset.filterOption = option.id;
      input.addEventListener("change", () => {
        if (input.checked) selected.add(key);
        else selected.delete(key);
        updateCount();
        options.onChange();
      });
      const text = document.createElement("span");
      text.textContent = option.name || option.id;
      label.append(input, text);
      return label;
    }

    function updateCount() {
      count.textContent = String(selected.size);
      count.hidden = selected.size === 0;
      clearButton.disabled = selected.size === 0;
      openButton.setAttribute("aria-label", selected.size ?
        text("catalog.filterCount", "筛选器，已选择 " + selected.size + " 项", {count: selected.size}) :
        text("catalog.filter", "筛选器"));
    }

    function openPanel() {
      if (open) return;
      open = true;
      previousFocus = document.activeElement;
      panel.dataset.open = "true";
      panel.setAttribute("aria-hidden", "false");
      backdrop.hidden = false;
      openButton.setAttribute("aria-expanded", "true");
      document.body.classList.add("catalog-filter-open");
      if (!historyEntry) {
        history.pushState({catalogFilters: true}, "");
        historyEntry = true;
      }
      closeButton.focus();
    }

    function closePanel(useHistory) {
      if (!open) return;
      open = false;
      panel.dataset.open = "false";
      panel.setAttribute("aria-hidden", "true");
      backdrop.hidden = true;
      openButton.setAttribute("aria-expanded", "false");
      document.body.classList.remove("catalog-filter-open");
      if (useHistory && historyEntry) {
        historyEntry = false;
        history.back();
      } else {
        historyEntry = false;
      }
      if (previousFocus && previousFocus.focus) previousFocus.focus();
    }

    function syncPanelAccessibility(desktop) {
      if (desktop) panel.removeAttribute("aria-hidden");
      else if (!open) panel.setAttribute("aria-hidden", "true");
    }

    function trapFocus(event) {
      const focusable = Array.from(panel.querySelectorAll(
        "button:not([disabled]), input:not([disabled]), summary, [tabindex]:not([tabindex='-1'])"));
      if (!focusable.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    function clearSelections() {
      if (!selected.size) return false;
      selected.clear();
      renderGroups();
      updateCount();
      return true;
    }

    return {
      clearSelections: clearSelections,
      search: () => (searches[0] ? searches[0].value.trim() : ""),
      selectedFilters: () => Array.from(selected).sort().map((key) => key.replace("\u0000", ":")),
      selectedTags: () => new Set(selected),
      setGroups: setGroups,
      tagLabels: () => new Map(labels)
    };
  }

  function tagKey(group, option) {
    return String(group || "").toLowerCase() + "\u0000" +
      String(option || "").toLowerCase();
  }

  function chevronIcon() {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("aria-hidden", "true");
    const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    const base = window.MirrorStatic && window.MirrorStatic["icons.svg"] ||
      "/static/public/icons.svg";
    use.setAttribute("href", base + "#chevron-down");
    svg.appendChild(use);
    return svg;
  }

  window.DownloadFilters = {create: create};
})();
