package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"mirror-server/internal/config"
)

func main() {
	path := flag.String("config", "node.yaml", "下载节点配置文件路径")
	flag.Parse()
	_, err := config.LoadNode(*path, func(field, value string) {
		fmt.Printf("警告：配置字段 %s 缺失，已使用默认值 %s\n", field, value)
	})
	if errors.Is(err, config.ErrExampleCreated) {
		fmt.Fprintln(os.Stderr, "已生成下载节点示例配置，请确认安全字段后重新启动。")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "下载节点配置加载失败：%v\n", err)
		os.Exit(1)
	}
	fmt.Println("下载节点基础配置加载完成，健康服务将在后续子阶段启用。")
}
