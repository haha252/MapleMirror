(function () {
  if (typeof ResizeObserver !== "function" ||
      typeof MutationObserver !== "function") return;
  document.querySelectorAll("[data-project-grid]").forEach(setupGrid);

  function setupGrid(container) {
    const assignments = new Map();
    const observedCards = new Set();
    let columnCount = 0;
    let scheduledFrame = 0;

    function projectCards() {
      return Array.from(container.children)
        .filter((item) => item.classList.contains("project-card"));
    }

    function cssNumber(styles, name, fallback) {
      const value = Number.parseFloat(styles.getPropertyValue(name));
      return Number.isFinite(value) ? value : fallback;
    }

    function shortestColumn(heights) {
      let best = 0;
      for (let index = 1; index < heights.length; index += 1) {
        if (heights[index] < heights[best]) best = index;
      }
      return best;
    }

    function layout() {
      scheduledFrame = 0;
      const cards = projectCards();
      if (!cards.length) {
        assignments.clear();
        columnCount = 0;
        container.classList.remove("project-grid--masonry");
        container.style.removeProperty("height");
        return;
      }
      const styles = getComputedStyle(container);
      const nextColumns = Math.max(1,
        Math.round(cssNumber(styles, "--project-grid-columns", 1)));
      const gap = Math.max(0, cssNumber(styles, "--project-grid-gap", 16));
      const availableWidth = container.clientWidth;
      if (availableWidth <= 0) return;
      if (nextColumns !== columnCount) {
        assignments.clear();
        columnCount = nextColumns;
      }
      const cardWidth = Math.max(0,
        (availableWidth - gap * (columnCount - 1)) / columnCount);
      cards.forEach((card) => { card.style.width = cardWidth + "px"; });
      const heights = Array(columnCount).fill(0);
      cards.forEach((card) => {
        let column = assignments.get(card);
        if (!Number.isInteger(column) || column >= columnCount) {
          column = shortestColumn(heights);
          assignments.set(card, column);
        }
        const top = heights[column];
        card.style.left = column * (cardWidth + gap) + "px";
        card.style.top = top + "px";
        heights[column] = top + card.getBoundingClientRect().height + gap;
      });
      container.style.height = Math.max(0, Math.max(...heights) - gap) + "px";
      container.classList.add("project-grid--masonry");
    }

    function scheduleLayout() {
      if (!scheduledFrame) scheduledFrame = requestAnimationFrame(layout);
    }

    const resizeObserver = new ResizeObserver(scheduleLayout);
    resizeObserver.observe(container);

    function observeCard(card) {
      if (!card.classList.contains("project-card") || observedCards.has(card)) return;
      observedCards.add(card);
      resizeObserver.observe(card);
    }

    function forgetCard(card) {
      if (!observedCards.delete(card)) return;
      resizeObserver.unobserve(card);
      assignments.delete(card);
    }

    projectCards().forEach(observeCard);
    new MutationObserver((records) => {
      records.forEach((record) => {
        record.removedNodes.forEach((item) => {
          if (item.nodeType === Node.ELEMENT_NODE) forgetCard(item);
        });
        record.addedNodes.forEach((item) => {
          if (item.nodeType === Node.ELEMENT_NODE) observeCard(item);
        });
      });
      scheduleLayout();
    }).observe(container, {childList: true});
    window.addEventListener("resize", scheduleLayout);
    scheduleLayout();
  }
})();
