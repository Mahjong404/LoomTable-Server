# LoomTable Server

LoomTable 的 Go 后端服务。Server 是 Workspace、Base、Table、Field、View、Record 和 Managed Attachment 元数据的事实来源。

## 当前状态

当前仓库已开始 P0 Server 实现。当前增量包括 HTTP 运行骨架、认证边界、健康检查、Server Meta、PostgreSQL 迁移入口和初始存储模型；Workspace、Table、Record 的业务 Handler 将在此基础上继续接入。

## 文档

- [Server 文档索引](./docs/README.md)
- [领域上下文](./CONTEXT.md)
- [术语对照表](./docs/terminology.md)
- [架构总览](./docs/architecture/overview.md)
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

需要 Go 1.22+。当前骨架仍通过 `LOOMTABLE_AUTH_TOKEN_HASH` 注入单个开发用认证哈希；这是业务 Handler 接入前的过渡实现，不是最终 P0 认证合同。最终实现必须由显式 Bootstrap/管理命令在 PostgreSQL 中创建稳定 Actor 和具名 Token，普通启动不得把环境变量作为长期认证旁路。

配置 `LOOMTABLE_DATABASE_URL` 和过渡用 `LOOMTABLE_AUTH_TOKEN_HASH` 后运行：

```text
go run ./cmd/loomtable-server
```

首次初始化数据库时，先执行显式 Migration：

```text
go run ./cmd/loomtable-migrate -dir migrations
```

Personal Docker Compose 的环境变量示例见 `.env.example`；Attachment 文件卷在 P0 只作为预留基础设施，`attachments` capability 默认未启用。

许可证：GPL-3.0。

