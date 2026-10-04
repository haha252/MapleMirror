(function () {
  var a = window.admin, u = window.adminNodeInspection;
  if (!u) return;
  var extras = {}, requests = {}, sla = {};
  function kv(rows) {
    return rows.map(function (row) {
      return '<dt>' + a.esc(row[0]) + '</dt><dd>' + a.esc(row[1]) + '</dd>';
    }).join("");
  }
  function taskMarkup(nodeID, data) {
    if (data.error) return '<p class="nodes-error">任务读取失败：' + a.esc(data.error) + '</p>';
    if (!data.tasks || !data.tasks.length) return '<p class="muted">没有未完成的同步任务</p>';
    return '<div class="nodes-task-list">' + data.tasks.map(function (task) {
      var bad = task.state === "failed", retry = bad || task.state === "retry_wait";
      var attrs = ' data-node="' + a.esc(nodeID) + '" data-task="' + a.esc(task.task_id) + '"';
      return '<article class="nodes-task">' + u.badge(a.taskStateLabel(task.state), bad ? "bad" : "warn") +
        '<strong>' + a.esc(task.file_name || a.taskTypeLabel(task.task_type)) + '</strong>' +
        (task.error_message ? '<p class="' + (bad ? "nodes-error" : "muted") + '">' + a.esc(task.error_message) + '</p>' : "") +
        '<p class="muted">最近变化：' + a.esc(task.updated_at || task.created_at || "暂无") + '</p>' +
        (task.retry_after ? '<p class="muted">下次重试：' + a.esc(task.retry_after) + '</p>' : "") +
        '<div class="nodes-task-actions">' + (retry ? '<button class="admin-secondary" type="button" data-node-task-action="retry"' + attrs + '>重试</button>' : "") +
        '<button class="admin-secondary" type="button" data-node-task-action="cancel"' + attrs + '>取消任务</button></div></article>';
    }).join("") + '</div>';
  }
  function renderSLA(data) {
    if (data.error) return '<span class="nodes-error">SLA 读取失败：' + a.esc(data.error) + '</span>';
    if (!data.windows || !data.windows.length) return '<span class="muted">暂无样本</span>';
    return data.windows.map(function (w) {
      return '<div><span>' + a.esc({"24h": "24 小时", "7d": "7 天", "30d": "30 天"}[w.window] || w.window) +
        '</span><strong>' + (w.insufficient_samples ? "样本不足" : (Number(w.availability_ratio) * 100).toFixed(2) + "%") + '</strong></div>';
    }).join("");
  }
  function render(node) {
    var id = node.node_id, sync = node.sync || {}, pressure = node.pressure || {}, p = u.progress(node);
    a.text("nodes-detail-name", node.public_name || id);
    a.text("nodes-detail-id", id + " · " + a.regionLabel(node.region));
    u.html("nodes-detail-state", u.states(node) + u.badge(u.syncLabel(node), node.routing_ready ? "ok" : "warn"));
    document.getElementById("nodes-management").href = "/admin/nodes/" + encodeURIComponent(id) + "/management";
    document.getElementById("nodes-all-tasks").href = "/admin/sync?node_id=" + encodeURIComponent(id);
    document.querySelectorAll(".nodes-detail-actions [data-node-action]").forEach(function (button) { button.setAttribute("data-node", id); });
    var toggle = document.getElementById("nodes-enable-toggle");
    toggle.textContent = node.state === "disabled" ? "启用节点" : "禁用节点";
    toggle.setAttribute("data-node-action", node.state === "disabled" ? "enable" : "disable");
    var reasons = [];
    if (!node.sync) reasons.push("同步状态暂不可用");
    if (sync.routing_ready_reason) reasons.push(sync.routing_ready_reason);
    if (u.state(node) === "offline") reasons.push("当前不提供下载，下面展示上次上报的运行数据。");
    else if (u.online(node) && node.download_ready === false) reasons.push("当前没有可提供公开下载的副本或入口不可用。");
    if (u.online(node) && !u.fresh(node)) reasons.push("运行报告缺失或已过期，下面的容量数据仅供参考。");
    if (u.lowDisk(node)) reasons.push("磁盘余量不足（低于 5 GiB 或总容量的 5%），请检查容量与预留空间。");
    if (u.fresh(node) && pressure.throughput_limited) reasons.push("检测到吞吐受限，调度权重已降低。");
    var reason = document.getElementById("nodes-detail-reason");
    reason.hidden = !reasons.length; reason.textContent = reasons.join(" ");
    reason.className = "admin-record__message" + (Number(sync.failed_tasks) || Number(sync.mismatched_assets) ? " admin-record__message--bad" : "");
    u.html("nodes-sync-progress", p ? '<div class="nodes-progress-value"><strong>' + p.verified + ' <span>/ ' +
      p.required + '</span></strong><span>' + (p.percent === null ? "暂无目标文件" : p.percent.toFixed(1) + "%") + '</span></div>' +
      u.progressBar(node) : '<p class="muted">同步状态暂不可用</p>');
    u.html("nodes-sync-counts", [["待调度", "pending_tasks"], ["已下发", "sent_tasks"], ["执行中", "running_tasks"],
      ["等待重试", "retry_wait_tasks"], ["失败", "failed_tasks"], ["缺失文件", "missing_assets"]].map(function (row) {
      var count = sync[row[1]];
      return '<div><span>' + row[0] + '</span><strong class="' + (row[1] === "failed_tasks" && count > 0 ? "nodes-error" : "") + '">' +
        (count === undefined ? "—" : a.esc(count)) + '</strong></div>';
    }).join(""));
    a.text("nodes-inventory", !node.sync ? "库存状态未知" : (sync.has_inventory_report ?
      sync.latest_inventory_complete ? "完整库存已上报" : "库存尚未上报完整" : "暂无库存报告") +
      " · " + (sync.active_control_session ? "控制会话正常" : "无活动控制会话"));
    var asset = u.disk(node), partial = u.disk(node, true), ratio = u.ratio(node), usable = u.fresh(node);
    var traffic = u.mirrorTraffic(node), routing = u.routingRatio(node);
    var suffix = usable ? "" : "（上次）";
    var active = pressure.public_active_downloads !== undefined ? pressure.public_active_downloads : pressure.active_downloads;
    u.html("nodes-runtime", kv([
      ["镜像实际吞吐", traffic ? a.bytes(traffic.totalBPS) + "/s" : "暂无镜像采样"],
      ["公网下载吞吐", traffic ? a.bytes(traffic.publicBPS) + "/s" : "暂无镜像采样"],
      ["Swarm 上传吞吐", traffic ? a.bytes(traffic.swarmBPS) + "/s" : "暂无镜像采样"],
      ["机器出口 / 目标带宽", usable ? a.bandwidthText(node) : "暂无有效报告"],
      ["机器带宽占用", ratio === null ? "暂无" : Math.round(ratio * 100) + "%"],
      ["调度压力", routing === null ? "暂无" : Math.round(routing * 100) + "%"],
      ["资产盘可用", asset ? a.bytes(asset.available) + suffix : "暂无上报"],
      ["资产盘预留", asset ? a.bytes(asset.reserved) + suffix : "暂无上报"],
      ["分片盘可用", partial ? a.bytes(partial.available) + suffix : "暂无上报"],
      ["分片盘预留", partial ? a.bytes(partial.reserved) + suffix : "暂无上报"],
      ["公网活动下载", usable && active !== undefined ? active : "暂无有效报告"],
      ["Swarm 活动上传", usable && pressure.swarm_active_uploads !== undefined ? pressure.swarm_active_uploads : "暂无有效报告"],
      ["吞吐状态", u.throughput(node)],
      ["有效带宽估计", u.effectiveBandwidth(node)],
      ["最近心跳", u.ago(node.last_heartbeat_unix_ms)], ["最近报告", u.ago(node.pressure_reported_unix_ms)],
      ["下载优先级", node.download_priority === undefined ? "暂无" : node.download_priority]
    ]));
    u.html("nodes-tech", kv([["Node ID", id], ["软件版本", node.software_version || "未知"],
      ["Control 协议", node.control_protocol || "未知"], ["库存 revision", sync.latest_inventory_revision === undefined ? "暂无" : sync.latest_inventory_revision],
      ["路由诊断", sync.routing_ready_detail || "暂无"], ["最近心跳时间", node.last_heartbeat_at || "暂无"],
      ["最近报告时间", pressure.reported_at || "暂无"]]));
    var data = extras[id];
    u.html("nodes-detail-tasks", data ? taskMarkup(id, data.tasks) : '<p class="muted">正在读取任务…</p>');
    u.html("nodes-detail-sla", sla[id] ? renderSLA(sla[id].data) : '<span class="muted">正在读取…</span>');
  }
  function load(nodeID) {
    if (requests[nodeID]) return requests[nodeID];
    var base = "/admin/api/nodes/" + encodeURIComponent(nodeID);
    var slaRequest = sla[nodeID] && sla[nodeID].expires > Date.now() ? Promise.resolve() :
      u.read(base + "/sla").then(function (data) { sla[nodeID] = {data: data, expires: Date.now() + 60000}; }, function (err) {
        sla[nodeID] = {data: {error: err.message}, expires: Date.now() + 5000};
      });
    requests[nodeID] = Promise.all([
      u.read("/admin/api/sync/tasks?node_id=" + encodeURIComponent(nodeID) + "&active=1&page_size=5")
        .then(function (data) { extras[nodeID] = {tasks: data}; }, function (err) {
          extras[nodeID] = {tasks: {error: err.message}};
        }), slaRequest
    ]).then(function () { delete requests[nodeID]; });
    return requests[nodeID];
  }
  window.adminNodeDetail = {render: render, load: load};
})();
