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
	s.renderPage(w, s.localizeIndexablePage(r, "/api-docs", pageData{
		Title:        "API 文档",
		BrowserTitle: "枫源镜像公共下载 API 文档 - 项目、文件与自动下载接口",
		Description:  "枫源镜像公共下载 API 文档，介绍项目与文件查询、网页下载、程序下载、PoW 验证、授权令牌和自动更新器接入方式，帮助脚本、客户端、CI 和后端服务稳定获取 GitHub Release 文件，并支持集成方设计稳定的下载流程。",
		Subtitle:     "面向网页、脚本、客户端与自动更新器的公开下载接口说明。",
		BodyClass:    "page-api-docs",
		Body:         template.HTML(apiDocsBody),
		Styles:       []string{"/static/public/api-docs.css", "/static/public/api-docs-copy.css"},
		Scripts:      []string{"/static/public/api-docs.js"},
	}))
}

const apiDocsBody = `
<div class="api-docs-list">
  <section class="api-endpoint api-docs-intro">
    <h2 data-i18n="api.introTitle">下载接入方式</h2>
    <p data-i18n="api.intro">你可以按实际场景选择下载接入方式。如果你是在网页、官网、论坛、公告页或前端页面里放下载按钮，推荐跳转主站验证页；如果你是在脚本、客户端、CI、自动更新器或后端程序里自动下载文件，使用程序 API 链路。</p>
    <div class="api-jump-links" data-i18n-aria-label="api.introTitle" aria-label="下载接入方式">
      <a href="#web-download-flow" data-i18n="api.webTitle">方式一：跳转主站验证页下载</a>
      <a href="#api-download-flow" data-i18n="api.apiTitle">方式二：程序调用 API 下载</a>
      <a href="#other-public-apis" data-i18n="api.otherTitle">其他接口</a>
    </div>
  </section>
` + apiDocsContractDetails + `

  <section class="api-endpoint api-flow" id="web-download-flow">
    <h2 data-i18n="api.webTitle">方式一：跳转主站验证页下载</h2>
    <p data-i18n="api.webDescription">这种方式适合网页下载按钮。你不需要自己处理挑战、PoW、下载令牌，也不需要关心当前由哪个下载节点提供文件。</p>
    <p class="api-note" data-i18n="api.webNote">需要特别注意：这里要跳转的是主站地址，不是下载节点地址。外部前端应该把用户带到主站的验证页面，由主站完成验证、授权和节点选择，最后再跳转到真实下载节点开始下载。</p>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/{project_id}/{version}/{file_name}</code></div>
    <p class="api-label" data-i18n="api.verificationExample">验证页地址示例</p>
    <pre><code>https://fyhub.cn/fcl/1.3.0.9/FCL-release-1.3.0.9-arm64-v8a.apk</code></pre>
    <p data-i18n="api.webAddress">这里的 https://fyhub.cn 就是枫源镜像主站公共入口，不是某个下载节点的 public_download_base_url。</p>

    <details class="api-details">
      <summary data-i18n="api.projectLookup">如果不知道项目是哪一个，可以先查询项目列表</summary>
      <p data-i18n="api.projectLookupDescription">这个接口可以用来获取 project_id、项目展示名和项目可用状态。前端一般只需要在初始化下载页、项目选择器或外部下载列表时调用它。</p>
      <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects</code></div>
      <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
      <pre><code>GET /api/public/v1/projects</code></pre>
    </details>

    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects/{project_id}/assets</code></div>
    <p data-i18n="api.assetsDescription">拿到 project_id 后，前端可以查询该项目下有哪些版本和文件。返回结果里会包含 version、file_name、architecture、system、size_bytes、digest_sha256 和 available 等字段。</p>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>GET /api/public/v1/projects/example/assets</code></pre>
    <p class="api-label" data-i18n="api.exampleResponse">Example Response</p>
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
    <p data-i18n="api.webDownloadDescription">前端可以根据这些字段生成下载按钮。用户点击按钮时，跳转到主站验证页：</p>
    <pre><code>https://fyhub.cn/example/v1.2.3/example-windows-amd64.zip</code></pre>
    <ol class="api-steps">
      <li data-i18n="api.stepShowVerification">展示下载验证页面。</li>
      <li data-i18n="api.stepSolveChallenge">在浏览器中完成下载挑战计算。</li>
      <li data-i18n="api.stepAuthorize">向主节点领取短时下载授权。</li>
      <li data-i18n="api.stepChooseNode">选择可用下载节点。</li>
      <li data-i18n="api.stepRedirect">跳转到真实下载地址开始下载。</li>
    </ol>
    <p data-i18n="api.webWarning">外部网站不要直接拼接节点下载地址，也不要调用程序下载用的 /api/public/v2/api/* 接口来替代这个流程。</p>
  </section>

  <section class="api-endpoint api-flow" id="api-download-flow">
    <h2 data-i18n="api.apiTitle">方式二：程序调用 API 下载</h2>
    <p data-i18n="api.apiDescription">这种方式适合命令行工具、自动更新器、CI 脚本、下载器或后端服务。程序需要自己查询资产、完成 API PoW 验证，然后携带下载令牌访问下载节点。</p>

    <h3 data-i18n="api.step1Title">第一步：查询项目列表</h3>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects</code></div>
    <p data-i18n="api.step1Description">查询当前已启用的项目列表并取得 project_id。项目即使暂时没有可路由下载资产也可能出现在结果中，应以 available 判断当前是否可下载。</p>
    <table class="api-params"><thead><tr><th data-i18n="api.parameter">参数</th><th data-i18n="api.type">类型</th><th data-i18n="api.description">描述</th></tr></thead><tbody><tr><td colspan="3" class="empty" data-i18n="api.none">无</td></tr></tbody></table>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>GET /api/public/v1/projects</code></pre>
    <p class="api-label" data-i18n="api.exampleResponse">Example Response</p>
    <pre><code>{
  "status": "success",
  "message": "查询成功",
  "request_id": "req_...",
  "data": {
    "projects": [
      {
        "project_id": "example",
        "repository": "owner/example",
        "display_name": "示例项目",
        "description": "示例说明",
        "homepage_url": "https://fyhub.cn",
        "available": true
      }
    ]
  }
}</code></pre>

    <h3 data-i18n="api.step2Title">第二步：查询项目资产</h3>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/projects/{project_id}/assets</code></div>
    <p data-i18n="api.step2Description">程序应选择一个 available=true 的资产，并记录它的 asset_id。如果 available=false，表示当前没有可用下载节点持有这个文件，程序应该稍后重试。</p>
    <table class="api-params"><thead><tr><th data-i18n="api.parameter">参数</th><th data-i18n="api.type">类型</th><th data-i18n="api.description">描述</th></tr></thead><tbody><tr><td>project_id</td><td>Path</td><td data-i18n="api.projectIdentifier">项目标识</td></tr></tbody></table>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>GET /api/public/v1/projects/example/assets</code></pre>
    <p class="api-label" data-i18n="api.exampleResponse">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "assets": [
      {
        "asset_id": "asset_123",
        "version": "v1.2.3",
        "download_path": "/example/v1.2.3/example.zip",
        "prerelease": false,
        "file_name": "example.zip",
        "architecture": "amd64",
        "system": "win",
        "size_bytes": 10485760,
        "digest_sha256": "0123456789abcdef...",
        "available": true,
        "unavailable_reason": ""
      }
    ]
  }
}</code></pre>

    <h3 data-i18n="api.step3Title">第三步：创建 API V2 顺序工作量挑战</h3>
    <p class="api-danger-note" role="alert" data-i18n="api.step3Warning">旧版“下载授权协议 V1”弃用提醒：仅 /api/public/v1/api/challenges 与 /api/public/v1/api/authorizations 这两个旧 PoW/授权接口使用 SHA-256 前导零 nonce 搜索，并且当前部署可以直接关闭它们并返回 410 API_VERSION_RETIRED。这里不代表 /api/public/v1/ 下的项目、资产、封禁列表和更新日志接口被弃用。新客户端必须直接实现下面的 V2 repeated-squaring 流程，不能只替换接口路径。</p>
    <div class="api-route"><span class="api-method method-post">POST</span><code>/api/public/v2/api/challenges</code></div>
    <p data-i18n="api.step3Description">为指定资产创建 3072 位 RSA repeated-squaring 挑战。响应中的 modulus 和 base 是 384 字节无符号大端整数的无填充 base64url 编码。</p>
    <table class="api-params"><thead><tr><th data-i18n="api.parameter">参数</th><th data-i18n="api.type">类型</th><th data-i18n="api.description">描述</th></tr></thead><tbody><tr><td>asset_id</td><td>JSON</td><td data-i18n="api.assetIdentifier">要下载的资产标识</td></tr></tbody></table>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>POST /api/public/v2/api/challenges
{"asset_id":"asset_123"}</code></pre>
    <p class="api-label" data-i18n="api.exampleResponse">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "challenge_id": "challenge_123",
    "asset_id": "asset_123",
    "algorithm": "rsa-repeated-squaring-v1",
    "modulus_id": "模数标识",
    "modulus": "512 字符 base64url 整数",
    "base": "512 字符 base64url 整数",
    "iterations": 96000,
    "encoding": "base64url-uint-be-384",
    "expires_at": "2026-09-03T12:05:00Z"
  }
}</code></pre>

    <h3 data-i18n="api.step4Title">第四步：顺序计算 solution</h3>
    <p data-i18n="api.step4Description">从 y = base 开始，严格执行 iterations 次 y = y² mod modulus，再把 y 编码为 384 字节定长大端 base64url。不得提交十进制、十六进制或可变长整数。</p>
` + apiVDFPrincipleDetails + `

    <h3 data-i18n="api.step5Title">第五步：提交 solution 并领取下载授权</h3>
    <div class="api-route"><span class="api-method method-post">POST</span><code>/api/public/v2/api/authorizations</code></div>
    <p data-i18n="api.step5Description">提交顺序工作量结果并领取短时、单节点绑定的下载授权。challenge_id、asset_id 与 solution 都必须来自同一条 V2 挑战链路。程序 API 当前没有需要客户端上报的 telemetry 字段。</p>
    <table class="api-params"><thead><tr><th data-i18n="api.parameter">参数</th><th data-i18n="api.type">类型</th><th data-i18n="api.description">描述</th></tr></thead><tbody><tr><td>challenge_id</td><td>JSON</td><td data-i18n="api.challengeIdentifier">挑战标识</td></tr><tr><td>asset_id</td><td>JSON</td><td data-i18n="api.assetIdentifier">资产标识</td></tr><tr><td>solution</td><td>JSON</td><td data-i18n="api.solutionDescription">512 字符的定长 base64url repeated-squaring 结果</td></tr></tbody></table>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>POST /api/public/v2/api/authorizations
{"challenge_id":"challenge_123","asset_id":"asset_123","solution":"512 字符 base64url 整数"}</code></pre>
    <p class="api-label" data-i18n="api.exampleResponse">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "authorization_id": "auth_123",
    "download_url": "由服务端返回的实际下载节点 URL",
    "download_token": "43 字符短时随机令牌",
    "expires_at": "2026-09-03T12:05:00Z",
    "range_concurrency_limit": 32,
    "max_bytes": 20971520
  }
}</code></pre>

    <h3 data-i18n="api.step6Title">第六步：请求下载节点</h3>
    <p data-i18n="api.step6Description">程序应直接访问授权响应里的 download_url，并通过请求头携带 download_token。download_url 通常指向下载节点；授权成功后实际文件请求允许从与挑战阶段不同的出口访问，但令牌属于 Bearer 凭据，泄露后可能被他人使用。客户端还应遵守响应中的 range_concurrency_limit 与 max_bytes。</p>
    <div class="api-route"><span class="api-method method-get">GET</span><code>{download_url}</code></div>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>GET {download_url}
Authorization: Bearer &lt;download_token&gt;</code></pre>
    <p data-i18n="api.rangeDescription">如果需要断点续传，可以使用单段 Range：</p>
    <pre><code>GET {download_url}
Authorization: Bearer &lt;download_token&gt;
Range: bytes=1048576-2097151</code></pre>
  </section>

  <section class="api-endpoint api-flow" id="other-public-apis">
    <h2 data-i18n="api.otherTitle">其他接口</h2>
    <p data-i18n="api.otherDescription">下面这些接口不是程序下载流程的步骤，只用于订阅、状态查询或排障。</p>
  </section>

  <section class="api-endpoint" id="blocklist-feed">
    <h2 data-i18n="api.blocklistTitle">封禁列表订阅</h2>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/blocklist.txt</code></div>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/blocklist.json</code></div>
    <p data-i18n="api.blocklistDescription">返回当前生效的公共下载封禁列表。内容包含 quota.yaml 静态黑名单和本站手动/自动封禁记录，不包含远程订阅源快照，响应在服务端缓存 60 秒。</p>
    <p class="api-label" data-i18n="api.txtExampleResponse">TXT Example Response</p>
    <pre><code># [枫源镜像封禁] 封禁原因: traffic_limit_exceeded, 来源: local_auto_ban, 封禁后尝试次数: 3
192.0.2.123</code></pre>
    <p class="api-label" data-i18n="api.jsonExampleResponse">JSON Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "blocks": [
      {"entry": "192.0.2.123", "reason": "traffic_limit_exceeded", "attempts_after_block": 3, "blocked_at": "2026-06-21T12:00:00Z"}
    ]
  }
}</code></pre>
  </section>
` + apiDeveloperSyncDetails + `

  <section class="api-endpoint" id="changelog-api">
    <h2 data-i18n="nav.changelog">更新日志</h2>
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v1/changelog</code></div>
    <p data-i18n="api.changelogDescription">按时间倒序查询本站更新记录。minimum_level 可选 info、notice、warn 或 critical；q 搜索标题和可见描述；limit 默认 20、最大 50。存在下一批时响应返回不透明的 next_cursor。</p>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>GET /api/public/v1/changelog?minimum_level=notice&amp;q=下载&amp;limit=20</code></pre>
  </section>

` + apiDocsAuthorizationStatusDetails + `
</div>`
