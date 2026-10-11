# LoomTable Server 文档

## 领域和合同

- [Server 概要设计](./design/overview.md)
- [Server 详细设计](./design/detailed.md)
- [领域上下文](../CONTEXT.md)
- [术语对照表](./terminology.md)
- [Field Type 规范](./field-types.md)
- [OpenAPI](./api/openapi.yaml)

## 架构

- [架构总览](./architecture/overview.md)
- [源码结构](./architecture/source-layout.md)
- [存储模型](./architecture/storage-model.md)
- [同步模型](./architecture/sync-model.md)
- [性能预算](./performance-budget.md)

## 运维和决策

- [Personal 部署与运维](./operations/personal-deployment.md)
- [开发日志](./development-log.md)
- [ADR 目录](./adr/)
- [P0 不引入 PostGIS](./adr/0009-defer-postgis-beyond-p0.md)
- [Canonical Record 值与可重建查询投影分离](./adr/0010-canonical-values-with-query-projection.md)
- [P1 Attachment 合同](./adr/0011-attachment-p1-contract.md)


## Wave 1.5 合同实施准备

[Devin Server 独立入口](./wave1.5/README.md) 定义六条待授权 API Gate、合同/OpenAPI 先行顺序、隔离测试和 Plugin 交付输入。当前仅准备文档，不自动启动代码、数据库或 API 修改；设计批准不解锁 Future。实际 wire 合同仍以本仓库 OpenAPI 为准。
