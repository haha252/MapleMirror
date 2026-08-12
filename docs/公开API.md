# 公开 API

> 状态：已实现，进入 M6 前评估
> 版本前缀：`/api/public/v1` 与 `/api/public/v2`
> 需求基线：`docs/开发要求.md` 定稿 v1.1（2026-05-27）
> 阶段边界：本文记录项目查询、V2 RSA repeated-squaring 下载挑战、可选 V1 SHA-256 API、节点绑定令牌、Range 下载、额度与流量入账、统计聚合、SLA 和错误格式。

## 1. 通用原则

| 主题 | 规则 |
| --- | --- |
| 身份 | 公开 API 不使用账号、API Key 或管理面板会话 |
| 网页验证 | 正常页只使用 V2 3072 位 RSA repeated-squaring 和单 Worker 原生 BigInt；惩罚页仍使用独立 SHA-256 前导 128 零位逻辑 |
| API 验证 | API V2 与正常网页共用 repeated-squaring 协议；API V1 在 `api_pow.v1_enabled=true` 时保持原 SHA-256 合同 |
| 参数隔离 | 挑战按来源、协议版本和算法显式验证，未知值不得回落到 V1 或 Web 分支 |
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
| `403` | `CHALLENGE_FAILED` | 网页挑战或 PoW 校验失败 |
| `403` | `CLIENT_BLOCKED` | 客户端命中静态或订阅黑名单 |
| `404` | `ASSET_NOT_FOUND` | 项目、版本或资产不存在 |
| `409` | `CHALLENGE_IN_PROGRESS` | 同一挑战正在签发授权，请稍后重试 |
| `409` | `NO_ROUTABLE_NODE` | 当前没有可用下载节点 |
| `410` | `API_VERSION_RETIRED` | API V1 挑战或授权已由配置关闭 |
| `410` | `WEB_PROTOCOL_RETIRED` | V1 Web 挑战与授权已固定停用 |
| `416` | `RANGE_NOT_SATISFIABLE` | Range 不合法或超出文件范围 |
| `429` | `PUBLIC_RESOURCE_RATE_LIMITED` | 主节点公共资源请求过于频繁 |
| `429` | `CLIENT_RATE_LIMITED` | 客户端挑战创建频率达到自适应拒绝等级 |
| `429` | `REQUEST_QUOTA_EXHAUSTED` | 地址级或网段级请求额度不足 |
| `429` | `TRAFFIC_LIMIT_EXCEEDED` | 地址级或网段级每日流量预算不足 |
| `500` | `PUBLIC_INTERNAL_ERROR` | 服务端处理失败，使用请求 ID 排查 |
| `503` | `VDF_BUSY` | VDF 预期答案计算并发上限已满 |

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

返回保留窗口内资产，按版本、预发布标记、可选架构和可选系统组织。只有至少一个在线、未禁用且有公网下载地址的节点在最近心跳之后已验证持有该资产时，才可标记为 `available=true`。`nodes.routing_ready` 仍表示整节点完成全量目标库存对账，不再作为单个文件公开可下载的必要条件。

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
        "architecture": "amd64",
        "system": "win",
        "variant": "binary",
        "display_label": "Windows x64",
        "priority": 0,
        "size_bytes": 123456,
        "digest_sha256": "sha256:64位十六进制摘要",
        "available": true,
        "unavailable_reason": ""
      }
    ]
  }
}
```

`architecture` 为空字符串表示该项目未启用架构区分；启用后返回提取值，未命中时返回 `None`。`system` 为空字符串表示该项目未启用系统区分；启用后只返回规范化值 `win`、`linux`、`darwin`、`harmony` 或未命中占位 `None`。

`variant`、`display_label` 和 `priority` 为资产分类规则生成的可选展示字段；未配置分类规则时可能为空或为零，旧客户端可以忽略。

### 3.3 首页聚合目录

`GET /api/public/v1/catalog`

返回主站首页渲染所需的筛选器、项目与资产聚合数据。无查询参数时，`projects` 仍是现有完整目录，`suggested_projects` 为空，旧客户端可继续只读取 `projects`。

搜索和筛选示例：

```text
GET /api/public/v1/catalog?q=ffmpeg&filter=software_type:launcher&filter=supported_system:windows
```

- `q` 可选，去除首尾空格后最长 100 个 Unicode 字符，搜索不区分大小写。
- `filter` 可重复，格式为 `{selector_id}:{option_id}`；重复项会去重。
- `page_size` 可选；设置后启用分页，必须大于零且不得超过主节点的 `server.catalog_batch_rows × 3`。
- `cursor` 可选；使用响应返回的不透明游标继续读取对应的主结果或建议结果，不得解析、修改或跨查询复用。
- 未知筛选器、未知公开选项或非法格式返回 `400 INVALID_REQUEST`。
- 客户端应对筛选项排序后生成稳定 URL，便于浏览器复用同一条件的缓存。

```json
{
  "filter_groups": [
    {
      "id": "software_type",
      "name": "软件类型",
      "options": [
        {"id": "launcher", "name": "启动器"}
      ]
    }
  ],
  "projects": [
    {
      "project_id": "example",
      "display_name": "示例项目",
      "repository": "owner/repo",
      "available": true,
      "icon_url": "/static/project-icons/example",
      "architecture_selector_enabled": false,
      "system_selector_enabled": false,
      "architecture_match_enabled": false,
      "system_match_enabled": false,
      "default_selection_mode": "selectors",
      "default_version": "v1.2.3",
      "tags": {
        "software_type": ["launcher"],
        "keywords": ["example"]
      },
      "assets": []
    }
  ],
  "suggested_projects": [],
  "next_projects_cursor": "不透明游标"
}
```

首次携带 `page_size` 时，`projects` 和 `suggested_projects` 分别最多返回该数量，并分别通过
`next_projects_cursor`、`next_suggested_projects_cursor` 表示是否还有下一批。续页请求继续携带相同的
`q`、`filter` 和 `page_size`，并增加其中一个 `cursor`；响应只填充该游标所属的结果数组。没有下一批时省略对应游标。
响应式布局跨断点后可以在续页请求中调整 `page_size`，游标中的绝对偏移量保证不会因此重复或漏掉项目。

游标绑定目录版本、规范化查询、结果区域和绝对偏移量，并记录上一批大小作为未传 `page_size` 时的回退值。目录配置在翻页期间发生热重载时，旧游标返回
`409 CATALOG_CHANGED`，客户端应丢弃当前两组结果并从首批重新请求。不带 `page_size` 和 `cursor` 的请求继续返回完整结果，
用于兼容旧客户端。

`architecture_selector_enabled` 和 `system_selector_enabled` 是分类方式无关的下载页展示开关。旧的 `architecture_match_enabled`、`system_match_enabled` 暂时保留兼容，其值与对应新开关一致。

`default_selection_mode` 是项目下载界面的默认选择方式，固定返回规范化后的 `selectors`（条件选择）或 `file`（文件选择）。浏览器按项目记忆用户上次手动切换结果；存在有效记忆时优先使用用户选择，否则使用该接口返回的默认值。

直接搜索会匹配精确项目 ID、完整项目名称、标签值、已公开标签选项的完整显示名称，以及包含搜索词的项目名称。其中标签值支持包含匹配，例如搜索 `3f` 可以命中标签 `3fui`；网页项目卡片会显示此次搜索命中的标签。只有直接结果为零时才启用 Unicode Damerau-Levenshtein 拼写兜底；搜索长度 2–4、5–8、9 以上分别最多允许 1、2、3 个字符差异，最多返回 3 个项目，放入 `suggested_projects`。

搜索词是硬条件：存在直接搜索结果时，只在这些项目中应用标签条件。所有已选标签按 AND 完全匹配，全部命中的项目进入 `projects`；未全部命中但至少命中一个的项目进入 `suggested_projects`；一个标签都未命中的项目不返回。部分匹配推荐先按命中标签数量降序，再按搜索相关度、项目名称和项目 ID 稳定排序。

搜索和标签匹配只读取主节点内存索引；项目资产、版本和实时可用性仍在缓存未命中时从数据库读取。服务端以规范化搜索词、排序去重后的筛选项、索引版本和分页位置为键缓存已编码响应与 ETag，TTL 固定 1 分钟；每一页使用独立 ETag。响应带 `Cache-Control: private, max-age=60`。客户端带 `If-None-Match` 命中时返回 `304 Not Modified`。公共资源限流位于目录缓存之前，因此任何到达服务器的请求，包括服务端缓存命中和 304，均计一次请求；浏览器在 `max-age` 新鲜期直接使用私有缓存时不会访问服务器。

### 3.4 更新日志

`GET /api/public/v1/changelog`

按时间倒序返回部署机 `changelog/YYYY-MM/*.yaml` 中的公开更新记录。

| 参数 | 规则 |
| --- | --- |
| `minimum_level` | 最低等级，默认为 `info`，可选 `info`、`notice`、`warn`、`critical` |
| `q` | 可选搜索词，最长 100 个 Unicode 字符，匹配标题和可见描述文本 |
| `limit` | 每批数量，默认 20，最大 50 |
| `cursor` | 响应返回的不透明续读游标，不得解析、修改或跨查询复用 |

```json
{
  "status": "success",
  "message": "查询成功",
  "request_id": "请求标识",
  "data": {
    "items": [
      {
        "level": "notice",
        "title": "上线更新日志页面",
        "occurred_at": "2026-07-30T12:20:00+08:00",
        "description_html": "新增 <strong>更新日志</strong> 页面。"
      }
    ],
    "next_cursor": "不透明游标"
  }
}
```

`description_html` 已由服务端限制为安全的行内格式，只包含加粗、斜体、删除线、
`http/https` 链接和换行。配置热重载导致快照变化时，旧游标返回
`409 CHANGELOG_CHANGED`，客户端应清空已有记录并重新查询首批。

## 4. 下载接入方式

公开下载分为两条线。网页、官网、论坛、公告页或前端页面里的下载按钮，推荐跳转主站验证页；命令行工具、自动更新器、CI 脚本、下载器或后端服务，使用程序 API 链路。

- [方式一：跳转主站验证页下载](#41-方式一跳转主站验证页下载)
- [方式二：程序调用-api-下载](#42-方式二程序调用-api-下载)
- [其他接口](#43-其他接口)

### 4.1 方式一：跳转主站验证页下载

这种方式适合网页下载按钮。外部前端不需要自己处理挑战、PoW、下载令牌，也不需要关心当前由哪个下载节点提供文件。

需要特别注意：这里要跳转的是**主站地址**，不是下载节点地址。外部前端应该把用户带到主站的验证页面，由主站完成验证、授权和节点选择，最后再跳转到真实下载节点开始下载。

验证页路径格式：

```text
/{project_id}/{version}/{file_name}
```

示例：

```text
https://mirror.example.com/fcl/1.3.0.9/FCL-release-1.3.0.9-arm64-v8a.apk
```

这里的 `https://mirror.example.com` 应该是主站公共入口，不是某个下载节点的 `public_download_base_url`。

如果前端还不知道有哪些项目，可以先查询项目列表：

```text
GET /api/public/v1/projects
```

这个接口可以用来获取 `project_id`、项目展示名和项目可用状态。前端一般只需要在初始化下载页、项目选择器或外部下载列表时调用它。

拿到 `project_id` 后，前端可以查询该项目下有哪些版本和文件：

```text
GET /api/public/v1/projects/{project_id}/assets
```

返回结果里会包含 `version`、`file_name`、`architecture`、`system`、`size_bytes`、`digest_sha256` 和 `available` 等字段。前端可以根据这些字段生成下载按钮。用户点击按钮时，跳转到主站验证页：

```text
/{project_id}/{version}/{file_name}
```

例如，资产返回：

```json
{
  "asset_id": "asset_123",
  "version": "v1.2.3",
  "file_name": "example-windows-amd64.zip",
  "architecture": "amd64",
  "system": "win",
  "available": true
}
```

前端下载按钮应该跳转到：

```text
https://mirror.example.com/example/v1.2.3/example-windows-amd64.zip
```

用户打开验证页后，主站会自动完成下面的流程：

1. 展示下载验证页面。
2. 在浏览器中完成下载挑战计算。
3. 向主节点领取短时下载授权。
4. 选择可用下载节点。
5. 跳转到真实下载地址开始下载。

外部网站不要直接拼接节点下载地址，也不要调用程序下载用的 `/api/public/v2/api/*` 接口来替代这个流程。正常网页只创建一个 `vdf-worker.js` Worker，使用原生 BigInt 顺序计算，不使用 WebGPU，也不回退 SHA。

### 4.2 方式二：程序调用 API 下载

这种方式适合命令行工具、自动更新器、CI 脚本、下载器或后端服务。程序需要自己查询资产、完成 API PoW 验证，然后携带下载令牌访问下载节点。

第一步，查询项目列表：

```text
GET /api/public/v1/projects
```

第二步，查询项目资产：

```text
GET /api/public/v1/projects/{project_id}/assets
```

程序应选择一个 `available=true` 的资产，并记录它的 `asset_id`。如果 `available=false`，表示当前没有可用下载节点持有这个文件，程序应该稍后重试。

第三步，创建 API PoW 挑战：

```text
POST /api/public/v2/api/challenges

{
  "asset_id": "asset_123"
}
```

第四步，计算 `solution`。把 `modulus` 和 `base` 按 `base64url-uint-be-384` 解码，从 `y=base` 开始执行响应指定次数的：

```text
y = y² mod modulus
```

结果必须重新编码为恰好 384 字节、512 字符的无填充 base64url。第五步，提交 `solution` 并领取下载授权：

```text
POST /api/public/v2/api/authorizations

{
  "challenge_id": "挑战标识",
  "asset_id": "asset_123",
  "solution": "512 字符的定长 base64url 整数"
}
```

成功后会返回 `download_url` 和 `download_token`。

第六步，程序应直接访问授权响应里的 `download_url`，并通过请求头携带下载令牌：

```text
GET {download_url}
Authorization: Bearer <download_token>
```

如果需要断点续传，可以使用单段 Range：

```text
GET {download_url}
Authorization: Bearer <download_token>
Range: bytes=1048576-2097151
```

`download_url` 通常指向下载节点。程序调用 API 的这条链路里，下载文件时访问节点地址是正确的；但网页按钮下载时，入口仍应该是主站验证页。

### 4.3 其他接口

下面这些接口不是“方式二：程序调用 API 下载”的步骤，只用于订阅、状态查询或排障。

#### 4.3.1 封禁列表订阅

`GET /api/public/v1/blocklist.txt`

`GET /api/public/v1/blocklist.json`

返回当前生效的公共下载封禁列表。TXT 地址行与 JSON `entry` 字段均使用完整真实 IP 或完整 CIDR 网段，不做脱敏。内容包含 `quota.yaml` 静态黑名单和本站手动/自动封禁记录，不包含远程订阅源快照。服务端缓存生成后的封禁快照 60 秒，TTL 内重复请求不会重新查询和合并列表。
TXT 只展示封禁原因、来源、尝试次数和封禁时间，不写过期时间；JSON 仍会返回结构化的 `blocked_at`。

TXT 响应头：

```http
Content-Type: text/plain; charset=utf-8
Cache-Control: public, max-age=60, must-revalidate
```

TXT 示例：

```txt
# [枫源镜像封禁] 封禁原因: traffic_limit_exceeded, 来源: local_auto_ban, 封禁后尝试次数: 3, 封禁时间: 2026-06-21T12:00:00Z
192.0.2.123
```

JSON 示例：

```json
{
  "status": "success",
  "message": "查询成功",
  "data": {
    "cache_expires_at": "2026-06-21T12:01:00Z",
    "blocks": [
      {
        "entry": "192.0.2.123",
        "reason": "traffic_limit_exceeded",
        "attempts_after_block": 3,
        "blocked_at": "2026-06-21T12:00:00Z"
      }
    ]
  }
}
```

#### 4.3.2 授权状态查询

授权查询接口完整字段见[授权查询](#7-授权查询)。

## 5. 网页下载挑战授权接口

网页页面使用 V2 RSA repeated-squaring 挑战；这些接口仅服务浏览器下载链路。V1 Web 挑战和授权固定返回 `410 WEB_PROTOCOL_RETIRED`，非 PoW 的 `/api/public/v1/web/verifications` 保持原合同。

浏览器推荐入口为主站上的 `GET /{project_id}/{version}/{file_name}`，例如 `/fcl/1.3.0.9/FCL-release-1.3.0.9-arm64-v8a.apk`。首页下载按钮和外部网站都应跳转到该独立验证页，由页面完成 V2 挑战、领取下载授权并跳转到节点 `download_url`。旧版 `GET /download/{asset_id}` 暂时保留为兼容入口。

### 5.1 创建网页挑战

`POST /api/public/v2/web/challenges`

挑战保存在主节点内存中，不写入数据库。Web V2、API V1 和 API V2 对同一精确来源共享令牌桶、outstanding 上限和全局容量。

创建挑战前会先检查 `quota.yaml` 黑名单。命中静态黑名单或订阅源黑名单时返回 `403 CLIENT_BLOCKED`，不会创建挑战。

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
    "algorithm": "rsa-repeated-squaring-v1",
    "modulus_id": "模数标识",
    "modulus": "512 字符定长 base64url 整数",
    "base": "512 字符定长 base64url 整数",
    "iterations": 96000,
    "encoding": "base64url-uint-be-384",
    "expires_at": "2026-05-28T12:00:00Z"
  }
}
```

### 5.2 提交网页挑战并领取授权

`POST /api/public/v2/web/authorizations`

请求：

```json
{
  "challenge_id": "挑战标识",
  "asset_id": "asset_123",
  "solution": "512 字符定长 base64url 整数",
  "telemetry": {
    "solve_elapsed_ms": 3523,
    "platform": "Linux x86_64",
    "hardware_concurrency": 12,
    "device_memory_gib": 8
  }
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
    "download_url": "https://node.example/example/v1.2.3/example-windows-amd64.zip",
    "download_token": "43 字符短时随机令牌",
    "expires_at": "2026-05-28T12:05:00Z",
    "range_concurrency_limit": 32,
    "max_bytes": 246912
  }
}
```

成功响应表示主节点已经把授权详情同步到被绑定的下载节点。`telemetry` 可缺省，其粗粒度设备信息和纯求解耗时只用于 JSONL 统计，不参与验证、abuse、额度或路由判断。

## 6. API V1 SHA-256 兼容接口

`api_pow.v1_enabled=true` 时，以下两个 V1 接口的请求、响应和错误合同保持不变；显式设为 `false` 时两者返回 `410 API_VERSION_RETIRED`。V1 授权状态查询始终保留，本次不实现代码删除或自动下线计划。

### 6.1 创建 API PoW 挑战

`POST /api/public/v1/api/challenges`

挑战保存在主节点内存中，不写入数据库；主节点按客户端前缀和挑战类型执行轻量限流，并定期清理过期挑战。

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

的二进制摘要满足前导零位数要求。示例中的 `23` 是本次挑战的实际难度，不是固定默认值。

### 6.2 提交 API PoW 并领取授权

`POST /api/public/v1/api/authorizations`

请求：

```json
{
  "challenge_id": "挑战标识",
  "asset_id": "asset_123",
  "nonce": "客户端找到的nonce"
}
```

成功响应与网页授权一致，并且同样表示目标下载节点已经缓存授权。挑战只在授权记录、流量预留和短下载令牌生成整体成功后才被消费；额度不足、无可路由节点、令牌生成失败或服务端错误不会消费挑战，客户端可在挑战过期前重试。同一挑战并发提交时，正在处理中的请求返回 `CHALLENGE_IN_PROGRESS`。

授权签发前会再次检查黑名单，覆盖“挑战创建后客户端被封禁”的窗口。命中后返回 `403 CLIENT_BLOCKED`，不会签发下载令牌。

若授权签发因为 `REQUEST_QUOTA_EXHAUSTED` 或 `TRAFFIC_LIMIT_EXCEEDED` 失败，主节点会把客户端前缀写入本站自动封禁表。默认封禁 7 天，时长由 `quota.yaml` 的 `blocklist.auto_ban_duration` 调整；过期后自动不再生效。订阅源黑名单不受该过期时间影响，只跟随订阅源当前快照。

挑战创建先按主节点记录的可信资产大小从 `pow_size_tiers` 选择基础难度，再应用 `abuse_control` 的精确地址与网段窗口。可疑来源在基础难度上增加位数，普通挑战最终不超过 `abuse_control.challenge.max_bits`。客户端必须始终按响应中的 `difficulty` 或 `leading_zero_bits` 计算；拒绝等级返回 `429 CLIENT_RATE_LIMITED` 和 `Retry-After`。已封禁 API 客户端始终收到 `403 CLIENT_BLOCKED`，不会收到浏览器惩罚 PoW；独立惩罚 PoW 不使用文件大小分档，也不受普通挑战上限影响。

## 7. 授权查询

`GET /api/public/v1/authorizations/{authorization_id}`

V2 别名为 `GET /api/public/v2/authorizations/{authorization_id}`，两者返回相同授权状态。

必须携带该授权对应的下载令牌，且客户端前缀必须与授权记录一致。M5 返回授权基本状态和已由主节点幂等入账的真实发送字节；`node_id` 字段为公开节点名，不返回内部节点 ID。

```text
GET /api/public/v1/authorizations/{authorization_id}
Authorization: Bearer <download_token>
```

```json
{
  "status": "success",
  "message": "查询成功",
  "request_id": "请求标识",
  "data": {
    "authorization_id": "授权标识",
    "asset_id": "asset_123",
    "node_id": "公开节点名",
    "state": "issued",
    "expires_at": "2026-05-28T12:05:00Z",
    "bytes_accounting_enabled": false,
    "sent_bytes": null
  }
}
```

## 8. 节点下载接口

节点文件服务路径由主节点返回的 `download_url` 决定。推荐形式：

```text
GET /{project_id}/{version}/{file_name}
Authorization: Bearer <download_token>
Range: bytes=0-1048575
```

浏览器下载场景可使用一次性查询参数传递令牌，例如 `?token=<download_token>`；节点日志必须脱敏并避免把完整 URL 写入普通日志。旧 `/downloads/{asset_id}` 路径仅用于兼容已签发或外部缓存的旧链接。

### 8.1 成功响应头

| 响应头 | 说明 |
| --- | --- |
| `Accept-Ranges: bytes` | 支持断点续传 |
| `Content-Length` | 本次响应字节数 |
| `Content-Range` | `206` 时返回 |
| `Content-Disposition` | 使用安全文件名 |
| `X-Request-ID` | 节点侧请求 ID |
| `X-Authorization-Request-ID` | 主节点签发请求 ID，可选 |

### 8.2 Range 示例

请求：

```text
GET /example/v1.2.3/example-windows-amd64.zip HTTP/1.1
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

## 9. 下载令牌

公共下载授权返回的 `download_token` 是 32 字节随机数的 base64url 表示，长度固定为 43 字符。主节点只保存令牌的 SHA-256 哈希，并通过控制通道把授权详情下发到被绑定的下载节点；公开授权接口会等待下载节点确认缓存授权后才返回成功，下载节点按本地缓存中的哈希和授权字段校验请求。

授权详情至少绑定：

| 声明 | 必填 | 说明 |
| --- | --- | --- |
| `authorization_id` | 是 | 主节点授权记录 |
| `asset_id` | 是 | 只能下载该资产 |
| `node_id` | 是 | 只能由该节点接受 |
| `client_prefix` | 是 | 主节点签发时记录的客户端 IP 前缀，用于额度、流量和排障归因 |
| `expires_at` | 是 | 短时有效期 |
| `max_bytes` | 是 | 最大允许发送字节，默认等于资产大小乘以 `quota.yaml` 的 `authorization_max_bytes_multiplier`；下载节点按真实响应体发送字节累计，不按请求 Range 范围预扣 |
| `range_concurrency_limit` | 是 | 并发 Range 限制，默认来自 `quota.yaml` 的 `range_concurrency_limit`，默认值 `32` |
| `request_id` | 是 | 主节点签发请求 ID |

下载节点不得接受哈希不存在、过期、跨节点、跨资产或已本地作废的令牌。下载令牌允许在有效期内因 VPN、移动网络或跨地区出口变化而从不同客户端前缀继续使用；主节点仍在签发时按客户端前缀执行额度与封禁控制。旧版 `download.v2` Ed25519 签名令牌在兼容期仍按原声明校验；新签发的公共下载令牌使用 opaque token。

## 10. M5 统计字段

统计页和部分 API 返回下列字段：

| 字段 | M4 值 |
| --- | --- |
| `quota_enabled` | `true` |
| `traffic_accounting_enabled` | `true` |
| `statistics_enabled` | `true` |
| `sla_enabled` | `true`，样本不足时显示“统计样本不足” |
| `authorization_count` | 主节点成功签发下载令牌总次数 |
| `web_authorization_count` | 网页验证链路成功签发下载令牌次数 |
| `api_authorization_count` | 公开 API PoW 链路成功签发下载令牌次数 |
| `started_transfer_count` | 首次产生正字节响应体的授权数 |
| `daily_bytes` | 当前统计日真实发送字节 |
| `node_sla` | 24h、7d、30d SLA |

## 11. M4 实现说明

- 主节点公共接口挂载在 `server.public_listen`。
- 下载节点文件服务挂载在 `/{project_id}/{version}/{file_name}`；旧 `/downloads/{asset_id}` 仅作为兼容路径保留。
- 主节点必须配置 `download_token.signing_private_key_file` 和 `download_token.verify_public_key_file`；下载节点把验证公钥保存到 `storage.state_db`，`download_token.verify_public_key_file` 只作为旧版本导入或非交互兜底路径。
- 旧版 `download_token.signing_key_file` HMAC 共享密钥已废弃，启动时不得继续使用；升级后旧 `download.v1` 令牌需要重新签发。
- `download_url` 返回主节点当前选定下载节点的完整公网下载地址；浏览器和 API 客户端应直接向该地址发起下载，请勿再经主节点转发文件流量。
- M4 已支持单段 HTTP Range；multipart Range 不作为 M4 必须能力。

## 12. 日志、封禁信息与兼容

- 公共 API 响应不得包含节点内部地址、控制端口、证书、磁盘路径或 GitHub Token；封禁响应会返回当前客户端的完整 IP，公开 blocklist JSON/TXT 会返回完整封禁 IP 或网段，便于申诉和共享封禁情报。
- 日志不得记录完整 `download_token`、完整网页挑战 payload、完整 PoW 规范字符串或完整 URL 查询令牌。
- 客户端相关日志使用完整 IP 或完整封禁网段，并按首次及 2 的幂次数采样；`blocked_after_attempts` 表示该客户端前缀封禁后仍尝试下载的累计次数。
- JSON 字段新增必须保持向后兼容；删除或重命名字段前必须更新本文并经过阶段确认。
- 所有中文错误、页面文案和文档使用 UTF-8。

## 13. M5 额度与统计接口变化

M5 启用后，公开 API 不再使用 `QUOTA_NOT_ENABLED` 占位错误。网页挑战授权和公开 API PoW 授权在签发下载令牌前必须完成真实请求额度扣减和每日流量预算预留。

### 13.1 新增或变更错误码

| HTTP 状态 | 错误码 | 含义 |
| --- | --- | --- |
| `429` | `REQUEST_QUOTA_EXHAUSTED` | IPv4 `/32`、IPv4 `/24`、IPv6 `/128` 或 IPv6 `/64` 请求额度不足 |
| `429` | `TRAFFIC_LIMIT_EXCEEDED` | 地址级或网段级每日实际流量预算不足 |
| `409` | `AUTHORIZATION_REVOKED` | 下载授权已撤销或因额度异常被阻断 |

错误响应仍使用中文 `message`、稳定 `code` 和 `request_id`，并返回 `X-Request-ID`。

### 13.2 授权查询响应

`GET /api/public/v1/authorizations/{authorization_id}` 在 M5 后可返回真实入账字段：

该接口必须携带与 `authorization_id` 匹配的下载令牌，且令牌内客户端前缀必须匹配当前请求客户端前缀。

```json
{
  "status": "success",
  "message": "查询成功",
  "request_id": "请求标识",
  "data": {
    "authorization_id": "授权标识",
    "asset_id": "asset_123",
    "node_id": "公开节点名",
    "state": "issued",
    "expires_at": "2026-05-28T12:05:00Z",
    "bytes_accounting_enabled": true,
    "sent_bytes": 1048576,
    "first_transfer_at": "2026-05-28T12:01:00Z"
  }
}
```

`sent_bytes` 只表示已由主节点幂等入账的真实发送字节，可能滞后于节点正在传输的瞬时值。

### 13.3 统计数据页字段

M5 后统计数据页展示：

| 字段 | 口径 |
| --- | --- |
| 下载授权次数 | 主节点成功签发下载令牌总次数 |
| Web 下载授权次数 | 网页验证链路成功签发下载令牌次数 |
| API 下载授权次数 | 公开 API PoW 链路成功签发下载令牌次数 |
| 开始传输授权数 | 授权首次产生正字节响应体次数 |
| 当日流量 | 当前统计日真实发送字节 |
| 累计流量 | 所有已入账真实发送字节 |
| 项目统计 | 项目维度授权数、开始传输数和真实发送字节 |

公开统计不得展示完整客户端 IP、额度桶精确余额、单个授权明细、单个请求 ID 列表、节点内部地址或控制面信息。

### 13.4 节点状态页字段

M5 后节点状态页展示公开节点名称、公开连接状态、下载就绪、最近更新时间、负载分档和近 `24h`、`7d`、`30d` SLA。样本不足时显示“统计样本不足”。`download_ready` 只表示该节点当前至少有一个仍处于 `target_inventory.required` 的可公开下载已校验资产副本，`routing_ready` 仍保留为同步/对账合同，不再作为节点页主标识。节点连接状态只表达 `online`、`offline`、`disabled` 等可用性，不表示全量同步进度；全量同步进度只在管理诊断中展示。
M6 起，主节点会定期通过下载节点 `public_download_base_url` 访问 `/.well-known/mirror-node/probes/{challenge_id}` 并验签；公网不可达达到配置阈值时，节点会暂停公开下载路由但不改变控制面连接状态；响应字段/签名错误时，节点连接状态会被标记为 `offline`。

节点状态页不得展示真实带宽目标、内部压力原始值、管理地址、控制通道地址、证书信息、磁盘路径、完整客户端 IP 或下载令牌。

### 13.5 M4 令牌兼容

M5 上线前已签发且未过期的 M4 下载令牌仍按签名、节点、资产和过期时间校验；令牌内客户端前缀用于签发归因，不阻止下载期间出口变化。若该授权缺少 M5 流量预留，主节点在首次流量事件入账时走兼容路径补建或标记旧授权预留；已真实发送的字节必须入账，后续超限请求可拒绝或撤销。

## 14. M6 最终对外字段和安全边界

M6 将公开 API 和公共页面收口为首版最终交付合同。新增字段必须保持向后兼容；删除或重命名前必须先更新本文并经过阶段确认。

### 14.1 最终对外字段

| 接口或页面 | 可公开字段 |
| --- | --- |
| 项目列表 | `project_id`、`repository`、`display_name`、`available` |
| 项目资产 | `asset_id`、`version`、`prerelease`、`file_name`、`architecture`、`system`、`size_bytes`、`digest_sha256`、`available`、`unavailable_reason` |
| 网页挑战 | `challenge_id`、兼容挑战字段、`expires_at` |
| API PoW 挑战 | `challenge_id`、`asset_id`、`nonce_seed`、`algorithm`、`leading_zero_bits`、`expires_at`、`canonical_format` |
| 授权领取 | `authorization_id`、`download_url`、`download_token`、`expires_at`、`range_concurrency_limit`、`max_bytes` |
| 授权查询 | `authorization_id`、`asset_id`、脱敏节点标识、`state`、`expires_at`、`bytes_accounting_enabled`、`sent_bytes`、`first_transfer_at` |
| 统计页面 | 下载授权次数、开始传输授权数、当日真实流量、累计真实流量、项目聚合统计、统计时区、最近更新时间 |
| 节点状态页面 | 公开节点名、公开连接状态、下载就绪、最近更新时间、负载分档、24h/7d/30d SLA、样本不足提示 |

### 14.2 仍不得公开的内部字段

- 管理面板密码、会话 Cookie、下载令牌私钥、配对码明文、证书私钥、完整 CSR 或证书正文。
- 除封禁响应与公开 blocklist JSON/TXT 外，统计和普通业务响应不得公开完整客户端 IP、完整客户端网段、额度桶精确余额、黑名单和豁免规则明细。
- 节点内部地址、控制端口、管理监听地址、本地磁盘路径、临时文件路径。
- GitHub Token、源站敏感请求头、同步任务内部错误全文。
- 单个流量事件完整明细、完整请求 URL 查询令牌、可绕过挑战的 PoW 或网页挑战内部材料。

### 14.3 M6 安全和负载验收

| 用例 | 公开 API 通过标准 |
| --- | --- |
| 伪造代理头 | 非受信代理来源不能改变客户端前缀；主节点和下载节点仅信任 `proxy.trusted_cidrs` 中的直连代理；可信代理头畸形时拒绝请求，不回退成可能被豁免的代理地址 |
| 伪造令牌 | 签名或声明篡改后下载失败 |
| 跨节点复用 | 绑定节点不一致时下载失败 |
| 跨资产复用 | 请求资产与令牌资产不一致时下载失败 |
| 并发授权 | 同一地址和网段额度不透支，错误使用 `REQUEST_QUOTA_EXHAUSTED` 或 `TRAFFIC_LIMIT_EXCEEDED` |
| Range 下载 | 同一令牌多段 Range 不增加下载授权次数，授权用量和统计均按真实响应体发送字节入账，不按客户端声明的 Range 范围入账 |
