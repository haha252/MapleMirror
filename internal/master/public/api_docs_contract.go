package public

const apiDocsContractDetails = `
  <section class="api-endpoint api-contract" id="api-contract">
    <h2 data-i18n="api.contractTitle">接入前先看：稳定性与通用约定</h2>
    <p data-i18n="api.contractStable">本文明确列出的接口才属于面向第三方开发者的稳定接入契约。新客户端应优先使用项目/资产查询接口与下载授权协议 V2；未在本文列出的公开可访问端点，不应被视为长期兼容承诺。</p>

    <h3 data-i18n="api.contractStableTitle">稳定开发者接口</h3>
    <pre><code>GET  /api/public/v1/projects
GET  /api/public/v1/projects/{project_id}/assets
POST /api/public/v2/api/challenges
POST /api/public/v2/api/authorizations
GET  /api/public/v2/authorizations/{authorization_id}
GET  /api/public/v1/blocklist.txt
GET  /api/public/v1/blocklist.json
GET  /api/public/v1/changelog
POST /api/developer/v1/projects/{project_id}/sync</code></pre>

    <p data-i18n="api.contractVersioning">注意：本文所说的“下载授权协议 V1”只指 /api/public/v1/api/challenges 与 /api/public/v1/api/authorizations 这两个旧版 PoW/授权接口，不代表整个 /api/public/v1/ 命名空间被弃用。项目、资产、封禁列表和更新日志等 V1 资源接口仍是当前稳定接口。</p>

    <h3 data-i18n="api.contractInternalTitle">站点内部公共端点</h3>
    <p data-i18n="api.contractInternal">/api/public/v1/catalog、/api/public/v1/stats、/api/public/v1/stats/details、/api/public/v2/web/* 与网页验证相关接口用于本站前端或内部展示。它们虽然可以从公网访问，但当前不作为第三方稳定 API 契约，字段和行为可能随前端实现调整。第三方程序不要依赖这些接口。</p>

    <h3 data-i18n="api.contractEnvelopeTitle">JSON 响应格式</h3>
    <p data-i18n="api.contractEnvelope">除 TXT 封禁订阅、下载节点文件响应和上面明确标记为站点内部的数据端点外，本文中的 JSON API 使用统一 envelope。成功时读取 data；失败时根据 HTTP 状态码与稳定的 code 分支处理，并保留 request_id 用于排障。</p>
    <pre><code>{
  "status": "success",
  "message": "...",
  "request_id": "req_...",
  "data": {}
}</code></pre>
    <pre><code>{
  "status": "error",
  "message": "...",
  "request_id": "req_...",
  "code": "STABLE_ERROR_CODE"
}</code></pre>
    <p data-i18n="api.contractPostJSON">POST 接口发送 JSON 请求体。建议显式设置 Content-Type: application/json；公开下载 API 的 JSON 请求体上限为 16 KiB。</p>

    <h3 data-i18n="api.contractRateTitle">限流、重试与错误处理</h3>
    <p data-i18n="api.contractRate">公共接口受可配置的来源级资源限流与下载风控约束。收到 429 或 503 时，如果响应带 Retry-After，应按该秒数退避后重试，不要固定高频轮询。Developer API 另有 X-RateLimit-Limit、X-RateLimit-Remaining、X-RateLimit-Reset。</p>
    <table class="api-params"><thead><tr><th>HTTP</th><th>code</th><th data-i18n="api.description">描述</th></tr></thead><tbody>
      <tr><td>401</td><td>DOWNLOAD_TOKEN_INVALID</td><td data-i18n="api.errorToken">下载令牌无效、过期或不属于当前授权查询来源。</td></tr>
      <tr><td>403</td><td>CHALLENGE_FAILED / CLIENT_BLOCKED</td><td data-i18n="api.errorForbidden">挑战不匹配、校验失败，或当前来源已被限制。</td></tr>
      <tr><td>409</td><td>NO_ROUTABLE_NODE / CHALLENGE_IN_PROGRESS</td><td data-i18n="api.errorConflict">当前没有可用节点，或同一挑战仍在处理。</td></tr>
      <tr><td>410</td><td>API_VERSION_RETIRED</td><td data-i18n="api.errorRetired">旧版下载授权协议 V1 已在当前部署中关闭。</td></tr>
      <tr><td>429</td><td>PUBLIC_RESOURCE_RATE_LIMITED / CLIENT_RATE_LIMITED / CHALLENGE_RATE_LIMITED / REQUEST_QUOTA_EXHAUSTED / TRAFFIC_LIMIT_EXCEEDED</td><td data-i18n="api.errorRate">请求、挑战或流量额度受到限制；优先遵循 Retry-After。</td></tr>
      <tr><td>503</td><td>CHALLENGE_CAPACITY_REACHED / VDF_BUSY</td><td data-i18n="api.errorBusy">挑战服务暂时繁忙；通常会返回 Retry-After。</td></tr>
      <tr><td>500</td><td>PUBLIC_INTERNAL_ERROR</td><td data-i18n="api.errorInternal">服务端内部错误；记录 request_id 后重试或反馈。</td></tr>
    </tbody></table>

    <h3 data-i18n="api.contractBindingTitle">客户端来源绑定</h3>
    <p data-i18n="api.contractBinding">下载授权协议的 challenge 创建与 authorization 提交必须从同一个被服务端识别的客户端网络前缀完成；授权状态查询也要求与签发时的来源前缀一致。因此不要让不同出口 IP 的机器分别完成挑战创建、solution 提交和状态查询。授权成功后，download_token 是 Bearer 凭据；实际访问下载节点时允许出口发生变化，因此必须像密码一样保护该令牌。</p>
  </section>
`
