package public

const apiDocsAuthorizationStatusDetails = `
  <section class="api-endpoint" id="authorization-status-api">
    <div class="api-route"><span class="api-method method-get">GET</span><code>/api/public/v2/authorizations/{authorization_id}</code></div>
    <p data-i18n="api.authorizationDescription">携带对应 download_token 查询 V2 授权状态、过期时间、公开节点名和已入账真实发送字节。旧的 /api/public/v1/authorizations/{authorization_id} 目前仅作为兼容别名保留，新客户端不要使用它。</p>
    <table class="api-params"><thead><tr><th data-i18n="api.parameter">参数</th><th data-i18n="api.type">类型</th><th data-i18n="api.description">描述</th></tr></thead><tbody><tr><td>authorization_id</td><td>Path</td><td data-i18n="api.authorizationIdentifier">授权标识</td></tr><tr><td>Authorization</td><td>Header</td><td>Bearer &lt;download_token&gt;</td></tr></tbody></table>
    <p class="api-note" data-i18n="api.authorizationNodeName">node_name 是新的明确字段；node_id 为兼容旧客户端暂时保留，目前同样返回公开节点名称，并不暴露内部节点 ID。新客户端请读取 node_name。</p>
    <p class="api-label" data-i18n="api.exampleRequest">Example Request</p>
    <pre><code>GET /api/public/v2/authorizations/auth_123
Authorization: Bearer &lt;download_token&gt;</code></pre>
    <p class="api-label" data-i18n="api.exampleResponse">Example Response</p>
    <pre><code>{
  "status": "success",
  "data": {
    "authorization_id": "auth_123",
    "asset_id": "asset_123",
    "node_name": "public-node-name",
    "node_id": "public-node-name",
    "state": "issued",
    "expires_at": "2026-09-03T12:05:00Z",
    "bytes_accounting_enabled": true,
    "sent_bytes": 1048576,
    "first_transfer_at": "2026-09-03T12:01:10Z"
  }
}</code></pre>
  </section>
`
