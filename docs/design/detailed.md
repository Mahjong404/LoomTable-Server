# LoomTable Server 详细设计

## 1. 推荐目录

```text
cmd/
└── loomtable-server/
internal/
├── auth/
├── workspace/
├── base/
├── table/
├── schema/
├── view/
├── query/
├── mutation/
├── attachment/
├── sync/
├── storage/postgres/
└── httpapi/
migrations/
openapi/
tests/
```

## 2. HTTP 请求流程

```text
HTTP Request
→ Request ID
→ Bearer Token 验证
→ API Version / Capability 检查
→ 请求解码和输入校验
→ Domain Module
→ PostgreSQL Transaction（如需要）
→ Change Log
→ Response 编码
```

HTTP 层不实现业务规则。它负责参数解码、认证、错误映射和响应格式；Field、Revision、Filter 和 Mutation 语义由对应 Module 负责。

## 3. Query 流程

```text
QueryRequest
→ Filter AST 校验
→ Field Type Operator 校验
→ Filter AST 编译为 PostgreSQL 查询
→ Sort 和 Cursor 条件
→ Projection
→ Record JSONB 解码
→ QueryResult + nextCursor + changeCursor
```

约束：

- 默认排除软删除 Record。
- Filter 和 Sort 不在 Plugin 重算。
- Cursor 必须包含稳定排序所需的信息。
- 空值和空字符串按领域规则区分。
- 不允许用户输入直接拼接 SQL。
- 复杂或未支持的操作返回明确错误，不静默降级。

## 4. Mutation 流程

```text
MutationRequest
→ clientMutationId 去重检查
→ Command 校验
→ expectedRevision 校验
→ Field Type normalize
→ PostgreSQL Transaction
   ├── 写入 Record / Schema
   ├── 更新 Relation Index（如有）
   ├── 写入 Change Log
   └── 更新 Revision
→ 返回 per-command result
```

过期 Revision 返回 Conflict，不能自动覆盖。重复的 `clientMutationId` 返回第一次应用结果或明确的幂等结果。

## 5. 存储事务

以下数据变化必须在同一事务中完成：

- Record values 和 Revision。
- Relation 查询索引和 Record Mutation。
- Attachment 引用与 Attachment 元数据。
- Schema 变更与 Change Log。

文件二进制写入和数据库事务之间使用可恢复的上传状态，不能在数据库已引用文件但文件尚未完成时报告成功。

## 6. Schema 变更

- Field ID 永久稳定。
- 字段改名只更新名称。
- 类型变更先生成迁移预览。
- 无法转换的值进入迁移错误集。
- 删除 Field 先软删除。
- 未知 Field Type 不得静默转换为 Text。
- 迁移记录 Server Schema Version。

## 7. Attachment

### Managed Attachment

1. 初始化元数据。
2. 获得上传目标。
3. 写入文件卷。
4. 校验大小、MIME、Hash 和必要的图片元数据。
5. 标记可用。
6. Record Mutation 写入 Attachment 引用。

### Vault Attachment

Server 保存 Vault 相对路径和元数据，不读取用户 Vault。跨设备可用性由用户的 Vault 管理方式决定，Server 不假设同步完成。

## 8. Change Log

Change Log 用于：

- Change Cursor。
- Personal 多客户端刷新。
- 未来实时推送。
- 冲突诊断。
- 审计基础。

第一阶段不要求完整事件溯源。Change 是持久化变化索引，不替代当前 Record 状态。

## 9. 认证和安全

- Personal 使用 Bearer Token。
- Token 只在 Server 侧生成或通过安全初始化流程注入。
- Server 不向客户端返回数据库凭据。
- Attachment Content 下载必须经过授权。
- 文件路径使用服务端生成的 storage key。
- 日志默认脱敏。
- PostgreSQL 不直接暴露到公网。

## 10. 测试

- Domain Module 纯 Go 测试。
- PostgreSQL Migration 和 Repository 集成测试。
- Query Filter、Sort、Cursor 集成测试。
- Mutation 幂等和 Revision Conflict 测试。
- OpenAPI 合同测试。
- Attachment 文件卷和恢复测试。
- 20k/50k 数据集性能测试。
- Docker Compose 健康检查、升级和恢复 Smoke Test。

