(function () {
  function actionButton(className, label) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = className;
    if (label) button.textContent = label;
    return button;
  }

  function folderIcon(href) {
    const namespace = "http://www.w3.org/2000/svg";
    const icon = document.createElementNS(namespace, "svg");
    const use = document.createElementNS(namespace, "use");
    icon.setAttribute("aria-hidden", "true");
    use.setAttribute("href", href);
    icon.appendChild(use);
    return icon;
  }

  function create(root, items, options) {
    const versionsView = root.querySelector(".file-browser__level--versions");
    const filesView = root.querySelector(".file-browser__level--files");
    const versionsBox = root.querySelector(".file-browser__versions");
    const filesBox = root.querySelector(".file-browser__files");
    const backButton = root.querySelector(".file-browser__back");
    const pathText = root.querySelector(".file-browser__path");
    const versions = options.uniqueVersions(items);
    let currentVersion = "";

    function renderVersions() {
      versionsBox.innerHTML = "";
      versions.forEach((version) => {
        const button = actionButton("file-browser__version", "");
        const label = document.createElement("span");
        label.textContent = version;
        button.appendChild(folderIcon(root.dataset.folderIcon));
        button.appendChild(label);
        button.addEventListener("click", function () { openVersion(version); });
        versionsBox.appendChild(button);
      });
    }

    function fileMeta(item) {
      const status = item.available ? "" : " · 暂不可下载";
      return options.bytesText(item.size_bytes) + status;
    }

    function renderFiles() {
      const files = items.filter((item) => item.version === currentVersion);
      const candidate = options.recommendedAsset(files);
      const recommendation = candidate && candidate.available ? candidate : null;
      filesBox.innerHTML = "";
      files.forEach((item) => {
        const recommended = item === recommendation;
        const className = "file-browser__file" +
          (recommended ? " file-browser__file--recommended" : "");
        const button = actionButton(className, "");
        const name = document.createElement("span");
        name.className = "file-browser__file-name";
        name.textContent = item.file_name || "未命名文件";
        if (recommended) {
          const tag = document.createElement("span");
          tag.className = "file-browser__recommendation";
          tag.textContent = "推荐下载";
          button.appendChild(tag);
        }
        const meta = document.createElement("span");
        meta.className = "file-browser__file-meta " + (item.available ? "muted" : "warn");
        meta.textContent = fileMeta(item);
        button.insertBefore(name, button.firstChild);
        button.appendChild(meta);
        button.disabled = !item.available;
        if (item.available) {
          button.addEventListener("click", function () { options.onDownload(item); });
        }
        filesBox.appendChild(button);
      });
      if (!files.length) {
        const empty = document.createElement("p");
        empty.className = "file-browser__empty muted";
        empty.textContent = "该版本暂无文件";
        filesBox.appendChild(empty);
      }
    }

    function openVersion(version) {
      currentVersion = versions.includes(version) ? version : versions[0] || "";
      renderVersions();
      versionsView.hidden = true;
      filesView.hidden = false;
      pathText.textContent = "版本 / " + currentVersion;
      options.onVersion(currentVersion);
      renderFiles();
    }

    function showVersions(asset, version) {
      currentVersion = asset ? asset.version : version || currentVersion;
      renderVersions();
      versionsView.hidden = false;
      filesView.hidden = true;
    }

    backButton.addEventListener("click", function () {
      showVersions(null, currentVersion);
    });
    showVersions(options.asset || null, options.version || "");
    return {
      showVersions: showVersions
    };
  }

  window.DownloadFileBrowser = {create: create};
})();
