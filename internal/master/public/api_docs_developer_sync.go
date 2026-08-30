package public

const apiDeveloperSyncDetails = `
  <section class="api-endpoint" id="developer-sync-api">
    <h2 data-i18n="api.developerSyncTitle">项目开发者更新检测</h2>
    <div class="api-route"><span class="api-method method-post">POST</span><code>/api/developer/v1/projects/{project_id}/sync</code></div>
    <p data-i18n="api.developerSyncDescription">供镜像项目所有者在发布 GitHub Release 后主动触发一次该项目的更新检测。Developer API Token 与单个项目绑定，只能触发对应项目；Token 可在管理后台项目卡片中生成或重置。</p>
    <table class="api-params"><thead><tr><th data-i18n="api.parameter">参数</th><th data-i18n="api.type">类型</th><th data-i18n="api.description">描述</th></tr></thead><tbody><tr><td>project_id</td><td>Path</td><td data-i18n="api.projectIdentifier">项目标识</td></tr><tr><td>Authorization</td><td>Header</td><td data-i18n="api.developerSyncAuth">Bearer 项目 Developer API Token</td></tr></tbody></table>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>POST /api/developer/v1/projects/example/sync
Authorization: Bearer &lt;project_token&gt;</code></pre>
    <p data-i18n="api.developerSyncQuota">默认每个项目每天允许 100 次有效触发。响应会返回 X-RateLimit-Limit、X-RateLimit-Remaining 和 X-RateLimit-Reset；额度耗尽时返回 429。</p>
    <p data-i18n="api.developerSyncDuplicate">正常接受请求时返回 202。若同一项目已有扫描正在执行，同样返回 202，但不会启动第二次扫描，也不会再次消耗每日额度。</p>
    <p class="api-label" data-i18n="api.exampleResponse">Example Response</p>
    <pre><code>{
  "status": "success",
  "message": "更新检测任务已接受",
  "data": {"project_id": "example", "accepted": true, "already_running": false}
}</code></pre>
    <p data-i18n="api.developerSyncStatus">常见状态码：401 表示 Token 缺失或无效；403 表示 Token 与项目不匹配；404 表示项目不存在；409 表示项目已禁用；429 表示每日额度耗尽或认证失败次数过多。</p>
  </section>
`
