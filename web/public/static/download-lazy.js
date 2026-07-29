(function () {
  function create(batchRows, remainingRows, onVisible) {
    const supported = typeof IntersectionObserver === "function";
    const observed = new Map();
    const sections = new Set();
    const observer = supported ? new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting && observed.has(entry.target)) {
          onVisible(observed.get(entry.target));
        }
      });
    }) : null;
    let resizeFrame = 0;

    function columns(container) {
      const value = Number.parseInt(
        getComputedStyle(container).getPropertyValue("--project-grid-columns"), 10
      );
      return Number.isInteger(value) && value > 0 ? value : 1;
    }

    function batchSize(container) {
      return columns(container) * batchRows;
    }

    function unwatch(section) {
      if (!observer || !section.trigger) return;
      observer.unobserve(section.trigger);
      observed.delete(section.trigger);
      section.trigger = null;
    }

    function watch(section, cards) {
      unwatch(section);
      if (cards) section.batchCards = cards;
      sections.add(section);
      if (!observer || !section.cursor || section.loading || section.failed ||
          !section.batchCards.length) return;
      const triggerRow = batchRows - remainingRows - 1;
      const index = Math.min(
        columns(section.container) * triggerRow, section.batchCards.length - 1
      );
      section.trigger = section.batchCards[index];
      if (section.trigger.getBoundingClientRect().bottom <= 0) {
        onVisible(section);
        return;
      }
      observed.set(section.trigger, section);
      observer.observe(section.trigger);
    }

    window.addEventListener("resize", () => {
      if (resizeFrame) cancelAnimationFrame(resizeFrame);
      resizeFrame = requestAnimationFrame(() => {
        resizeFrame = 0;
        sections.forEach((section) => watch(section));
      });
    });

    return {supported: supported, batchSize: batchSize, watch: watch, unwatch: unwatch};
  }

  window.DownloadLazyLoader = {create: create};
})();
