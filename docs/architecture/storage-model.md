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

Workspace 与 Base 和其他可变元数据一样保存 `revision`；P0 支持直接读取与乐观并发重命名，但不提供删除或恢复。完整元数据列表保持不分页，并由领域层执行硬上限：每 Actor 100 Workspace、每 Workspace 500 Base、每 Base 500 Table、每 Table 500 Field 和 100 View。

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

创建 Table 时，Server 在同一事务中创建 Table、可重命名的 `text` Primary Field 和初始 Grid View，并将 Field ID 保存为 `table.primary_field_id`。初始 View 只包含 Primary Field，使用 `standard` 行高且无 Filter/Sort。P0 的 Table 始终有 Primary Field。

P0 的 `date` 值是 `YYYY-MM-DD` 形式的纯日期，不携带时区；日期时间值属于后续字段能力。

P0 不执行 Field Type 迁移。类型变更必须等迁移预览和错误集合同步定义后再开放。

## Personal Actor 和 Token

Personal Server 的稳定 Actor 与 Access Token 哈希都保存在 PostgreSQL。显式 Bootstrap 或管理命令在事务中创建 Actor 和 Token 记录；Token 使用稳定的 `tok_...` ID 和必填名称。运行时认证以未撤销的数据库 Token 记录为唯一事实来源。一个 Actor 可以拥有多个具名 Token，每个 Token 可以单独撤销；P0 不设置自动过期时间。更换或撤销 Token 不改变 Actor ID，Server 启动也不会隐式创建或轮换 Token。

Token Secret 默认由 32 字节 CSPRNG 生成并编码为 `ltp_` 加 Base64URL；数据库只保存 SHA-256，不保存明文。管理命令是 `loomtable-admin auth bootstrap/create/list/revoke`，明文仅在创建时显示一次。

Token 名称保存规范化后的值及用于 Active 唯一性的 locale-neutral Unicode case-fold key；活动 Token 名称在同一 Actor 内唯一。Bootstrap 只负责首次 Actor 和首个 Token，后续 Token 由 Create 命令生成，撤销最后一个 Token也是合法操作。

## 查询索引和空间边界

普通 Record Query 使用 30 分钟 TTL 的无状态 Keyset Cursor；数据库不为分页持有长事务或成员快照。稳定 Sort 总是追加 `record.id ASC`。具体 Filter/Sort 使用参数化 SQL，JSONB 热点在 20k Records 基准后按需增加表达式或投影索引。

P0 不安装 PostGIS。Location 的 WGS 84 `lat`/`lng` 继续存入 JSONB，矩形视口查询通过安全的数值提取完成，Server 应用层执行最多 500 Feature 的自适应聚类。`geoWithin`、Polygon 等后续空间条件出现时再重新评估 PostGIS。

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
- Workspace、Base、Table 和 View List 默认按 `created_at ASC, id ASC`；Field List 按 `position_index ASC, id ASC`。同一父对象内允许同名。
- 新 Field 使用当前最大 `position_index + 1`；删除保留空位，恢复沿用原位置。Schema 不提供 P0 Field 重排操作。
- 恢复操作保留原 ID。
- P0 不提供硬删除 API。
- Managed Attachment 的物理文件不能因为一次普通删除立即清理。
- 物理清理需要引用计数、保留期和可恢复备份策略。

Change 和幂等结果由 Server 启动时及每 24 小时运行的有界后台任务按配置保留期清理；`forever` 完全禁用该清理。该任务不依赖外部 Scheduler。
