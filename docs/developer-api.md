# Developer Sync API

Developer Sync API 供镜像项目所有者在发布 GitHub Release 后主动请求枫源镜像立即执行一次该项目的 Release 更新检测。

## Endpoint

~~~
POST /api/developer/v1/projects/{project_id}/sync
Authorization: Bearer <project_token>
~~~

Token 与单个 project_id 绑定。项目所有者不能使用自己的 Token 触发其他项目，也不能借此访问管理后台或节点控制接口。

完整 Token 只会在管理后台首次生成或重置时返回一次。主节点数据库只保存 SHA-256 哈希和可识别前缀，不保存 Token 明文。重置 Token 后旧 Token 立即失效，但当天已经消耗的调用额度不会重置。

## 调用示例

~~~bash
curl -X POST   -H "Authorization: Bearer mmdev_xxx"   "https://mirror.example/api/developer/v1/projects/example/sync"
~~~

正常接受请求时返回 202 Accepted。扫描异步执行，HTTP 请求不会等待 GitHub Release 扫描和后续节点同步完成。

如果同一个项目已经有扫描正在执行，请求仍返回 202 Accepted，但 data.already_running 为 true，并且不会再次消耗每日额度。

## 每日额度

默认每个项目每天允许 100 次有效触发，按主节点配置的统计时区在午夜重置。

响应包含：

~~~
X-RateLimit-Limit
X-RateLimit-Remaining
X-RateLimit-Reset
~~~

额度耗尽时返回 429 Too Many Requests，并额外返回 Retry-After。

## 常见状态码

- 202：请求已接受，或该项目已有扫描正在执行。
- 401：Bearer Token 缺失或无效。
- 403：Token 与 URL 中的项目不匹配。
- 404：项目不存在。
- 409：项目当前已禁用。
- 429：每日额度已耗尽，或短时间内认证失败次数过多。

管理后台的项目卡片可以查看 API 地址、Token 前缀、今日使用量和最近调用时间，并可随时重置 Token。
