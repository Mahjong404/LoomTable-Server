# Personal 部署与运维

## 目标

Personal 模式将 LoomTable Server 部署在用户本机或用户控制的远程服务器上。Plugin 只连接和检查服务，不安装、启动或升级 Docker。

## 第一阶段服务

```text
loomtable-server
postgres
managed-attachment-volume
```

第一阶段不要求 Redis、Kafka、RabbitMQ 或独立对象存储服务。Managed Attachment 使用 Docker 挂载文件卷。
P0 不启用 Attachment capability，但保留文件卷和备份清单位置，便于后续启用时不改变部署拓扑。

## 连接流程

1. 用户启动 Docker Compose。
2. 运维者显式执行 Server Migration 命令。
3. 首次部署时，运维者显式执行 Bootstrap/管理命令，创建稳定 Actor 和随机初始 Token；终端只显示一次 Secret，数据库只保存哈希。
4. Plugin 打开连接向导。
5. 用户选择本地或远程 URL。
6. 输入或粘贴 Token。
7. Plugin 请求健康检查和 Server Meta。
8. Plugin 检查 API 版本和最低兼容版本。
9. Plugin 检查数据库迁移状态。
10. 用户选择 Workspace。

`/v1/meta.bootstrapState` 是公开的 `required | complete | unknown`：无 Personal Actor 时为 `required`，已有 Actor 时为 `complete`，数据库状态无法判定时为 `unknown`。它不暴露 Token 数量；即使最后一个 Token 已撤销，Actor 已存在时仍为 `complete`。`/readyz` 不因未 Bootstrap 而失败。

## 配置原则

- 原生 Server 默认只监听 `127.0.0.1:31201`。Compose 的 Server 容器监听 `:31201`，宿主机仅发布 `127.0.0.1:31201:31201`；局域网或远程访问必须显式覆盖监听地址，并使用 TLS 反向代理或可信内网通道。
- PostgreSQL 凭据只存在 Server 环境中。
- Plugin 只保存 Server URL 和访问 Token。
- PostgreSQL 中未撤销的 Token 哈希是运行时认证的唯一事实来源；明文 Token 不落库。
- Token Secret 只由管理命令生成；最终 P0 不接受调用者通过参数、stdin 或环境变量指定 Secret。普通 Server 启动不读取环境变量作为长期认证旁路，也不隐式创建或轮换 Token。
- P0 不提供公开 Token 生成 API，localhost 也不免认证。
- Personal Server 使用一个持久化的稳定 Actor；同一 Actor 可以拥有多个具名 Active Token。每个 Token 可独立撤销，P0 不设置到期时间。Token 更换、Server 重启和数据库恢复都不改变 Actor ID；P0 默认授予其所有 Workspace 权限，不实现登录、邀请或角色系统。
- PostgreSQL 端口不应直接暴露到公网。
- Attachment 文件卷（即使当前 capability 未启用，也作为预留卷）必须持久化。
- Secret 不写入日志。
- 生产远程部署应使用 HTTPS 或可信内网通道。

## Actor 和 Token 初始化

- `bootstrap --name` 必须是显式、可重复检查但不会重复创建身份的操作：只在尚未初始化时创建 Personal Actor 和首个 Token；已有 Actor 时报告现有状态，不新增 Token，也不显示任何 Secret。
- 每个 Token 都有稳定的 `tok_...` ID 和必填名称；同一 Actor 可以同时保有多个未撤销 Token，便于按设备或用途独立轮换。
- Server 只在创建 Token 时向终端显示一次明文；后续只能校验、列出 ID、名称、创建/撤销状态等元数据或撤销哈希记录，不能还原明文。
- P0 Token 没有自动到期时间；撤销是终止其访问能力的唯一 P0 生命周期动作。
- Token 管理是本机管理命令，不通过公开业务 REST API 暴露。
- 管理入口固定为 `loomtable-admin auth bootstrap/create/list/revoke`。默认 Secret 为 `ltp_` 加 32 字节 CSPRNG Base64URL；数据库保存 SHA-256，`tok_...` 仅作为 Token 元数据 ID。
- Token 名称执行 Unicode Trim、NFC、控制字符拒绝和 100 码点上限；同一 Actor 的 Active Token 名称按 locale-neutral Unicode Default Case Folding 后唯一。`create --name` 只生成随机 Secret，`list` 只显示元数据，`revoke` 接收 `tok_...` ID，并允许撤销最后一个 Token。
- Actor、Token 哈希及撤销状态随 PostgreSQL 一起备份和恢复。

Compose 管理命令示例：

```text
docker compose --profile ops run --rm admin auth bootstrap --name "Primary device"
docker compose --profile ops run --rm admin auth create --name "Laptop"
docker compose --profile ops run --rm admin auth list
docker compose --profile ops run --rm admin auth revoke --token-id tok_...
```

## 健康检查

Server 至少提供：

- `/healthz`：无需认证的进程存活检查。
- `/readyz`：无需认证的数据库、迁移和必要存储就绪检查。
- 有待执行 Migration 时，Server 保持存活但 `/readyz` 返回 `503 MIGRATION_REQUIRED`。
- 存活检查。
- 数据库连接检查。
- 迁移状态。
- API 版本。
- Server 版本。
- 能力列表。
- Attachment capability 启用时的存储可写性检查；未启用时不因预留卷不可写阻塞 P0 ready。

## 备份

完整备份必须同时包含：

- PostgreSQL 数据。
- Managed Attachment 文件卷。
- Server 版本。
- Schema 版本。
- 备份生成时间。
- 恢复所需的元数据。

只备份 PostgreSQL 不算完整 LoomTable 备份。

P0 提供 PowerShell 与 Bash Docker Compose 脚本，不通过业务 REST API 暴露备份接口。脚本生成一个跨平台 `.tar.gz` 版本化归档，包含 PostgreSQL custom-format dump、Managed Attachment Volume、Server/Schema 版本、生成时间、文件清单及 SHA-256 校验和；拒绝覆盖已有输出。独立验证命令必须能在恢复前发现损坏或不兼容归档；P0 合并前还必须在真实 Docker/PostgreSQL 环境完成脚本验收。

归档默认不加密，但创建时尽可能使用仅当前用户可读的权限，并明确提示数据库包含 Actor、Token Hash 和业务数据。Cursor HMAC Key 随数据库转储备份，不在 Manifest 中输出明文。

仓库提供以下等价入口：

```text
scripts/operations/backup.ps1 OUTPUT.tar.gz
scripts/operations/validate-backup.ps1 OUTPUT.tar.gz
scripts/operations/restore.ps1 OUTPUT.tar.gz -Confirm

scripts/operations/backup.sh OUTPUT.tar.gz
scripts/operations/validate-backup.sh OUTPUT.tar.gz
scripts/operations/restore.sh OUTPUT.tar.gz --confirm
```

备份拒绝覆盖既有输出。验证会检查归档格式、必需成员与 SHA-256；恢复会先验证归档并拒绝 Server 仍在运行的环境。

`acceptance.ps1` / `acceptance.sh` 使用随机 Compose Project、临时端口和临时数据卷，自动执行 Migration、Bootstrap、写入测试 Workspace、备份、销毁测试卷、恢复和数据核对；无论成功失败都会清理隔离资源。它只用于验收环境，不连接或复用生产 Compose Project。

## 恢复

恢复要求 Server 停止、目标为空并由运维者显式确认；只有传入 `--confirm` 才能覆盖非空目标。归档必须先在隔离目录验证：

1. 启动指定版本的 PostgreSQL。
2. 恢复数据库。
3. 恢复 Managed Attachment 文件卷。
4. 检查对象数量和附件引用。
5. 执行必要的迁移。
6. 运行健康检查。
7. 再允许 Plugin 连接。

## 升级

- Server 升级前检查数据库和附件备份。
- Migration 只允许明确的 forward migration。
- Migration 通过显式 Server 管理命令执行；Server 不在启动时自动执行未知 Migration。
- Server 启动时报告所需迁移。
- 不支持自动删除未知字段或未知数据。
- Plugin 连接时检查 API 版本和能力列表。
- 升级失败时保留原数据卷和可回滚版本。

## 故障诊断

Plugin 应能展示：

- Server URL。
- Server 版本。
- API 版本。
- 最近成功请求时间。
- 最近错误码。
- Request ID 或诊断 ID。

Server 日志使用结构化格式，并默认不包含 Record 内容、Attachment 内容或 Token。

## 保留期清理

Server 在启动时及其后每 24 小时运行一次有界后台任务，按实际配置的保留期清理 Change 与幂等结果；`forever` 禁用清理。任务使用 PostgreSQL Advisory Lock 和数据库时钟，每类数据每次最多 10 批、每批 10,000 条，以短事务提交并记录删除数量和耗时；不要求外部 Scheduler。

`LOOMTABLE_CHANGE_RETENTION` 与 `LOOMTABLE_IDEMPOTENCY_RETENTION` 接受 `30d`、`90d`、`365d` 或 `forever`，默认均为 `30d`。Change 清理同时维护每个 Table 的过期水位，避免全局 Change Sequence 的间隙被误判为 Cursor 过期。

## Migration 生命周期

首个公开 P0 发布前允许把 Schema 调整折叠进 `001_initial.sql`，本阶段开发数据库视为可重建。P0 发布后冻结 001，只新增有序 Forward Migration；普通 Server 启动始终只报告 Migration 状态，不自动执行。
