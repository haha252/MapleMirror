package mirrorsync

import "fmt"

func syncStatusReason(item SyncStatus) (string, string) {
	detail := fmt.Sprintf("目标库存 %d 项，已校验 %d 项，缺失校验 %d 项，不一致资产 %d 项，未完成任务 %d 个，失败任务 %d 个，活动会话 %s，最近库存上报 %s，最近心跳 %s",
		item.RequiredAssets, item.VerifiedAssets, item.MissingAssets, item.MismatchedAssets,
		item.RunningTasks, item.FailedTasks, yesNo(item.ActiveControlSession),
		inventoryStatus(item), blankAsDash(item.LastHeartbeatAt))
	switch {
	case item.RoutingReady:
		return "", ""
	case item.FailedTasks > 0:
		return "存在失败的同步任务，等待重试或人工重置", detail
	case item.OutstandingTasks > 0:
		return "仍有同步任务未完成", detail
	case item.RequiredAssets == 0:
		return "主节点尚未下发目标库存", detail
	case item.MissingAssets > 0 && !item.HasInventoryReport:
		return "尚未上报完整库存，未完成最终对账", detail
	case item.MissingAssets > 0 && !item.LatestInventoryComplete:
		return "库存上报尚未完成，未完成最终对账", detail
	case item.MissingAssets > 0:
		return "目标库存尚未全部校验完成", detail
	case !item.ActiveControlSession && item.LastHeartbeatAt == "":
		return "控制连接尚未建立", detail
	case !item.ActiveControlSession:
		return "最近心跳正常，但当前控制会话记录未保持活动", detail
	default:
		return "未满足同步就绪条件", detail
	}
}

func inventoryStatus(item SyncStatus) string {
	switch {
	case !item.HasInventoryReport:
		return "未上报"
	case item.LatestInventoryComplete:
		return fmt.Sprintf("完整(revision=%d)", item.LatestInventoryRevision)
	default:
		return fmt.Sprintf("未完成(revision=%d)", item.LatestInventoryRevision)
	}
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func blankAsDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
