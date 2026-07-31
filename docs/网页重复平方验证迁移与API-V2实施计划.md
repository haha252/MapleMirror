# 网页重复平方验证迁移与 API V2 实施计划

## 1. 实现目标

将正常网页下载从 SHA-256 前导零搜索迁移到 3072 位 RSA repeated-squaring 顺序工作量，并新增使用相同协议的公开 API V2。

实现完成后的行为：

- 正常网页只调用 V2 repeated-squaring 挑战和授权。
- API V2 使用相同算法、编码和授权结果。
- API V1 保持当前 SHA-256 请求、响应和错误合同，默认开启。
- `api_pow.v1_enabled=false` 时，只有 V1 API 挑战和授权入口返回 `410 API_VERSION_RETIRED`。
- V1 授权状态查询继续可用。
- 旧 V1 Web 挑战和授权固定返回 `410 WEB_PROTOCOL_RETIRED`。
- 非 PoW `/api/public/v1/web/verifications` 保持不变。
- 封禁惩罚页继续使用当前 SHA-256 前导 128 零位展示逻辑。
- 不实现 V1 的代码删除、删除步骤、固定下线时间或后续清理方案。
- 不修改下载节点合同、下载 URL、短令牌、额度扣减、流量预留和流量入账。
- 不增加数据库迁移；挑战、RSA 陷门和 PoW 遥测都不写业务数据库。

本协议在代码和配置中可以使用 `vdf` 作为简称，公开说明使用“RSA repeated-squaring 顺序工作量”或“陷门时间锁挑战”。它降低单挑战横向并行的收益，但不承诺不同 CPU 等速，也不承诺原生 GPU/ASIC 实现没有常数级优势。

## 2. 不在本次范围内

- API V1 的物理删除、删除脚本或删除发布计划。
- 固定日落日期、自动关闭 V1 或基于使用量自动修改开关。
- VDF 公开证明或第三方无状态快速验证。
- 多主节点之间共享挑战或 RSA 陷门。
- 外部 RTX、Vega、移动端或其他机器的性能验收。
- 遥测数据的报表、数据库导入、聚合服务或可视化页面。
- 授权事务提交后客户端断线导致的短令牌恢复机制。

## 3. 分支与工作区

实施前运行：

```bash
git status --short --branch
git log -1 --oneline
```

确认基线后创建：

```bash
git switch -c feat/public-v2-repeated-squaring
```

如果分支已存在，不得删除、强制移动或重置。不得使用破坏性命令清理用户改动，不得混入无关重构。是否提交、合并或推送由用户后续决定。

## 4. 协议定义

### 4.1 常量与编码

| 项目 | 值 |
| --- | --- |
| 算法 | `rsa-repeated-squaring-v1` |
| 模数位数 | 3072 |
| 整数长度 | 384 字节 |
| 字节序 | 无符号大端 |
| 文本编码 | 无填充 base64url |
| `encoding` | `base64url-uint-be-384` |
| `modulus_id` | `base64url(SHA-256(modulus_bytes))` |
| 预期答案摘要 | SHA-256，32 字节 |

`modulus`、`base`、`solution` 都编码为 384 字节定长整数。384 字节对应 512 个 base64url 字符；V2 solution 必须严格为 512 字符，不允许 `=`、空白、`+` 或 `/`。解码后重新编码必须与原字符串完全一致。

### 4.2 客户端计算

```text
y = base
重复 iterations 次：
    y = y² mod modulus
```

结果为：

```text
y = base^(2^iterations) mod modulus
```

### 4.3 服务端快速计算

服务端持有等价陷门 `lambda(N)`：

```text
e = 2^iterations mod lambda(N)
y = base^e mod N
expected_digest = SHA-256(I2OSP(y, 384))
```

挑战内只保存 `expected_digest`，不保存明文答案。`I2OSP` 必须补齐前导零并固定输出 384 字节。

### 4.4 base 生成

使用密码学安全随机源在 `[2, N-2]` 采样，要求：

```text
gcd(base, N) = 1
```

如果快速计算得到明显退化结果 `1` 或 `N-1`，重新采样。任何生成失败都必须释放已预留的挑战名额。

### 4.5 solution 验证顺序

1. 挑战存在且未过期。
2. 来源、资产、入口类型、协议版本和算法一致。
3. solution 是规范的 384 字节 base64url 整数。
4. `0 < solution < N`。
5. 对提交字节计算 SHA-256。
6. 使用常量时间比较预期摘要。
7. 进入授权事务。

旧挑战在模数轮换后仍需完成第 4 步，因此挑战应保留公开模数的只读引用或可按 `modulus_id` 查询的公开模数信息；不需要保留旧陷门。

## 5. RSA 模数管理

新增独立模数管理器：

- 主节点启动时同步生成 3072 位 RSA 模数。
- 长期内存对象保存公开 `N` 和快速计算所需的等价陷门材料。
- 不把 `p`、`q`、`lambda(N)`、私钥或等价陷门写入文件、配置、数据库或日志。
- Go GC 不能保证历史大整数副本安全清零，文档只承诺应用不主动持久化或记录。
- 初次生成失败时 V2 初始化失败，不回退 SHA。
- 每 `vdf.key_rotation_interval` 在后台生成候选，校验成功后原子替换。
- 轮换失败保留当前模数并告警。
- 轮换日志只记录 `modulus_id`、模数年龄和错误。
- 管理器提供幂等 `Close()`，停止轮换协程。
- 密钥生成器和时钟必须可注入，单元测试不反复生成真实 3072 位密钥。

## 6. 挑战模型与护栏

### 6.1 挑战字段

不要继续让一个 `Kind` 字符串同时承担来源、算法和版本。建议模型：

```go
type Challenge struct {
    ID              string
    SourceKind      string // web | api
    ProtocolVersion string // v1 | v2
    Algorithm       string
    AssetID         string
    AssetSizeBytes  int64
    ClientPrefixKey string
    CreatedAt       string
    ExpiresAt       string

    Nonce      string // V1
    Difficulty int    // V1

    ModulusID      string   // V2
    Iterations     uint64   // V2
    Multiplier     int      // V2: 1/2/4
    SolutionDigest [32]byte // V2
}
```

授权来源统计只看 `SourceKind`；验证器按 `ProtocolVersion + Algorithm` 分派。未知版本或算法必须拒绝，不能落入默认 Web/SHA 分支。

### 6.2 状态流转

```text
reserved -> open -> issuing -> consumed
    |         |         |
    |         |         +-- 事务提交前失败：回到 open
    |         +------------ 过期：deleted
    +---------------------- 创建失败：released
```

- `reserved` 已占用来源 outstanding，但尚未完成资产查询或预期答案计算。
- `open` 接受答案；错误答案不消费挑战，但继续计入 abuse 权重。
- `issuing` 拒绝并发提交，返回 `409 CHALLENGE_IN_PROGRESS`。
- 授权事务提交前失败恢复 `open`。
- 数据库事务提交成功立即消费挑战并释放 outstanding。
- 节点同步失败属于提交后失败，不能恢复挑战并再次扣额度。
- 所有删除路径通过同一个辅助函数，确保计数只释放一次。

### 6.3 共享限制

Web V2、API V1、API V2 对同一精确来源共用：

- 一只挑战令牌桶。
- 一个 outstanding 计数。
- 一个全局 outstanding 总量。

创建使用原子的 `reserve/commit/release`：清理过期记录、检查桶、检查来源上限、检查全局上限、扣令牌和增加预留必须在同一临界区完成。数据库查询和大整数计算不得在锁内执行。

VDF 预期答案计算增加非阻塞全局并发上限。饱和时返回 `503 VDF_BUSY` 和短 `Retry-After`，释放 outstanding，但不退还已经消耗的请求桶令牌。

## 7. 配置

### 7.1 主配置

```yaml
vdf_size_tiers:
  - min_size: "0 B"
    iterations: 12000
  - min_size: "5 MiB"
    iterations: 48000
  - min_size: "50 MiB"
    iterations: 96000
  - min_size: "150 MiB"
    iterations: 192000
  - min_size: "300 MiB"
    iterations: 384000
  - min_size: "500 MiB"
    iterations: 768000

vdf:
  challenge_ttl: "2m"
  key_rotation_interval: "24h"
  elevated_multiplier: 2
  severe_multiplier: 4
  max_iterations: 10000000
  max_parallel_creations: 4

api_pow:
  algorithm: "sha256"
  challenge_ttl: "2m"
  v1_enabled: true
```

最终 V2 迭代数：

```text
min(base_iterations * multiplier, vdf.max_iterations)
```

现有 `pow_size_tiers` 和 V1 abuse bits 继续服务 API V1。V2 使用相同 abuse 等级，但映射为 `1/2/4` 倍率；拒绝等级仍返回 429。

### 7.2 challenge limits

在 quota 配置增加：

```yaml
challenge_limits:
  bucket_capacity: 30
  bucket_full_refill: "10m"
  max_outstanding_exact: 4
  max_outstanding_total: 100000
```

### 7.3 校验与迁移

- tier 至少一档，第一档必须为 `0 B`，阈值严格递增。
- `iterations > 0` 且不超过硬上限。
- `1 < elevated_multiplier < severe_multiplier`。
- `key_rotation_interval > challenge_ttl`。
- 所有限制值必须为正。
- `v1_enabled` 使用 `*bool` 或等价方式区分未配置与显式 `false`。
- 自动把 `altcha.challenge_ttl` 迁移为 `vdf.challenge_ttl`，新字段已存在时以新字段为准。
- 删除旧 `altcha.challenge_ttl` 后，正常网页和 V2 读取 `vdf.challenge_ttl`。
- 同步外部示例、嵌入模板、YAML repair 和未知字段保留测试。

## 8. HTTP 合同

### 8.1 V2 路由

```text
POST /api/public/v2/web/challenges
POST /api/public/v2/web/authorizations
POST /api/public/v2/api/challenges
POST /api/public/v2/api/authorizations
GET  /api/public/v2/authorizations/{authorization_id}
```

V2 继续使用现有统一 response envelope，字段位于 `data`。

### 8.2 挑战响应

```json
{
  "challenge_id": "...",
  "asset_id": "...",
  "algorithm": "rsa-repeated-squaring-v1",
  "modulus_id": "...",
  "modulus": "...",
  "base": "...",
  "iterations": 192000,
  "encoding": "base64url-uint-be-384",
  "expires_at": "..."
}
```

### 8.3 Web V2 授权请求及遥测

```json
{
  "challenge_id": "...",
  "asset_id": "...",
  "solution": "...",
  "telemetry": {
    "solve_elapsed_ms": 3523,
    "platform": "Linux x86_64",
    "hardware_concurrency": 12,
    "device_memory_gib": 8
  }
}
```

API V2 可以只提交前三个必要字段；`telemetry` 为可选，不影响授权合同。服务端从 HTTP `User-Agent` 读取浏览器标识，不信任客户端自行声明算法、迭代数或倍率。

遥测限制：

- `solve_elapsed_ms` 是 Worker 开始运算到得到 solution 的 `performance.now()` 差值，只测纯求解。
- `platform` 优先使用浏览器可用的粗粒度平台值，最多 128 字符。
- `hardware_concurrency` 仅接受合理正整数并设置上限。
- `device_memory_gib` 可缺省，设置合理范围。
- 无效或越界遥测字段被丢弃或归零，不得改变 solution 验证结果。
- 遥测字段不参与 abuse、授权、额度、路由或安全决策。

成功响应保持现有下载 URL、短令牌、到期时间、并发和字节上限字段。

### 8.4 V1 开关

- `api_pow.v1_enabled=true`：V1 API challenge/authorization 与当前合同完全一致。
- `api_pow.v1_enabled=false`：只有这两个入口返回 `410 API_VERSION_RETIRED`。
- V1 authorization status 始终保留。
- V1 Web challenge/authorization 固定返回 `410 WEB_PROTOCOL_RETIRED`。
- 不实现后续自动删除、自动关停或代码清理。

### 8.5 状态查询别名

当前状态处理器硬编码 V1 路径长度。实现时抽出按 ID 查询的公共函数，V1/V2 路由各自安全解析 ID，返回相同授权状态。

### 8.6 缓存

下载验证 HTML、挑战、授权、授权状态和 410 响应统一使用：

```text
Cache-Control: private, no-store
```

静态 JS/CSS/WASM 继续使用内容摘要版本号和 immutable 缓存。

## 9. 前端

### 9.1 加载边界

正常下载页：

- 加载 `download-pow.js`。
- 新增并加载一个版本化 `vdf-worker.js`。
- 不加载 `pow-loader.js`。
- 不加载或声明 `pow.wasm`。

惩罚页继续加载 `pow-loader.js`、`punishment-pow.js` 和 `pow.wasm`。

### 9.2 能力检测

```js
typeof BigInt === "function" && typeof Worker === "function"
```

主脚本和 Worker 不使用 `0n`、`1n` 等 BigInt 字面量，避免旧浏览器在能力提示前发生语法错误。不支持时明确提示升级，不回退 SHA。

### 9.3 Worker

- 只创建一个 Worker。
- 使用 `(y * y) % modulus` 顺序计算。
- 按小批次累计精确 `completed`。
- 目标每 100ms 内上报 `{completed, iterations}`。
- 最后一条进度必须满足 `completed === iterations`。
- 页面直接用 `completed / iterations` 显示百分比，不再使用概率分位估计。
- 用 `performance.now()` 记录纯求解耗时。
- 成功、失败、重试和 `pagehide` 时终止 Worker。

### 9.4 编解码

实现集中、可测试的浏览器辅助函数：

- base64url 到 384 字节。
- 大端字节到 BigInt。
- BigInt 到补齐前导零的 384 字节。
- 384 字节到无填充 base64url。

不得把十进制、十六进制或可变长整数作为线上 solution。

## 10. PoW 遥测日志

### 10.1 写入位置

新增独立遥测 writer，根目录为：

```text
filepath.Join(filepath.Dir(config.Logging.Directory), "pow")
```

默认 `logging.directory=logs/master`，主日志位置保持不变；PoW 遥测使用主日志目录的同级目录，所以实际路径为：

```text
logs/pow/YYYY-MM-DD.jsonl
```

日期使用配置的统计时区。文件按天切换，使用 JSON Lines，每行一个完整 JSON 对象，便于后续使用 `jq`、Go、Python 或日志采集器处理。

### 10.2 记录结构

```json
{
  "schema_version": 1,
  "recorded_at": "2026-08-01T12:34:56.789+08:00",
  "request_id": "...",
  "authorization_id": "...",
  "challenge_id": "...",
  "asset_id": "...",
  "asset_size_bytes": 524288000,
  "source_kind": "web",
  "protocol_version": "v2",
  "pow_algorithm": "rsa-repeated-squaring-v1",
  "pow_iterations": 768000,
  "pow_multiplier": 1,
  "solve_elapsed_ms": 14074,
  "challenge_age_ms": 14520,
  "user_agent": "...",
  "platform": "Linux x86_64",
  "hardware_concurrency": 12,
  "device_memory_gib": 8
}
```

可信度划分：

- `pow_algorithm`、`pow_iterations`、`pow_multiplier`、资产字段和 `challenge_age_ms` 必须来自服务端挑战状态。
- `user_agent` 来自 HTTP 头，但仍属于客户端可伪造信息。
- `solve_elapsed_ms`、`platform`、`hardware_concurrency`、`device_memory_gib` 都是客户端上报，只用于统计。

### 10.3 安全与可靠性

- 不记录 solution、预期答案、答案摘要、modulus、RSA 陷门或完整请求体。
- 不记录 Cookie、Authorization 头或下载令牌。
- 字符串统一截断并由 JSON 编码器转义，禁止直接拼接 JSON。
- 数字字段做上下限检查；异常值置空或丢弃。
- 同一成功授权最多写一条，避免重试重复统计。
- 只在 V2 Web 授权成功后写遥测；没有 telemetry 时仍可写服务端字段，客户端字段为空。
- writer 写失败不得回滚或拒绝授权，只通过主日志输出限频告警。
- writer 并发安全，跨日时关闭旧文件并打开新文件。
- 文件目录权限建议 `0750`，日志文件建议 `0640`。
- 使用现有 `logging.retention_days` 清理过期 PoW 日志；压缩不是本次必需项。
- `Server.Close()` 必须关闭 writer。

建议新建独立包，例如：

```text
internal/master/powtelemetry/
```

包含 `Writer`、`Record`、日期轮换、保留期清理和单元测试，避免继续扩大公共 HTTP 文件。

## 11. 普通签发日志

现有“下载令牌已签发”日志增加：

- `pow_algorithm`
- `pow_protocol_version`
- `challenge_age_ms`

V1 继续记录：

- `pow_difficulty`

V2 记录：

- `pow_iterations`
- `pow_multiplier`
- `modulus_id`

字段取挑战保存的最终值，不重新推导。同步更新控制台中文标签、过滤白名单和日志测试。独立 `logs/pow` 遥测记录用于后续统计，普通签发日志用于实时排障，两者职责分开。

## 12. 主要代码落点

| 范围 | 位置与建议 |
| --- | --- |
| VDF 配置 | `internal/config/` 新建 tier/迁移/校验文件 |
| quota 限制 | `internal/config/quota.go` 与 quota 模板增加 `challenge_limits` |
| 模数管理 | `internal/master/public/` 新建 key manager 文件 |
| 数学与编码 | 新建 VDF core/encoding 文件，避免塞入 handler |
| 挑战内存 | 扩展 `challenge_memory.go`，必要时拆出 reservation 文件 |
| V1/V2 handler | 从 `challenges.go` 拆分，保持每文件不超过 250 行 |
| 授权分派 | 修改 `authz.go`，显式按版本和算法验证 |
| 来源统计 | 修改 `authorization_insert.go` 等位置，不再通过未知 Kind 默认推断 |
| V2 路由 | 修改 `server_http.go`，增加 V2 和 V1 410 wrapper |
| 状态别名 | 抽出公共 authorization-by-ID 逻辑 |
| 前端 | 修改 `download-pow.js`，新增 `vdf-worker.js` |
| 页面资源 | `download_pow_page.go` 移除普通页 SHA loader/WASM |
| 遥测 writer | 新建 `internal/master/powtelemetry/` |
| 日志 | 更新 `internal/logging/` 标签、过滤和测试 |
| 文档 | 更新在线 API、`docs/公开API.md`、配置与数据模型文档 |

## 13. 实现顺序

1. 增加配置结构、默认值、迁移、外部/嵌入模板和配置测试。
2. 实现 VDF 数学、定长编码、固定向量和模数管理器。
3. 重构挑战字段，实现原子 reservation、共享桶和 outstanding。
4. 实现 V2 handlers、V1 开关、V1 Web 410 和状态查询别名。
5. 接入授权验证、事务消费边界和签发日志字段。
6. 实现 `vdf-worker.js`、准确进度、能力提示和遥测采集。
7. 实现 `logs/pow` writer，并在 V2 Web 授权成功后写记录。
8. 更新 API、配置和数据模型文档。
9. 运行自动测试、本机构建和本机浏览器/性能检查。

## 14. 精简测试要求

自动测试只保留关键合同：

- 固定数学向量、顺序/陷门计算一致。
- 384 字节编码、非法字符、错误长度、越界和错误答案。
- 跨资产、跨来源、跨 Web/API、跨 V1/V2 拒绝。
- 过期、轮换后旧挑战、并发提交、提交前失败重试、成功消费。
- Web/API/V1/V2 共享桶，单来源 4 个 outstanding，并发不突破上限。
- VDF 计算并发饱和和全局容量稳定错误码。
- V2 全链路、V1 原合同、V1 开关、V1 Web 410、状态查询别名。
- `altcha.challenge_ttl` 迁移、显式 `v1_enabled=false`、模板一致性。
- 正常页不加载 SHA WASM，惩罚页保持不变，Worker 单线程且进度到 100%。
- 遥测字段限长/限值、并发写入、跨日分段、保留期清理。
- 遥测 writer 失败不影响成功授权，输出文件每行都是合法 JSON。
- 遥测记录中不存在 solution、答案、陷门、Cookie、令牌或完整请求体。

为密钥生成器、时钟和遥测日期提供测试注入，避免真实等待和不稳定测试。

## 15. 本机验证

只要求在实施者当前本机和本机现有工具上验证，不要求连接或借用其他设备。

本机检查内容：

- 当前可用浏览器能完成 V2 challenge、Worker 求解、authorization 和下载跳转。
- 六档基础迭代数分别记录纯 Worker 求解耗时。
- 记录本机 CPU、系统、浏览器版本和功耗环境，避免只写 GPU 型号。
- 进度单调、最终精确 100%，页面保持响应。
- 页面没有调用 WebGPU，没有启动多个 Worker。
- 本机无依赖 Python 参考求解器至少完成一个固定向量和最高基础档。
- 查看 `logs/pow/YYYY-MM-DD.jsonl`，确认设备、迭代数、倍率和耗时字段正确。
- 如果本机缺少可自动化浏览器能力，完成手工验证并记录步骤；无法验证的项目明确列出。

候选六档时间只是配置目标。若本机结果明显偏离，应先检查实现和测量边界，再决定是否调整默认 iterations；不推导其他设备的性能结论。

## 16. 最终验证命令

```bash
env GOCACHE=/tmp/mirror-server-go-cache go test ./...
env GOCACHE=/tmp/mirror-server-go-cache go build -o /tmp/mirror-master-vdf ./cmd/master
./scripts/build-linux-amd64.sh
./scripts/build-windows.sh
sh scripts/check-file-lines.sh
git diff --check
git status --short --branch
```

构建脚本产生或更新构建产物时，最终审计不得混入 `dist/`、缓存和临时文件。

## 17. 完成定义

- V2 Web/API、V1 开关、V1 Web 410 和状态别名实现完成。
- 配置迁移、外部示例、嵌入模板一致。
- 普通网页与惩罚页资源边界正确。
- `logs/pow` 每日 JSONL 遥测可用，失败不影响授权。
- 精简自动测试、主节点构建、两平台脚本、行数门禁和 diff 检查通过。
- 本机能执行的协议互通、浏览器行为和耗时检查已完成。
- 未实现任何 API V1 自动删除或后续清理逻辑。
- 工作区没有无关修改，且未自行合并或推送。
