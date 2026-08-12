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
3. 首次部署时，运维者显式执行 Bootstrap/管理命令，创建稳定 Actor 和初始 Token 哈希。
4. Plugin 打开连接向导。
5. 用户选择本地或远程 URL。
6. 输入或粘贴 Token。
7. Plugin 请求健康检查和 Server Meta。
8. Plugin 检查 API 版本和最低兼容版本。
9. Plugin 检查数据库迁移状态。
10. 用户选择 Workspace。

## 配置原则

- PostgreSQL 凭据只存在 Server 环境中。
- Plugin 只保存 Server URL 和访问 Token。
- PostgreSQL 中未撤销的 Token 哈希是运行时认证的唯一事实来源；明文 Token 不落库。
- 初始 Token 可以由环境变量提供给一次显式 Bootstrap 命令，或由管理命令生成。普通 Server 启动不读取环境变量作为长期认证旁路，也不隐式创建或轮换 Token。
- P0 不提供公开 Token 生成 API，localhost 也不免认证。
- Personal Server 使用一个持久化的稳定 Actor；同一 Actor 可以拥有多个具名 Active Token。每个 Token 可独立撤销，P0 不设置到期时间。Token 更换、Server 重启和数据库恢复都不改变 Actor ID；P0 默认授予其所有 Workspace 权限，不实现登录、邀请或角色系统。
- PostgreSQL 端口不应直接暴露到公网。
- Attachment 文件卷（即使当前 capability 未启用，也作为预留卷）必须持久化。
- Secret 不写入日志。
- 生产远程部署应使用 HTTPS 或可信内网通道。

## Actor 和 Token 初始化

- Bootstrap 必须是显式、可重复检查但不会重复创建身份的操作：已有 Personal Actor 时复用其 ID，除非运维者明确执行另一个 Token 管理动作，否则不新增或替换 Token。
- 每个 Token 都有稳定的 `tok_...` ID 和必填名称；同一 Actor 可以同时保有多个未撤销 Token，便于按设备或用途独立轮换。
- Server 只在创建 Token 时向终端显示一次明文；后续只能校验、列出 ID、名称、创建/撤销状态等元数据或撤销哈希记录，不能还原明文。
- P0 Token 没有自动到期时间；撤销是终止其访问能力的唯一 P0 生命周期动作。
- Token 管理是本机管理命令，不通过公开业务 REST API 暴露。
- 管理入口固定为 `loomtable-admin auth bootstrap/create/list/revoke`。默认 Secret 为 `ltp_` 加 32 字节 CSPRNG Base64URL；数据库保存 SHA-256，`tok_...` 仅作为 Token 元数据 ID。
- Actor、Token 哈希及撤销状态随 PostgreSQL 一起备份和恢复。

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

P0 通过 Server 管理命令或运维脚本执行备份和恢复，不通过业务 REST API 暴露备份接口。备份必须带有 Server 版本、Schema 版本和生成时间等版本清单。

## 恢复

恢复流程必须先在隔离目录验证：

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
