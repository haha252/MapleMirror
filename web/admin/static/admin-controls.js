(function () {
  function enhanceControls(root) {
    root = root || document;
    root.querySelectorAll("input, select, textarea").forEach(function (el) {
      var type = String(el.getAttribute("type") || "").toLowerCase();
      if (type === "checkbox" || type === "radio" || type === "hidden") return;
      el.classList.add("admin-control");
      if (el.tagName === "SELECT") el.classList.add("admin-select");
    });
  }

  enhanceControls(document);
  if (!("MutationObserver" in window)) return;
  new MutationObserver(function (items) {
    items.forEach(function (item) {
      item.addedNodes.forEach(function (node) {
        if (node.nodeType !== 1) return;
        if (node.matches && node.matches("input, select, textarea")) {
          enhanceControls(node.parentNode || document);
        } else {
          enhanceControls(node);
        }
      });
    });
  }).observe(document.body, { childList: true, subtree: true });
})();
