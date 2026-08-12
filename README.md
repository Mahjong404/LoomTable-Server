# LoomTable Server

LoomTable 的 Go 后端服务。Server 是 Workspace、Base、Table、Field、View、Record 和 Managed Attachment 元数据的事实来源。

## 当前状态

当前仓库处于 P0 实现阶段。已接入 PostgreSQL 认证与 Bootstrap 状态、严格 JSON 请求边界，以及 Workspace/Base/Table 的列表、读取、创建和并发控制；Table 创建会原子生成 Primary Field 与初始 Grid View。Record 已实现直接读取和原子批量 Mutation，包括九种 P0 值校验、幂等、冲突、No-op、回收状态、Change 与查询投影。Field/View 管理、Record Query/Change 拉取、运维脚本和完整集成验收仍在继续。

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

原生进程默认只监听 `127.0.0.1:31201`；只有显式设置 `LOOMTABLE_HTTP_ADDR` 才会改变监听地址。Docker Compose 同样只把 `31201` 发布到宿主机回环地址。局域网或远程部署必须显式配置监听地址，并通过 TLS 反向代理或可信内网通道暴露服务。

首次初始化数据库时，先执行显式 Migration：

```text
go run ./cmd/loomtable-migrate -dir migrations
```

Personal Docker Compose 的环境变量示例见 `.env.example`；Attachment 文件卷在 P0 只作为预留基础设施，`attachments` capability 默认未启用。

许可证：GPL-3.0。

