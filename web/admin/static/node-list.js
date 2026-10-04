(function () {
  var a = window.admin, u = window.adminNodeInspection;
  if (!u) return;
  var list = document.getElementById("nodes-list"), rows = {}, order = [], items = [];
  function matches(node) {
    var filter = document.getElementById("nodes-filter").value;
    var region = document.getElementById("nodes-region").value;
    var search = document.getElementById("nodes-search").value.trim().toLowerCase();
    if (search && (node.public_name + " " + node.node_id).toLowerCase().indexOf(search) < 0) return false;
    if (region !== "all" && (node.region || "unknown") !== region) return false;
    if (filter === "attention") return u.attention(node);
    if (filter === "online") return u.online(node);
    if (filter === "download") return node.download_ready === true;
    if (filter === "ready") return u.online(node) && node.routing_ready;
    if (filter === "sync") return u.online(node) && !node.routing_ready;
    if (filter === "offline" || filter === "disabled") return u.state(node) === filter;
    return true;
  }
  function sort() {
    var mode = document.getElementById("nodes-sort").value;
    order = items.slice().sort(function (x, y) {
      if (mode === "name") return String(x.public_name || x.node_id).localeCompare(y.public_name || y.node_id, "zh-CN");
      if (mode === "bandwidth") return (u.routingRatio(y) === null ? -1 : u.routingRatio(y)) - (u.routingRatio(x) === null ? -1 : u.routingRatio(x));
      if (mode === "disk") return (u.disk(x) ? u.disk(x).available : Infinity) - (u.disk(y) ? u.disk(y).available : Infinity);
      return Number(u.attention(y)) - Number(u.attention(x));
    }).map(function (node) { return node.node_id; });
  }
  function rowMarkup(node) {
    var p = u.progress(node), pressure = node.pressure || {}, occupancy = u.routingRatio(node);
    var disk = u.disk(node), offline = !u.online(node);
    var hint = u.lowDisk(node) ? '<span class="nodes-warning">磁盘余量不足</span>' :
      pressure.throughput_limited && u.fresh(node) ? '<span class="nodes-warning">吞吐受限</span>' : "";
    var syncText = u.syncLabel(node), sync = node.sync || {};
    var syncClass = Number(sync.failed_tasks) || Number(sync.mismatched_assets) ? " nodes-error" : " muted";
    var traffic = u.mirrorTraffic(node), bandwidth = traffic ? a.bytes(traffic.totalBPS) + "/s" : "暂无镜像采样";
    return '<span class="nodes-cell"><strong>' + a.esc(node.public_name || node.node_id) +
      '</strong><span class="muted">' + a.esc(a.regionLabel(node.region)) + '</span>' + hint + '</span>' +
      '<span class="nodes-cell nodes-cell--state" data-label="服务状态">' + u.states(node) + '</span>' +
      '<span class="nodes-cell nodes-cell--number" data-label="同步进展"><span>' +
      (p ? p.verified + " / " + p.required : "暂无数据") + '</span>' + u.progressBar(node) +
      '<span class="' + syncClass + '">' + a.esc(syncText) + '</span></span>' +
      '<span class="nodes-cell nodes-cell--number" data-label="吞吐 / 压力"><span>' + a.esc(bandwidth) +
      '</span><span class="muted">调度压力 ' + (occupancy === null ? "暂无" : Math.round(occupancy * 100) + "%") +
      '</span><span class="' + (u.lowDisk(node) ? "nodes-warning" : "muted") + '">资产盘 ' +
      a.esc(disk ? a.bytes(disk.available) + (offline || !u.fresh(node) ? "（上次）" : "") : "暂无上报") + '</span></span>' +
      '<span class="nodes-cell nodes-cell--report" data-label="最近上报"><span class="muted">心跳 ' +
      a.esc(u.ago(node.last_heartbeat_unix_ms)) + '</span><span class="muted">报告 ' +
      a.esc(u.ago(node.pressure_reported_unix_ms)) + '</span></span>';
  }
  function render(selected, reorder) {
    if (reorder || !order.length) sort();
    var byID = {}, visible = 0;
    items.forEach(function (node) { byID[node.node_id] = node; });
    Object.keys(rows).forEach(function (id) { if (!byID[id]) { rows[id].remove(); delete rows[id]; } });
    items.forEach(function (node) { if (order.indexOf(node.node_id) < 0) order.push(node.node_id); });
    order = order.filter(function (id) { return !!byID[id]; });
    order.forEach(function (id, index) {
      var node = byID[id], row = rows[id];
      if (!row) {
        row = document.createElement("button"); row.type = "button"; row.className = "nodes-row";
        row.setAttribute("data-node-row", id); row.setAttribute("aria-controls", "nodes-detail-panel");
        rows[id] = row;
      }
      row.setAttribute("aria-label", "查看" + (node.public_name || id) + "详情");
      row.setAttribute("aria-pressed", String(id === selected));
      row.hidden = !matches(node);
      if (!row.hidden) visible++;
      var markup = rowMarkup(node);
      if (row._nodeMarkup !== markup) { row.innerHTML = markup; row._nodeMarkup = markup; }
      var position = list.children[index];
      if (position !== row) list.insertBefore(row, position || null);
    });
    var empty = list.querySelector(".nodes-list-empty");
    if (empty) empty.remove();
    if (!visible) {
      empty = document.createElement("p"); empty.className = "nodes-list-empty muted";
      empty.textContent = items.length ? "没有符合条件的节点" : "暂无节点"; list.appendChild(empty);
    }
    a.text("node-summary", visible + " / " + items.length + " 个节点");
    var filter = document.getElementById("nodes-filter").value;
    document.querySelectorAll("[data-node-filter]").forEach(function (button) {
      button.setAttribute("aria-pressed", String(button.getAttribute("data-node-filter") === filter));
    });
  }
  function metrics() {
    a.text("nodes-total", items.length);
    a.text("nodes-online", items.filter(u.online).length);
    a.text("nodes-download", items.filter(function (n) { return n.download_ready === true; }).length);
    a.text("nodes-ready", items.filter(function (n) { return u.online(n) && n.routing_ready; }).length);
    a.text("nodes-attention", items.filter(u.attention).length);
    var reported = items.map(u.mirrorTraffic).filter(function (sample) { return !!sample; });
    var bytes = reported.reduce(function (sum, sample) { return sum + sample.totalBPS; }, 0);
    u.html("nodes-throughput", reported.length ? a.esc(a.bytes(bytes)) + '<small>/s</small>' : "—");
    document.getElementById("nodes-throughput").parentElement.title = "公网下载 + Swarm 上传；有效报告 " + reported.length + " 个节点。未上报镜像吞吐的节点不计入。";
  }
  window.adminNodeList = {
    update: function (nodes, selected) { items = nodes; metrics(); render(selected); },
    render: render, metrics: metrics,
    row: function (id) { return rows[id]; }
  };
})();
