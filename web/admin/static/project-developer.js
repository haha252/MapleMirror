(function () {
  var a = window.admin;
  window.adminProjectDeveloper = function (getProject, isSelected) {
    var revealedTokens = {};
  function developerPanel(id) {
    var panels = document.querySelectorAll("[data-developer-panel]");
    for (var i = 0; i < panels.length; i++) {
      if (panels[i].getAttribute("data-developer-panel") === id) return panels[i];
    }
    return null;
  }

  function loadDeveloperInfo(id) {
    var panel = developerPanel(id);
    if (!panel || panel.hidden) return;
    panel.innerHTML = '<p class="muted">正在读取 Developer API 信息...</p>';
    a.api("/admin/api/projects/" + encodeURIComponent(id) + "/developer-api")
      .then(function (info) { if (isSelected(id)) renderDeveloperInfo(id, info); })
      .catch(function (err) {
        if (isSelected(id) && panel) panel.innerHTML = '<p class="muted">' + a.esc(err.message) + '</p>';
      });
  }

  function renderDeveloperInfo(id, info) {
    var panel = developerPanel(id);
    if (!panel) return;
    var project = getProject(id);
    var token = revealedTokens[id] || "", placeholder = token || "<YOUR_TOKEN>";
    var curl = 'curl -X POST -H "Authorization: Bearer ' + placeholder + '" "' + (info.endpoint || "") + '"';
    var state = info.token_configured ? "已启用 · " + (info.token_prefix || "") + "••••" : "尚未生成 Token";
    var disabled = project && !project.Enabled ?
      '<p class="project-developer__notice">项目当前已禁用：Token 会保留，但外部更新 API 暂停接受触发。</p>' : "";
    var reveal = token ? '<div class="project-developer__token"><span>本次生成的完整 Token（服务器不会再次返回）</span><code>' +
      a.esc(token) + '</code><button class="admin-secondary" data-copy-value="' + a.esc(token) + '">复制 Token</button></div>' : "";
    panel.innerHTML = '<div class="project-developer__head"><strong>Developer Sync API</strong><span>' +
      a.esc(state) + '</span></div><div class="project-developer__endpoint"><span>POST</span><code>' +
      a.esc(info.endpoint || "") + '</code></div><div class="project-developer__usage"><span>今日使用 <strong>' +
      a.esc(info.used_today || 0) + " / " + a.esc(info.daily_limit || 100) + '</strong></span><span>剩余 <strong>' +
      a.esc(info.remaining_today == null ? 100 : info.remaining_today) + '</strong></span><span>最近调用 <strong>' +
      a.esc(info.last_used_at || "暂无") + '</strong></span></div>' + disabled + reveal +
      '<div class="admin-actions project-developer__actions"><button class="admin-secondary" data-copy-value="' +
      a.esc(info.endpoint || "") + '">复制 API</button><button class="admin-secondary" data-copy-value="' +
      a.esc(curl) + '">复制调用示例</button><button class="admin-secondary ' +
      (info.token_configured ? "admin-danger" : "admin-primary") + '" data-developer-token="' + a.esc(id) +
      '" data-token-exists="' + (!!info.token_configured) + '">' +
      (info.token_configured ? "重置 Token" : "生成 Token") + '</button></div>';
  }

  function rotateDeveloperToken(id, exists) {
    var run = function () {
      a.api("/admin/api/projects/" + encodeURIComponent(id) + "/developer-api/token", {method: "POST", body: "{}"})
        .then(function (data) {
          revealedTokens[id] = data.token || "";
          renderDeveloperInfo(id, data.developer_api || {});
          a.setStatus("Developer API Token 已生成，请立即复制并妥善保存");
        }).catch(function (err) { a.setStatus(err.message); });
    };
    if (!exists) return run();
    a.confirmAction("重置 Developer API Token",
      "旧 Token 将立即失效，使用旧 Token 的 GitHub Actions 或 CI 必须同步更新。确认重置？", run);
  }

  function copyValue(value) {
    if (!navigator.clipboard) return a.setStatus("复制失败，请手动复制");
    navigator.clipboard.writeText(value || "").then(function () {
      a.setStatus("已复制到剪贴板");
    }).catch(function () { a.setStatus("复制失败，请手动复制"); });
  }

  return {panel: developerPanel, load: loadDeveloperInfo, rotate: rotateDeveloperToken, copy: copyValue};
  };
})();
