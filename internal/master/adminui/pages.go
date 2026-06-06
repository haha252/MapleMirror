package adminui

type adminPage struct {
	ID       string
	Title    string
	Subtitle string
	Script   string
}

func adminPageForPath(path string) (adminPage, bool) {
	pages := map[string]adminPage{
		"/admin/": {
			ID: "overview", Title: "管理总览",
			Subtitle: "节点、同步与流量状态", Script: "overview.js",
		},
		"/admin/nodes": {
			ID: "nodes", Title: "节点管理",
			Subtitle: "节点接入、配对审批、诊断与控制", Script: "nodes.js",
		},
		"/admin/sync": {
			ID: "sync", Title: "同步管理",
			Subtitle: "Release 扫描、同步任务与重新对账", Script: "sync.js",
		},
		"/admin/projects": {
			ID: "projects", Title: "镜像项目管理",
			Subtitle: "项目卡片、仓库规则、启停与重置", Script: "projects.js",
		},
		"/admin/projects/new": {
			ID: "project-edit", Title: "新增项目",
			Subtitle: "按分类填写镜像项目配置", Script: "project-edit.js",
		},
		"/admin/projects/edit": {
			ID: "project-edit", Title: "修改项目",
			Subtitle: "按分类维护镜像项目配置", Script: "project-edit.js",
		},
		"/admin/security": {
			ID: "security", Title: "安全管理",
			Subtitle: "封禁、授权排障、流量事件与审计摘要", Script: "security.js",
		},
	}
	page, ok := pages[path]
	return page, ok
}
