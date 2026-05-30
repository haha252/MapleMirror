package public

import "database/sql"

func inventoryReportLabel(complete sql.NullBool) string {
	switch {
	case !complete.Valid:
		return "未上报"
	case complete.Bool:
		return "完整"
	default:
		return "未完成"
	}
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}
