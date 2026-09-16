# WebSocket 控制面与 Swarm 节点同步改造计划

> 状态：**设计 / 探索中，部分决策已锁定**
> 当前代码基线：`6ad225b266ca231b089d067f891dbd491f92171d`（2026-09-09）
> 当前协议：`control.v1`，见 [节点通信协议.md](./节点通信协议.md)
> 本文职责：作为本次改造的**唯一实施恢复点**。后续协议决策、优先级、代码改动、测试、迁移进度和踩坑记录都必须持续更新本文。
> 注意：在 `control.v2` 正式上线前，`节点通信协议.md` 仍描述当前生产协议；本文描述目标架构和实施过程，不得提前把未实现行为写成已上线事实。

---

## 0. 为什么需要这份文档

这次改造不只是把一个 HTTP 轮询接口换成 WebSocket。当前 mirror-server 已经存在一套较成熟的节点控制协议和大量经过线上/历史问题修复形成的边界条件：任务租约、控制序号、库存修订、任务结果补报、流量事件持久化、下载授权下发、公网探测、peer fallback、文件校验、重连保护等。

因此实施原则是：

1. 先完整盘点当前 `Master <-> Node` 的控制行为和历史不变量；
2. WebSocket 只替换/升级控制面的传输和并发模型，不能丢掉已有可靠性约束；
3. 同一轮完成真正的 Swarm 多源分块同步，而不是只把现有“单 peer 多线程 Range”换个名字；
4. 任何新增通信都必须登记消息优先级；
5. 任何完成项都必须在本文打勾并写明相关文件/测试，确保对话中断后可以只依靠仓库恢复工作。

---

# 1. 已锁定的架构决策

以下事项已经明确，除非后续发现硬性技术冲突，不再重复讨论。

- [x] 节点主动连接 Master。
- [x] 一个 `node_id` 只允许一个 active control session；新会话接管后旧会话必须失效，旧会话后续消息不得倒灌状态。
- [x] 控制面使用 **WSS 长连接**，继续保留 TLS 1.3 + 节点 mTLS 身份认证。
- [x] 应用协议继续使用 JSON；第一版不引入 Protobuf/MessagePack。
- [x] 新控制协议使用独立版本（目标为 `control.v2`），不能假装与当前 `control.v1` wire contract 兼容。
- [x] 文件字节本体继续使用 HTTPS，不通过 WebSocket 传输。
- [x] 节点间文件数据继续使用 HTTP Range/分块读取，控制面只传播任务、manifest、peer、availability、状态等元数据。
- [x] 任务生命周期与 WS session 生命周期解耦；WS 断线不能直接使正在运行的同步任务失败。
- [x] 核心同步可靠性保留现有已验证的业务不变量（持久任务、lease/attempt、ACK 前落盘、terminal result 最终送达、inventory revision），但 **v2 不受现有 wire/函数/表结构绑架**；如果重构后更简单，应重写实现。
- [x] 但流量事件、授权终态、任务结果等本来就要求精确入账/最终送达的数据，仍保留 durable queue + ACK/idempotency；“不用全局消息重放”不等于“所有消息 best-effort”。
- [x] 状态/进度类消息允许 coalesce；队列拥塞时可用最新状态覆盖旧状态。
- [x] 控制消息优先级范围为 **1~128**，所有消息类型必须登记在本文的优先级注册表中，代码不得重新出现散落的隐式发送顺序。
- [x] Master 负责资产/任务/peer discovery/manifest/协调，不逐 piece/block 微操 Node 的下载来源；task 状态机允许在 v2 中重整，只保留必要状态与历史不变量。
- [x] Node 本地负责 Swarm piece/block scheduler、peer 选择、超时、重试、慢源降权和下载并发。
- [x] 允许**未完整下载**的节点向其他节点分享已经通过 manifest 校验的 piece。
- [x] 最终文件提交前仍必须执行权威整文件 `size + SHA-256` 校验；piece 校验不能替代最终整文件校验。

### 1.1 术语冻结

v2 wire/storage/scheduler 统一使用 BitTorrent 风格术语：

- **piece**：manifest hash、availability、verified/share 的最小完整性单位；
- **block**：一次 HTTP Range 请求/传输单位，一个 piece 由一个或多个 blocks 组成；
- 新协议字段使用 `piece_*` / `block_*`，不再使用含义模糊的 `chunk_*`；
- 旧 `control.v1` 文件名、历史提交或当前实现中的 `chunk`/`inventory_chunks.go` 保持原名，仅表示 legacy 代码，不作为 v2 wire contract。

- [x] Swarm 一次性按完整目标实现，不先上线一个长期存在的“伪 Swarm / 单源并发”中间终态。
- [x] 已使用 v2 的节点 WS 掉线后只重连 WS，不自动切回旧控制协议形成双控制面。
- [x] 迁移期 Master 可以同时接受 legacy `control.v1` raw mTLS 节点和 `control.v2` WSS 节点；v2 enrollment 为 WSS JSON RPC；完成迁移后删除 v1 listeners。
- [x] **公网数据面探测是保留 HTTPS 的特殊例外。** Master 必须真的访问 Node 公网下载入口，才能验证下载链路可达；不能用现有 WSS 控制连接代替该探测。

### 1.3 最终实现原则：质量优先，但拒绝无收益复杂度

本次是消息面的正式重构，不把“少改代码/复用旧实现”当目标。判断标准是：**能明显降低长期复杂度、修掉旧模型根因的重构应该做；只为理论完备、未来假设或极小性能收益增加的机制不做。**

因此分成三类：

**应当重构，不为兼容 v1 妥协：**

- v2 新 Envelope 不继承 v1 的 `node_id/request_id/global sequence/accepted_sequence` 包袱；Node 身份来自 mTLS session；
- 删除“业务函数自己读 socket 等 ACK”“heartbeat_ack 充当万能 ACK”“10/200ms poll read”等旧模型；
- 新建明确的 `Session -> Reader -> Dispatcher` 与 `Priority Scheduler -> Writer`；
- ACK/RPC 统一用 `message_id/reply_to` correlation，但业务幂等使用自己的 identity，不再用全局 sequence；
- task dispatch 引入明确的 `attempt_id`，新 attempt 自动使旧 ACK/result 失效，替代一部分现有 sequence/stale-result 复杂逻辑；
- transport handler 与业务 service/repository 解耦，v2 测试尽量不依赖 raw socket；
- 若现有 Node 本地 task/result 表拆分导致重复逻辑，可以在迁移中重整，而不是强行原样保留。

**保留，因为收益明确：**

- 1~128 消息优先级注册表、bounded queue、coalesce；
- task/traffic/authorization 等真正需要最终送达的数据保持 durable；
- Swarm piece hash、persistent partial、多源、bootstrap 单 seed、最终 whole SHA-256；
- asset/temp 两个文件系统的容量采集与 Node 本地 admission；
- Node-global inbound sync bandwidth limit；
- public probe 继续真实 HTTPS 探测；
- re-enrollment 不再误删 TB 级资产数据。

**暂不实现，除非真实 benchmark/威胁模型证明需要：**

- 通用 Desired/Observed framework 或通用 durable message bus；
- availability/source delta + gap-repair 协议；直接发小型完整 snapshot；
- 通用 bulk segment/assembly framework；
- 每个 HTTP block 的目标节点私钥签名、nonce/replay cache；首版用短 TTL bearer capability；
- 同一 piece 多 writer、endgame duplicate；
- 动态 block size；
- Master 全局 peer RTT/吞吐评分；peer 健康度留在下载 Node 本地；
- 新 Prometheus/metrics 子系统、远程 diagnostic RPC、动态 config push；
- 为证书重新登记强制重哈希所有已缓存资产。

---

# 2. 当前实现真实基线（探索结果）

## 2.1 当前控制面不是 HTTP polling

当前 `control.v1` 实际结构是：

```text
Node
  -> tls.Dial(TCP)
  -> TLS 1.3 + mTLS
  -> 长连接
  -> 4-byte big-endian length + JSON Envelope
  -> Master
```

相关代码：

- `internal/protocol/framing.go`
- `internal/protocol/envelope.go`
- `internal/master/control/server.go`
- `internal/master/control/server_read.go`
- `internal/node/control/client.go`
- `internal/node/control/session_loop.go`
- `cmd/master/server_runtime.go`

当前代码为了在“同步等待响应”和“Master 主动下发消息”之间折中，存在多处 10ms/200ms 的短 read deadline/polling；WS v2 的主要价值之一就是把这种“请求/响应串行 + 插队读取”改造成真正的全双工 read loop + prioritized write loop。

## 2.2 当前 Envelope

`internal/protocol/envelope.go` 已有：

```text
protocol_version
message_id
message_type
sent_at
node_id
request_id
reply_to
sequence
payload
```

v2 应保留 `message_id` / `reply_to` / `sequence` 的语义价值，但要重新定义哪些消息使用 durable sequence，不能机械地让每个 transient event 都进入全局可靠重放。

## 2.3 当前 Frame 边界

当前：

- 最大 frame：1 MiB；
- heartbeat：16 KiB；
- pressure report：16 KiB；
- inventory 会同时按 1000 项和约 1/2 MaxFrameBytes 分块；
- `FrameReader` 专门修复过“短 read deadline 发生在 frame 中间时丢掉部分字节”的问题。

WS 自己提供 message framing 后可以删除自定义 4-byte frame，但仍必须设置：

- 最大 WS application message bytes；
- JSON decode 深度/大小边界；
- write timeout；
- ping/pong deadline；
- outbound queue byte/message 上限。

## 2.4 当前 Peer fallback 不是 Swarm

当前 `internal/master/control/sync_fallback.go`：

- 最多返回 3 个 fully verified peer；
- 只允许 `node_inventory.state = verified` 且整文件 digest/size 匹配的 peer；
- 大文件（默认 >= 32 MiB）会预生成约 8 个连续 Range part；
- 每个 part 都绑定单独复制 token。

当前 `internal/node/syncer/fallback.go` / `fallback_parts.go`：

- peer 是**逐个尝试**；
- 对一个 peer 可以并行下载多个 Range；
- 不是同时从 A/B/C 多个 peer 获取不同 piece/range；
- 下载完成后再对整个临时文件 SHA-256。

所以当前功能应准确称为“single-peer parallel range fallback”，不能直接复用为目标 Swarm 调度器。

## 2.5 当前 partial data 重启即删除

`cmd/node/main.go` 每次启动会调用：

```text
syncer.CleanTempDirectory(...)
```

`internal/node/syncer/temp_cleanup.go` 会清空整个有效 temp 目录。

这与 Swarm 的“未完成节点分享 verified pieces”直接冲突。v2 必须把可恢复 partial state 从普通临时垃圾升级为受数据库管理的持久 partial store；启动时不能再无条件删除。

## 2.6 当前磁盘剩余空间为什么一直为 0

协议和 Master/UI 其实已经有 `free_bytes`：

- `protocol.Heartbeat.FreeBytes`
- `protocol.PressureReport.FreeBytes`
- Master runtime store
- 管理后台节点页面

但 Node 发送端目前在以下两处都硬编码：

```go
FreeBytes: 0
```

位置：

- `internal/node/control/client.go`
- `internal/node/control/pressure.go`

因此问题核心不是“缺字段”，而是**没有真正读取 storage 所在文件系统的可用空间**。

目标修复：

- 对 `storage.directory` 所在 filesystem 获取 available bytes；
- Linux/Unix 使用 `statfs` 等平台实现；Windows 使用对应磁盘空间 API；
- 采集失败不能伪装成 0 bytes（0 有“磁盘真的满了”的语义），v2 应增加 valid/unknown 语义；
- 最好同时上报 `total_bytes / available_bytes`，后续 Swarm 调度使用 available；
- Master 分配任务时必须考虑“剩余待下载 bytes + 安全预留”，避免把大文件派给容量不足节点。

## 2.7 当前 Master 是否会为了哈希下载整文件

**GitHub Release 当前路径不会。**

当前 `internal/master/mirrorsync/github.go` 直接读取 GitHub Releases API 返回的：

- asset size；
- asset digest。

`scanner_helpers.go` 要求 digest 能规范化为有效 SHA-256；缺失/非法 digest 的资产会被拒绝，不会由 Master 下载一次来补 digest。

因此 Swarm 设计必须避免引入一个新的回退：为了获得 piece hashes 让 Master 把每个文件重新下载一遍。

另外，历史 `e67817c` 的脚本源实现不在当前 HEAD 的祖先链中；当前基线不能按该分支能力设计。

---

## 2.8 最终审计发现的 v1 周期性/结构性浪费

这些不是必须保留的“业务能力”，而是旧控制模型造成的成本，v2 应直接消除：

1. **全局 sequence/accepted_sequence 贯穿几乎所有消息。** 它主要用于补偿串行读写和万能 ACK；v2 改为 domain identity 后删除。
2. **heartbeat/pressure 重复承载大量相同状态。** v2 合并为 `node.status`。
3. **heartbeat handler 过重。** 当前每次 heartbeat 都开启 SQLite transaction，并可能执行 missing repair / inventory cleanup / limit reconcile。v2 高频 status handler 不做这些业务扫描。
4. **task running ACK 重复续报。** v2 初次 `sync.accepted` 后，用 `node.status.active_tasks(task_id+attempt_id)` 维持 lease，不再单独周期 running ACK。
5. **Node 当前每分钟生成一次完整 inventory report。** 正常 task result 已经会把成功/失败实时告诉 Master，一分钟一次全量 DB scan/report 属于冗余安全轮询。
6. **session loop 通过 10ms/200ms timeout poll 同时兼顾读 socket 与本地 wake。** v2 单 reader + channel/queue 后完全删除。
7. **task dispatch 经常附着在“任意消息处理后顺手再扫一次”。** v2 改为明确 scheduler wake：任务生成、slot/capacity 变化、task terminal、Node reconnect 等事件才触发。
8. **Master runtime 心跳/压力结构重复。** `runtimeHeartbeat` 与 `runtimePressureReport` 大量字段相同；v2 合并为 `runtimeNodeStatus`。
9. **部分 v1 schema 已成为遗留。** `node_heartbeats` / `node_pressure_reports` 当前生产路径基本不再读写，`node_control_sessions.last_message_sequence` 只服务 v1；v1 删除阶段应做明确 migration cleanup，而不是永久背着。

这些地方应视为“需要重构掉的旧实现”，不能因为历史测试很多就原样迁移。测试应迁移的是业务不变量，而不是旧 polling/sequence 行为。

---

# 3. 当前控制通信完整盘点

这一节用于防止迁移时漏消息。状态分为：

- **当前使用**：当前 HEAD 有实际收发路径；
- **声明但非主路径**：协议常量存在，但当前主 handler 并未作为独立消息完整使用；
- **v2 新增**：Swarm/新控制面需要。

## 3.1 Enrollment / Session

| 当前/目标消息 | 方向 | 当前状态 | 当前实现位置 | v2 处理 |
|---|---|---|---|---|
| `enroll_request` | Node -> Master | 当前使用 | node `enroll.go`, master `enrollment.go` | v2 WSS JSON enrollment RPC |
| `enroll_pending` | Master -> Node | 当前使用 | 同上 | 保留 |
| `enroll_certificate` request | Node -> Master | 当前使用 | 同上 | 保留 |
| `enroll_certificate` delivery | Master -> Node | 当前使用 | 同上 | 保留 |
| `hello` | Node -> Master | 当前使用 | `client.go`, `hello.go` | `session.hello`，版本/能力协商 |
| `welcome` | Master -> Node | 当前使用 | 同上 | `session.welcome` |
| `protocol_error` | 双向语义 | 当前使用 | `session_reject.go`, responses | 保留，最高等级控制错误 |
| `node_disabled` | Master -> Node | 常量存在 | 当前 disabled 主要通过状态/routing 语义处理 | v2 明确成 managed-state event，不强制断控制连接 |
| `certificate_rejected` | Master -> Node | 常量存在 | 当前启动拒绝更多走 protocol error | v2 明确证书吊销/拒绝语义 |

Enrollment 当前虽然使用 `https://` 配置形式，但客户端最终只是取 `URL.Host` 后 `tls.Dial(tcp)`；并不是真正 HTTP。v2 配置必须改成真正的 `wss://.../path` URL 语义。

## 3.2 Liveness / Capacity / Inventory

| 消息 | 方向 | 当前状态 | 可靠性要求 | v2 |
|---|---|---|---|---|
| `heartbeat` | Node -> Master | 当前使用 | 最新值即可；但 last-seen 必须可靠更新 | `node.status`，可 coalesce |
| `heartbeat_ack` | Master -> Node | 当前使用且被复用为大量 ACK | v1 correlation/sequence | v2 删除；改为真正需要的 domain ACK，且禁止 ACK-of-ACK |
| `pressure_report` | Node -> Master | 当前使用 | 最新值即可 | `node.status` / `node.pressure`，可 coalesce |
| `inventory_report` | Node -> Master | 当前使用、分片、有 revision | **完整 snapshot 必须闭环** | 保留 revision + snapshot complete 语义 |
| `inventory_reconcile_request/result` | 双向 | 常量存在 | 当前主要通过 `inventory_reconcile` sync task + 强制完整库存完成 | v2 不重复造两套语义，统一进入 reconciliation 流程 |

## 3.3 Sync task

| 消息 | 方向 | 当前状态 | 可靠性要求 | v2 |
|---|---|---|---|---|
| `sync_task` | Master -> Node | 当前使用 | durable desired work + lease | `sync.task` |
| `sync_task_ack` | Node -> Master | 当前使用 | 必须与 task/session 绑定，重复幂等 | `sync.accepted/observed` |
| `sync_task_result` | Node -> Master | 当前使用，Node DB 持久补报 | 必须最终送达，Master 拒绝 stale/wrong binding | `sync.result` |
| `sync_task_progress` | Node -> Master | 常量存在，当前不是核心使用路径 | transient | v2 正式实现，可按 task coalesce |
| `sync.cancel` | Master -> Node | v2 新增 | 高优先级 desired-state | 必须实现，支持取消/撤销正在排队或运行任务 |

## 3.4 Download authorization / accounting

| 消息 | 方向 | 当前状态 | 可靠性要求 | v2 |
|---|---|---|---|---|
| `download_authorization` | Master -> Node | 当前使用 | 用户下载热路径，必须低延迟且最终明确交付 | 保留，进入高优先级 durable queue |
| `download_authorization_ack` | Node -> Master | 当前使用 | Node 已持久化授权 | v2 保留为 `download.authorization.ack`；Master 收到即结束，不再回 ACK-of-ACK |
| `authorization_status_event` | Node -> Master | 当前使用 | terminal 状态最终送达 | 保留 durable queue |
| `authorization_status_event_ack` | Master -> Node | 当前使用 | correlation + accepted | 保留 |
| `traffic_event` | Node -> Master | 当前使用、本地 SQLite 未确认队列 | **不可丢失，精确去重/入账** | 保留 durable queue |
| `traffic_event_ack` | Master -> Node | 当前使用 | event identity / dedupe | v2 保留；按 traffic event id/sequence 幂等 |
| `traffic_replay_request` | Master -> Node | 常量存在 | 当前 Node 已主动从本地 pending queue 补报 | v2 默认不需要单独 replay 命令；若保留必须登记 |

## 3.5 Public probe

控制消息：

```text
Master -> Node : challenge（当前附着在 heartbeat_ack）
Node -> Master : public_probe_ready
```

数据面验证：

```text
Master -> Node public_download_base_url
GET /.well-known/mirror-node/probes/<challenge_id>
```

目标：challenge 在 v2 中拆成独立 WS 消息，不再依赖下一次 heartbeat ACK；真正 HTTP probe 继续保留。

## 3.6 v2 Swarm 新增通信

至少需要：

```text
swarm.manifest.report
swarm.manifest.ack

swarm.sources
swarm.sources

swarm.availability
swarm.availability
(removed: full availability snapshot)

(removed: peer score stays local)
(removed: peer score stays local)      # 可选；优先本地计算，Master 只做粗粒度健康度

sync.cancel
```

实现阶段如新增其他 message type，必须先更新本文优先级表，再写代码。

### 3.7 legacy v1 幽灵消息（明确不迁移）

最终反向审计确认 `control.v1` 的 allow-list 中存在若干**只有常量、没有生产发送/处理代码**的历史预留消息：

```text
sync_task_progress
traffic_replay_request
inventory_reconcile_request
inventory_reconcile_result
node_disabled
certificate_rejected
```

这些消息不能因为“v1 允许”就自动复制进 v2：

- `sync_task_progress`：由 v2 的 transient task/swarm progress 状态替代；
- `traffic_replay_request`：继续以 durable outbox + reconnect resend 实现，不增加伪 RPC；
- `inventory_reconcile_request/result`：由现有 inventory reconcile task + snapshot revision 语义承接；
- `node_disabled`：disabled 是 Master desired/routing state，不需要单独幽灵消息；
- `certificate_rejected`：统一为 session/protocol error code + WS close reason；
- v2 registry completeness test 只覆盖**真实定义并实现**的 v2 消息，禁止保留“允许但无人处理”的类型。

---

# 4. 控制消息优先级注册表（必须持续维护）

## 4.1 规则

已冻结：**数字越小优先级越高**，业务消息合法范围为 `1~128`；`0` 保留给 transport/session 内部动作，不作为 wire message priority。

实现保持简单：

- priority 是 registry 属性，调用点不能传魔法数字；
- writer 使用一个有界 priority queue；不再引入多层“消息总线”；
- durable 语义由现有 DB/outbox/task 状态机负责，priority queue 本身不做持久化；
- 可替换状态只使用 `coalesce_key` 覆盖旧值；
- 队列限制 message count + total bytes；
- 为避免极端持续高优先级流量饿死状态消息，采用简单 burst fairness（例如连续发送 N 条高优先级后允许一条已等待的低优先级消息），不实现复杂 aging/weighted scheduler；
- WS Ping/Pong/Close 与 hello/welcome 是 transport/handshake fast path，不被普通队列阻塞。

## 4.2 注册表（最终初版）

> 这是实现的 source of truth 草案。代码中的 registry completeness test 必须保证新增 control message 未登记 priority 时直接失败。

| Priority | v2 消息 | 方向 | 可靠性 / ACK | 合并策略 | 说明 |
|---:|---|---|---|---|---|
| 1 | `session.hello` / `session.welcome` | 双向 | handshake | 否 | fast path，不进普通业务 backlog |
| 2 | `protocol.error` | 双向 | 当前 session | 否 | fatal 时随后 close |
| 4 | `sync.cancel` | M->N | task state 驱动 | 按 task 最新 | 立即停止无效 attempt |
| 8 | `sync.result` | N->M | **durable，需 `sync.result.ack`** | 否 | Node terminal result 未 ACK 前保留 |
| 9 | `sync.result.ack` | M->N | 幂等 | 否 | `reply_to + task_id + attempt_id` 必须同时匹配 |
| 10 | `sync.accepted` / `sync.rejected` | N->M | 幂等，不再需要二次 ACK | 按 task+attempt | task 落盘/本地 admission 后响应 |
| 12 | `sync.task` | M->N | Master task+attempt lease；accepted 丢失可重发原 task | 按 task+attempt | 新 attempt / retry |
| 16 | `download.authorization` | M->N | 重发直到看到 ack | 按 authorization_id | Node 幂等落本地 |
| 17 | `download.authorization.ack` | N->M | 幂等，**无 ACK-of-ACK** | 否 | Master 收到即标记已送达 |
| 20 | `authorization.status` | N->M | durable，需 ack | 否 | expired/terminal status |
| 21 | `authorization.status.ack` | M->N | 幂等 | 否 | Node 可标 reported |
| 24 | `traffic.event` | N->M | durable，需 ack | 否 | 精确流量入账 |
| 25 | `traffic.event.ack` | M->N | 幂等 | 否 | Node 删除/确认 pending event |
| 28 | `public_probe.challenge` | M->N | TTL state | 按 challenge 最新 | 随后仍由 Master 做真实 HTTPS probe |
| 29 | `public_probe.ready` | N->M | 幂等，无遮挡 ACK | 否 | ready 丢失时 challenge 可重发 |
| 33 | `swarm.manifest.report` | N->M | 需 manifest ack | 按 manifest_id | 直到 Master authoritative/冲突响应 |
| 34 | `swarm.manifest.ack` | M->N | 幂等 | 否 | accepted / conflict / rejected |
| 36 | `swarm.sources.request` | N->M | best effort | 按 asset/task | token 临近过期/来源失效时请求刷新 |
| 38 | `swarm.sources` | M->N | 可重建完整 snapshot | 按 asset/task 最新 | peer list + short-TTL capabilities |
| 40 | `swarm.availability` | N->M | 可重建完整 bitset | 按 asset 最新 | revision 仅防倒序旧 snapshot |
| 60 | `inventory.snapshot.segment` | N->M | revision durable boundary | 否 | 领域自己的 1000-item segmentation |
| 61 | `inventory.snapshot.ack` | M->N | 幂等 | 否 | 只在完整 revision 接受后 ACK |
| 76 | `node.status` | N->M | 最新状态，无 application ACK | 只留最新 | heartbeat/pressure/capacity/active attempts 合并 |

Enrollment **不进入 control priority queue**：v2 enrollment 使用独立的 server-auth TLS WSS 文本 JSON 短 RPC。它建立短生命周期 WebSocket，但不复用 control session、priority queue 或 mTLS。

### 4.2.1 ACK 规则：禁止 ACK-of-ACK

只在发送方需要据此清理 durable pending 时才有显式 ACK：

```text
sync.result                -> sync.result.ack
authorization.status       -> authorization.status.ack
traffic.event              -> traffic.event.ack
inventory complete revision-> inventory.snapshot.ack
manifest.report            -> manifest.ack
```

命令接受类不再二次确认：

```text
sync.task                  -> sync.accepted / sync.rejected
download.authorization     -> download.authorization.ack
probe.challenge             -> probe.ready
```

如果 accepted/authorization ack 丢失，Master 重发**同一个业务 identity** 的原命令；Node 幂等检测后重新回复即可。Node 不为了确认“我的 ACK 被确认了”再等待第三条消息。

### 4.3 v1 -> v2 映射（只迁移真实生产语义）

- `hello/welcome` -> `session.hello/session.welcome`；
- `heartbeat + pressure_report` -> `node.status`；不保留 application heartbeat ACK；
- `inventory_report` -> `inventory.snapshot.segment`；
- `sync_task/ack/result` -> `sync.task/accepted/result`；
- download authorization / status / traffic 保留各自业务幂等语义，但删除 v1 的 ACK-of-ACK；
- `public_probe_ready` -> `public_probe.ready`，challenge 独立推送；
- v1 幽灵消息不迁移；
- `inventory_reconcile` 继续作为现有 `sync.task` 的 task type，不再新造独立控制消息。

### 4.4 大消息：不建立通用 bulk 子协议

最终审计认为此前的通用 `begin + indexed segments + commit + assembly TTL` 属于过度实现。当前已知大对象可以更简单处理：

- WS 单 logical message hard cap 仍为约 `1 MiB`；
- manifest 在 `piece_count <= 8192` 时 hash blob base64 约 342 KiB，可直接作为**一条低优先级 WS message**；
- inventory 已经有成熟的“1000 items/segment + revision + complete”语义，v2 直接迁移，不抽象成通用 bulk framework；
- 未来若出现新的 >1 MiB payload，由该业务自己定义分页/分段，不提前造公共 assembly 层。

在当前 100 Mbps 级节点链路上，约 342 KiB manifest 的线速发送时间只有几十毫秒；为消除这几十毫秒的不可抢占窗口而引入通用分段状态机，收益不足以抵消复杂度。

---

# 5. v2 WS 会话模型

## 5.1 Session / Reader / Dispatcher / Writer

Node 与 Master 都采用清晰的单读单写模型：

```text
WebSocket
  ├─ reader goroutine -> decode/validate -> dispatcher -> typed handler
  └─ writer goroutine <- bounded priority scheduler <- producers
```

业务 handler **禁止直接读/写 WebSocket**。发送只能 enqueue；接收只能由唯一 reader 解析后分发。这样从结构上删除 v1 的 interleaved-read/ACK hack。

## 5.2 v2 Envelope：去掉 global sequence 和 Node 自报身份

v2 建议直接使用：

```json
{
  "v": 2,
  "type": "sync.result",
  "id": "01J...",
  "reply_to": "01J...",
  "payload": {}
}
```

字段：

```text
v          protocol major，当前固定 2
type       message type
id         每条消息唯一 ID
reply_to   可选；ACK/RPC response 指向 request message id
payload    typed JSON payload
```

明确删除：

- `node_id`：来自已认证 session，消息自行声明反而有 spoof/一致性问题；
- `request_id`：HTTP request tracing 只在相关业务 payload/log context 中保留，不做所有控制消息的强制字段；
- `sequence/accepted_sequence`：v1 为串行流和万能 ACK 补出来的全局顺序，不适合全双工 WS；
- 强制 `sent_at`：只有业务语义需要时间时写在 payload；普通消息日志使用本地接收/发送时间。

幂等/新旧判断全部回到业务 identity：

- sync task：`task_id + attempt_id`；
- traffic：`event_sequence/event_id`；
- authorization：`authorization_id + status/version`；
- inventory：`revision`；
- probe：`challenge_id`；
- manifest：`manifest_id`；
- availability：`asset_id + manifest_id + revision`。

ACK 仍通过 `reply_to` 做传输 correlation，但 ACK 是否能改变持久状态必须同时校验业务 ID；只匹配 message id 不够。

## 5.2.1 hello/welcome

`session.hello` 只带真正需要的协商信息：

```text
software_version
features[]          # 例如 swarm_v1 / partial_serve_v1
active task attempts（用于重连快速 reconcile，可有上限）
```

不做 `protocol_min/protocol_max` 通用协商：`/control/v2` 就是明确的 v2 endpoint。未来 v3 用新 path/major。feature flags 只用于 v2 内灰度能力。

`session.welcome` 返回 `session_id`、heartbeat/status interval、managed/routing state 等。

## 5.3 可靠性：统一 transport 机制，业务状态保持 domain-owned

不做一张万能 durable-outbox 表，也不为每种消息复制一套 socket/ACK 代码。

WS runtime 提供统一能力：

```text
priority enqueue
coalesce latest state
message_id/reply_to future/callback
ACK timeout / session close
reconnect wakeup
```

真正的持久状态仍归业务域所有，因为它们的幂等键和清理语义不同：

- task：Node 本地 task execution/result 状态；
- traffic：流量 event queue/sequence；
- authorization：local authorization terminal status；
- inventory：revision/cursor。

实现时可以重整现有表（例如合并 `local_sync_tasks` 与 `pending_sync_task_results` 的重复状态），但不把所有 payload JSON 塞进一个通用 outbox 表。

建议提供一个很薄的 `ReliableProducer`/ACK callback 接口，让这些 domain store 共用发送与确认框架；**统一 transport，不统一业务数据模型**。

## 5.4 重连

目标 backoff：指数退避 + jitter，例如：

```text
1s -> 2s -> 4s -> 8s -> 16s -> 30s cap，±20% jitter
```

Master 重启时避免所有 Node 同时打满连接。

重连后不是重播所有 transient frame，而是：

1. session hello / capability / protocol negotiate；
2. durable outbox 继续补发；
3. task attempt reconciliation：Master/Node 对比 `task_id + attempt_id`，恢复仍有效 attempt、补报 terminal result，旧 attempt 直接忽略；
4. inventory revision 对账；
5. Master 重新发送相关 manifest/source snapshot，Node 上报当前完整 availability bitset；
6. 立即发送最新 `node.status`。

## 5.5 v2 Task attempt 模型

当前 v1 的 `sent/running + global sequence + lease timestamp + repeated running ACK` 可以简化为显式 attempt：

```text
Task (logical)
  pending
    -> offered(attempt_id, lease)
    -> running(attempt_id)
    -> succeeded / failed / retry_wait / obsolete / cancelled
```

规则：

1. Master 每次真正派发/重试生成新的随机 `attempt_id`；
2. `sync.task` 带 `task_id + attempt_id + lease_expires_at`；
3. Node 在本地持久化该 attempt 后发一次 `sync.accepted`；不再周期发送“running ACK”；
4. `node.status` 携带当前 active `task_id + attempt_id`（数量受 sync slot 限制），Master 用它刷新 running lease；
5. `sync.result` 必须带相同 `attempt_id`；Master 只接受当前 attempt，旧 attempt result 幂等 ACK 后丢弃；
6. lease 过期且 Node status 不再声明该 attempt 时，Master 可生成新 attempt；
7. cancel 以 `task_id` 为主，并可带当前 attempt_id；Node 本地 cancel 后终止该 execution。

这保留 lease/retry/stale-result 的业务能力，但比“所有控制消息共享一个 sequence”更直接。

Node 本地实现可以考虑把 `local_sync_tasks + pending_sync_task_results` 收敛成一张 `local_task_executions`，保存 `task_id/attempt_id/state/terminal result/reported_at`，但是否迁表由实现阶段根据代码复杂度决定；**不要求为了兼容 v1 表结构继续维护两套同源状态。**

---

# 6. Swarm 完整目标设计

## 6.1 Manifest

每个可 Swarm 的资产有确定 manifest，例如：

```json
{
  "manifest_id": "...",
  "asset_id": "...",
  "asset_size": 8589934592,
  "asset_sha256": "...",
  "piece_layout_version": 1,
  "piece_size": 8388608,
  "piece_count": 1024,
  "piece_hash_algorithm": "sha256",
  "piece_hash_blob": "<base64 raw 32-byte hashes>"
}
```

### Manifest 必须满足

- piece layout 完整覆盖 `[0, size)`；
- 不重叠、不留洞；
- 最后一个 piece 允许小于 piece_size；
- piece hash 固定 SHA-256；
- manifest identity 与 `asset_id + authoritative whole digest + piece_layout_version` 绑定；
- 资产 digest/size 一旦变化，旧 manifest 和所有 partial availability 全部作废；
- Node 只允许把 manifest 标记为 verified 的 piece 暴露给 peer。

### Manifest 大小硬边界

- `piece_count <= 8192`；
- piece hash 使用顺序拼接的原始 32-byte SHA-256 blob，JSON 中 base64；
- `hash_blob_bytes == piece_count * 32` 必须严格校验；
- manifest 作为**单条 WS message**发送，不再做通用 segment/assembly；
- Master DB 保存 compact blob + `piece_size/piece_count/hash_algorithm`；
- 在 `128 MiB` piece 下仍超过 8192 pieces（约 >1 TiB）的资产，本版不启用 Swarm，回退 whole-file 同步。

最大 hash blob 原始约 256 KiB、base64 约 342 KiB，明显低于 1 MiB hard cap。

### 不让 Master 下载整文件的方案（当前推荐）

1. Master 已从 GitHub API 获得权威 `size + whole SHA-256`；
2. 第一台 seed Node 从源站正常下载；
3. Node 在下载流经过时同时计算：
   - whole SHA-256；
   - 每个固定 piece 的 SHA-256；
4. 在 whole digest/size **最终通过权威校验之前**，piece hash 不能发布为 authoritative manifest；
5. whole 校验成功后，Node 把已经计算好的 manifest 通过 WSS 报给 Master；
6. Master 做结构/asset binding 校验并持久化；
7. 后续 Node 可以基于 manifest 从多个 peer/origin 获取 piece/block；
8. 每个 piece 的全部 blocks 到齐后立即校验，通过后即可上报 availability 并对外分享；
9. 全部 pieces 完成后仍再做整文件权威 SHA-256，随后 atomic commit。

这样 Master **不产生一次额外整文件下载流量**，首个 seed Node 也不需要为了 manifest 再读磁盘一遍。

### Manifest bootstrap：复用 v2 task attempt/lease 语义，不再单独造 bootstrap 状态机

在 authoritative manifest 不存在时，Master 的调度逻辑增加一个约束：**同一 asset 同时只允许一个 bootstrap `asset_download` attempt 获得 origin 权限**。这可以通过查询当前 task attempts 实现，不要求新增独立 bootstrap lease 表。

- 其他节点任务保持 `pending`（可带内部原因 `await_manifest` 用于可观测性，但不必增加公开状态机分支）；
- seed attempt lease 过期/失败后，调度器可选择另一 Node 生成新 attempt；
- seed 完成 whole SHA-256 后先发送 manifest，Master ACK 后再接受该 task 的成功结果；
- 如果 Node 在“文件已完整落盘但 manifest 尚未 ACK”时断线/重启，重新执行该 task 时先复用现有完整文件并从本地重新计算 manifest，不重新下载 origin；
- manifest 生效后，其余 pending task 正常 claim，进入 Swarm。

因此不新增 `manifest_bootstrap_lease` 表或第二套 generation 状态机；task `attempt_id` 本身就是 stale bootstrap 的 generation。Node 也不需要独立 manifest durable cache。

### Manifest 信任模型：已冻结

已确认采用以下实用模型：

- manifest 只能由“刚刚对该 asset 完成权威 whole SHA-256 校验”的 mTLS Node 上报；
- Master 接受第一份结构合法 manifest；
- 不主动要求其他完整节点做 quorum/confirmation；若后续因重生成自然收到同一 asset 的另一份 manifest，则做一致性检查；
- 若出现冲突，立即冻结该 asset 的 partial-piece Swarm，只允许 fully verified whole-file peer/origin，记录高优先级告警；
- 最终整文件 SHA-256 永远是最后防线，所以错误 manifest 最坏导致下载失败/浪费，不允许错误文件 commit。

当前威胁模型不为“已获得合法节点证书的 Byzantine 恶意 Node”额外引入双节点 quorum。若后续需要提升到该威胁模型，再增加双节点独立确认；当前第一份 whole-verified manifest 可立即成为 authoritative。

## 6.2 BitTorrent-style piece / block sizing

已冻结：不使用固定“8 分片”，而采用 BitTorrent 的核心思路，并针对 HTTP 数据面做适配。

必须区分两个层次：

- `piece`：完整性验证、availability bitmap、rarest-first 调度、partial peer 对外分享的最小单位；只有整个 piece 的 SHA-256 验证通过后才能标记 HAVE/verified；
- `block`：一次 HTTP Range 传输的调度单位。一个 piece 可以由多个 block 组成，block 可以失败重试或换 peer，但在 piece hash 通过前不得对外宣称该 piece 可分享。

这和 BitTorrent 的实践一致：piece 是哈希/availability 粒度，实际请求可以比 piece 更小。我们不照搬 BT 历史上 16 KiB 的网络 request block，因为这里的数据面是 HTTPS Range，请求头、TLS/HTTP 调度成本更高。

Piece 选择规则：

1. piece size 必须为 2 的幂；
2. 以约 `1024` pieces 为中心目标，优先让正常资产落在约 `512~1024` pieces；
3. HTTP 场景下默认最小 piece 暂定 `1 MiB`，避免几十 MiB 文件产生数千个 Range；
4. 默认最大自动 piece 暂定 `64 MiB`，协议允许上限保留到 `128 MiB`；
5. 选择“满足 piece_count <= 1024 的最小 2 的幂 piece size”，再执行 min/max clamp；
6. 最后一个 piece 可短于标准 piece size；
7. 同一个 asset 的 piece layout 一旦 authoritative manifest 发布，所有 Node 必须完全一致。

示例（按上述候选默认值）：

```text
  100 MiB  -> 1 MiB piece   -> 100 pieces
  500 MiB  -> 1 MiB piece   -> 500 pieces
    1 GiB  -> 1 MiB piece   -> 1024 pieces
    2 GiB  -> 2 MiB piece   -> 1024 pieces
   10 GiB  -> 16 MiB piece  -> 640 pieces
   50 GiB  -> 64 MiB piece  -> 800 pieces
  100 GiB  -> 64 MiB piece  -> 1600 pieces（触及默认 max；仍可接受）
```

Block 首版不做动态自适应：benchmark `1/2/4 MiB` 后选一个固定默认值（当前优先 `2 MiB`），`block_size = min(default_block_size, piece_size)`。一个 piece 内可由多个 blocks 组成，但 piece hash 完成前不能进入 verified bitmap。

只对 piece size 做资产级自适应；block 动态调节属于性能优化，等真实数据证明有必要再加。

## 6.3 Node partial store

不能继续使用“启动时整个 `data/tmp` 清空”的模型。

Node 只新增一张最小 partial 状态表即可：

```text
swarm_partials
  asset_id
  manifest_id
  partial_path
  verified_bitmap
  updated_at
```

不新增独立 `swarm_manifest_cache`：manifest authoritative 副本由 Master 持久化；Node 重连时重新获取。`bytes_verified` 可由 bitmap + piece layout 计算，task/generation 不写进 partial 主键，使 partial 能跨 task retry 复用。

设计重点：

- bitmap/range 批量持久化，不能每完成一个 piece 都制造一次 SQLite 高频写；
- inflight block/piece 只需内存态；verified piece 才进持久 bitmap；
- crash/restart 后恢复 persisted verified bitmap；不在启动时全量重哈希。进程启动后某 piece 第一次被本地调度使用或对 peer serve 前做 lazy SHA-256 revalidation，并在内存中缓存本次进程已复验状态；失败则清除该 bit；
- obsolete asset / manifest revision 变化立即隔离旧 partial；
- partial GC 按 desired state、TTL、磁盘压力执行；
- 不能清理 storage 根目录/父目录等现有安全检查必须保留；
- atomic commit 仍使用现有“保护旧资产”的替换逻辑。

## 6.4 Availability：只发完整 bitset snapshot

此前的 delta + revision-gap + resync request 属于不必要复杂度。`piece_count <= 8192` 时完整 availability 只有最多 8192 bits = **1024 bytes**（base64 也只有约 1.4 KiB）。

因此 Node 只发送：

```text
asset_id
manifest_id
revision        # 仅用于丢弃倒序旧 snapshot
verified_bitset # 完整 snapshot
```

规则：

- piece 状态变化在约 100~250ms 窗口内 coalesce；
- 每次发送最新**完整 bitset**，不发送 delta；
- Master 只接受 revision 更高的 snapshot；
- 丢一条没有任何修复协议，下一条完整 snapshot 自然覆盖；
- 重连后 Node 重新发当前 snapshot；
- Master availability 只保存在内存，不写高频 SQLite；Master restart 后等 Node 重报。

同理，Master -> Node 的 peer 信息只用一个 `swarm.sources` **完整 source snapshot**。节点数远小于 piece 数，没有必要维护 `sources.update + sources.snapshot` 两套协议。

## 6.5 HTTP peer serving

当前 `/internal/replication/<asset>` 只允许完整 verified local asset。

目标需要同时支持：

1. committed fully verified asset；
2. partial file 中已经 verified 的 piece。

强约束：

- HTTP Range 可以请求 verified piece 内的 block 子区间；请求范围必须完整落在**单个 source 已 verified piece**内，不能跨越到未 verified piece；block 对齐/最大尺寸按协议策略限制；
- partial piece 的全部 blocks 写入完成、hash 通过并状态提交前不得对外 visible；
- 未 verified / inflight / hole 一律拒绝；
- source node / target node / asset / manifest / expiry 都要绑定授权；
- 保留防止本地文件在验证后被改写的保护；
- 最终 committed file 的现有完整校验缓存策略不能因 Swarm 退化；
- Range 响应必须严格校验返回字节数。

当前复制 token 是短 TTL、并可绑定单 Range、且本地 one-time claim。Swarm 下每个 block 都发一个一次性 token 会造成巨量控制消息，因此应改成**短期 session capability**：

```text
source_node_id
target_node_id
asset_id
manifest_id
expires_at
scope = swarm_chunk_read
```

允许目标节点在 TTL 内请求该 manifest 的任意 verified piece 内允许的 block range，同时保留总并发/速率/滥用限制。

### Swarm capability：第一版保持 bearer token

此前设计的“每个 block 使用 Node 私钥签 canonical request + timestamp + nonce + replay cache”安全性更强，但与当前威胁模型不匹配，属于过度实现。

第一版采用与现有下载授权一致的简单模型：

- Master 使用现有 Ed25519 signer 签 `swarm.v1` capability；
- claims 绑定 `capability_id/source_node_id/target_node_id/asset_id/manifest_id/expires_at/scope`；
- capability 只通过目标 Node 的 mTLS WSS 下发，并只通过 HTTPS 发送给 source；
- TTL 保持很短（初始约 2 分钟），允许在 TTL 内重复读取该 manifest 已 verified piece 内的 blocks；
- source 校验签名、source/asset/manifest/expiry，并实施并发/带宽限制；
- `target_node_id` 用于调度与审计，但**明确不是密码学上的请求者证明**；拿到 bearer token 的主体在 TTL 内可以使用它。

当前系统的公网下载 token 本身也是 bearer credential。除非未来威胁模型提升到“需要抵抗节点侧 token 窃取/合法节点互相冒用”，否则不增加每-block 签名和 replay cache。

## 6.6 Node piece/block scheduler

目标不是 Master 指定“piece 7 的 block 从 B 下载”。Node 自己维护：

- missing pieces；
- inflight pieces / blocks；
- peer availability；
- peer 最近吞吐与连续失败次数（只保留简单 EWMA/backoff）；
- origin availability；
- per-peer concurrency；
- global sync bandwidth limit；
- block timeout + piece deadline；
- retry/backoff；

最低目标算法：

- 优先来源少的 piece（rarest-first 的简化版）以提高 swarm 扩散能力；
- 同等稀有度时选择当前最快/健康 peer；
- 对失败/超时 peer 指数退避；
- 同一 piece 首版只允许一个 worker/source owner；该 worker 按 block 顺序写入。block 失败/超时可从下一个缺失 block 换 source 继续；不同 pieces 之间并行，从而实现真正多源；
- origin 既可作为 bootstrap，也可作为 peer 不足时 fallback；
- 已 verified piece 立即进入可分享集合。

### Piece 写入并发边界

- 一个 piece 只有一个 owner/writer；blocks 在该 piece 内首版顺序执行，不做同-piece 并行写；
- verified bitmap 只在 piece 全部 blocks 落盘且 SHA-256 通过后更新；
- crash recovery 不做启动时全量重哈希；persisted verified piece 在本进程第一次 serve/复用前 lazy rehash，通过后标记为 runtime-trusted。接收方仍会按 manifest hash 再验证，因此不会导致错误文件 commit；
- cancel/obsolete/manifest conflict 后停止新下载与新 peer serve。

首版**不实现 endgame duplicate**，因此无需 scratch file/双 writer 仲裁。若真实压测证明尾部少量慢 block 显著拉长整体完成时间，再作为独立优化加入。

## 6.7 Block timeout

固定 block 较小后，不需要复杂的“RTT/速度预测 + connect/header/idle/total/piece 五层 deadline”模型。首版：

- 使用 HTTP transport 自身的 connect/TLS/header timeout；
- 每个 block 一个简单总 timeout（初始可取 60s，benchmark 后微调）；
- timeout/短读/hash fail -> 当前 peer 简单指数 backoff，block 回到队列换源；
- task 继续受 v2 task attempt 的整体执行 timeout/lease 约束。

只有真实慢链路证明 60s 模型误杀正常传输时，再增加 no-progress watchdog 或速度自适应 timeout。

## 6.8 容量与带宽资源模型

### Disk capacity：Master 看状态，Node 做最终 admission

`storage.directory` 与 partial/temp 目录可能处于不同文件系统，因此 Node 至少采集：

```text
asset_fs:   available_bytes / total_bytes / valid
partial_fs: available_bytes / total_bytes / valid
same_filesystem: bool
sync_reservable_bytes   # Node 根据当前本地 reservations 计算的保守可接任务空间
```

不要让 Master 维护一套精确 distributed disk reservation；WS 状态有延迟，做了也不是强一致。正确职责：

- Master 根据最近 `node.status` 做粗筛，避免明显没空间的节点；
- Node 收到 `sync.task` 后在本地原子做最终 admission/reservation；空间不足就返回 `sync.rejected(reason=insufficient_space)`，Master 重新调度/等待；
- Node 本地 reservation 防止多个并发大任务同时看到同一份 free space；
- 同一文件系统只扣一次；partial 与 asset 跨盘时分别考虑剩余 partial 写入和最终 commit copy 的空间；
- filesystem stat 失败用 `valid=false`，不能把 unknown 当 0。

reservation 不要求真实 fallocate，只是本地 admission 账本；实际 ENOSPC 仍安全失败并释放 reservation。

### Sync bandwidth 必须 Node-global

当前 `sync.bandwidth_limit` 的 primary download limiter 是**每个任务独立**的，并发任务可能累计到 N 倍限制。Swarm 改造时：

- origin + peer inbound sync 共用一个 Node-global token bucket/budget；
- per-peer/per-task limit 只能作为 global limit 之下的子限制；
- `sync.bandwidth_limit` 语义正式定义为整个 Node 的同步入站上限。

### Public download 与 Swarm upload：不做复杂抢占调度器

首版采用两个简单约束：

1. 所有 mirror HTTP egress 继续经过 Node-global outbound limiter，保证总出口不超过目标；
2. Swarm peer upload 再额外经过一个 `swarm_upload_limit_bps` 子 limiter。public download 只受 global limiter。

因此在双方都饱和时，Swarm 最多占其子上限，public 天然保留其余出口；不需要实现可抢占 priority token bucket、动态 spare borrowing 等复杂算法。默认值在 benchmark/生产带宽策略中确定，可由管理员配置。

Node status 分开统计：

```text
public_active_downloads
swarm_active_uploads
sync_active_downloads
public_egress_bps
swarm_egress_bps
sync_ingress_bps
```

如果未来确实希望“public 空闲时 Swarm 吃满、public 一来瞬间让路”，再增加 residual-bandwidth borrowing。首版不做。

---

# 6.9 Node status 与 Inventory：事件驱动，避免周期性重活

## `node.status`

建议默认每 `15s` 发送最新状态，并在以下重要变化时立即 enqueue（coalesce 到最新）：

- sync slot 从 0 -> 可用或可用 -> 0；
- filesystem 可用空间跨过安全阈值/明显变化；
- public/swarm/sync 活动状态发生明显变化；
- active task attempt 集合改变；
- managed/routing 相关本地状态变化。

Master 收到 status 后：

- **立即只更新内存 runtime state**，用于调度/后台；
- `nodes.last_heartbeat_at` 等 DB 展示字段按约 `30s` 节流持久，或在 managed state/地址/配置能力发生变化时立即写；
- 不在每个 status handler 中跑 inventory cleanup、missing repair、项目 limit reconcile；这些由各自明确事件/scheduler 触发；
- transport liveness 主要由 WS Ping/Pong + session close 判断，`node.status` 是业务状态，不承担万能 ACK。

这样几十个/上百个 Node 的实时状态不会按 5~15s 周期制造同量级 SQLite transaction + reconciliation。

## Inventory

Inventory 继续使用完整 revision snapshot，而不是复杂 delta，但发送条件改为：

1. v2 session 建立/重连后的首次完整 snapshot；
2. Master 显式要求 inventory reconcile/force snapshot；
3. Node 检测到非正常 task-result 路径导致的本地库存变化/修复；
4. 低频 safety snapshot（初始默认约 `30min`，可配置/benchmark）。

正常 `asset_download/delete/inventory_reconcile` task 的结果本身已经即时更新 Master 对该资产的认知，不需要因此每分钟再扫整库。若短时间内多处本地变化要求 snapshot，使用 debounce/coalesce，只生成一次最新完整 snapshot。

---

# 7. Master 如何“尽快知道”节点与 Swarm 状态

目标不是提高 heartbeat 频率，而是**事件直推 + 可恢复 snapshot**。

Master 应快速获得：

- task accepted/result；
- node slots/capacity 变化；
- disk available 变化；
- manifest 生成；
- 新 verified pieces；
- asset complete；
- inventory 变化。

建议：

1. 关键状态变化立即 enqueue/wake writer；同 key 状态 coalesce 到最新；
2. `swarm.availability` 做 100~250ms 小窗口批量合并，每次发送最新完整 bitset；
3. task result / cancel / authorization 不等批处理窗口；`node.status` 周期 15s，但关键 slot/capacity/task-set 变化可立即推送；
4. Master runtime registry 收到 availability 后立刻更新 peer 候选；
5. source 集合变化时，只向正在下载该 asset 的 Node 推送最新完整 `swarm.sources`；
6. 重连直接重发 status/inventory/availability/source snapshot，不维护历史 delta；正常 inventory safety snapshot 降到低频，不再每分钟全量上报。

这比把 heartbeat 从 15s 改成 1s 更快，也更省控制流量。

---

# 8. 历史回归不变量清单（实施时逐项保留）

以下均来自**当前 HEAD 的祖先提交**，不是旁支猜测。

| Commit | 历史问题 / 已形成的不变量 | v2 要求 |
|---|---|---|
| `638edd1` | 控制连接串行读写会卡住/不畅 | 单 reader + 单 prioritized writer；不得恢复“函数自己读 socket 等 ACK” |
| `9f4b551` | 短 deadline 在 frame 中途导致流量事件上报异常 | WS message framing 取代自定义 partial frame；不可因 timeout 丢半条逻辑消息 |
| `11c4574` | Master 下发任务等待 ACK 时必须允许其他控制消息插队 | v2 dispatcher 天然支持全双工 interleave |
| `05320c8` | 会话报告、task result queue、heartbeat 边界 | task result 继续 durable，控制唤醒不依赖轮询周期 |
| `a1b5837` | 控制历史增长、重连风暴、过大任务 frame | 限 queue/history；重连 jitter；manifest/sources 同样有 frame hard limit |
| `df111ea` | frame 写失败后已 claim/sent task 要回收 | v2 改为 desired-state/lease 后仍要保证 write failure 不永久吞 task |
| `b987049` | Node 必须 ACK 前持久化本地 running task | 保留 |
| `36d9754` | ACK 必须真正覆盖对应发送对象，旧实现用 accepted sequence 防误确认 | v2 改为 `reply_to + domain identity(task/attempt/event/auth/revision)` 双重校验，不保留 global sequence |
| `10a3026` | reply correlation 错误不能确认本地事件 | 所有 ACK future 必须验证 `reply_to`/对象 ID |
| `c06a0dd` | task result 优先级不能被低价值消息饿死 | 已进入 priority registry：result=8 |
| `99ef8c4` | verified peer 失败后不能热循环重试 | peer score + exponential backoff |
| `9260a8a` | inventory reconcile 完成必须绑定真正完整库存闭环 | 保留 revision/snapshot complete |
| `26d73e7` | inventory revision 幂等边界必须持久化 | 保留 |
| `35d0880` | sync result 必须校验 task 与 asset 绑定 | manifest/task/result 都做 binding 校验 |
| `62b545a` | 过期/stale sync result 不得污染新任务 | result 带 task/generation/manifest identity |
| `e065d2c` | 替换失败必须保住旧资产 | Swarm commit 仍走 atomic/rollback-safe commit |
| `b9273df` | 同步任务必须有执行超时 | Swarm task 仍有总 deadline，另增 block/piece deadline |
| `2217907` | 下载响应不能无限超出预期大小 | 每 block/piece 和整文件都严格 size bound |
| `d3f848b` / `ef386a4` | 源地址 SSRF/解析内网限制 | origin fetch 继续保留；peer URL 必须来自 Master-authenticated node registry，不能信任任意 payload URL |
| `cc312dc` | peer replication token 使用必须收紧 | 新 swarm capability 仍绑定 source/target/asset/manifest/TTL |
| `c2f0387` | 公网探测不能被 DNS/内网解析绕过 | HTTP public probe 继续保留安全 DialContext |
| `5bb60af` | 文件验证后被改写时不能继续对外提供 | committed/partial serve 都要防 TOCTOU |
| `8e230d4` | 对外文件完整性不能因为缓存/路径简化而跳过 | full-file serving 继续完整性策略 |
| `8db5cf6` | 校验失败后必须触发库存纠正 | piece/full verify fail 均触发 availability/inventory correction |
| `99744cb` / `b335561` | HEAD 语义曾互相影响 | Swarm internal endpoint 与 public download endpoint 分离测试，不误改 public HEAD contract |
| `29aca1e` | disabled node 仍可保持控制连接和 heartbeat | routing disabled 与 control disconnected 分开 |
| `17ebe64` | 公网探测必须等 Node 准备好 challenge 响应 | v2 独立 challenge/ready 保留该握手 |
| `311e846` / `ac2ebd0` | 公网探测 timeout/retry 语义 | 保留，不因 WS 改造删除 |
| `81ce0fc` / `de158ec` / `eba8e43` | 容量为 0、过期 lease、wake 时序不能吞任务 | v2 capacity/desired reconciliation 必须覆盖 |
| `922b173` / `2ab8e17` | 重复消息不能重复副作用，但可刷新有效容量信息 | handler 分离 idempotent state refresh 与 side effect |
| `5ff3d96` | running task 续报不能饿死其他候选 | priority + coalesce + fairness |
| `11c3fe9` / `ade7300` | 重启时中断任务恢复/上报有明确边界 | Swarm partial 恢复不能把旧 running task 假装仍安全运行；重连需 reconcile |
| `3385235` | 历史上选择启动清空 tmp | v2 必须有意识地**替换**此不变量：普通垃圾仍清，受 DB 管理 swarm partial 不清 |
| `f4523be` / `7fad604` / `09cd8de` | SQLite 高频写入/历史膨胀曾是实际问题 | availability/progress 不能按 piece 高频落 Master SQLite；Node bitmap 批量提交 |
| `4ef94cc` | 当前证书重登记会调用 destructive local reset，连资产目录一起删除 | v2 将 identity/control reset 与 asset data reset 拆开；证书重登记默认保留 `local_assets` 与文件，只强制 inventory snapshot + public probe/reconcile，不因证书问题全量重哈希 TB 级缓存 |
| `cc312dc` / current replication tests | legacy replication token 是 one-time bearer，且本地 used-token map 无 TTL GC | swarm capability 使用独立 `swarm.v1` claims + 短 TTL + 有界并发；不复用 legacy one-time map，也不首版增加每-block request signature |

后续发现其他相关历史提交时追加到本表，不要只写在聊天里。

## 8.1 现有回归测试锚点

最终探索逐个核对了上表祖先提交：关键历史修复当时新增/修改的测试绝大多数仍存在于当前树。v2 实现不能简单删除这些测试；应优先改造成 transport-neutral contract test，或增加 v2 等价测试后再移除仅验证 raw framing 的部分。主要锚点：

| 约束 | 当前仍存在的主要测试 |
|---|---|
| 全双工插队 / response correlation | `internal/node/control/responses_test.go`, `internal/master/control/task_dispatch_interleaved_test.go`, `internal/node/control/task_batch_test.go` |
| frame/partial read 历史问题 | `internal/master/control/io_test.go`, `internal/protocol/framing_test.go`（v1 保留期继续跑；v2 新增 WS message limit/segment test） |
| task claim/write failure/lease | `internal/master/control/task_claim_test.go`, `task_dispatch_lease_sweep_test.go`, `sync_claim_test.go`, `server_read_wake_retry_test.go` |
| task durable local ACK/result/restart | `internal/node/control/task_batch_test.go`, `task_results_test.go`, `task_results_fresh_test.go`, `task_timeout_test.go` |
| stale/asset binding/result inventory | `sync_result_binding_test.go`, `sync_result_stale_test.go`, `sync_result_inventory_test.go`, `latest_publish_test.go` |
| retry/backoff/peer fallback | `sync_retry_test.go`, `sync_fallback_test.go`, `internal/node/syncer/fallback*_test.go` |
| inventory revision/reconcile | `inventory_revision_test.go`, `inventory_reconcile_test.go`, `inventory_publish_batch_test.go`, `reports*_test.go` |
| disabled node/control-vs-routing | `disabled_routing_test.go`, `pressure_bandwidth_test.go`, `internal/master/assignment/assignment_test.go` |
| public probe ready/retry/SSRF | `public_probe_ready_test.go`, `public_probe_retry_test.go`, `public_probe_loopback_test.go`, Node `public_probe*_test.go` |
| source URL SSRF | `internal/node/syncer/url_policy_test.go` |
| response size bounds | `internal/node/syncer/download_size_test.go` |
| committed file TOCTOU/hash verify | `internal/node/files/handler_verify_test.go` |
| replication auth/range/one-time legacy token | `internal/node/files/replication_test.go`, Master `sync_fallback_test.go` |
| public HEAD/Range/token behavior | `internal/node/files/handler_test.go`, `metadata_test.go`, `headers_test.go`, `limit_writer_test.go` |
| temp cleanup / atomic replacement | `internal/node/syncer/temp_test.go`, delete/commit related tests |
| SQLite schema/version/history growth | `internal/storage/schema_contract_test.go`, `sqlite_test.go`, `version_*_test.go` |

Phase A/B 开始时先建立一张“旧测试 -> v2 等价测试”的 checklist；只有在等价约束已经由 v2 测试承接后，才允许删除依赖 `net.Pipe`/raw frame 的 legacy-only 测试。

---

# 9. 实施结果地图

以下区域均已按最终实现逐项处理；`[x]` 表示已实现或由独立 v2/Swarm 新文件承接。唯一未勾选项是生产 v1 下线 gate。

## 9.1 Protocol

- [x] 新建 v2 protocol package/namespace：固定 `control.v2`，feature flags 只在 hello 中声明；保留 v1 legacy package 直到迁移结束。
- [x] v2 Envelope：仅 `v/type/id/reply_to/payload`；不继承 `node_id/request_id/sequence`。
- [x] 按 domain 拆 v2 payload：session/node/task/inventory/accounting/swarm，避免继续把所有 payload 堆进单文件。
- [x] 新增 priority registry（建议 `internal/protocol/priority.go`），**代码中的唯一 priority source of truth**。
- [x] `internal/protocol/framing.go`：仅供 v1 legacy；v2 不再使用 4-byte framing。
- [x] `node_control_sessions.last_message_sequence` 属于 v1；v2 session 不读取/更新该字段，legacy 删除阶段迁移/移除。
- [x] 为所有 message type 加 registry completeness test：新增类型未登记 priority 时测试直接失败。

## 9.2 Master control transport

- [x] `cmd/master/main.go` / control service startup：增加 WSS HTTP server/listener。
- [x] `internal/config/master.go`：新增 `control_ws_listen` / `enrollment_ws_listen`，legacy listener 迁移期保留。
- [x] `internal/master/control/server.go`：拆分 legacy v1 与 v2 WS session。
- [x] `server_read.go`：v2 删除 200ms socket polling；改 event-driven reader。
- [x] `server_flow.go`：handler 与 transport 解耦。
- [x] `server_ack.go`：不再滥用 `heartbeat_ack` 充当所有 ACK。
- [x] `task_dispatch_interactive.go`：v2 由 dispatcher/outbound queue 取代手工 interleaved read。
- [x] `download_authorizations.go`：授权进入 durable high-priority outbound，不再 send->blocking read ACK。
- [x] `runtime.go` / `runtime_wake.go`：v2 active session 采用可 wake 的 event-driven outbound；单 node 单 active session。`LastSequence` 不被 v2 使用，字段仅为 v1 rollout 兼容，最终随 v1 schema 删除。
- [x] 合并 `runtimeHeartbeat` / `runtimePressureReport` 为 v2 `runtimeNodeStatus`，避免同一状态写两份。
- [x] `node_control_sessions` 仍可保留“每节点最新连接元数据”供后台/跨模块查询，但 v2 不存 message sequence；active socket/session truth 只在 runtime。
- [x] 新增 bounded priority outbound queue + coalescing + 简单 burst fairness；观测先走 runtime/admin/log。
- [x] task scheduler 改为显式 wake source（任务生成、attempt terminal、slot/capacity 变化、session connect），不在任意消息后顺手全量扫 task。

## 9.3 Node control transport

- [x] `internal/config/node.go`：迁移期新增 `master.control_ws_address` / `master.enrollment_ws_address`；优先 v2，空值时允许 legacy v1 fallback。
- [x] `cmd/node/control_supervisor.go`：WS reconnect/backoff/jitter。
- [x] `internal/node/control/client.go`：TLS TCP client -> WSS client；reader/writer 分离。
- [x] `session_loop.go`：删除硬编码发送顺序和 10/200ms read polling，改 priority scheduler。
- [x] `responses.go`：删除“等 ACK 时顺便处理 sync_task/auth”的 read ownership hack；改 correlation dispatcher。
- [x] 重写 task message adapter：引入 `attempt_id`，ACK 前持久化；可视实现复杂度合并 Node 本地 task/result 重复状态。
- [x] `traffic.go`：保留 pending traffic durable queue。
- [x] `authorization_status.go`：保留 durable terminal status。
- [x] `download_authorizations.go`：保留先落 DB 后 ACK。
- [x] `inventory_reports.go`：保留 revision/完整 snapshot，改为 connect/force/dirty/低频 safety 触发；删除 1min 固定全量扫描。
- [x] `public_probe.go`：challenge 变独立 v2 message。
- [x] 新 `node.status` handler：runtime 立即更新、DB last_seen 节流持久；不得每条 status 执行 task/inventory reconciliation。
- [x] `enroll.go`：raw TLS framed enrollment -> WSS JSON client。

## 9.4 Disk capacity

- [x] 新增跨平台 filesystem capacity sampler，分别采集 asset filesystem 与 partial filesystem。
- [x] Heartbeat/Capacity 不再 `FreeBytes: 0`；改为 `available/total/valid + runtime reservations`。
- [x] 明确 unknown vs true-zero。
- [x] 管理后台分别显示 asset/partial filesystem capacity。
- [x] Master task assignment / project assignment / swarm admission 引入容量 reservation 与 safety reserve。
- [x] 跨文件系统 commit 按“partial 仍占空间 + asset 需要完整复制”计算 admission。
- [x] 测试 Linux sampler、目录不存在向最近父目录采样、unknown/error 语义、同盘/异盘 reservation、可用空间骤降、ENOSPC copy fallback；Windows sampler 通过交叉编译测试。

## 9.5 Master Swarm

- [x] 新增 manifest 持久表。
- [x] 新增 manifest validation / conflict handling。
- [x] 新增 in-memory swarm registry。
- [x] Master 内存保存 per-node/per-asset `availability revision + bitset`；不高频落 DB。
- [x] source snapshot 只推给实际需要该 asset 的 active task/session。
- [x] Master restart/Node reconnect 后由 Node 主动上报完整 availability snapshot；无需 gap-repair RPC。
- [x] `sync_fallback.go` 逐步退场；source selection 从“最多 3 个完整 peer”升级为完整+partial peer set。
- [x] replication capability signer 改为 swarm scoped capability。
- [x] manifest bootstrap 通过 v2 task `attempt_id/lease` 限制为每 asset 单 seed；无需独立 bootstrap lease 表。

## 9.6 Node Swarm / syncer

- [x] `syncer/executor.go`：从“primary whole file -> peer fallback”改为 manifest-aware download orchestration。
- [x] `source_probe.go`：origin 仍可 bootstrap/fallback，保留 SSRF 安全策略。
- [x] `fallback.go` / `fallback_parts.go`：不再作为目标调度核心；迁移/替换为 multi-source piece/block scheduler。
- [x] piece worker pool + Node-local peer throughput/failure backoff；不同 pieces 多源并行，同 piece 单 worker。
- [x] 固定 block size + 简单 per-block timeout/backoff；不首版做速度预测/no-progress/piece deadline。
- [x] adaptive piece layout implementation。
- [x] 下载 origin 时同时计算 whole hash + piece hashes，whole hash 成功后发布 manifest。
- [x] 每个 piece 的 blocks 到齐后验证 piece SHA-256。
- [x] 全 pieces 后整文件 SHA-256 + size 再验证。
- [x] `commit.go` 保留旧文件保护与 atomic replace。
- [x] partial state crash recovery。
- [x] obsolete/TTL partial GC；运行时磁盘压力由 atomic reservation/admission 阻止继续过量写入，不在首版引入并发 pressure-GC。
- [x] Node-global inbound sync limiter，统一约束 origin + peer + 并发 task。
- [x] restart 后 piece lazy revalidation：首次 serve/本地复用前重哈希，不做启动时全量扫描。

## 9.7 Node HTTP file service

- [x] 新增 `internal/node/files/swarm.go` 服务 verified partial pieces；legacy `replication.go` 保持 whole-file v1/whole-peer 兼容。
- [x] `download_verify.go`：区分 committed whole-file integrity 与 partial piece integrity。
- [x] 新 internal swarm block endpoint 或严格扩展 replication endpoint。
- [x] capability token 校验 source/target/asset/manifest/TTL。
- [x] `swarm.v1` 短 TTL Master-signed bearer capability；不复用 legacy one-time used-token map。
- [x] global outbound limiter + `swarm_upload_limit_bps` 子 limiter。
- [x] 读取 partial piece/block 时防止并发写/TOCTOU/holes。
- [x] 保留公共下载 endpoint 行为，不让 internal swarm 改动破坏 HEAD/Range/token 逻辑。

## 9.8 Storage

Master 预计只新增必要的 Swarm authoritative 数据：

```text
asset_piece_manifests   # authoritative manifest metadata + compact hash blob
# availability/source 为 runtime state，不单独高频持久化
# bootstrap 直接使用 task attempt/lease，不建 bootstrap lease 表
```

Node 最小新增：

```text
swarm_partials          # asset/manifest/path/verified bitmap/updated_at
# manifest 从 Master 获取；不建独立 swarm_manifest_cache
# task execution/result 是否合表由 v2 task 重构决定
```

同时更新：

- [x] migrations；
- [x] upgrade path；
- [x] schema contract tests；
- [x] version plan / version docs；
- [x] DB maintenance / GC。
- [ ] legacy v1 删除阶段清理确认无生产引用的 `node_heartbeats` / `node_pressure_reports` 与 `node_control_sessions.last_message_sequence` 等字段/表；迁移前用 grep + schema tests 双重确认。

## 9.9 Admin / observability：先复用现有后台与结构化日志

仓库当前没有 Prometheus/通用 metrics 子系统，本次不为 WS/Swarm 单独引入一套。首版后台至少显示：

```text
control_transport = v2 or v1
connected_at / last_seen
asset_fs / partial_fs available/total/reserved
public_active_downloads
swarm_active_uploads
cluster control_v1 / control_v2 / unknown counts
```

结构化日志继续使用现有 `node_id/session_id/task_id/asset_id/manifest_id/message_type` 等上下文。首版**不建设** per-task peer count、verified-piece progress、origin-vs-peer bytes histogram 或独立 Prometheus exporter；这些只有在真实运维/benchmark 需要时再从现有 runtime state 增量暴露，避免为了展示而引入第二套高频统计面。

---

# 10. 实施阶段与验收门槛

Swarm 是本项目目标的一部分，阶段拆分只用于降低开发/回归风险，不代表上线一个长期残缺版本。

## Phase A — 设计冻结 + 回归基线

- [x] 锁定 priority 数值方向：`1=最高，128=最低`。
- [x] 锁定 manifest 信任策略：首个 whole-verified manifest authoritative，冲突熔断。
- [x] 锁定 piece/block algorithm：BT-style 约 1024 pieces + 独立 HTTP block；具体常量允许 benchmark 微调。
- [x] 补齐当前关键通信 contract/regression tests，包括稳定 domain message ID、reply correlation、takeover、消息上限和 inventory segment 重放幂等。
- [x] 记录 `go test ./...` 基线：2026-09-11 当前 HEAD 全绿。
- [x] 确认现有配置迁移路径：Master 双栈 -> Node 逐台 WSS -> disable legacy -> 删除 v1。

验收：本文无未识别控制消息；历史不变量都有对应测试或明确人工验收项。

### v1 -> v2 配置与滚动升级顺序（已冻结）

迁移期不复用/改写现有 raw TLS listener 的协议语义，避免旧 Node 连接到新协议端口后进入重连风暴。

Master：

- 保留现有 `server.control_listen` / `server.enrollment_listen` 作为 legacy v1；
- 新增 `server.control_ws_listen` 与 `server.enrollment_ws_listen`；
- control WSS 要求 mTLS；enrollment WSS 仅服务端认证，保持独立 bootstrap 信任边界；
- endpoint 固定版本化：WSS `/control/v2` 与 WSS `/enroll/v2`；
- 复用现有 Master 服务端证书材料与 Node signing CA，不降低 TLS 1.3 约束。
- `control/v2` 默认必须由 mirror-master 自己终止 TLS 1.3 + mTLS，并从真实 TLS `PeerCertificates` 建立 Node session；不得因为 WSS 是 HTTP Upgrade 就信任 `X-Forwarded-Client-Cert`、`X-SSL-Client-Cert` 等普通代理头替代客户端证书认证；
- 因此 control WSS 不应放在会终止客户端 mTLS 的普通 CDN/反向代理后面。若未来需要代理模式，必须另做显式受信代理协议和来源约束；enrollment WSS 只要求服务端 TLS。

Node：

- 迁移期新增显式 `master.control_ws_address` / `master.enrollment_ws_address`；
- 新版 Node 若配置 WSS 地址则优先 v2；未配置时仍可暂时走现有 v1 地址，便于滚动升级；
- v2 稳定并全量后，删除 legacy fallback，再通过配置迁移收敛字段命名，不长期维护两套传输。

生产升级顺序：

1. 先升级 Master，开启 v1 + v2 双栈；
2. 逐台升级 Node，并逐台切换到 WSS，Master 后台记录 v1/v2 占比；
3. 全部 Node 连续稳定运行并通过公网探测/库存/授权/流量/Swarm 验收后，先将 legacy listener disabled by default；
4. 观察一个稳定窗口后删除 raw TLS v1 framing、listener 与兼容配置。

该顺序允许无全站停机迁移，也避免要求所有 Node 同时换版本。

## Phase B — v2 Protocol + WS transport skeleton

- [x] 新 v2 Envelope (`v/type/id/reply_to/payload`) + priority registry；删除 global sequence。
- [x] WSS control server/client。
- [x] mTLS 身份认证。
- [x] 单 node 单 active session / takeover + session-bound node identity。
- [x] single reader + prioritized writer。
- [x] bounded queue / coalescing / starvation protection。
- [x] reconnect backoff + jitter。
- [x] legacy v1 与 v2 可并存，但 v2 handler 不调用 v1 socket/sequence helper。

验收：反复 Master restart / Node restart / network flap 不出现重复 session、死锁、goroutine 泄漏、消息永久堵塞。

## Phase C — 现有 control.v1 业务完整迁移

- [x] `node.status`（合并 heartbeat/pressure/capacity/state），runtime-first + DB throttled persistence。
- [x] inventory revision/snapshot：connect/force/dirty/低频 safety，不再 1min 固定全量扫描。
- [x] `sync.task/accepted/rejected/result/cancel` + `attempt_id` task model。
- [x] traffic durable queue。
- [x] download authorization durable delivery。
- [x] authorization terminal status。
- [x] public probe challenge/ready；HTTP verify 保留。
- [x] enrollment 从 legacy raw framed TLS 改为 WSS JSON RPC。
- [x] disabled/routing semantics。

验收：v2 对现有功能的行为不低于 v1；历史回归测试全部通过。

## Phase D — Disk capacity 修复

- [x] 实际 filesystem capacity sampler。
- [x] unknown semantics。
- [x] capacity 上报和后台显示。
- [x] Master 粗筛 + Node-local atomic admission/reservation；不足返回明确 reject。

验收：Linux/Windows 都不再固定 0；容量不足不会接受新的大文件下载。

## Phase E — Manifest bootstrap

- [x] adaptive piece layout。
- [x] origin stream 同时 whole+piece hash。
- [x] verified whole file 后 manifest report。
- [x] Master manifest validation/persistence/conflict handling。
- [x] Master manifest persistence；Node 不建独立 manifest cache，必要时从完整文件重算。

验收：Master 不下载资产本体也能获得稳定 piece manifest；digest 改变会使旧 manifest 失效。

## Phase F — Persistent partial + partial serving

- [x] Node partial DB/bitmap。
- [x] crash/restart restore。
- [x] verified piece publication。
- [x] HTTP partial piece/block serving。
- [x] swarm scoped capability。
- [x] partial GC。

验收：Node 下载 30% 后重启，已 verified pieces 可恢复并继续被合法 peer 使用；未 verified bytes 绝不对外提供。

## Phase G — Multi-source Swarm scheduler

- [x] `swarm.sources` 完整 snapshot。
- [x] `swarm.availability` 完整 bitset snapshot。
- [x] 简单 rarest-first + 本地 throughput/backoff source selection；不同 pieces 并行，同 piece 单 worker。
- [x] 多 peer 同时下载。
- [x] origin bootstrap/fallback。
- [x] 固定 block size + 简单 block timeout/backoff。
- [x] bandwidth/concurrency integration。
- [x] full hash final commit。

验收：至少 3 个 Node 场景中，目标 Node 能同时从多个 source 拉不同 pieces/blocks；未完成 Node 的 verified pieces 会被其他节点使用；任意 peer 掉线不使整个 task 失败。

## Phase H — Observability + rollout + legacy removal

- [x] admin 显示 WS/Swarm 状态。
- [x] admin 诊断字段 + structured log context。
- [x] v1/v2 节点占比统计。
- [ ] 灰度节点。
- [ ] 全量 v2。
- [ ] legacy v1 listener disabled by default。
- [ ] 最终删除 raw TLS framing/control transport。
- [ ] 删除 v1-only sequence/session schema 与已无生产用途的 heartbeat/pressure legacy tables（确认无引用后）。
- [x] 更新 `节点通信协议.md` 为正式 v2 contract。

---

# 11. 故障注入 / 边界测试矩阵（实现后验收）

以下条目均已有直接单测、历史回归测试或真实 E2E 证据；括号内给出主要锚点。生产 rollout 本身不属于本矩阵，见 Phase H。

### WS / session

- [x] half-open connection：新 session takeover 不等待旧 peer close handshake，旧连接 `CloseNow()`；WS Ping 有 5s timeout（`TestControlV2NewSessionTakesOverSameNode`）。
- [x] Master restart：真实 3 Node E2E 中重启 Master，Node durable result replay 将原 `running` task 收敛到 `succeeded`，不重复下载。
- [x] Node restart：真实 E2E 多次重启 3 Node 后重新建立 mTLS WSS；partial restart 另有 `TestOpenSwarmPartialRestoresPersistedBitmapAfterProcessRestart`。
- [x] 两个相同 node_id 同时连接：后建立且 hello 合法的 session 原子接管旧 session（`TestControlV2NewSessionTakesOverSameNode`）。
- [x] old session 在 takeover 后继续发消息：每条业务消息处理前校验当前 registry connection；旧 socket 被 `CloseNow()`，transport takeover test 覆盖。
- [x] queue 满：durable 消息不得静默丢弃（`TestQueueFullDoesNotDropDurable`）。
- [x] 大量 coalescible status/availability 时高优先级消息仍优先且低优先级不永久饥饿（`TestQueuePriority`, `TestQueueBurstFairnessPreventsStarvation`, `TestQueueCoalescesLatest`）。
- [x] durable ACK 丢失后重连：Manifest outbox 可由新 Client 原样重报，Master crash E2E 验证 result replay（`TestV2PendingManifestReplaysAfterClientRebuild`）。
- [x] `reply_to` 错配 + 正确 `reply_to` 但业务 ID/attempt 错配：Master/Node `v2_correlation_test.go` 全套拒绝错误 correlation。
- [x] WS message 超限 / binary frame / malformed JSON：`TestReadV2EnvelopeRejectsBinaryMalformedAndOversize`。
- [x] 高频 `node.status` runtime-first、DB last_seen 节流且 runtime 生命周期隔离：`TestV2StatusPersistenceThrottleIsRuntimeScoped`, `TestV2StatusWakeOnlyOnUsefulCapacityOrSlotChange`。
- [x] inventory connect/force/低频 safety snapshot 收敛：Node 新 session 先领取新 revision；Master 持久 segment receipt，完全相同重放幂等、冲突重放拒绝（`TestV2InventoryRevisionIsReservedBeforeAck`, `TestV2InventorySegmentReceipt*`，并经真实 E2E 重连验证）。
- [x] unknown message：v2 没有“未知 optional”类别，未注册类型统一视为协议错误并拒绝（`TestEnvelopeRejectsUnknownMessageType`, registry completeness test）。

### Swarm

- [x] peer 只拥有部分 pieces / 多 peer 分别贡献：`TestV2SwarmUsesDifferentPeersForDifferentPieces`，partial serving 由 `TestSwarmPartialLazyRevalidationAndRevocation` 覆盖。
- [x] availability 完整 snapshot 丢失后自然收敛：后到完整 bitset 直接替换旧 revision，不要求 gap repair（`TestV2AvailabilityFullSnapshotMayResetRevision`, `TestAvailabilityWakeIsCoalesced`）。
- [x] peer 在 block 中途短读/断线后换 source：`TestV2SwarmSwitchesPeerAfterShortBlock`。
- [x] peer 返回短 body / 长 body / 错 `Content-Range`：`TestFetchSwarmRangeRejectsMalformedResponses`, `TestValidateSwarmContentRange`。
- [x] peer 数据导致 piece hash 错误：任务不能成功，也不能发布 verified local asset（`TestV2SwarmRejectsPieceHashMismatch`）。
- [x] manifest 与 authoritative whole digest/size 绑定错误：`TestV2ManifestRejectsAuthoritativeAssetBindingMismatch`。
- [x] manifest conflict：冻结 partial Swarm、清 availability、取消旧 attempt、切 whole-file retry（`TestV2ManifestConflictFreezesPartialSwarmAndRetriesWholeFile`）。
- [x] 文件源站无 Range：自动 whole-origin fallback 并独立重算 Manifest（`TestV2SwarmFallsBackToWholeOriginWhenOriginIgnoresRange`, `...CrossChecksManifest`）。
- [x] 所有 peer 失效后 origin fallback：`TestV2SwarmUsesOriginWhenAllPeersFail`。
- [x] origin 失效但 Swarm 已有完整 piece 集合：`TestV2SwarmCompletesFromPeerWhenOriginUnavailable`；真实 E2E 将 origin 改为 `https://1.1.1.1:1/unreachable` 后 Node 2/3 仍成功。
- [x] Node 重启恢复 partial：`TestOpenSwarmPartialRestoresPersistedBitmapAfterProcessRestart`。
- [x] partial 文件被外部改写：首次 serving lazy rehash 失败后撤销内存与 SQLite availability（`TestSwarmPartialLazyRevalidationAndRevocation`）。
- [x] committed file 被外部改写：whole-file serving 保留既有 hash/mtime 校验边界（`handler_verify_test.go`, `replication_verify_test.go`）。
- [x] asset digest/size 更新：scanner 将旧 inventory 置 stale 并删除旧 piece manifest/conflict（`TestScanMarksVerifiedInventoryStaleWhenSameGitHubAssetChanges`）。
- [x] task cancel 时已 verified piece 不被撤销：task execution 状态与 shareable piece state 解耦（`TestV2CancelKeepsVerifiedSwarmPiecesShareable`）。
- [x] 磁盘满 / ENOSPC：Node reservation 提前拒绝；copy fallback 注入真实 `syscall.ENOSPC` 时旧 target 保持原样（`TestReserveImpossible`, `TestMoveAssetFilePreservesTargetOnENOSPC`）。
- [x] 可用空间突然下降 / unknown：每次 reservation 重新采样，下降后拒绝新 reservation；unknown 最终 admission 拒绝（`TestReserveDownloadResamplesSuddenCapacityDrop`, `TestReserveDownloadRejectsUnknownFilesystemCapacity`）。
- [x] GC 与下载/serve：首版 GC 仅在 Node 启动、control/file service 启动前运行，因此架构上不存在运行态 GC/serve 并发；安全目录和 TTL 行为由 `swarm_gc_test.go` 覆盖。
- [x] manifest bootstrap 单 seed + seed failover：`TestV2ManifestBootstrapAllowsOnlyOneActiveSeedAndFailsOver`；stale attempt 由 attempt/correlation tests 拒绝。
- [x] bootstrap seed 只有 Manifest ACK 后才进入 terminal result；ACK 前断线可从 durable Manifest outbox 重报（`TestV2ManifestAckRequiresExactReplyTo`, `TestV2PendingManifestReplaysAfterClientRebuild`）。
- [x] manifest 8192-piece 上限、hash blob 长度和 1 MiB WS hard cap：`TestV2ManifestRejectsPieceCountAboveProtocolLimit`, manifest structure validation，transport oversize test；超 layout 资产走 whole-file。
- [x] block Range 必须完全位于一个 verified piece：`TestSwarmRejectsCapabilityAndRangeFailures`。
- [x] 多个 origin/peer sync task 共享 Node-global inbound limiter（`TestSyncLimiterIsSharedAcrossReadersAndTasks`）。
- [x] public download 与 Swarm upload 同时受 global egress / Swarm child limiter，内部字节不写 public accounting（`TestHandlerSwarmUsesGlobalAndChildLimiterWithoutPublicAccounting`）。
- [x] asset_fs / partial_fs 同盘和异盘 reservation；跨盘 commit 空间不足安全失败且旧资产保留（`TestReserveDownloadSameFS`, `TestReserveDownloadDifferentFilesystems`, ENOSPC replacement test）。
- [x] crash 后 bitmap 存在但 piece 内容损坏：restart 恢复 bitmap、`Trusted` 清空，首次 serve lazy revalidation 撤销坏 piece（restart + files Swarm tests）。
- [x] certificate re-enroll 默认保留实际资产、`local_assets` 与受管 partial，只清身份/控制状态（`TestResetNodeIdentityForReEnrollmentPreservesAssetsAndPartials`）。

### Accounting / public download

- [x] traffic event 不丢不重记：durable queue + dedupe/window tests；真实 Chrome E2E 的 Master `sent_bytes` 精确等于下载文件 2,319,424 bytes。
- [x] authorization ACK/status 重连恢复：local durable status + exact correlation tests，legacy regression tests继续覆盖状态版本变化。
- [x] public probe 仍真实经过 Node HTTP(S) endpoint：`public_probe*_test.go`，真实 E2E 三 Node 均达到 routing-ready。
- [x] public download HEAD/Range 行为无回归：`handler_test.go`, `metadata_test.go`, `limit_writer_test.go` 全量回归继续通过。
- [x] Swarm internal bytes 不误记为最终用户公共流量：handler test + peer-only E2E 验证 Node 1 internal transfer 不生成 public traffic event。

---

# 12. 已冻结设计事项（含过度实现审计）

## Q1. Priority 数字方向 — 已冻结

采用 `1=最高，128=最低`。`0` 不允许业务消息使用，保留给 transport/session 内部不可排队的 fast-path 语义。

## Q2. Manifest trust model — 已冻结

采用“第一台完成 authoritative whole SHA-256 校验的 mTLS Node 可立即发布 authoritative manifest”。后续完整节点交叉确认；manifest 冲突时立即冻结该 asset 的 partial Swarm，只允许 origin / fully-verified whole-file peer，并产生高优先级告警。当前不要求双节点 quorum。

## Q3. Piece sizing — 首版常量已冻结并实现

采用 BitTorrent-style `piece + block` 两层模型：piece 为 hash/HAVE 粒度，以约 1024 pieces 为目标并使用 2 的幂；HTTP Range block 独立更小。首版已实现 `min piece=1 MiB`、`auto max=64 MiB`、协议允许 `128 MiB`、`piece_count<=8192`、固定 `block=2 MiB`。未来 benchmark 只允许微调常量，不改变 wire/layout/故障语义。

## Q4. Enrollment transport — 最终冻结为 WSS JSON 短 RPC

Enrollment 不是长期 control session。v2 使用独立 server-auth TLS 的 WSS `/enroll/v2` 文本 JSON endpoint，保留 pairing/CSR/certificate 业务语义；建立的 WebSocket 仅服务单次短 RPC，不复用 control priority/session runtime。

## Q5. WS library — 已冻结

采用 `github.com/coder/websocket`。原因：当前仍活跃维护，原生 `context.Context`、client `Dial` 基于 `net/http.Client`、支持并发写、close handshake/ping-pong，且可通过 `net.Conn` wrapper 帮助现有测试与迁移。明确不采用已 deprecated 的 `golang.org/x/net/websocket`。默认禁用 permessage-deflate，控制消息体较小，不为压缩引入额外 CPU/状态复杂度。

## Q6. Bootstrap seed 在 whole hash 完成前是否允许 speculative sharing — 默认冻结为否

第一台 bootstrap Node 在 whole SHA-256 尚未与权威 digest 对上之前，只能计算并暂存 piece hashes，不能把这些 piece 宣告为 authoritative verified，也不能供其他节点作为 verified source。whole hash 成功后 manifest 一次性 authoritative，随后所有未完成 Node 都可以立即分享自己已验证的 pieces。

这样首轮每个 asset 至少需要一个完整 origin seed，但避免把“尚未被 whole digest 锚定的哈希”扩散到整个 swarm。若未来确实需要把第一次分发也做成 speculative streaming，应作为独立协议扩展，而不是混进 verified 语义。

## Q7. 可靠性抽象 — 已简化

不建设通用 Desired/Observed 或万能 durable message bus；v2 重构 task attempt/ACK 模型，domain 持久状态可重整，但 transport 只提供统一 queue/correlation/ACK callback，不复制多套 socket 逻辑。

## Q8. Swarm 状态传播 — 已简化

availability 发送完整 bitset，sources 发送完整 snapshot；不做 delta/gap-repair/resync RPC，也不向 Master 上报细粒度 peer score。

## Q9. Swarm capability — 已简化

采用短 TTL Master-signed bearer capability，不做每-block Node 私钥签名/nonce/replay cache。若未来威胁模型提高再升级。

## Q10. 性能优化 — 延后

首版不做动态 block size、endgame duplicate、通用 bulk segmentation、Prometheus metrics、config push、diagnostic RPC。它们都必须由真实 benchmark/运维需求触发，而不是预先实现。

## Q11. v2 wire 是否兼容 v1 内部字段 — 已冻结为否

v2 是新协议，不保留 global sequence、accepted_sequence、node_id、万能 heartbeat ACK。业务可靠性改用 domain identity + attempt/revision/event id，避免把 v1 的修补层带入新实现。

## Q12. Enrollment transport — 已冻结为短生命周期 WSS RPC

Enrollment 采用独立 **WSS 文本 JSON 短 RPC**，满足既定的 WSS 传输要求，但不建立长期 control session，也不引入 priority queue/reader-writer scheduler。领证前只有服务端 TLS 认证；证书批准并领取后关闭 enrollment WebSocket，随后用 mTLS 建立 `/control/v2`。公网 probe 仍是独立 HTTPS 数据面验证。

## Q13. Status / inventory 周期模型 — 已简化

`node.status` 高频只维护 runtime 最新状态，DB 展示字段节流持久；不再让 heartbeat 驱动任务/库存扫描。Inventory 由 session connect、显式 force、异常本地变化和低频 safety snapshot 驱动，取消 v1 的 1 分钟固定全量扫描。

## Q14. ACK 模型 — 已冻结

删除 global accepted sequence 和 ACK-of-ACK。需要最终确认的 durable 事件使用 `reply_to + domain identity` ACK；命令型消息的 accepted/authorization ack 丢失时，由发送方重发原命令，接收方幂等重回响应。

---

# 13. 当前进度记录

## 2026-09-11 — 第一轮代码/历史探索

已完成：

- [x] 确认项目根目录 `/opt/projects/mirror-server`。
- [x] 确认基线 HEAD `6ad225b266ca...`。
- [x] 确认当前控制面是真正的 raw mTLS TCP 长连接 + JSON framing，不是 HTTP polling。
- [x] 枚举当前协议 message types 和主要 handler/send path。
- [x] 阅读当前 Master/Node session loop、任务派发、授权、库存、流量、公网探测路径。
- [x] 阅读 peer fallback / range replication / token / download verify 路径。
- [x] 确认当前 peer fallback 是单 peer 多 Range，不是 Swarm。
- [x] 确认 Node 启动会清空 temp，和 partial recovery 冲突。
- [x] 定位 `free_bytes` 恒 0 的直接原因。
- [x] 确认 GitHub Release Master 不为 whole SHA-256 下载文件，digest/size 来自 GitHub API。
- [x] 检查当前祖先历史中控制、同步、文件校验相关的关键回归修复，并建立不变量表。
- [x] 确认历史 `e67817c` 脚本源提交不在当前 HEAD 祖先链，不能误当当前功能。
- [x] 形成 v2 WSS + full Swarm 初版设计。
- [x] 当前 HEAD `go test ./...` 基线全绿（2026-09-11）。

实施状态：

- [x] Q1：priority 使用 `1=最高，128=最低`。
- [x] Q2：首个 whole-verified mTLS Node manifest 可立即 authoritative，冲突熔断 partial Swarm。
- [x] Q3：采用 BitTorrent-style piece/block 两层模型，约 1024 pieces；具体常量 benchmark 后允许微调。
- [x] Q4 初版曾考虑 enrollment WSS；最终审计改为 WSS JSON RPC（见 Q4 最终结论）。
- [x] Q5：WS library 采用 `github.com/coder/websocket`，默认禁用 permessage-deflate。
- [x] 首版常量已冻结并实现：约 1024 pieces、1 MiB min / 64 MiB auto max、8192 piece hard cap、2 MiB block；后续仅 benchmark 微调。
- [x] 已完成关键“历史不变量 -> v2 回归测试”映射，并新增 correlation/takeover/inventory replay 等测试。
- [x] 锁定 WS library：`github.com/coder/websocket`。
- [x] 锁定 URL/config migration 细节并记录双栈升级顺序。
- [x] Phase A–G 已实现并完成本机真实多节点 E2E；Phase H 仅剩生产灰度后才能执行的 legacy 下线门槛。

---

## 2026-09-11 — 第二轮设计冻结

用户确认：

- [x] priority：`1=最高，128=最低`；
- [x] manifest：首个 whole-verified mTLS Node 可直接 authoritative，后续冲突熔断；
- [x] 分块：可直接采用成熟 BitTorrent 思路。经协议资料复核，设计细化为 BT-style `piece + block` 两层模型，而不是硬编码一张文件大小表；
- [x] enrollment 最终经 YAGNI 审计改为 WSS JSON 短 RPC；长期 control 才使用 WSS；
- [x] WebSocket 实现库：采用 `github.com/coder/websocket`；
- [x] bootstrap：whole hash 锚定 manifest 前不 speculative share；manifest 生效后允许所有 incomplete Node 分享 verified pieces。


## 2026-09-11 — 最终残余问题审计

本轮改为从现有代码/历史回归反向找遗漏，新增并冻结：

- [x] v1 allow-list 中 6 个“有常量无实现”的幽灵消息不迁移到 v2；
- [x] manifest 使用 compact hash blob，`piece_count <= 8192`，超出本版 Swarm 上限则回退 whole-file；
- [x] manifest bootstrap 复用 v2 task attempt/lease 做单 seed，不新增独立 bootstrap lease table；
- [x] legacy replication `target_node_id` 并未认证请求者；首版明确采用短 TTL bearer threat model，不伪称其提供 target 密码学证明；
- [x] `swarm.v1` 不使用 legacy one-time used-token map，因此不引入其无界 replay 集合；
- [x] capacity 区分 asset_fs / partial_fs，并加入 runtime reservation / cross-filesystem commit 空间模型；
- [x] `sync.bandwidth_limit` 改为 Node-global，不允许并发 task 各吃一份完整额度；
- [x] public download upload 高于 Swarm peer upload，分别统计连接数/带宽；
- [x] persistent partial restart 后采用 lazy piece revalidation，不在启动路径全量 hash；接收端仍按 manifest 再验证；
- [x] v2 证书重登记拆分 identity reset 与 destructive data reset，默认保留缓存与 local_assets，仅强制库存/公网探测恢复，不全量重 hash。
- [x] control WSS 默认由 mirror-master 直接终止 mTLS，不接受代理头伪造 client certificate；普通 TLS-terminating CDN/reverse proxy 不在控制链路支持范围。
- [x] 不建立通用 bulk segmentation；manifest 单帧受 1 MiB cap，inventory 保留领域自己的分页/segment。
- [x] 修正 piece/block 矛盾：peer 可提供 verified piece 内的 block 子 Range，不能跨越未验证 piece。
- [x] bootstrap seed 必须 manifest ACK 后才完成 task；断线后可从已落盘完整文件重算 manifest，不建 Node manifest cache。

截至本轮结束，没有剩余需要产品/架构拍板的问题。piece/block 的 `1 MiB / 64 MiB / ~1024` 常量仍按既定计划通过 benchmark 微调，但算法、wire contract 与故障语义不再依赖该 benchmark。

## 2026-09-11 — 最终“不过度、也不迁就旧实现”审计

第一次 YAGNI 收缩一度过度偏向“复用现有状态机”。再次检查当前 control 代码后修正原则：v1 已有大量 `global sequence + heartbeat_ack + blocking read/interleaved read` 耦合，v2 不应为了少改代码保留这些结构。

最终决定：

- **重写 wire/runtime 核心**：minimal Envelope、session-bound identity、single reader/writer、dispatcher、priority scheduler、reply_to correlation；
- **task 改成 attempt_id 模型**，用 domain identity 取代 global sequence 处理 stale ACK/result；
- 只保留历史业务不变量，不承诺沿用现有函数/表结构；Node task/result 表可视实现需要合并；
- enrollment 改成 WSS JSON RPC，而不是为了统一外观使用 WS；
- availability/source 用完整 snapshot；manifest 不做通用 segmentation；
- Swarm 同一 piece 单 worker，不做 same-piece 多 writer/endgame；
- block 首版固定大小，不做动态算法；
- capacity 由 Master 粗筛、Node 本地最终 reservation，避免 distributed reservation；
- outbound 使用 global limiter + swarm 子上限，不做抢占式 bandwidth scheduler；
- partial restart 采用 lazy piece revalidation，不启动全量 hash；
- short-TTL bearer capability，不做 per-block signature/nonce/replay cache；
- 不增加新 metrics/config/diagnostic 平台型功能。
- `node.status` runtime-first + DB throttled，inventory 从 1min polling 改为 event/force/connect + 低频 safety snapshot。
- 删除 ACK-of-ACK；durable event 才需要终态 ACK，command accepted 丢失靠原命令幂等重发恢复。
- 合并 runtime heartbeat/pressure 状态；v1 下线时清理死表/sequence 字段，不保留永久兼容垃圾。

该版本的目标不是“最少代码”，而是**最少长期机制数量**：每个新增机制都必须解决已知问题或完整 Swarm 所必需的问题。

## 2026-09-12 — 实现完成与真实 E2E 验收

已完成：

- [x] `control.v2` WSS + TLS 1.3/mTLS、单 reader / priority writer、bounded/coalescing/fairness、session takeover。
- [x] Enrollment `/enroll/v2` WSS 文本 JSON 短 RPC，真实 3 Node CSR -> 审批 -> 领证 -> mTLS control 验证通过。
- [x] domain-stable message ID + `reply_to`/业务身份双重 correlation；错误 task/auth/traffic/manifest/source ACK 不推进状态。
- [x] inventory 新 session 预留新 revision，Master 持久化 `(node_id, revision, segment)` receipt；同 payload 重放幂等、冲突 payload 拒绝。
- [x] Manifest bootstrap、persistent partial、verified partial serving、多源 scheduler、whole hash final commit、conflict 熔断。
- [x] Master 崩溃/重启后 Node durable result replay 自动把 `running` 收敛到 `succeeded`。
- [x] 真实 peer-only E2E：将 origin 改为 `https://1.1.1.1:1/unreachable` 后，Node 2/3 仍通过 Node 1 的公网 HTTPS Swarm endpoint 完成，同一 2,319,424-byte 文件三份 SHA-256 均为 `5942c9b0934e510ee61eb3e30273f1b3fe2590df93933a93d7c58b81d19c8ff5`。
- [x] Chrome 152 headless 真实打开项目下载页面，由页面完成 VDF challenge -> authorization -> Node download；下载字节数 2,319,424，与 Master accounting 完全一致，SHA-256 与权威摘要一致。
- [x] internal Swarm bytes 未进入 public download accounting。
- [x] E2E 暴露并修复 `/dev/null` 误判 TTY、pairing code 文件换行、Master SQLite 单连接嵌套查询自锁、half-open takeover 阻塞、inventory 稳定 ID 跨节点碰撞/重放幂等等问题。
- [x] 2026-09-12 节点间同步策略复验：真实 3 Node + 两个公网 HTTPS peer tunnel，origin 固定为 `https://1.1.1.1:1/unreachable`；Node 3 的 3 个 pieces 实际由 Node 1 提供 piece 0/2、Node 2 提供 piece 1，代理抓到 `/internal/swarm/e2e-asset` 的三个带 capability Range 请求；最终 SHA-256 与权威摘要一致，两个源节点 public accounting 均保持 0。
- [x] 该次复验暴露并修复两个运行问题：`indexnow.enabled=false` 时 typed-nil notifier 导致 Master scanner panic；重复 `sync.result.ack` 在首次 ACK 已确认后被错误视为 correlation failure 并触发 Node WSS 重连。
- [x] 完整 peer 调度补齐 piece-level 稳定轮转：不同 pieces 主动在多个等价完整 peer 间分摊，同一 piece 的 blocks 保持同一首选 peer，失败再按 source list fallback；新增 `TestV2SwarmSpreadsCompletePiecesAcrossPeers`。
- [x] 最终静止工作树门槛：`go test ./...`、`go vet ./...`、`go test ./internal/quality`、`git diff --check` 全部通过；关键 control/Swarm/file/syncer 包在 `CC=clang CGO_ENABLED=1 go test -race` 下无 race 报告。
- [x] Windows amd64 交叉构建通过：Master、Node 与 `internal/node/capacity` test binary 均可成功编译。
- [x] 最终最新版二进制 Chrome 152 全新 profile smoke：页面重新完成 VDF -> authorization -> Node download；文件大小 `2,319,424` bytes，SHA-256=`5942c9b0934e510ee61eb3e30273f1b3fe2590df93933a93d7c58b81d19c8ff5`，Master 最新 `download_history.sent_bytes=2,319,424`，与实际文件字节数精确一致。

仍属于**生产 rollout gate**、而非未实现功能：

- [ ] 生产环境逐台灰度 Node 到 v2 并观察稳定窗口；
- [ ] 所有生产 Node v2 稳定后，将 legacy v1 listener 默认关闭；
- [ ] 再经过稳定观察期后删除 raw TLS v1 transport 与 v1-only schema。

# 14. 文档维护规则

后续每次修改相关代码时：

1. 新增/删除 message type -> **先更新第 4 节优先级注册表**；
2. 改可靠性语义 -> 更新第 3、5 节；
3. 改 Swarm 数据结构/算法 -> 更新第 6 节；
4. 发现历史坑 -> 加入第 8 节并写 commit/test；
5. 新增受影响文件 -> 更新第 9 节；
6. 完成功能 -> 勾选第 10 节并记录对应测试；
7. 发现新未决问题 -> 加入第 12 节，不要只留在对话上下文；
8. 每一个实施阶段结束都必须执行实际测试并把结果/关键 commit 写进第 13 节。

本文的目标不是“看起来完整”，而是保证任何新对话只要先读本文和当前代码，就能继续推进而不重新猜设计背景。

---

# 15. 实施文件审计结果

这一节已在实现/最终回归期间逐项完成。`[x]` 表示文件已实际审阅；后缀说明它是直接修改，还是作为 legacy v1 保留并由新 v2 文件承接。未删除的 v1 文件不是遗漏，而是 Phase H 生产灰度期间的兼容面。

## 15.1 Master control/session

- [x] `internal/master/control/server.go` — changed
- [x] `internal/master/control/server_read.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/server_flow.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/server_ack.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/io.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/hello.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/session_reject.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/session_reason.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/session_sequence.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sessions.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/runtime.go` — changed
- [x] `internal/master/control/runtime_wake.go` — changed
- [x] `internal/master/control/runtime_inventory.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/runtime_pressure.go` — changed
- [x] `internal/master/control/runtime_reports.go` — changed
- [x] `internal/master/control/runtime_version.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`

## 15.2 Master task/inventory/reconciliation

- [x] `internal/master/control/sync.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_ack.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_claim.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_dispatch_ready.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_fallback.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_ready.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_result_binding.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_result_inventory.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_retry.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/sync_task_frame.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/task_dispatch.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/task_dispatch_capacity.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/task_dispatch_interactive.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_accept.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_cleanup.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_current.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_quarantine.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_reconcile.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_repair.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_revision.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/inventory_target.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/limit_reconcile.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/node_targets.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`

## 15.3 Master authorization/accounting/probe/enrollment

- [x] `internal/master/control/download_authorizations.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/authorization_status.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/traffic.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/traffic_cache.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/traffic_scope.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/traffic_unknown.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/enrollment.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/delivery.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/approval.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/public_probe.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/public_probe_http.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/public_probe_ready.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/public_probe_ready_service.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`
- [x] `internal/master/control/public_probe_store.go` — reviewed - no change required；v1 compatibility retained, control.v2 is covered by `v2_*.go`

## 15.4 Node control

- [x] `internal/node/control/client.go` — changed
- [x] `internal/node/control/run.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/session_loop.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/responses.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/tasks.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/task_results.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/task_result_queue.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/task_result_store.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/interrupted_tasks.go` — changed
- [x] `internal/node/control/task_limiter.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/inventory_reports.go` — changed
- [x] `internal/node/control/inventory_chunks.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/inventory_force_report.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/inventory_verify.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/pressure.go` — changed
- [x] `internal/node/control/traffic.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/download_authorizations.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/authorization_status.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/public_probe.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/public_probe_retry.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/enroll.go` — changed
- [x] `internal/node/control/identity.go` — reviewed - no change required；legacy v1 path retained, v2 is covered by `v2_*.go`
- [x] `internal/node/control/identity_enrollment.go` — changed
- [x] `internal/node/control/bandwidth.go` + platform-specific implementations（参考其跨平台拆分方式实现 disk capacity sampler） — reviewed/changed as a group；corresponding v2/config/storage implementation and tests completed

## 15.5 Syncer / file service

- [x] `internal/node/syncer/executor.go` — changed
- [x] `internal/node/syncer/executor_helpers.go` — reviewed - no change required；legacy whole-file behavior retained, Swarm covered by `v2_swarm_*.go` / `swarm_*.go`
- [x] `internal/node/syncer/source_probe.go` — reviewed - no change required；legacy whole-file behavior retained, Swarm covered by `v2_swarm_*.go` / `swarm_*.go`
- [x] `internal/node/syncer/url_policy.go` — changed
- [x] `internal/node/syncer/fallback.go` — reviewed - no change required；legacy whole-file behavior retained, Swarm covered by `v2_swarm_*.go` / `swarm_*.go`
- [x] `internal/node/syncer/fallback_parts.go` — changed
- [x] `internal/node/syncer/fallback_limits.go` — reviewed - no change required；legacy whole-file behavior retained, Swarm covered by `v2_swarm_*.go` / `swarm_*.go`
- [x] `internal/node/syncer/rate_limit.go` — changed
- [x] `internal/node/syncer/asset_records.go` — reviewed - no change required；legacy whole-file behavior retained, Swarm covered by `v2_swarm_*.go` / `swarm_*.go`
- [x] `internal/node/syncer/commit.go` — changed
- [x] `internal/node/syncer/temp_cleanup.go` — changed
- [x] `internal/node/syncer/directory_cleanup.go` — reviewed - no change required；legacy whole-file behavior retained, Swarm covered by `v2_swarm_*.go` / `swarm_*.go`
- [x] `internal/node/syncer/inventory_reconcile.go` — reviewed - no change required；legacy whole-file behavior retained, Swarm covered by `v2_swarm_*.go` / `swarm_*.go`
- [x] `internal/node/files/replication.go` — reviewed - no change required；public/legacy endpoint contract retained, partial serving covered by `swarm.go`
- [x] `internal/node/files/download_verify.go` — reviewed - no change required；public/legacy endpoint contract retained, partial serving covered by `swarm.go`
- [x] `internal/node/files/asset_request.go` — reviewed - no change required；public/legacy endpoint contract retained, partial serving covered by `swarm.go`
- [x] `internal/node/files/handler.go` — changed
- [x] `internal/node/files/concurrency.go` — reviewed - no change required；public/legacy endpoint contract retained, partial serving covered by `swarm.go`
- [x] `internal/node/files/rate_limit.go` — reviewed - no change required；public/legacy endpoint contract retained, partial serving covered by `swarm.go`
- [x] `internal/node/files/limit_writer.go` — reviewed - no change required；public/legacy endpoint contract retained, partial serving covered by `swarm.go`
- [x] `internal/node/files/traffic.go` — changed
- [x] `internal/node/files/authorization_state.go` — changed
- [x] `internal/node/files/authorization_token.go` — reviewed - no change required；public/legacy endpoint contract retained, partial serving covered by `swarm.go`

## 15.6 Protocol / TLS / startup / config / storage

- [x] `internal/protocol/version.go` — reviewed - no change required；control.v1 contract retained, v2 covered by `internal/protocol/v2/`
- [x] `internal/protocol/envelope.go` — reviewed - no change required；control.v1 contract retained, v2 covered by `internal/protocol/v2/`
- [x] `internal/protocol/framing.go` — reviewed - no change required；control.v1 contract retained, v2 covered by `internal/protocol/v2/`
- [x] `internal/protocol/payloads.go` — reviewed - no change required；control.v1 contract retained, v2 covered by `internal/protocol/v2/`
- [x] `internal/controltls/` 全部生产文件 — reviewed/changed as a group；corresponding v2/config/storage implementation and tests completed
- [x] `internal/downloadtoken/` 全部生产文件（Swarm capability 是否复用或拆独立 signer） — reviewed/changed as a group；corresponding v2/config/storage implementation and tests completed
- [x] `cmd/master/main.go` — changed
- [x] `cmd/master/server_runtime.go` — changed
- [x] `cmd/master/heartbeat_sweep.go` — reviewed - no change required；shared behavior remains valid for v1/v2
- [x] `cmd/node/main.go` — changed
- [x] `cmd/node/control_supervisor.go` — changed
- [x] `cmd/node/file_service.go` — changed
- [x] `internal/config/master.go` — changed
- [x] `internal/config/node.go` — changed
- [x] master/node migrations + upgrade code + schema contract/version plan — changed；Master v21 / Node v6 migration、upgrade、schema contract 与版本文档已同步

审计结论：所有上列 legacy/shared 文件均已逐项复核；需要改动的已修改，需要继续服务 v1 的保持原合同，v2 新实现集中在独立 `v2_*` / `swarm*` 文件。真正尚未执行的删除动作只有 Phase H 的生产 rollout gate。

## 2026-09-12 — GitHub 更新链路与弱网/断续真实演练

- [x] 使用真实公开仓库 `sharkdp/bat` 模拟版本更新：先将 3 个节点固定在 `v0.26.0`，随后由 Master 自己通过 GitHub Release 扫描发现 `v0.26.1`。最新一次干净扫描 `state=succeeded`，`selected_releases=1`、`accepted_assets=1`、`rejected_assets=20`；新资产 GitHub asset id 为 `323549181`，大小 `3,546,574` bytes，SHA-256 为 `726f04c8f576a7fd18b7634f1bbf2f915c43494c1c0f013baa3287edb0d5a2a3`，`source_url` 由扫描恢复为 GitHub 官方下载 URL。
- [x] 正常网络滚动更新流程实测：3 个节点均自动生成/领取新版本下载任务；每个节点都是先 `v0.26.1 verified`，随后旧 `v0.26.0` 删除任务才完成。最终 old inventory=`removed`、new inventory=`verified`，三份新文件 SHA-256 全部一致；无非终态任务残留。
- [x] Master↔Node 弱网/断续：Node 2 peer-only 下载过程中，control.v2 被强制断开并连续拒绝重连，客户端按 1/2/4/8/16s backoff；数据面继续完成，断线期间进入 `waiting_manifest` 并把 Manifest + terminal result 持久化到 `pending_swarm_manifests`。WSS 恢复后 durable replay 自动完成 ACK/outbox 清理。因为 reconnect inventory 已先证明资产 verified，人工制造的冗余下载任务在 Master 侧安全收敛为 `cancelled: 库存已验证，无需重新下载`，随后相同 result 被幂等 ACK。
- [x] Master↔Node 反复短断续：Node 1 在同一 `attempt_id` 的慢速 peer-only 下载中连续两次切断 WSS；每次均自行重连（第一次 1s backoff、第二次 2s），未生成新 attempt，最终 task=`succeeded`。
- [x] Node↔Node 弱网：peer HTTP relay 使用约 200ms 延迟 + 64 KiB/s 限速，Swarm 在多 peer 间正常分 piece。
- [x] Node↔Node 半包/断流：测试 relay 对 Node 1 的 piece 0/2 故意只发送 131,072 bytes 后断 TCP；约 2 秒后 Node 2 收到相同 piece 的 Range 重试请求，证明 block 失败后发生 source failover。Node 3 最终 whole SHA-256 正确，`swarm_partials` 清零，task=`succeeded`。
- [x] 最终一致性检查：Master/3 Node `PRAGMA integrity_check=ok`，`foreign_key_check` 为空；每节点只有一个 active control session；3 个节点的 Manifest outbox、未上报 result outbox、public traffic event 均为 0；内部 Swarm 流量未计入 public traffic。
- [x] 当前演练未观察到新的 panic、protocol error、SQLite constraint/database-health 错误。测试环境中出现的端口占用、旧 chaos 状态和人工 `source_url=unreachable` 均来自测试编排残留，清场/重新扫描后已明确排除为产品问题。
