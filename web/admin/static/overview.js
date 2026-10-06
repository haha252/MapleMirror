(function () {
  var a = window.admin, w = window.adminWorkspace;
  if (!a || !w || a.page() !== "overview") return;
  var nodes = [], scans = [], offset = 0, expiry = 90000, sampledAt = 0;
  function online(n) { return n.state !== "disabled" && ["online", "syncing", "ready"].indexOf(n.connection_state || n.state) >= 0; }
  function fresh(n) { return online(n) && Number(n.pressure_reported_unix_ms) > 0 && Date.now() + offset - Number(n.pressure_reported_unix_ms) <= expiry; }
  function traffic(n) {
    var sample = (n.pressure || {}).mirror_traffic;
    if (!fresh(n) || !sample || !(Number(sample.window_seconds) > 0)) return null;
    var download = Number(sample.public_bandwidth_bps), swarm = Number(sample.swarm_bandwidth_bps);
    return isFinite(download) && isFinite(swarm) && download >= 0 && swarm >= 0 ? download + swarm : null;
  }
  function lowDisk(n) {
    if (!fresh(n)) return false;
    var p = n.pressure || {};
    return [p.asset_fs, p.partial_fs].some(function (fs) {
      if (!fs || !fs.valid) return false;
      var free = Math.max(0, Number(fs.available_bytes) - Number(fs.reserved_bytes || 0));
      return free < 5 * 1073741824 || Number(fs.total_bytes) > 0 && free / Number(fs.total_bytes) < .05;
    }) || !p.asset_fs && p.free_bytes !== undefined && Number(p.free_bytes) < 5 * 1073741824;
  }
  function attention(n) {
    var sync = n.sync || {}, p = n.pressure || {};
    return n.state !== "disabled" && (!online(n) || n.download_ready === false || !fresh(n) || lowDisk(n) || p.throughput_limited || Number(sync.failed_tasks) > 0 || Number(sync.mismatched_assets) > 0);
  }
  function render() {
    var active = nodes.filter(online), samples = active.map(traffic).filter(function (n) { return n !== null; });
    a.text("metric-online", active.length + " / " + nodes.length);
    a.text("metric-download", nodes.filter(function (n) { return n.download_ready === true; }).length);
    a.text("metric-throughput", samples.length ? (samples.reduce(function (v, n) { return v + n; }, 0) / 1048576).toFixed(2) + " MiB/s" : "— MiB/s");
    document.getElementById("metric-throughput").title = "公网下载 + Swarm 上传；有效样本 " + samples.length + " / " + active.length + " 个在线节点";
    var problems = nodes.filter(attention), failed = scans.filter(function (s) { return s.last_scan_state === "failed" || s.last_error_message; });
    a.text("metric-attention", problems.length + failed.length);
    w.list("overview-nodes", nodes, function (n) { return n.node_id; }, function (n) {
      var rate = traffic(n);
      return '<span><strong>' + a.esc(n.public_name || n.node_id) + '</strong><span class="sub">' +
        a.esc(n.download_ready === true ? "下载可用" : n.state === "disabled" ? "不参与调度" : "下载不可用或未知") + '</span></span><span>' +
        a.badge(online(n) ? "在线" : n.state === "disabled" ? "已禁用" : "离线") + '</span><span class="ws-row-meta"><strong>' +
        a.esc(rate === null ? "暂无有效采样" : (rate / 1048576).toFixed(2) + " MiB/s") + '</strong><span class="sub">' + a.esc(n.last_heartbeat_at || "暂无心跳") + '</span></span>';
    }, "");
    w.html("overview-sync", [["扫描中", scans.filter(function (s) { return s.last_scan_state === "running"; }).length], ["扫描失败", failed.length],
      ["未完成节点任务", nodes.reduce(function (v, n) { return v + ["pending_tasks", "sent_tasks", "running_tasks", "retry_wait_tasks", "failed_tasks"].reduce(function (count, field) { return count + Number((n.sync || {})[field] || 0); }, 0); }, 0)],
      ["同步失败任务", nodes.reduce(function (v, n) { return v + Number((n.sync || {}).failed_tasks || 0); }, 0)]].map(function (r) {
        return '<div><span>' + a.esc(r[0]) + '</span><strong>' + r[1] + '</strong></div>';
      }).join(""));
    var items = problems.map(function (n) { return notice(n.public_name || n.node_id, !online(n) ? "节点离线" : !fresh(n) ? "运行报告过期或缺失" : lowDisk(n) ? "可用磁盘空间不足" : n.pressure && n.pressure.throughput_limited ? "吞吐受限" : "下载准备或同步状态需要检查", "/admin/nodes"); })
      .concat(failed.map(function (s) { return notice(s.project_name || s.project_id, s.last_error_message || "扫描失败", "/admin/sync"); }));
    w.detail("待处理事项", "", (items.length ? items.join("") : '<p class="ws-note">当前没有需要处理的问题。</p>') +
      '<p class="muted">镜像吞吐仅统计公网下载与 Swarm 上传。' + (samples.length < active.length ? '部分在线节点缺少有效采样，当前为部分合计。' : '主机其他网络流量不计入。') + '</p>');
  }
  function notice(name, text, href) { return '<a class="ws-attention" href="' + href + '">' + a.badge("需要关注") + '<h3>' + a.esc(name) + '</h3><p>' + a.esc(text) + '</p></a>'; }
  function load() {
    var requests = [w.read("/admin/api/nodes?inspection=1").then(function (data) {
      nodes = data.nodes || []; offset = Number(data.server_now_unix_ms || Date.now()) - Date.now(); expiry = Number(data.report_stale_after_ms) || 90000;
    })];
    if (Date.now() - sampledAt >= 30000) requests.push(w.read("/admin/api/overview").then(function (data) {
      sampledAt = Date.now(); scans = data.scans || []; var stats = data.stats || {};
      a.text("metric-daily", a.bytes(stats.daily_sent_bytes));
    }));
    return Promise.all(requests).then(function () { render(); w.updated("overview-updated"); });
  }
  document.getElementById("overview-nodes").onclick = function (event) { if (event.target.closest("[data-ws-key]")) location.href = "/admin/nodes"; };
  document.getElementById("overview-open-attention").onclick = w.open;
  var refresh = w.poll(load, 1000);
  document.getElementById("workspace-refresh-now").onclick = function () { sampledAt = 0; refresh(); };
})();
