package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"mirror-server/internal/config"
)

func main() {
	path := flag.String("config", "config.yaml", "主节点配置文件路径")
	projectsPath := flag.String("projects", "projects.yaml", "项目清单配置文件路径")
	quotaPath := flag.String("quota", "quota.yaml", "额度配置文件路径")
	flag.Parse()
	warn := func(field, value string) {
		fmt.Printf("警告：配置字段 %s 缺失，已使用默认值 %s\n", field, value)
	}
	var created bool
	if _, err := config.LoadMaster(*path, warn); handleLoad(err, "主节点配置", &created) {
		os.Exit(1)
	}
	if _, err := config.LoadProjects(*projectsPath, warn); handleLoad(err, "项目清单", &created) {
		os.Exit(1)
	}
	if _, err := config.LoadQuota(*quotaPath, warn); handleLoad(err, "额度配置", &created) {
		os.Exit(1)
	}
	if created {
		fmt.Fprintln(os.Stderr, "已生成主节点所需示例配置，请确认安全字段后重新启动。")
		return
	}
	fmt.Println("主节点基础配置加载完成，健康服务将在后续子阶段启用。")
}

func handleLoad(err error, name string, created *bool) bool {
	if errors.Is(err, config.ErrExampleCreated) {
		*created = true
		return false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s加载失败：%v\n", name, err)
		return true
	}
	return false
}
