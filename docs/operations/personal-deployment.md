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

## 连接流程

1. 用户启动 Docker Compose。
2. Plugin 打开连接向导。
3. 用户选择本地或远程 URL。
4. 输入或粘贴 Token。
5. Plugin 请求健康检查和 Server Meta。
6. Plugin 检查 API 版本和最低兼容版本。
7. Plugin 检查数据库迁移状态。
8. 用户选择 Workspace。

## 配置原则

- PostgreSQL 凭据只存在 Server 环境中。
- Plugin 只保存 Server URL 和访问 Token。
- PostgreSQL 端口不应直接暴露到公网。
- Attachment 文件卷必须持久化。
- Secret 不写入日志。
- 生产远程部署应使用 HTTPS 或可信内网通道。

## 健康检查

Server 至少提供：

- 存活检查。
- 数据库连接检查。
- 迁移状态。
- API 版本。
- Server 版本。
- 能力列表。
- Attachment 存储可写性检查。

## 备份

完整备份必须同时包含：

- PostgreSQL 数据。
- Managed Attachment 文件卷。
- Server 版本。
- Schema 版本。
- 备份生成时间。
- 恢复所需的元数据。

只备份 PostgreSQL 不算完整 LoomTable 备份。

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

