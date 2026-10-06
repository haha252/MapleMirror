package adminui

import "strings"

type adminPage struct {
	ID       string
	Title    string
	Subtitle string
	Script   string
}

func adminPageForPath(path string) (adminPage, bool) {
	if strings.HasPrefix(path, "/admin/nodes/") &&
		(strings.HasSuffix(path, "/management") || strings.HasSuffix(path, "/projects")) {
		return adminPage{
			ID: "node-management", Title: "节点管理",
			Subtitle: "下载优先级、自动分配、容量上限与手动项目选择", Script: "node-projects.js",
		}, true
	}
	pages := map[string]adminPage{
		"/admin/": {
			ID: "overview", Title: "管理总览",
			Subtitle: "运行状态、流量与需要关注事项", Script: "overview.js",
		},
		"/admin/nodes": {
			ID: "nodes", Title: "节点管理",
			Subtitle: "节点概况、同步状态与详细诊断", Script: "nodes.js",
		},
		"/admin/sync": {
			ID: "sync", Title: "同步管理",
			Subtitle: "项目扫描与节点同步任务", Script: "sync.js",
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
			ID: "security", Title: "安全中心",
			Subtitle: "安全概览、封禁与管理审计", Script: "security.js",
		},
	}
	page, ok := pages[path]
	return page, ok
}
