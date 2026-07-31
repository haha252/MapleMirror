package public

import (
	"html/template"
	"net/http"
)

func (s Server) apiDocsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
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
  <section class="api-endpoint api-docs-intro">
    <h2>下载接入方式</h2>
    <p>你可以按实际场景选择下载接入方式。如果你是在网页、官网、论坛、公告页或前端页面里放下载按钮，推荐跳转主站验证页；如果你是在脚本、客户端、CI、自动更新器或后端程序里自动下载文件，使用程序 API 链路。</p>
    <div class="api-jump-links" aria-label="下载接入方式">
      <a href="#web-download-flow">方式一：跳转主站验证页下载</a>
      <a href="#api-download-flow">方式二：程序调用 API 下载</a>
      <a href="#other-public-apis">其他接口</a>
    </div>
  </section>

  <section class="api-endpoint api-flow" id="web-download-flow">
    <h2>方式一：跳转主站验证页下载</h2>
    <p>这种方式适合网页下载按钮。你不需要自己处理挑战、PoW、下载令牌，也不需要关心当前由哪个下载节点提供文件。</p>
    <p class="api-note">需要特别注意：这里要跳转的是主站地址，不是下载节点地址。外部前端应该把用户带到主站的验证页面，由主站完成验证、授权和节点选择，最后再跳转到真实下载节点开始下载。</p>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/{project_id}/{version}/{file_name}</code></div>
    <p class="api-label">验证页地址示例</p>
    <pre><code>https://mirror.example.com/fcl/1.3.0.9/FCL-release-1.3.0.9-arm64-v8a.apk</code></pre>
    <p>这里的 <code>https://mirror.example.com</code> 应该是主站公共入口，不是某个节点的 <code>public_download_base_url</code>。</p>

    <details class="api-details">
      <summary>如果不知道项目是哪一个，可以先查询项目列表</summary>
      <p>这个接口可以用来获取 <code>project_id</code>、项目展示名和项目可用状态。前端一般只需要在初始化下载页、项目选择器或外部下载列表时调用它。</p>
      <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects</code></div>
      <p class="api-label">Example Request</p>
      <pre><code>GET /api/public/v1/projects</code></pre>
    </details>

    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects/{project_id}/assets</code></div>
    <p>拿到 <code>project_id</code> 后，前端可以查询该项目下有哪些版本和文件。返回结果里会包含 <code>version</code>、<code>file_name</code>、<code>architecture</code>、<code>system</code>、<code>size_bytes</code>、<code>digest_sha256</code> 和 <code>available</code> 等字段。</p>
    <p class="api-label">Example Request</p>
    <pre><code>GET /api/public/v1/projects/example/assets</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "assets": [
      {
        "asset_id": "asset_123",
        "version": "v1.2.3",
        "file_name": "example-windows-amd64.zip",
        "architecture": "amd64",
        "system": "win",
        "available": true
      }
    ]
  }
}</code></pre>
    <p>前端可以根据这些字段生成下载按钮。用户点击按钮时，跳转到主站验证页：</p>
    <pre><code>https://mirror.example.com/example/v1.2.3/example-windows-amd64.zip</code></pre>
    <ol class="api-steps">
      <li>展示下载验证页面。</li>
      <li>在浏览器中完成下载挑战计算。</li>
      <li>向主节点领取短时下载授权。</li>
      <li>选择可用下载节点。</li>
      <li>跳转到真实下载地址开始下载。</li>
    </ol>
    <p>外部网站不要直接拼接节点下载地址，也不要调用程序下载用的 <code>/api/public/v2/api/*</code> 接口来替代这个流程。正常网页只使用单个 Worker 和原生 BigInt 完成 RSA repeated-squaring，不使用 WebGPU，不回退 SHA。</p>
  </section>

  <section class="api-endpoint api-flow" id="api-download-flow">
    <h2>方式二：程序调用 API 下载</h2>
    <p>这种方式适合命令行工具、自动更新器、CI 脚本、下载器或后端服务。程序需要自己查询资产、完成 API PoW 验证，然后携带下载令牌访问下载节点。</p>

    <h3>第一步：查询项目列表</h3>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects</code></div>
    <p>查询已启用且至少存在可展示 Release 的项目列表，拿到后续要使用的 <code>project_id</code>。</p>
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

    <h3>第二步：查询项目资产</h3>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects/{project_id}/assets</code></div>
    <p>程序应选择一个 <code>available=true</code> 的资产，并记录它的 <code>asset_id</code>。如果 <code>available=false</code>，表示当前没有可用下载节点持有这个文件，程序应该稍后重试。</p>
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

    <h3>第三步：创建 API V2 顺序工作量挑战</h3>
    <div class="api-route"><span class="api-method method-post">POST</span><code>/api/public/v2/api/challenges</code></div>
    <p>为指定资产创建 3072 位 RSA repeated-squaring 挑战。响应中的 <code>modulus</code> 和 <code>base</code> 是 384 字节无符号大端整数的无填充 base64url 编码。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td>asset_id</td><td>JSON</td><td>要下载的资产标识</td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>POST /api/public/v2/api/challenges
{"asset_id":"asset_123"}</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "challenge_id": "challenge_123",
    "algorithm": "rsa-repeated-squaring-v1",
    "modulus_id": "模数标识",
    "modulus": "512 字符 base64url 整数",
    "base": "512 字符 base64url 整数",
    "iterations": 96000,
    "encoding": "base64url-uint-be-384"
  }
}</code></pre>

    <h3>第四步：顺序计算 solution</h3>
    <p>从 <code>y = base</code> 开始，严格执行 <code>iterations</code> 次 <code>y = y² mod modulus</code>，再把 y 编码为 384 字节定长大端 base64url。不得提交十进制、十六进制或可变长整数。</p>

    <h3>第五步：提交 solution 并领取下载授权</h3>
    <div class="api-route"><span class="api-method method-post">POST</span><code>/api/public/v2/api/authorizations</code></div>
    <p>提交顺序工作量结果并领取短时、单节点绑定的下载授权。<code>telemetry</code> 可选且只用于统计，不影响授权。API V1 在配置开启时仍保持原 SHA-256 合同。</p>
    <table class="api-params"><thead><tr><th>参数</th><th>类型</th><th>描述</th></tr></thead><tbody><tr><td>challenge_id</td><td>JSON</td><td>挑战标识</td></tr><tr><td>asset_id</td><td>JSON</td><td>资产标识</td></tr><tr><td>nonce</td><td>JSON</td><td>满足前导零要求的 nonce</td></tr></tbody></table>
    <p class="api-label">Example Request</p>
    <pre><code>POST /api/public/v2/api/authorizations
{"challenge_id":"challenge_123","asset_id":"asset_123","solution":"512 字符 base64url 整数"}</code></pre>
    <p class="api-label">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "authorization_id": "auth_123",
    "download_url": "https://node.example/example/v1.2.3/example-windows-amd64.zip",
    "download_token": "43 字符短时随机令牌",
    "expires_at": "2026-05-28T12:05:00Z"
  }
}</code></pre>

    <h3>第六步：请求下载节点</h3>
    <p>程序应直接访问授权响应里的 <code>download_url</code>，并通过请求头携带下载令牌。这里的 <code>download_url</code> 通常指向下载节点；程序调用 API 的这条链路里，下载文件时访问节点地址是正确的。</p>
    <div class="api-route"><span class="api-method method-get">GET</span><code>{download_url}</code></div>
    <p class="api-label">Example Request</p>
    <pre><code>GET {download_url}
Authorization: Bearer &lt;download_token&gt;</code></pre>
    <p>如果需要断点续传，可以使用单段 Range：</p>
    <pre><code>GET {download_url}
Authorization: Bearer &lt;download_token&gt;
Range: bytes=1048576-2097151</code></pre>
  </section>

  <section class="api-endpoint api-flow" id="other-public-apis">
    <h2>其他接口</h2>
    <p>下面这些接口不是程序下载流程的步骤，只用于订阅、状态查询或排障。</p>
  </section>

  <section class="api-endpoint" id="blocklist-feed">
    <h2>封禁列表订阅</h2>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/blocklist.txt</code></div>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/blocklist.json</code></div>
    <p>返回当前生效的公共下载封禁列表。内容包含 <code>quota.yaml</code> 静态黑名单和本站手动/自动封禁记录，不包含远程订阅源快照，响应在服务端缓存 60 秒。</p>
    <p class="api-label">TXT Example Response</p>
    <pre><code># [枫源镜像封禁] 封禁原因: traffic_limit_exceeded, 来源: local_auto_ban, 封禁后尝试次数: 3
2.59.169.232</code></pre>
    <p class="api-label">JSON Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "blocks": [
      {"entry": "2.59.169.232", "reason": "traffic_limit_exceeded", "attempts_after_block": 3, "blocked_at": "2026-06-21T12:00:00Z"}
    ]
  }
}</code></pre>
  </section>

  <section class="api-endpoint" id="changelog-api">
    <h2>更新日志</h2>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/changelog</code></div>
    <p>按时间倒序查询本站更新记录。<code>minimum_level</code> 可选 info、notice、warn 或 critical；<code>q</code> 搜索标题和可见描述；<code>limit</code> 默认 20、最大 50。存在下一批时响应返回不透明的 <code>next_cursor</code>。</p>
    <p class="api-label">Example Request</p>
    <pre><code>GET /api/public/v1/changelog?minimum_level=notice&amp;q=下载&amp;limit=20</code></pre>
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
</div>`
