package public

import (
	"html/template"
	"net/http"
)

func (s Server) apiDocsPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	s.renderPage(w, pageData{
		Title:        "API 文档",
		BrowserTitle: "API 文档 - 枫源镜像",
		Subtitle:     "面向用户的公共 API",
		BodyClass:    "page-api-docs",
		Body:         template.HTML(apiDocsBody),
		Styles:       []string{"/static/public/api-docs.css"},
	})
}

const apiDocsBody = `
<div class="api-docs-list">
  <section class="api-endpoint">
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects</code></div>
    <p>查询已启用且至少存在可展示 Release 的项目列表。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td colspan="3" class="empty">无</td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>GET /api/public/v1/projects</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "message": "查询成功",
  "request_id": "req_...",
  "data": {
    "projects": [
      {"project_id": "example", "display_name": "示例项目", "available": true}
    ]
  }
}</code></pre>
  </section>

  <section class="api-endpoint">
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects/{project_id}/assets</code></div>
    <p>查询项目资产，返回版本、文件名、架构、可选系统、大小、SHA-256 摘要和可用状态。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td>project_id</td><td>Path</td><td>项目标识</td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>GET /api/public/v1/projects/example/assets</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "assets": [
      {"asset_id": "asset_123", "file_name": "example.zip", "architecture": "amd64", "system": "win", "available": true}
    ]
  }
}</code></pre>
  </section>

  <section class="api-endpoint">
    <div class="api-route"><span class="api-method method-post">POST</span><code>/api/public/v1/api/challenges</code></div>
    <p>为指定资产创建 SHA-256 前导零 PoW 挑战。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td>asset_id</td><td>JSON</td><td>要下载的资产标识</td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>POST /api/public/v1/api/challenges
{"asset_id":"asset_123"}</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "challenge_id": "challenge_123",
    "algorithm": "sha256",
    "leading_zero_bits": 23,
    "canonical_format": "download.v1:{challenge_id}:{asset_id}:{nonce_seed}:{nonce}"
  }
}</code></pre>
  </section>

  <section class="api-endpoint">
    <div class="api-route"><span class="api-method method-post">POST</span><code>/api/public/v1/api/authorizations</code></div>
    <p>提交 PoW 结果并领取短时、单节点绑定的下载授权。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td>challenge_id</td><td>JSON</td><td>挑战标识</td></tr><tr><td>asset_id</td><td>JSON</td><td>资产标识</td></tr><tr><td>nonce</td><td>JSON</td><td>满足前导零要求的 nonce</td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>POST /api/public/v1/api/authorizations
{"challenge_id":"challenge_123","asset_id":"asset_123","nonce":"456789"}</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "authorization_id": "auth_123",
    "download_url": "https://node.example/example/v1.2.3/example-windows-amd64.zip",
    "expires_at": "2026-05-28T12:05:00Z"
  }
}</code></pre>
  </section>

  <section class="api-endpoint">
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/authorizations/{authorization_id}</code></div>
    <p>携带对应下载令牌查询授权状态、过期时间、公开节点名和已入账真实发送字节。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td>authorization_id</td><td>Path</td><td>授权标识</td></tr><tr><td>Authorization</td><td>Header</td><td><code>Bearer &lt;download_token&gt;</code></td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>GET /api/public/v1/authorizations/auth_123
Authorization: Bearer &lt;download_token&gt;</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "authorization_id": "auth_123",
    "state": "issued",
    "bytes_accounting_enabled": true,
    "sent_bytes": 1048576
  }
}</code></pre>
  </section>

  <section class="api-endpoint">
    <div class="api-route"><span class="api-method method-get">GET</span><code>/{project_id}/{version}/{file_name}</code></div>
    <p>下载节点文件服务地址由授权响应的 <code>download_url</code> 决定，支持单段 HTTP Range。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td>Authorization</td><td>Header</td><td><code>Bearer &lt;download_token&gt;</code></td></tr><tr><td>Range</td><td>Header</td><td>可选，例如 <code>bytes=0-1048575</code></td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>GET /example/v1.2.3/example-windows-amd64.zip
Authorization: Bearer &lt;download_token&gt;
Range: bytes=0-1048575</code></pre>
  </section>
</div>`
