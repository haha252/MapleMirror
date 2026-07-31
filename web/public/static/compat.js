(function () {
  "use strict";

  function replaceChildren() {
    while (this.firstChild) this.removeChild(this.firstChild);
    for (var index = 0; index < arguments.length; index++) {
      var child = arguments[index];
      this.appendChild(child instanceof Node ? child : document.createTextNode(String(child)));
    }
  }

  function installReplaceChildren(target) {
    if (target && !target.replaceChildren) target.replaceChildren = replaceChildren;
  }

  installReplaceChildren(window.Element && Element.prototype);
  installReplaceChildren(window.Document && Document.prototype);
  installReplaceChildren(window.DocumentFragment && DocumentFragment.prototype);

  function createAbortController() {
    if (typeof window.AbortController === "function") return new window.AbortController();
    return {abort: function () {}, signal: null};
  }

  function withAbortSignal(options, signal) {
    if (signal) options.signal = signal;
    return options;
  }

  function onMediaChange(media, listener) {
    if (!media) return function () {};
    if (typeof media.addEventListener === "function") {
      media.addEventListener("change", listener);
      return function () { media.removeEventListener("change", listener); };
    }
    if (typeof media.addListener === "function") {
      media.addListener(listener);
      return function () { media.removeListener(listener); };
    }
    return function () {};
  }

  window.MirrorCompat = {
    createAbortController: createAbortController,
    onMediaChange: onMediaChange,
    withAbortSignal: withAbortSignal
  };
})();
