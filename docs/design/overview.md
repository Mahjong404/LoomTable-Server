# LoomTable Server 概要设计

## 1. 文档目的

本文定义 LoomTable Server 的服务职责、部署形态、模块边界、核心数据流和第一个可用版本的范围。数据库字段、事务和接口细节分别记录在存储、同步和 OpenAPI 文档中。

## 2. 服务职责

LoomTable Server 是 LoomTable 的数据事实来源，负责：

- Workspace、Base、Table 和 View 生命周期。
- Field Definition 和 Schema 变更。
- Record 查询、筛选、排序、分页和 Mutation。
- Revision、Conflict 和 Change Cursor。
- Managed Attachment 元数据和文件内容（P1，使用持久化文件卷）。
- Personal Token 认证。
- API 版本和能力声明。
- 数据库迁移、备份恢复和健康检查。

Server 不负责：

- Obsidian UI 绘制。
- Vault 文件同步。
- 直接控制用户电脑上的 Docker。
- 第一阶段的实时协作。
- 同时兼容多个数据库引擎。

## 3. 部署形态

Personal 第一阶段使用模块化单体：

```text
LoomTable Server（Go）
        ├── PostgreSQL
        └── Managed Attachment Volume
```

Server、PostgreSQL 和 Managed Attachment 文件卷由 Docker Compose 部署；Attachment capability 可通过配置关闭。Redis、消息队列和对象存储属于后续 Team 或远程生产部署能力。

## 4. 模块总览

```text
internal/
├── auth
├── workspace
├── base
├── table
├── schema
├── view
├── query
├── mutation
├── attachment
├── sync
├── storage
└── httpapi
```

模块是同一 Server 进程内的代码 Module，不是独立服务。模块之间通过领域 Interface 和事务边界协作。

## 5. P0 服务能力

- 健康检查和 Server Meta。
- Token 认证。
- Workspace、Base、Table 创建和查询。
- 基础 Field 管理。
- Grid 所需的 Query API。
- Record 创建、编辑、软删除和恢复。
- Revision Conflict。
- Change Cursor。
- Location 值的保存和查询。
- Map View 所需的有界视口 Point/Cluster 数据。
- PostgreSQL 迁移和 Personal 备份要求。

## 6. 质量目标

- 20k Records 是产品性能基线。
- 查询、筛选、排序和分页由 PostgreSQL 和 Server 执行。
- Plugin 不必下载完整 Table 才能打开 View。
- Map View 不必下载全部匹配 Record 才能显示 20k 规模数据。
- Mutation 具有幂等性。
- Schema 变更不会静默丢失数据。
- 启用 Managed Attachment 后，Server 重启仍能保持数据和附件引用一致。

## 7. 许可证

LoomTable Server 使用 GPL-3.0。第三方依赖、文件存储、地图服务和其他外部组件需要单独审核许可证和部署条款。

