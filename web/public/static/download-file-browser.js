(function () {
  function optionButton(className, label, selected) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = className;
    button.setAttribute("role", "option");
    button.setAttribute("aria-selected", String(selected));
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
    let currentAsset = null;

    function renderVersions() {
      versionsBox.innerHTML = "";
      versions.forEach((version) => {
        const button = optionButton("file-browser__version", "", version === currentVersion);
        const label = document.createElement("span");
        label.textContent = version;
        button.appendChild(folderIcon(root.dataset.folderIcon));
        button.appendChild(label);
        button.addEventListener("click", function () { openVersion(version); });
        versionsBox.appendChild(button);
      });
    }

    function fileMeta(item) {
      const status = item.available ? "可下载" : "暂不可下载";
      return options.bytesText(item.size_bytes) + " · " + status;
    }

    function renderFiles(preferred) {
      const files = items.filter((item) => item.version === currentVersion);
      const choice = files.includes(preferred) ? preferred : options.preferredAsset(files);
      filesBox.innerHTML = "";
      files.forEach((item) => {
        const button = optionButton("file-browser__file", "", item === choice);
        const name = document.createElement("span");
        name.className = "file-browser__file-name";
        name.textContent = item.file_name || "未命名文件";
        const meta = document.createElement("span");
        meta.className = "file-browser__file-meta " + (item.available ? "muted" : "warn");
        meta.textContent = fileMeta(item);
        button.appendChild(name);
        button.appendChild(meta);
        button.addEventListener("click", function () { selectFile(item); });
        filesBox.appendChild(button);
      });
      if (!files.length) {
        const empty = document.createElement("p");
        empty.className = "file-browser__empty muted";
        empty.textContent = "该版本暂无文件";
        filesBox.appendChild(empty);
      }
      currentAsset = choice;
      options.onSelect(choice);
    }

    function openVersion(version) {
      currentVersion = versions.includes(version) ? version : versions[0] || "";
      const preferred = currentAsset && currentAsset.version === currentVersion ? currentAsset : null;
      renderVersions();
      versionsView.hidden = true;
      filesView.hidden = false;
      pathText.textContent = "版本 / " + currentVersion;
      options.onVersion(currentVersion);
      renderFiles(preferred);
    }

    function showVersions(asset, version) {
      currentAsset = asset || currentAsset;
      currentVersion = asset ? asset.version : version || currentVersion;
      renderVersions();
      versionsView.hidden = false;
      filesView.hidden = true;
    }

    function selectFile(item) {
      if (!item || item.version !== currentVersion) return;
      renderFiles(item);
    }

    backButton.addEventListener("click", function () {
      showVersions(currentAsset, currentVersion);
    });
    showVersions(options.asset || null, options.version || "");
    return {
      showVersions: showVersions
    };
  }

  window.DownloadFileBrowser = {create: create};
})();
