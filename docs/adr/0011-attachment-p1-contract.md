# P1 Attachment 使用托管文件卷与显式可用状态

## 状态

已采用，作为 Attachment P1 的实现合同。

## 决策

Attachment 元数据保存在 PostgreSQL，Managed Attachment 内容保存在 Server 配置的文件卷。Attachment 使用 `pending` 和 `ready` 两个内容状态；软删除通过 `deletedAt` 表示。只有 `ready` 的 Attachment 才能写入 Record 的 `attachment` Field。

- `POST /v1/attachments/init` 必须携带 `Idempotency-Key`。
- Managed Attachment 初始化为 `pending`，Server 生成不可由客户端控制的 `storageKey`；上传通过 `PUT /v1/attachments/{attachmentId}/content` 原子写入临时文件并重命名，完成大小、SHA-256 和 MIME 探测后转为 `ready`。
- Managed Attachment 的默认单文件大小上限为 50 MiB，可由 `LOOMTABLE_ATTACHMENT_MAX_BYTES` 覆盖。初始化声明的 `size`（如果提供）必须与上传实际大小一致。
- Vault Attachment 不读取用户 Vault，只保存经过校验的 Vault 相对路径和元数据，初始化后直接为 `ready`；Vault Attachment 不接受 Server 内容上传或下载。
- 文件名执行 Unicode Trim、NFC、控制字符拒绝和 255 个 Unicode 码点限制。Vault 路径必须是相对路径，不得包含空成员、`.`、`..` 或反斜线分隔的绝对路径。
- `GET`、内容下载和删除都重新检查 Actor 归属；格式正确但不属于当前 Actor 的 ID 统一返回 `404`。
- `DELETE /v1/attachments/{attachmentId}?expectedRevision=...` 只软删除元数据，不立即删除物理文件。文件保留到后续清理任务，以保证备份和恢复窗口内可诊断。
- `attachment` Field 的 Config 为 `{ "maxCount": 1..100 }`，缺省为 10；Cell 值是 AttachmentRef 数组或 `null`。Record Mutation 必须验证每个引用的 ID、source、文件名和当前 Actor 所有权，并要求 Attachment 为 `ready` 且未删除。
- `attachments` capability 只有在启用 Attachment 模块并且文件卷配置成功时才声明。模块未启用时保留 OpenAPI 路由并返回 `501 CAPABILITY_NOT_ENABLED`。

## 后果

Attachment 内容不进入 JSONB 或 PostgreSQL 大字段；备份继续同时覆盖数据库和文件卷。引用计数、缩略图、对象存储 Adapter、Range 下载和 Vault 内容同步不属于本次 P1 垂直切片，后续在不改变 AttachmentRef 基本语义的前提下扩展。

