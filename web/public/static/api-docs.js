(function () {
  "use strict";
  const i18n = window.MirrorI18n;

  function fallbackCopy(text) {
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const copied = document.execCommand("copy");
    area.remove();
    if (!copied) throw new Error("copy command failed");
  }

  async function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return;
    }
    fallbackCopy(text);
  }

  document.querySelectorAll("[data-copy-markdown]").forEach(function (button) {
    button.addEventListener("click", async function () {
      const source = document.getElementById(button.dataset.copyMarkdown);
      const status = document.getElementById(button.getAttribute("aria-describedby"));
      if (!source) return;
      button.disabled = true;
      try {
        await copyText(source.textContent.trim());
        if (status) status.textContent = i18n ? i18n.t("api.copySuccess") : "原理 Markdown 已复制，可直接粘贴给 AI。";
      } catch (error) {
        if (status) status.textContent = i18n ? i18n.t("api.copyFailed") : "复制失败，请手动选择原理内容。";
      } finally {
        window.setTimeout(function () { button.disabled = false; }, 800);
      }
    });
  });
})();
