# LoomTable 存储模型

## 存储责任

| 数据 | 存储位置 |
|---|---|
| Workspace、Base、Table、Field、View | PostgreSQL |
| Record 元数据和普通字段值 | PostgreSQL |
| Attachment 元数据 | PostgreSQL |
| Managed Attachment 内容 | 本地文件卷，未来可替换为对象存储 |
| Vault Attachment 内容 | Obsidian Vault |
| Plugin UI 缓存 | Plugin 本地缓存，不是事实来源 |

## 核心对象

```text
workspace
base
table
field
view
record
attachment
relation_link
change
actor
auth_token
```

所有对象使用服务端生成的稳定不透明 ID。名称可以变化，ID 不因重命名变化。

## Record

Record 使用结构化元数据和字段值对象：

```text
record
├── id
├── table_id
├── revision
├── created_at
├── updated_at
├── deleted_at
└── values JSONB
```

示例值：

```json
{
  "fld_01TITLE": "客户拜访",
  "fld_01STATUS": "server-generated-option-id-in-progress",
  "fld_01TAGS": ["server-generated-option-id-important", "server-generated-option-id-shanghai"],
  "fld_01LOCATION": {
    "label": "陆家嘴",
    "address": "上海市浦东新区",
    "lat": 31.2336,
    "lng": 121.5055
  }
}
```

普通 Field 值使用 JSONB 作为第一阶段规范存储。Field Definition 单独保存类型、配置和版本。

`values` 对象的键必须是稳定的 Field ID，而不是 Field 名称。Field 改名只改变 Field Definition 的名称，不需要重写 Record 值。

缺失的 Field ID 表示 Unset Cell；显式 `null`、空字符串、空数组等仍是已经写入的值，并按对应 Field Type 的规则校验。更新 Record 时，`set` 写入或替换值，`unsetFieldIds` 删除键；请求中未出现的 Field 保持不变。

创建 Table 时，Server 自动创建一个可重命名的 `text` Primary Field，并将其 ID 保存为 `table.primary_field_id`。P0 的 Table 始终有 Primary Field。

P0 的 `date` 值是 `YYYY-MM-DD` 形式的纯日期，不携带时区；日期时间值属于后续字段能力。

P0 不执行 Field Type 迁移。类型变更必须等迁移预览和错误集合同步定义后再开放。

## Personal Actor 和 Token

Personal Server 的稳定 Actor 与 Access Token 哈希都保存在 PostgreSQL。显式 Bootstrap 或管理命令在事务中创建 Actor 和 Token 记录；运行时认证以未撤销的数据库 Token 记录为唯一事实来源。一个 Actor 可以拥有多个具名 Token，每个 Token 可以单独撤销；P0 不设置自动过期时间。更换或撤销 Token 不改变 Actor ID，Server 启动也不会隐式创建或轮换 Token。

## Relation（后续能力）

Relation 的值由目标 Table 和一个或多个 Record ID 组成。为了支持反向查询和未来的 Lookup/Rollup，Server 可以维护规范化的 `relation_link` 查询索引；该索引必须与 Record Mutation 在同一事务中更新。

Relation 不属于 P0 的九种字段。启用后只允许同一 Base 内的 Relation；被引用 Record 软删除后，Relation 保留引用并显示“已删除记录”，不级联删除其他 Record。

## Attachment（扩展合同）

Attachment 元数据示例：

```json
{
  "id": "att_01H...",
  "source": "managed",
  "filename": "photo.png",
  "mimeType": "image/png",
  "size": 245760,
  "storageKey": "attachments/2026/08/att_01H....png",
  "hash": "sha256:...",
  "width": 1920,
  "height": 1080
}
```

数据库不保存附件二进制内容。Managed Attachment 使用本地 Docker 文件卷；远程对象存储作为后续 Adapter。Vault Attachment 只保存 Vault 相对路径和必要元数据。

Attachment API 和存储模型在 P0 保留为扩展合同，但 `attachments` capability 默认未启用；P0 不创建 Attachment Field，也不接受 Attachment Record 值。

## 删除

- Record、Field、Table 和 Attachment 引用默认使用软删除或回收站。
- Record、Field、Table 和 View 的当前 Revision 用于并发控制；Record 的删除/恢复以及 Field、Table、View 的更新/删除都必须携带 `expectedRevision`。
- Table、Field、View List 与 Record Query 使用统一的 Lifecycle Scope：`active`、`deleted` 或 `all`，默认 `active`。该范围按对象自身的软删除状态筛选；祖先对象的可访问性规则仍然适用。P0 不为 Workspace/Base 提供删除能力，因此其 List 不接受该范围。
- 恢复操作保留原 ID。
- P0 不提供硬删除 API。
- Managed Attachment 的物理文件不能因为一次普通删除立即清理。
- 物理清理需要引用计数、保留期和可恢复备份策略。