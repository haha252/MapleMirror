(function () {
  var a = window.admin;
  if (!a) return;

  function detailItem(label, value) {
    return '<div class="admin-record__detail-item"><span>' + a.esc(label) +
      '</span><strong>' + a.esc(value) + '</strong></div>';
  }

  function systemLabel(value) {
    var key = String(value || "").toLowerCase();
    if (key === "win" || key === "windows") return "Windows";
    if (key === "linux") return "Linux";
    if (key === "mac" || key === "macos" || key === "darwin") return "macOS";
    if (key === "android") return "Android";
    return value || "";
  }

  function scanCard(scan, open) {
    var name = scan.project_name || scan.project_id;
    var state = a.scanStateLabel(scan.last_scan_state);
    var failed = scan.last_scan_state === "failed" || !!scan.last_error_message;
    var next = scan.enabled ? (scan.next_scan_at || "待安排") : "已停止自动扫描";
    return '<article class="admin-record" data-scan-card="' + a.esc(scan.project_id) + '">' +
      '<div class="admin-record__summary"><div class="admin-record__identity"><strong>' +
      a.esc(name) + '</strong><span class="sub">' + a.esc(next) + '</span></div>' +
      '<div class="admin-record__state">' + a.badge(scan.enabled ? "已启用" : "已禁用") +
      a.badge(state) + '</div><div class="admin-record__actions">' +
      '<button class="admin-secondary" data-scan-detail="' + a.esc(scan.project_id) +
      '" aria-expanded="' + open + '">' + (open ? "收起" : "查看详情") + '</button>' +
      '<button class="admin-secondary" data-scan-run="' + a.esc(scan.project_id) +
      '"' + (scan.enabled ? "" : " disabled") + '>立即扫描</button></div></div>' +
      (failed ? '<p class="admin-record__message admin-record__message--bad">' +
        a.esc(scan.last_error_message || "上次扫描失败") + '</p>' : "") +
      '<div class="admin-record__detail" data-scan-detail-box' + (open ? "" : " hidden") + '></div></article>';
  }

  function taskSubtitle(task) {
    var parts = [];
    if (task.project_name || task.project_id) parts.push(task.project_name || task.project_id);
    if (task.version) parts.push(task.version);
    var platform = [systemLabel(task.system), task.architecture].filter(Boolean).join(" ");
    if (platform) parts.push(platform);
    if (Number(task.size_bytes || 0) > 0) parts.push(a.bytes(task.size_bytes));
    return parts.join(" · ") || a.taskTypeLabel(task.task_type);
  }

  function taskDetail(task) {
    return '<div class="admin-record__detail-grid">' +
      detailItem("操作", a.taskTypeLabel(task.task_type)) +
      detailItem("目标节点", task.node_name || task.node_id || "") +
      detailItem("项目", task.project_name || task.project_id || "非文件任务") +
      detailItem("版本", task.version || "—") +
      detailItem("平台", [systemLabel(task.system), task.architecture].filter(Boolean).join(" ") || "—") +
      detailItem("创建时间", task.created_at || "—") +
      detailItem("更新时间", task.updated_at || "—") +
      detailItem("下次重试", task.retry_after || "—") + '</div>' +
      (task.error_message ? '<p class="admin-record__message admin-record__message--bad">' +
        a.esc(task.error_message) + '</p>' : "") +
      '<details class="admin-tech-details"><summary>显示技术信息</summary>' +
      a.compactKv({"Task ID": task.task_id || "", "Asset ID": task.asset_id || "",
        "Request ID": task.request_id || "", "Lease 到期": task.lease_expires_at || "",
        "完成时间": task.completed_at || ""}, "detail-plain") + '</details>';
  }

  function taskCard(task, open) {
    var title = task.file_name || a.taskTypeLabel(task.task_type);
    var state = a.taskStateLabel(task.state);
    var canRetry = task.state === "failed" || task.state === "retry_wait" ||
      (task.state === "cancelled" && task.error_message === "管理员取消");
    var canCancel = ["pending", "sent", "running", "retry_wait", "failed"].indexOf(task.state) >= 0;
    var attempts = Number(task.attempts || 0);
    return '<article class="admin-record" data-task-card="' + a.esc(task.task_id) + '">' +
      '<div class="admin-record__summary"><div class="admin-record__identity"><strong>' +
      a.esc(title) + '</strong><span class="sub">' + a.esc(taskSubtitle(task)) + '</span></div>' +
      '<div class="admin-record__state">' + a.badge(state) +
      (attempts ? '<span class="admin-record__meta">已尝试 ' + a.esc(attempts) + ' 次</span>' : "") +
      '</div><div class="admin-record__actions"><button class="admin-secondary" data-task-detail="' +
      a.esc(task.task_id) + '" aria-expanded="' + open + '">' + (open ? "收起" : "查看详情") + '</button>' +
      (canRetry ? '<button class="admin-secondary" data-task-action="retry" data-task="' +
        a.esc(task.task_id) + '">重试</button>' : "") +
      (canCancel ? '<button class="admin-secondary" data-task-action="cancel" data-task="' +
        a.esc(task.task_id) + '">取消</button>' : "") + '</div></div>' +
      (task.state === "failed" && task.error_message ? '<p class="admin-record__message admin-record__message--bad sync-error-preview">' +
        a.esc(task.error_message) + '</p>' : "") +
      '<div class="admin-record__detail" data-task-detail-box' + (open ? "" : " hidden") + '>' +
      (open ? taskDetail(task) : "") + '</div></article>';
  }

  window.adminSyncCards = {
    detailItem: detailItem,
    scanCard: scanCard,
    taskCard: taskCard,
    taskDetail: taskDetail
  };
})();
