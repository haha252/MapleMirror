package mirrorsync

func syncPhase(item SyncStatus) string {
	switch {
	case item.RoutingReady:
		return "ready"
	case item.ConnectionState == "disabled":
		return "disabled"
	case item.ConnectionState == "offline":
		return "offline"
	case item.FailedTasks > 0:
		return "failed"
	case item.RetryWaitTasks > 0:
		return "retry_wait"
	case item.RunningTasks > 0 || item.SentTasks > 0 || item.PendingTasks > 0:
		return "syncing"
	case item.RequiredAssets == 0:
		return "no_target"
	case !item.HasInventoryReport || !item.LatestInventoryComplete:
		return "inventory_pending"
	case item.MissingAssets > 0 || item.MismatchedAssets > 0:
		return "reconciling"
	case !item.ActiveControlSession:
		return "waiting_session"
	default:
		return "blocked"
	}
}
