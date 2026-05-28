# 管理 API

> 状态：待确认，M2 实现合同
> 版本前缀：`/api/admin/v1`
> 需求基线：`docs/开发要求.md` 定稿 v1.0（2026-05-27）
> 设计来源：`docs/M2-控制面详细设计.md`
> 阶段边界：本文只定义 M2 的节点配对审批、证书身份、节点控制状态及报告查询；扫描、同步任务执行、统计查询等后续业务不在 M2 启用。

## 1. 安全原则

| 主题 | 固定要求 |
| --- | --- |
| 服务位置 | 管理 API 只挂载在管理监听器，不挂载至公共 HTTP 或节点控制端口 |
| 传输保护 | M2 管理 API 使用 HTTPS，防止管理令牌明文传输 |
| 网络限制 | 来源必须属于显式管理网络范围；不以公共代理转发头扩大允许范围 |
| 管理令牌 | 每个请求必须携带强随机 Bearer 令牌；令牌只从配置指定环境变量读取 |
| 高风险认证 | 配对码创建/撤销、配对审批/拒绝、证书轮换、节点禁用/启用、同步强制重置必须额外验证管理员 mTLS |
| 日志脱敏 | 管理令牌、配对码明文、私钥、完整客户端 IP 和完整证书/CSR 正文不得写入普通日志 |
| 请求关联 | 每个管理请求生成新的请求 ID，响应返回 `X-Request-ID`，状态变更写入审计 |
| 阶段限制 | M2 不能把任何新节点或恢复节点标为在线可路由 |

## 2. 监听、认证与鉴权顺序

### 2.1 配置依赖

| 配置 | 当前状态 | M2 合同 |
| --- | --- | --- |
| `server.management_listen` | M1 已存在，当前默认并强制回环 | 管理 API 的唯一监听入口 |
| `admin.allowed_cidrs` | M1 已存在且必须配置 | 来源地址访问控制 |
| `admin.token_env` | M1 已存在且必须配置 | 指向强随机 Bearer 令牌环境变量 |
| `admin.high_risk_require_mtls` | M1 已存在且固定为 `true` | 不允许关闭高风险 mTLS |
| `admin.tls.cert_file/key_file` | M2 预计新增 | HTTPS 服务端身份 |
| `admin.tls.client_ca_file` | M2 预计新增 | 验证管理员客户端证书 |

若管理服务启用但 HTTPS 材料、令牌环境变量内容或高风险管理员 CA 不满足要求，管理服务不得启动为可用。

### 2.2 请求鉴权顺序

1. 接收 HTTPS 请求并生成新的主节点请求 ID；任何客户端传入的同名值只能作为校验后的父关联信息。
2. 根据 TCP 对端来源检查 `admin.allowed_cidrs`。M2 管理 API 不信任代理头作为开放管理来源的依据。
3. 读取 `Authorization: Bearer <token>`，与 `admin.token_env` 对应环境变量内容恒定时间比较。
4. 若路由为高风险操作，校验 TLS 客户端证书链、用途、有效期及管理员允许身份。
5. 校验 JSON 请求体并执行事务。
6. 状态变更操作写审计；响应返回 `X-Request-ID` 与 JSON 中的 `request_id`。

认证失败响应不得使调用方可靠区分是网络、令牌还是管理员证书不匹配；安全诊断只进入受控日志或审计摘要。

## 3. 通用 HTTP 合同

### 3.1 请求头

| 请求头 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | `Bearer <管理令牌>`，值不得记录 |
| `Content-Type` | 有 JSON 请求体时是 | `application/json; charset=utf-8` |
| `X-Request-ID` | 否 | 仅可作为父关联候选，服务端始终生成新的主请求 ID |

### 3.2 成功响应

```json
{
  "status": "success",
  "message": "操作已完成",
  "request_id": "服务端请求标识",
  "data": {}
}
```

### 3.3 错误响应

```json
{
  "status": "error",
  "code": "INVALID_REQUEST",
  "message": "请求内容不合法",
  "request_id": "服务端请求标识"
}
```

| HTTP 状态 | 错误码 | 含义 |
| --- | --- | --- |
| `400` | `INVALID_REQUEST` | JSON 或字段不合法 |
| `401` | `ADMIN_AUTH_FAILED` | 管理认证失败的统一外部表述 |
| `403` | `ADMIN_ACCESS_DENIED` | 认证通过但不允许当前操作 |
| `404` | `RESOURCE_NOT_FOUND` | 指定记录不存在 |
| `409` | `STATE_CONFLICT` | 状态已变化、操作不可重复或配对码已消费 |
| `410` | `RESOURCE_EXPIRED` | 配对码或登记领取窗口已过期 |
| `500` | `CONTROL_INTERNAL_ERROR` | 服务端处理失败，使用请求 ID 排查 |

## 4. 高风险路由分级

| 路由类别 | 管理网络 | Bearer 令牌 | 管理员 mTLS | 审计 |
| --- | --- | --- | --- | --- |
| 查询节点与最近报告 | 必须 | 必须 | 不强制 | 可记录查询异常，不强制状态审计 |
| 查询待审批列表 | 必须 | 必须 | 不强制 | 不返回敏感正文 |
| 查看单条 CSR 核对信息 | 必须 | 必须 | 必须 | 记录访问目标 |
| 创建/撤销配对码 | 必须 | 必须 | 必须 | 必须 |
| 审批/拒绝配对 | 必须 | 必须 | 必须 | 必须 |
| 禁用/启用节点 | 必须 | 必须 | 必须 | 必须 |
| 轮换节点证书 | 必须 | 必须 | 必须 | 必须 |
| 强制同步重置 | 必须 | 必须 | 必须 | 必须；M2 只重置状态，不运行同步 |

## 5. 配对码接口

### 5.1 创建配对码

`POST /api/admin/v1/pairing-codes`

鉴权：高风险。

请求：

```json
{
  "ttl_seconds": 300,
  "note": "华东节点首次接入"
}
```

| 字段 | 规则 |
| --- | --- |
| `ttl_seconds` | 可选，必须为短时有效且不得超过实现配置允许上限；默认来自 `node.pairing_code_ttl` |
| `note` | 可选运维说明，长度受限，不得包含凭据 |

成功响应 `201 Created`：

```json
{
  "status": "success",
  "message": "一次性配对码已创建，请安全传递并在有效期内使用",
  "request_id": "请求标识",
  "data": {
    "pairing_code_id": "配对记录标识",
    "pairing_code": "仅本次响应返回的明文配对码",
    "expires_at": "2026-05-28T02:05:00Z"
  }
}
```

事务与脱敏要求：

- 数据库只保存安全哈希、记录 ID、到期时间、创建请求 ID 和非敏感提示信息。
- 明文码只在创建成功响应中返回一次，不进入日志、审计详情或后续查询。

### 5.2 撤销配对码

`DELETE /api/admin/v1/pairing-codes/{pairing_code_id}`

鉴权：高风险。

| 结果 | HTTP 状态 | 行为 |
| --- | --- | --- |
| 尚未使用且未过期 | `200` | 标记撤销并审计 |
| 已消费 | `409` | 不影响已存在登记请求 |
| 已过期 | `410` | 不恢复使用 |
| 不存在 | `404` | 不披露其他凭据信息 |

## 6. 登记审批接口

### 6.1 查询登记列表

`GET /api/admin/v1/pairing-requests?status=pending`

鉴权：普通管理查询。

响应数据项：

| 字段 | 含义 | 是否敏感 |
| --- | --- | --- |
| `enrollment_id` | 登记标识 | 否 |
| `public_name` | 节点公开名称 | 否 |
| `public_key_fingerprint_hint` | 截断公钥指纹 | 有限展示 |
| `status` | `pending`、`approved`、`rejected`、`expired`、`collected` | 否 |
| `submitted_at`、`expires_at` | 时间 | 否 |
| `request_id` | 登记关联 ID | 否 |

不得返回配对码、证书私钥或完整 CSR 正文。

### 6.2 查看登记详情

`GET /api/admin/v1/pairing-requests/{enrollment_id}`

鉴权：高风险。

用于管理员核对完整公钥指纹、CSR 主体和请求能力。响应可包含 CSR 的必要公钥/主体信息或下载核对材料，但普通日志不得记录正文。

### 6.3 批准登记

`POST /api/admin/v1/pairing-requests/{enrollment_id}/approve`

鉴权：高风险。

请求：

```json
{
  "confirmed_public_name": "华东下载节点",
  "confirmed_public_key_fingerprint": "sha256:十六进制指纹"
}
```

成功事务必须：

1. 验证登记状态仍为 `pending` 且未过审批窗口。
2. 验证管理员确认的节点名称与公钥指纹和登记材料一致。
3. 签发仅供客户端认证的节点证书，写入证书身份记录。
4. 创建或绑定唯一节点 ID，初始状态为 `syncing`，`routing_ready=false`。
5. 将登记状态改为 `approved`，等待节点一次性领取证书。
6. 写入高风险审计，关联管理请求 ID、登记 ID、节点 ID 和证书 ID。

成功响应不返回私钥；证书链由节点通过登记通道领取。

### 6.4 拒绝登记

`POST /api/admin/v1/pairing-requests/{enrollment_id}/reject`

鉴权：高风险。

请求仅接受受限长度的非敏感理由摘要。拒绝后原配对码不恢复使用，节点必须获得新配对码才能重新登记。

## 7. 节点查询与控制接口

### 7.1 节点列表

`GET /api/admin/v1/nodes`

鉴权：普通管理查询。

响应项：

| 字段 | 含义 |
| --- | --- |
| `node_id` | 节点标识 |
| `public_name` | 公开名称 |
| `state` | `syncing`、`offline`、`disabled` 等 M2 控制状态 |
| `routing_ready` | M2 必须为 `false` |
| `last_heartbeat_at` | 最近有效心跳时间，可空 |
| `certificate_not_after` | 当前证书到期时间摘要 |
| `latest_inventory_revision` | 最近接收库存报告修订，可空 |
| `latest_pressure_at` | 最近压力报告时间，可空 |

### 7.2 节点详情

`GET /api/admin/v1/nodes/{node_id}`

鉴权：普通管理查询。

可返回：

- 节点管理和连接状态。
- 最近心跳摘要、最近库存报告接收结果、最近压力摘要。
- 当前活动证书指纹的截断展示与有效期。
- 相关最近审计事件的非敏感摘要。

不得返回节点私钥、证书 PEM、完整本地磁盘路径、完整管理来源地址或公共下载路由信息。

### 7.3 禁用节点

`POST /api/admin/v1/nodes/{node_id}/disable`

鉴权：高风险。

请求：

```json
{
  "reason": "运维禁用原因摘要"
}
```

事务结果：

- 管理状态改为 `disabled`。
- `routing_ready` 强制为 `false`。
- 当前活动控制连接被关闭，后续证书会话被拒绝。
- 写审计并携带管理请求 ID。

### 7.4 启用节点

`POST /api/admin/v1/nodes/{node_id}/enable`

鉴权：高风险。

启用仅允许节点重新使用有效证书建立控制连接；输出状态回到 `syncing` 或等待心跳，不得直接恢复 `routing_ready=true`。

### 7.5 轮换节点证书

`POST /api/admin/v1/nodes/{node_id}/certificates/rotate`

鉴权：高风险。

M2 合同：

- 验证节点存在且操作状态允许。
- 签发新证书身份或建立安全领取过程。
- 将旧活动证书标记失效，关闭旧证书建立的会话。
- 审计新旧证书记录 ID 和请求 ID，不写 PEM 或私钥。
- 新证书连接后节点仍处于同步中/不可路由。

### 7.6 强制同步重置

`POST /api/admin/v1/nodes/{node_id}/sync-reset`

鉴权：高风险。

M2 仅支持安全状态效果：

- 将节点控制状态置为 `syncing`。
- 确保 `routing_ready=false`。
- 写入审计。

M2 不下发资产同步任务、不扫描 Release、不执行库存对账；实际重置任务行为属于 M3。

## 8. 最近报告查询

| 方法和路径 | 鉴权 | M2 输出 |
| --- | --- | --- |
| `GET /api/admin/v1/nodes/{node_id}/heartbeats/latest` | 普通管理查询 | 最近心跳摘要和接收时间 |
| `GET /api/admin/v1/nodes/{node_id}/inventory-reports/latest` | 普通管理查询 | 最近库存报告元数据、条数、接收结果；不声称资产已校验 |
| `GET /api/admin/v1/nodes/{node_id}/pressure-reports/latest` | 普通管理查询 | 最近压力摘要；不声称已参与路由 |

库存报告详情若未来需要暴露，必须限制条目大小并继续遵守 M2 不认定可服务副本的边界。

## 9. 审计合同

状态修改或高风险数据查看必须记录：

| 字段 | 内容 |
| --- | --- |
| `operation` | 如 `pairing_code.create`、`pairing.approve`、`node.disable`、`certificate.rotate` |
| `target_type`、`target_id` | 被操作资源类型与标识 |
| `result` | 成功、拒绝或失败稳定结果 |
| `request_id` | 本次管理请求标识 |
| `admin_identity` | 管理员客户端证书身份的安全摘要，高风险操作必填 |
| `details_summary` | 不含凭据的中文摘要 |
| `created_at` | UTC 时间 |

严禁写入审计或普通日志：

- 管理令牌及 `Authorization` 原文。
- 配对码明文或完整可验证哈希。
- 证书私钥。
- 完整客户端 IP。
- 完整 CSR/证书 PEM 正文。

## 10. M2 之外的接口边界

| 能力 | 所属阶段 | M2 处理 |
| --- | --- | --- |
| 手动 GitHub Release 扫描 | M3 | 不提供有效执行路由 |
| 下发同步任务与最终库存对账 | M3 | 仅可保留状态重置入口 |
| 公共 API、下载授权或 Range | M4 | 不挂载 |
| 额度、流量入账、统计和 SLA | M5 | 不查询为可用业务结论 |

## 11. M3 扫描与同步管理接口

M3 已在现有管理 API 上增加 Release 扫描、同步任务和库存对账相关接口。所有接口仍只挂载在管理监听器，仍要求管理网络和 Bearer 管理令牌；会改变扫描、同步或节点就绪状态的操作属于高风险，必须叠加管理员 mTLS。

### 11.1 手动触发扫描

`POST /api/admin/v1/sync/scans`

鉴权：高风险。

请求：

```json
{
  "project_id": "example"
}
```

字段规则：

- `project_id` 可选；为空表示扫描 `projects.yaml` 中全部启用项目。
- 不允许传入任意仓库地址。
- 不允许临时覆盖 `include_prerelease`、`retain_versions` 或资产过滤规则。

成功响应 `202 Accepted`：

```json
{
  "status": "success",
  "message": "Release 扫描任务已创建",
  "request_id": "请求标识",
  "data": {
    "scan_id": "扫描任务标识",
    "state": "pending"
  }
}
```

手动扫描不能绕过 GitHub `sha256:` 摘要门禁；不能直接把节点设为 `routing_ready=true`。

### 11.2 查询扫描状态

`GET /api/admin/v1/sync/scans/latest?project_id=example`

鉴权：普通管理查询。

响应数据可包含：

| 字段 | 含义 |
| --- | --- |
| `scan_id` | 扫描任务标识 |
| `project_id` | 项目标识 |
| `state` | `pending`、`running`、`succeeded`、`failed`、`deferred` |
| `selected_releases` | 本轮选中的 Release 数 |
| `accepted_assets` | 通过摘要门禁的资产数 |
| `rejected_assets` | 摘要缺失、算法不符或过滤拒绝的资产数 |
| `next_allowed_scan_at` | GitHub 限频退避时间，可空 |
| `request_id` | 任务关联请求 ID |

响应不得返回 GitHub Token、完整外部错误正文或敏感请求头。

### 11.3 查询节点同步状态

`GET /api/admin/v1/nodes/{node_id}/sync-status`

鉴权：普通管理查询。

响应数据：

| 字段 | 含义 |
| --- | --- |
| `node_id` | 节点标识 |
| `routing_ready` | 是否完成 M3 目标库存最终对账 |
| `required_assets` | 当前目标资产数 |
| `verified_assets` | 已验证持有资产数 |
| `missing_assets` | 缺失资产数 |
| `mismatched_assets` | 摘要或大小不一致资产数 |
| `running_tasks` | 运行中任务数 |
| `failed_tasks` | 失败任务数 |
| `latest_inventory_revision` | 最近完整库存报告修订 |

M3 管理 API 可以展示同步就绪状态，但不得返回公共下载 URL、下载令牌或本地绝对路径。

### 11.4 重试与重新对账

| 方法和路径 | 鉴权 | 行为 |
| --- | --- | --- |
| `POST /api/admin/v1/nodes/{node_id}/sync-reconcile` | 高风险 | 要求节点上报完整库存并重新计算差异 |
| `POST /api/admin/v1/nodes/{node_id}/sync-tasks/{task_id}/retry` | 高风险 | 重试失败或等待中的同步任务 |
| `POST /api/admin/v1/nodes/{node_id}/sync-tasks/{task_id}/cancel` | 高风险 | 取消尚未完成且已因目标库存变化失效的任务 |

这些操作只影响 M3 同步任务和最终对账，不执行 M4 下载授权、M5 额度扣减或流量入账。

### 11.5 M3 审计

以下操作必须写入 `admin_audit_events`：

- 手动触发扫描。
- 强制节点重新对账。
- 重试或取消同步任务。
- 因最终对账把 `routing_ready` 置为 true 或置回 false。

审计摘要只记录项目 ID、节点 ID、任务 ID、结果和请求 ID，不记录 GitHub Token、管理令牌、完整本地路径或证书/私钥内容。

本文经确认后，M2/M3 管理 API 实现必须与字段、鉴权级别、状态效果和审计合同一致；新增路由或改变高风险分级前必须先更新本文。

## 12. M4 管理 API 边界

M4 不要求新增管理 API 路由。现有 M3 管理 API 可继续用于确认节点是否 `routing_ready=true`、资产是否同步完成、任务是否失败，以及节点是否被禁用。

### 12.1 M4 可查询的管理事实

| 现有接口 | M4 用途 |
| --- | --- |
| `GET /api/admin/v1/nodes` | 排查节点是否在线、禁用或可路由 |
| `GET /api/admin/v1/nodes/{node_id}/sync-status` | 排查某节点是否持有目标资产 |
| `GET /api/admin/v1/sync/scans/latest` | 排查公开页面资产列表是否来自最新扫描 |
| 同步任务重试/重新对账接口 | 修复 M4 下载授权前的副本缺失或不一致 |

### 12.2 M4 不新增的管理能力

| 能力 | 处理 |
| --- | --- |
| 查询公开下载授权次数统计 | M5 再实现 |
| 查询开始传输授权数 | M5 再实现 |
| 查询每日/累计流量 | M5 再实现 |
| 查询节点 SLA | M5 再实现 |
| 人工改写下载授权或绕过验证签发令牌 | 不提供 |
| 从管理 API 直接返回公共下载令牌 | 不提供 |

若 M4 实现过程中确需增加只读排障接口，必须继续挂载在管理监听器，遵守管理网络、Bearer 令牌、高风险 mTLS 分级、中文错误、请求 ID 和日志脱敏要求，并先更新本文。
