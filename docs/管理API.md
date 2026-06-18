# 管理 API

> 状态：已废止
> 现行入口：必须使用管理 Web 面板 `/admin/`
> 内部桥接：浏览器登录后仅调用同源 `/admin/api/*`

## 1. 现行管理边界

主节点不再对外单独提供裸 `/api/admin/v1/*` 管理接口。管理监听器只服务 Web 管理面板：

- `/` 和 `/admin` 跳转到 `/admin/`。
- 未登录用户进入 `/admin/login`。
- 已登录浏览器通过同源 `/admin/api/*` 完成管理操作。
- 公共监听不得挂载 `/admin/*` 或任何管理桥接接口。

历史 `/api/admin/v1/*` 路由不得作为部署、文档或脚本调用入口。需要管理节点、项目、扫描、统计、安全封禁或审计摘要时，必须通过 Web 面板完成。

## 2. 认证与高风险操作

管理面板使用账号密码登录、服务端会话、防爆破封禁和请求 ID 关联。高风险操作统一由有效网页登录会话授权，包括：

- 配对码创建、登记审批和拒绝。
- 节点禁用、启用、删除、同步重置和同步重试。
- 项目配置保存、项目数据重置和手动扫描触发。
- 管理登录封禁或公开下载封禁的新增、解除和批量解除。

部署配置不再包含裸管理 API 的 `admin.allowed_cidrs`、`admin.token_env`、`admin.token_file`、`admin.token_min_bytes`、`admin.high_risk_require_mtls` 或 `admin.tls.client_ca_file`。管理面板 HTTPS 只需要服务端证书：

```yaml
admin:
  web:
    https_enabled: true
    users_file: "secrets/admin-users.yaml"
    bootstrap_password_env: "MIRROR_ADMIN_WEB_PASSWORD"
    session_secret_file: "secrets/admin-web-session.key"
    session_ttl: "12h"
    login_failure_window: "24h"
    login_failure_limit: 3
    login_ban_duration: "168h"
  tls:
    cert_file: "secrets/admin-web.crt"
    key_file: "secrets/admin-web.key"
```

nginx 已终止公网 HTTPS 且主节点只绑定回环或受控内网时，可把 `admin.web.https_enabled` 设为 `false`，但公网侧仍必须使用 HTTPS。

## 3. 安全与脱敏

管理面板不得展示或记录：

- 密码、密码哈希、会话 Cookie、会话密钥。
- 下载令牌、下载令牌私钥、公共下载授权令牌。
- 节点私钥、证书私钥、完整 CSR 或证书 PEM。
- 下载节点本地绝对路径。

安全封禁列表允许展示封禁来源 IP 或客户端前缀；日志和审计摘要仍不得记录完整 IP。

状态修改、高风险查看和人工恢复操作必须写入审计摘要。审计只记录资源 ID、请求 ID、操作结果和非敏感中文原因。
