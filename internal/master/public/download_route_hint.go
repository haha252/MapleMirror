package public

import "net/http"

func (s Server) downloadMisrouted(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "当前 /downloads/ 请求到达了主节点，请将该路径反向代理到下载节点文件服务。", http.StatusNotFound)
}
