(function () {
  var a = window.admin;
  if (!a || a.page() !== "nodes") return;
  var clockOffset = 0, reportExpiry = 90000;
  function html(id, value) {
    var el = document.getElementById(id);
    if (el && el._nodeMarkup !== value) { el.innerHTML = value; el._nodeMarkup = value; }
  }
  function state(node) {
    if (node.state === "disabled") return "disabled";
    return String(node.connection_state || node.state || "unknown").toLowerCase();
  }
  function online(node) { return ["online", "syncing", "ready"].indexOf(state(node)) >= 0; }
  function label(node) {
    return online(node) ? "在线" : state(node) === "offline" ? "离线" : state(node) === "disabled" ? "已禁用" : "状态未知";
  }
  function badge(text, kind) { return '<span class="admin-badge admin-badge--' + kind + '">' + a.esc(text) + '</span>'; }
  function timestamp(value) { return Number(value) || 0; }
  function fresh(node) {
    var at = timestamp(node.pressure_reported_unix_ms);
    return online(node) && at > 0 && Date.now() + clockOffset - at <= reportExpiry;
  }
  function ago(value) {
    var at = timestamp(value);
    if (!at) return "暂无上报";
    var seconds = Math.max(0, Math.floor((Date.now() + clockOffset - at) / 1000));
    if (seconds < 60) return seconds + " 秒前";
    if (seconds < 3600) return Math.floor(seconds / 60) + " 分钟前";
    if (seconds < 86400) return Math.floor(seconds / 3600) + " 小时前";
    return Math.floor(seconds / 86400) + " 天前";
  }
  function disk(node, partial) {
    var p = node.pressure || {}, fs = partial ? p.partial_fs : p.asset_fs;
    if (fs && fs.valid) {
      var available = Math.max(0, Number(fs.available_bytes) - Number(fs.reserved_bytes || 0));
      return {available: available, total: Number(fs.total_bytes) || 0, reserved: Number(fs.reserved_bytes) || 0};
    }
    // Older reports omit filesystem validity; zero free_bytes is still a valid sample.
    if (!partial && !fs && p.free_bytes !== undefined) return {available: Math.max(0, Number(p.free_bytes)), total: 0, reserved: 0};
    return null;
  }
  function lowDisk(node) {
    if (!fresh(node)) return false;
    return [disk(node), disk(node, true)].some(function (d) {
      return d && (d.available < 5 * 1024 * 1024 * 1024 || d.total > 0 && d.available / d.total < .05);
    });
  }
  function attention(node) {
    if (state(node) === "disabled") return false;
    var sync = node.sync || {};
    return !online(node) || node.download_ready === false || !fresh(node) || lowDisk(node) ||
      !!(node.pressure || {}).throughput_limited || Number(sync.failed_tasks) > 0 || Number(sync.mismatched_assets) > 0;
  }
  function ratio(node) {
    var p = node.pressure || {}, target = Number(p.target_bandwidth_bps || node.target_bandwidth_bps || 0);
    if (!fresh(node) || p.actual_bandwidth_bps === undefined || target <= 0) return null;
    return Number(p.actual_bandwidth_bps) / target;
  }
  function throughput(node) {
    var p = node.pressure || {}, sample = p.download_pressure;
    if (!fresh(node)) return "暂无有效报告";
    if (p.throughput_limited) return "受限，已降低调度权重";
    if (!sample) return "样本不足（暂无吞吐采样）";
    var at = Date.parse(sample.sampled_at), age = Date.now() + clockOffset - at;
    if (!isFinite(at) || age > 120000 || age < -5000) return "样本不足（暂无有效采样）";
    if (!(Number(sample.window_seconds) > 0) || !(Number(sample.delivery_bandwidth_bps) > 0) ||
        !(Number(p.target_bandwidth_bps || node.target_bandwidth_bps) > 0)) return "样本不足";
    if (!(Number(sample.observed_peers) >= 2)) return "样本不足（有效下载端少于 2 个）";
    return "未检测到受限";
  }
  function effectiveBandwidth(node) {
    var p = node.pressure || {}, estimate = Number((p.download_pressure || {}).effective_bandwidth_bps);
    return fresh(node) && p.throughput_limited && estimate > 0 ? a.bytes(estimate) + "/s" : "暂无有效估计";
  }
  function mirrorTraffic(node) {
    var sample = (node.pressure || {}).mirror_traffic;
    if (!fresh(node) || !sample || !(Number(sample.window_seconds) > 0)) return null;
    var publicBPS = Number(sample.public_bandwidth_bps), swarmBPS = Number(sample.swarm_bandwidth_bps);
    if (!isFinite(publicBPS) || !isFinite(swarmBPS) || publicBPS < 0 || swarmBPS < 0) return null;
    return {publicBPS: publicBPS, swarmBPS: swarmBPS, totalBPS: publicBPS + swarmBPS};
  }
  function routingRatio(node) {
    var value = (node.pressure || {}).pressure_ratio;
    return fresh(node) && value !== undefined && value !== null && isFinite(Number(value)) ? Number(value) : null;
  }
  function syncLabel(node) {
    if (state(node) === "disabled") return "已禁用";
    if (state(node) === "offline") return "上次库存";
    if (!node.sync) return "同步状态未知";
    if (Number(node.sync.failed_tasks) > 0) return "同步失败";
    if (Number(node.sync.mismatched_assets) > 0) return "文件校验异常";
    return a.syncPhaseLabel(node.sync.sync_phase);
  }
  function progress(node) {
    var sync = node.sync;
    if (!sync) return null;
    var required = Number(sync.required_assets) || 0;
    var verified = Math.max(0, Math.min(required, Number(sync.verified_required_assets !== undefined ?
      sync.verified_required_assets : required - Number(sync.missing_assets || 0))));
    return {required: required, verified: verified, percent: required > 0 ? verified / required * 100 : null};
  }
  function states(node) {
    var kind = online(node) ? "ok" : state(node) === "offline" ? "bad" : "neutral";
    var text = state(node) === "disabled" ? "不参与调度" : node.download_ready === true ? "下载可用" :
      node.download_ready === false ? "下载不可用" : "下载状态未知";
    return badge(label(node), kind) + badge(text, state(node) === "disabled" ? "neutral" : node.download_ready ? "ok" : "warn");
  }
  function progressBar(node) {
    var p = progress(node);
    return p && p.percent !== null ? '<span class="nodes-progress' + (node.routing_ready ? '' : ' nodes-progress--pending') +
      '" role="progressbar" aria-label="文件验证进度" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' +
      p.percent.toFixed(1) + '"><span style="width:' + p.percent.toFixed(1) + '%"></span></span>' : "";
  }
  function diskText(node, partial) { var d = disk(node, partial); return d ? a.bytes(d.available) : "暂无上报"; }
  function read(path) {
    var controller = window.AbortController ? new AbortController() : null;
    var timer = controller ? window.setTimeout(function () { controller.abort(); }, 10000) : null;
    return a.api(path, controller ? {signal: controller.signal} : {}).then(function (data) {
      window.clearTimeout(timer); return data;
    }, function (err) {
      window.clearTimeout(timer);
      throw new Error(err.name === "AbortError" ? "读取超时，请重试" : err.message);
    });
  }
  window.adminNodeInspection = {
    html: html, state: state, online: online, label: label, badge: badge, fresh: fresh, ago: ago,
    disk: disk, lowDisk: lowDisk, attention: attention, ratio: ratio, throughput: throughput,
    effectiveBandwidth: effectiveBandwidth, mirrorTraffic: mirrorTraffic, routingRatio: routingRatio, syncLabel: syncLabel,
    progress: progress, progressBar: progressBar, states: states, diskText: diskText, read: read,
    setClock: function (data) {
      clockOffset = data.server_now_unix_ms ? Number(data.server_now_unix_ms) - Date.now() : 0;
      reportExpiry = Number(data.report_stale_after_ms) || 90000;
    }
  };
})();
