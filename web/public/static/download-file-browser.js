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

  function create(root, items, options) {
    const versionsBox = root.querySelector(".file-browser__versions");
    const filesBox = root.querySelector(".file-browser__files");
    const versions = options.uniqueVersions(items);
    let currentVersion = "";
    let currentAsset = null;

    function renderVersions() {
      versionsBox.innerHTML = "";
      versions.forEach((version) => {
        const button = optionButton("file-browser__version", version, version === currentVersion);
        button.addEventListener("click", function () { setVersion(version, currentAsset); });
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

    function setVersion(version, preferred) {
      currentVersion = versions.includes(version) ? version : versions[0] || "";
      renderVersions();
      options.onVersion(currentVersion);
      renderFiles(preferred);
    }

    function selectFile(item) {
      if (!item || item.version !== currentVersion) return;
      renderFiles(item);
    }

    setVersion(options.version || "", options.asset || null);
    return {
      select: function (asset, version) {
        setVersion(asset ? asset.version : version, asset || null);
      }
    };
  }

  window.DownloadFileBrowser = {create: create};
})();
