# LoomTable Server

LoomTable 的 Go 后端服务。Server 是 Workspace、Base、Table、Field、View、Record 和 Managed Attachment 元数据的事实来源。

## 当前状态

当前仓库处于 P0 集成验收阶段。Workspace/Base/Table/Field/View、Record Query/Mutation/Change、Grid/Map 查询、PostgreSQL Token 管理、保留期清理及跨平台备份/验证/恢复入口均已实现。纯 Go 测试、静态检查、OpenAPI 路由合同和 Compose 配置可在本地运行；需要 PostgreSQL/Docker 的端到端与运维恢复验收由测试环境执行后才可合并 `main`。

## 文档

- [贡献与分支规范](./CONTRIBUTING.md)
- [Server 文档索引](./docs/README.md)
- [领域上下文](./CONTEXT.md)
- [术语对照表](./docs/terminology.md)
- [架构总览](./docs/architecture/overview.md)
- [源码结构](./docs/architecture/source-layout.md)
- [存储模型](./docs/architecture/storage-model.md)
- [同步模型](./docs/architecture/sync-model.md)
- [Field Type 规范](./docs/field-types.md)
- [OpenAPI](./docs/api/openapi.yaml)
- [Personal 部署](./docs/operations/personal-deployment.md)

## 运行边界

- 使用 Go 和模块化单体架构。
- 第一阶段只支持 PostgreSQL。
- Personal 模式使用 Docker Compose。
- 第一阶段不要求 Redis、Kafka、RabbitMQ 或独立对象存储。
- API 合同由 `docs/api/openapi.yaml` 定义，并导入 Apifox。

## 本地运行

需要 Go 1.22+。认证由 PostgreSQL 中的 Personal Actor 与具名 Token 管理；普通 Server 启动不接受环境变量 Token 旁路，也不隐式创建身份。

配置 `LOOMTABLE_DATABASE_URL` 后运行：

```text
go run ./cmd/loomtable-server
```

首次部署在 Migration 后显式创建 Personal Actor 与初始 Token；Secret 只在创建时显示一次：

```text
go run ./cmd/loomtable-admin auth bootstrap --name "Primary device"
```

后续使用 `auth create --name`、`auth list` 和 `auth revoke --token-id` 管理具名 Token。Docker Compose 下使用 `docker compose --profile ops run --rm admin auth ...`。

原生进程默认只监听 `127.0.0.1:31201`；只有显式设置 `LOOMTABLE_HTTP_ADDR` 才会改变监听地址。Docker Compose 同样只把 `31201` 发布到宿主机回环地址。局域网或远程部署必须显式配置监听地址，并通过 TLS 反向代理或可信内网通道暴露服务。

首次初始化数据库时，先执行显式 Migration：

```text
go run ./cmd/loomtable-migrate -dir migrations
```

Personal Docker Compose 的环境变量示例见 `.env.example`；Attachment 文件卷在 P0 只作为预留基础设施，`attachments` capability 默认未启用。

常用 Compose 初始化命令：

```text
docker compose up -d postgres
docker compose --profile ops run --rm migrate -dir /app/migrations
docker compose --profile ops run --rm admin auth bootstrap --name "Primary device"
docker compose up -d server
```

备份、校验和恢复入口位于 `scripts/operations/`，PowerShell 与 Bash 版本执行相同的版本化归档合同。恢复必须在 Server 停止后显式确认。

许可证：GPL-3.0。

