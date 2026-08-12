# mirror-server

`mirror-server` 是一个由**主节点**和**下载节点**组成的镜像分发服务，面向 Release / 资产镜像场景。

主节点负责公共页面、公开 API、验证与授权、额度控制、管理面板、节点控制通道，以及 Release 同步。
下载节点负责本地资产校验、文件服务、Range 下载、同步执行和真实流量补报。

## 特性

- 公共下载页面与公开 API 分离
- 网页挑战与 API PoW 授权
- 短时下载令牌与节点绑定
- 下载节点 mTLS 控制面与首次登记
- SQLite 持久化存储
- 管理面板支持项目、节点、同步、封禁与审计操作
- 前端资源内嵌到二进制中，便于独立部署

## 目录

- `cmd/master`：主节点入口
- `cmd/node`：下载节点入口
- `configs/`：示例配置文件
- `configs/sponsor.example.json`：公开关于页赞助者列表示例；部署时实际文件仍放在主节点可执行文件同级目录并命名为 `sponsor.json`
- `configs/changelog.example/`：更新日志目录结构与单事件 YAML 示例
- `docs/`：设计、部署、API 与管理面板文档
- `internal/`：核心业务实现
- `web/`：嵌入式前端资源
- `scripts/`：构建与检查脚本

## 运行前提

- Go 1.26.0（以 `go.mod` 为准）
- 一个可写的工作目录，用于存放配置、SQLite 数据库、证书和日志
- 如需重新编译网页 PoW 的 WASM 资源，系统中可选安装 `clang` 或 `zig`

## 快速开始

### 1. 准备配置

仓库提供了示例配置文件：

- `configs/config.example.yaml`
- `configs/projects.example.yaml`
- `configs/quota.example.yaml`
- `configs/notices.example.yaml`
- `configs/filters.example.yaml`
- `configs/node.example.yaml`
- `configs/sponsor.example.json`

建议先把这些示例复制到自己的运行目录，再按实际环境修改。主节点与下载节点的密钥、证书和数据库路径也建议放到独立的 `secrets/` 和 `data/` 目录中。

### 2. 启动主节点

主节点启动命令默认读取五个 YAML 配置文件：

```bash
./mirror-master -config config.yaml -projects projects.yaml -quota quota.yaml \
  -notices notices.yaml -filters filters.yaml -changelog changelog
```

如果配置文件缺失，程序会先生成中文示例配置并安全退出。首次运行时，如果检测到所需的安全材料缺失，还会进入首次初始化流程，生成必要的示例配置和密钥材料后再退出，等待你确认后重新启动。

主节点默认提供：

- 公共页面与公开 API
- 管理面板
- 节点控制与首次登记入口
- Release 扫描与同步调度

### 3. 启动下载节点

下载节点启动命令：

```bash
./mirror-node
```

下载节点首次运行时，如果尚未登记，会进入交互式初始化，询问节点名、主节点控制/登记地址、TLS 服务端名称和一次性配对码。
完成登记后，节点会启动文件服务和健康检查，并通过控制通道接收同步与管理指令。

## 构建

仓库提供了两个常用构建脚本：

- `scripts/build-linux-amd64.sh`
- `scripts/build-windows.sh`

它们会：

1. 运行测试
2. 尝试重新编译网页 PoW 的 WASM 文件
3. 构建主节点和下载节点二进制
4. 将 `configs/` 复制到 `dist/` 目录，并把 `configs/sponsor.example.json` 复制为发布目录根部的 `sponsor.json`

例如：

```bash
./scripts/build-linux-amd64.sh
```

构建产物会输出到：

- `dist/linux-amd64/`
- `dist/windows-amd64/`

## 配置说明

### 主节点

主节点配置文件默认是 `config.yaml`，启动时还会读取：

- `projects.yaml`：项目镜像清单
- `quota.yaml`：额度、黑名单与白名单配置
- `notices.yaml`：公共页面公告
- `filters.yaml`：首页筛选器与目录查询缓存上限
- `changelog/YYYY-MM/*.yaml`：部署机维护的公开更新记录，修改后自动热重载

主配置中的 `vdf_size_tiers` 运行期间每 2 秒自动热重载；非法修改会沿用上一份有效配置。

主节点配置涵盖：

- 公共监听地址
- 管理监听地址
- 控制与登记 TLS
- SQLite 数据库
- 请求 ID 透传
- Release 扫描
- 下载令牌签发与验证
- 管理面板登录与会话

### 下载节点

下载节点配置文件默认是 `node.yaml`，主要包含：

- 节点名
- 文件服务监听地址
- 主节点控制/登记地址
- 本地资产目录与状态库
- 状态库中的节点身份、登记状态和下载令牌验证公钥
- 反向代理信任网段
- 旧版节点 TLS 材料导入路径

## 文档

如果你想进一步了解项目设计，建议按下面顺序阅读：

- `docs/配置与部署.md`
- `docs/公开API.md`
- `docs/管理面板.md`
- `docs/节点通信协议.md`
- `docs/数据模型与事务.md`
- `docs/更新日志维护.md`

## 开发

常用命令：

```bash
go test ./...
go build ./cmd/master
go build ./cmd/node
```

如果你想快速验证修改是否影响构建，优先跑完整测试：

```bash
go test ./...
```

## 许可证

本项目采用 [GNU Affero General Public License v3.0（AGPL-3.0-only）](LICENSE)。