(function () {
  var a = window.admin;
  if (!a) return;

  var taskStates = {
    pending: "等待下发",
    sent: "已下发",
    running: "同步中",
    retry_wait: "等待重试",
    failed: "失败",
    succeeded: "已完成",
    cancelled: "已取消",
    obsolete: "已失效"
  };
  var taskTypes = {
    asset_download: "下载文件",
    asset_delete: "删除文件",
    inventory_reconcile: "库存对账"
  };
  var scanStates = {
    running: "扫描中",
    succeeded: "扫描完成",
    success: "扫描完成",
    failed: "扫描失败",
    pending: "等待扫描"
  };
  var syncPhases = {
    ready: "全量就绪",
    syncing: "同步中",
    retry_wait: "等待重试",
    failed: "同步失败",
    inventory_pending: "等待完整库存",
    reconciling: "库存对账中",
    no_target: "等待目标库存",
    waiting_session: "等待控制连接",
    offline: "离线",
    disabled: "已禁用",
    blocked: "尚未就绪"
  };
  var auditOps = {
    "node.disable": "禁用节点",
    "node.disable.mtls": "禁用节点",
    "node.enable": "启用节点",
    "node.delete": "删除节点",
    "node.sync_reset": "重置节点同步",
    "node.download_priority": "调整下载优先级",
    "node.region": "修改节点地区",
    "project.reset": "重置项目",
    "project.developer_token.rotate": "重置 Developer API Token",
    "certificate.rotate": "轮换节点证书",
    "pairing.approve": "批准节点接入",
    "pairing.reject": "拒绝节点接入"
  };

  function mapped(map, value, fallback) {
    var key = String(value || "");
    return map[key] || fallback || key || "未知";
  }
  function resultLabel(value) {
    var key = String(value || "").toLowerCase();
    if (key === "success" || key === "succeeded" || key === "ok") return "成功";
    if (key === "failed" || key === "failure" || key === "error") return "失败";
    return value || "未知";
  }
  function regionLabel(value) {
    if (value === "mainland_china") return "中国大陆";
    if (value === "outside_mainland_china") return "非中国大陆";
    return "未设置";
  }
  function taskState(value) { return mapped(taskStates, value); }
  function taskType(value) { return mapped(taskTypes, value, "后台任务"); }
  function scanState(value) { return mapped(scanStates, value, value ? value : "尚未扫描"); }
  function syncPhase(value) { return mapped(syncPhases, value); }
  function auditOperation(value) { return mapped(auditOps, value, value || "管理操作"); }

  a.taskStateLabel = taskState;
  a.taskTypeLabel = taskType;
  a.scanStateLabel = scanState;
  a.syncPhaseLabel = syncPhase;
  a.auditOperationLabel = auditOperation;
  a.resultLabel = resultLabel;
  a.regionLabel = regionLabel;
})();
