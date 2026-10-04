(function () {
  if (!window.adminNodeInspection) return;
  var panel = document.getElementById("nodes-detail-panel"), backdrop = document.getElementById("nodes-detail-backdrop");
  var media = window.matchMedia("(max-width: 900px)"), selected = "", wasModal = false, returnFocus;
  var blocked = [document.querySelector(".site-header"), document.querySelector(".page-title-row"),
    document.querySelector(".nodes-metrics"), document.querySelector(".nodes-list-panel"), document.getElementById("pairing-toast")];
  function update() {
    var modal = !!selected && media.matches;
    panel.classList.toggle("is-open", !!selected);
    backdrop.hidden = !modal;
    panel.setAttribute("role", modal ? "dialog" : "region");
    if (modal) panel.setAttribute("aria-modal", "true");
    else panel.removeAttribute("aria-modal");
    blocked.forEach(function (el) { if (el) el.inert = modal; });
    if (modal && !wasModal) document.getElementById("nodes-detail-close").focus();
    if (!modal && wasModal && returnFocus && document.contains(returnFocus)) returnFocus.focus();
    wasModal = modal;
  }
  function show(id) {
    if (id !== selected) document.getElementById("nodes-detail-scroll").scrollTop = 0;
    if (id && !selected) returnFocus = document.activeElement;
    selected = id;
    document.getElementById("nodes-detail-empty").hidden = !!id;
    document.getElementById("nodes-detail-head").hidden = !id;
    document.getElementById("nodes-detail-scroll").hidden = !id;
    update();
  }
  if (media.addEventListener) media.addEventListener("change", update);
  else media.addListener(update);
  document.addEventListener("keydown", function (event) {
    if (!selected || !media.matches) return;
    if (!document.getElementById("admin-modal").hidden || !document.getElementById("admin-info-modal").hidden) return;
    if (event.key === "Escape") { event.preventDefault(); document.getElementById("nodes-detail-close").click(); }
    if (event.key !== "Tab") return;
    var controls = Array.from(panel.querySelectorAll('button:not(:disabled), a[href], summary')).filter(function (el) { return el.getClientRects().length; });
    if (!controls.length) return;
    var first = controls[0], last = controls[controls.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  });
  function sizeHeader() {
    var header = document.querySelector(".site-header");
    document.body.style.setProperty("--nodes-top-offset", Math.ceil(header.getBoundingClientRect().bottom) + 12 + "px");
  }
  sizeHeader(); window.addEventListener("resize", sizeHeader);
  if (window.ResizeObserver) new ResizeObserver(sizeHeader).observe(document.querySelector(".site-header"));
  window.adminNodeWindow = {show: show};
})();
