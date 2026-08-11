# LoomTable Server

LoomTable 的 Go 后端服务。Server 是 Workspace、Base、Table、Field、View、Record 和 Managed Attachment 元数据的事实来源。

## 当前状态

当前仓库处于设计和工程准备阶段，尚未开始 P0 功能实现。

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

许可证：GPL-3.0。

