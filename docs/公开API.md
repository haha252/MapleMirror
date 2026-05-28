# 公开 API

> 状态：已实现，进入 M5 前评估
> 版本前缀：`/api/public/v1`
> 需求基线：`docs/开发要求.md` 定稿 v1.1（2026-05-27）
> 阶段边界：本文记录 M4 的项目查询、网页 ALTCHA 下载授权、公开 API SHA-256 前导零 PoW、节点绑定令牌、Range 下载和错误格式；M5 前不启用额度扣减、流量入账、统计聚合或 SLA。

## 1. 通用原则

| 主题 | 规则 |
| --- | --- |
| 身份 | 公开 API 不使用账号、API Key 或管理令牌 |
| 网页验证 | 网页端只使用自托管 ALTCHA，不叠加滑块或网页自研 PoW |
| API 验证 | 公开 API 使用独立 SHA-256 前导零 PoW，默认前导 `23` 个二进制零位 |
| 参数隔离 | `altcha.*` 与 `api_pow.*` 不得混用或互相解释 |
| 授权 | 所有下载必须先由主节点签发短时、单节点绑定下载令牌 |
| 节点 | 下载节点只服务本地已验证资产，不接受未签名或跨节点令牌 |
| Range | 节点支持 HTTP Range 和断点续传，并限制同一令牌并发分片 |
| 请求 ID | 每个响应返回 `X-Request-ID`，JSON 中包含 `request_id` |

## 2. 通用响应

### 2.1 成功响应

```json
{
  "status": "success",
  "message": "操作已完成",
  "request_id": "请求标识",
  "data": {}
}
```

### 2.2 错误响应

```json
{
  "status": "error",
  "code": "INVALID_REQUEST",
  "message": "请求内容不合法",
  "request_id": "请求标识"
}
```

| HTTP 状态 | 错误码 | 含义 |
| --- | --- | --- |
| `400` | `INVALID_REQUEST` | 参数或 JSON 不合法 |
| `401` | `CHALLENGE_REQUIRED` | 需要先完成挑战 |
| `401` | `DOWNLOAD_TOKEN_INVALID` | 下载令牌无效、过期或签名不符 |
| `403` | `CHALLENGE_FAILED` | ALTCHA 或 PoW 校验失败 |
| `403` | `CLIENT_PREFIX_MISMATCH` | 客户端 IP 前缀与挑战或令牌不一致 |
| `404` | `ASSET_NOT_FOUND` | 项目、版本或资产不存在 |
| `409` | `CHALLENGE_CONSUMED` | 挑战已被使用 |
| `409` | `NO_ROUTABLE_NODE` | 当前没有可用下载节点 |
| `416` | `RANGE_NOT_SATISFIABLE` | Range 不合法或超出文件范围 |
| `429` | `QUOTA_NOT_ENABLED` | M4 占位；M5 才启用真实额度拒绝 |
| `500` | `PUBLIC_INTERNAL_ERROR` | 服务端处理失败，使用请求 ID 排查 |

## 3. 项目与资产查询

### 3.1 查询项目列表

`GET /api/public/v1/projects`

返回已启用且至少存在可展示 Release 的项目摘要。M4 只展示 M3 已扫描入库的数据。

```json
{
  "status": "success",
  "message": "查询成功",
  "request_id": "请求标识",
  "data": {
    "projects": [
      {
        "project_id": "example",
        "repository": "owner/repo",
        "display_name": "示例项目",
        "available": true
      }
    ]
  }
}
```

### 3.2 查询项目资产

`GET /api/public/v1/projects/{project_id}/assets`

返回保留窗口内资产，按版本、预发布标记和架构组织。只有至少一个 `routing_ready=true` 节点已验证持有的资产才可标记为 `available=true`。

```json
{
  "status": "success",
  "message": "查询成功",
  "request_id": "请求标识",
  "data": {
    "assets": [
      {
        "asset_id": "asset_123",
        "version": "v1.2.3",
        "prerelease": false,
        "file_name": "example-windows-amd64.zip",
        "architecture": "windows-amd64",
        "size_bytes": 123456,
        "digest_sha256": "sha256:64位十六进制摘要",
        "available": true,
        "unavailable_reason": ""
      }
    ]
  }
}
```

## 4. 网页 ALTCHA 授权接口

网页页面可以使用公开 API 下的 ALTCHA 接口；这些接口仅服务浏览器下载链路。

### 4.1 创建 ALTCHA 挑战

`POST /api/public/v1/web/challenges`

请求：

```json
{
  "asset_id": "asset_123"
}
```

成功响应：

```json
{
  "status": "success",
  "message": "挑战已创建",
  "request_id": "请求标识",
  "data": {
    "challenge_id": "挑战标识",
    "altcha": {
      "challenge": "ALTCHA挑战字段",
      "salt": "ALTCHA盐值",
      "algorithm": "sha256",
      "signature": "服务端签名"
    },
    "expires_at": "2026-05-28T12:00:00Z"
  }
}
```

### 4.2 提交 ALTCHA 并领取授权

`POST /api/public/v1/web/authorizations`

请求：

```json
{
  "challenge_id": "挑战标识",
  "asset_id": "asset_123",
  "altcha_payload": "ALTCHA组件提交内容"
}
```

成功响应：

```json
{
  "status": "success",
  "message": "下载授权已签发",
  "request_id": "请求标识",
  "data": {
    "authorization_id": "授权标识",
    "download_url": "https://node.example/downloads/asset_123",
    "download_token": "短时签名令牌",
    "expires_at": "2026-05-28T12:05:00Z",
    "range_concurrency_limit": 4,
    "max_bytes": 123456
  }
}
```

## 5. 公开 API PoW 授权接口

### 5.1 创建 API PoW 挑战

`POST /api/public/v1/api/challenges`

请求：

```json
{
  "asset_id": "asset_123"
}
```

成功响应：

```json
{
  "status": "success",
  "message": "挑战已创建",
  "request_id": "请求标识",
  "data": {
    "challenge_id": "挑战标识",
    "asset_id": "asset_123",
    "nonce_seed": "服务端随机量",
    "algorithm": "sha256",
    "leading_zero_bits": 23,
    "expires_at": "2026-05-28T12:00:00Z",
    "canonical_format": "download.v1:{challenge_id}:{asset_id}:{nonce_seed}:{nonce}"
  }
}
```

客户端需要寻找 `nonce`，使：

```text
SHA-256("download.v1:{challenge_id}:{asset_id}:{nonce_seed}:{nonce}")
```

的二进制摘要满足前导零位数要求。默认难度是前导 `23` 个二进制零位。

### 5.2 提交 API PoW 并领取授权

`POST /api/public/v1/api/authorizations`

请求：

```json
{
  "challenge_id": "挑战标识",
  "asset_id": "asset_123",
  "nonce": "客户端找到的nonce"
}
```

成功响应与网页授权一致。挑战提交后必须被消费，重复提交返回 `CHALLENGE_CONSUMED`。

## 6. 授权查询

`GET /api/public/v1/authorizations/{authorization_id}`

M4 返回授权基本状态，不返回 M5 流量结算结果。

```json
{
  "status": "success",
  "message": "查询成功",
  "request_id": "请求标识",
  "data": {
    "authorization_id": "授权标识",
    "asset_id": "asset_123",
    "node_id": "公开节点标识或脱敏节点名",
    "state": "issued",
    "expires_at": "2026-05-28T12:05:00Z",
    "bytes_accounting_enabled": false,
    "sent_bytes": null
  }
}
```

## 7. 节点下载接口

节点文件服务路径由主节点返回的 `download_url` 决定。推荐形式：

```text
GET /downloads/{asset_id}
Authorization: Bearer <download_token>
Range: bytes=0-1048575
```

也可在浏览器下载场景使用一次性查询参数传递令牌，但节点日志必须脱敏并避免把完整 URL 写入普通日志。

### 7.1 成功响应头

| 响应头 | 说明 |
| --- | --- |
| `Accept-Ranges: bytes` | 支持断点续传 |
| `Content-Length` | 本次响应字节数 |
| `Content-Range` | `206` 时返回 |
| `Content-Disposition` | 使用安全文件名 |
| `X-Request-ID` | 节点侧请求 ID |
| `X-Authorization-Request-ID` | 主节点签发请求 ID，可选 |

### 7.2 Range 示例

请求：

```text
GET /downloads/asset_123 HTTP/1.1
Authorization: Bearer <download_token>
Range: bytes=1048576-2097151
```

响应：

```text
HTTP/1.1 206 Partial Content
Accept-Ranges: bytes
Content-Range: bytes 1048576-2097151/12345678
Content-Length: 1048576
X-Request-ID: 节点请求标识
```

M4 可以不支持单个请求内的 multipart Range。若收到多段 Range，实现应返回 `400 INVALID_REQUEST` 或 `416 RANGE_NOT_SATISFIABLE`，并保持错误码稳定。

## 8. 下载令牌声明

下载令牌至少绑定：

| 声明 | 必填 | 说明 |
| --- | --- | --- |
| `token_version` | 是 | `download.v1` |
| `authorization_id` | 是 | 主节点授权记录 |
| `asset_id` | 是 | 只能下载该资产 |
| `node_id` | 是 | 只能由该节点接受 |
| `client_prefix` | 是 | 客户端 IP 前缀 |
| `expires_at` | 是 | 短时有效期 |
| `max_bytes` | 是 | 最大允许发送字节 |
| `range_concurrency_limit` | 是 | 并发 Range 限制 |
| `request_id` | 是 | 主节点签发请求 ID |

伪造、过期、跨节点、跨资产、跨客户端前缀复用的令牌必须被拒绝。

## 9. M5 前的占位字段

统计页和部分 API 可以返回下列占位字段，但必须明确未启用：

| 字段 | M4 值 |
| --- | --- |
| `quota_enabled` | `false` |
| `traffic_accounting_enabled` | `false` |
| `statistics_enabled` | `false` |
| `sla_enabled` | `false` |
| `authorization_count` | `null` 或“统计尚未启用” |
| `started_transfer_count` | `null` 或“统计尚未启用” |
| `daily_bytes` | `null` 或“统计尚未启用” |
| `node_sla` | `null` 或“统计尚未启用” |

M4 不得把这些占位值解释为真实统计结果。

## 10. M4 实现说明

- 主节点公共接口挂载在 `server.public_listen`。
- 下载节点文件服务挂载在 `/downloads/{asset_id}`。
- 主节点和下载节点必须配置同一份 `download_token.signing_key_file` 强随机密钥文件。
- M4 的 `download_url` 返回节点相对下载路径；部署时可由反向代理转换为实际节点域名。
- M4 已支持单段 HTTP Range；multipart Range 不作为 M4 必须能力。

## 11. 脱敏与兼容

- 公共 API 响应不得包含节点内部地址、控制端口、证书、磁盘路径、GitHub Token 或完整客户端 IP。
- 日志不得记录完整 `download_token`、完整 ALTCHA payload、完整 PoW 规范字符串或完整 URL 查询令牌。
- JSON 字段新增必须保持向后兼容；删除或重命名字段前必须更新本文并经过阶段确认。
- 所有中文错误、页面文案和文档使用 UTF-8。

## 12. M5 额度与统计接口变化

M5 启用后，公开 API 不再使用 `QUOTA_NOT_ENABLED` 占位错误。网页 ALTCHA 授权和公开 API PoW 授权在签发下载令牌前必须完成真实请求额度扣减和每日流量预算预留。

### 12.1 新增或变更错误码

| HTTP 状态 | 错误码 | 含义 |
| --- | --- | --- |
| `429` | `REQUEST_QUOTA_EXHAUSTED` | IPv4 `/32`、IPv4 `/24`、IPv6 `/128` 或 IPv6 `/64` 请求额度不足 |
| `429` | `TRAFFIC_LIMIT_EXCEEDED` | 地址级或网段级每日实际流量预算不足 |
| `409` | `AUTHORIZATION_REVOKED` | 下载授权已撤销或因额度异常被阻断 |

错误响应仍使用中文 `message`、稳定 `code` 和 `request_id`，并返回 `X-Request-ID`。

### 12.2 授权查询响应

`GET /api/public/v1/authorizations/{authorization_id}` 在 M5 后可返回真实入账字段：

```json
{
  "status": "success",
  "message": "查询成功",
  "request_id": "请求标识",
  "data": {
    "authorization_id": "授权标识",
    "asset_id": "asset_123",
    "node_id": "公开节点标识或脱敏节点名",
    "state": "issued",
    "expires_at": "2026-05-28T12:05:00Z",
    "bytes_accounting_enabled": true,
    "sent_bytes": 1048576,
    "first_transfer_at": "2026-05-28T12:01:00Z"
  }
}
```

`sent_bytes` 只表示已由主节点幂等入账的真实发送字节，可能滞后于节点正在传输的瞬时值。

### 12.3 统计数据页字段

M5 后统计数据页展示：

| 字段 | 口径 |
| --- | --- |
| 下载授权次数 | 主节点成功签发下载令牌次数 |
| 开始传输授权数 | 授权首次产生正字节响应体次数 |
| 当日流量 | 当前统计日真实发送字节 |
| 累计流量 | 所有已入账真实发送字节 |
| 项目统计 | 项目维度授权数、开始传输数和真实发送字节 |

公开统计不得展示完整客户端 IP、额度桶精确余额、单个授权明细、单个请求 ID 列表、节点内部地址或控制面信息。

### 12.4 节点状态页字段

M5 后节点状态页展示公开节点名称、公开状态、同步就绪、最近更新时间、负载分档和近 `24h`、`7d`、`30d` SLA。样本不足时显示“统计样本不足”。

节点状态页不得展示真实带宽目标、内部压力原始值、管理地址、控制通道地址、证书信息、磁盘路径、完整客户端 IP 或下载令牌。

### 12.5 M4 令牌兼容

M5 上线前已签发且未过期的 M4 下载令牌仍按签名、节点、资产、客户端前缀和过期时间校验。若该授权缺少 M5 流量预留，主节点在首次流量事件入账时走兼容路径补建或标记旧授权预留；已真实发送的字节必须入账，后续超限请求可拒绝或撤销。
