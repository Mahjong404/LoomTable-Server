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
  "title": "客户拜访",
  "status": "进行中",
  "tags": ["重要", "上海"],
  "location": {
    "label": "陆家嘴",
    "address": "上海市浦东新区",
    "lat": 31.2336,
    "lng": 121.5055
  }
}
```

普通 Field 值使用 JSONB 作为第一阶段规范存储。Field Definition 单独保存类型、配置和版本。

## Relation

Relation 的值由目标 Table 和一个或多个 Record ID 组成。为了支持反向查询和未来的 Lookup/Rollup，Server 可以维护规范化的 `relation_link` 查询索引；该索引必须与 Record Mutation 在同一事务中更新。

第一阶段只允许同一 Base 内的 Relation。被引用 Record 软删除后，Relation 保留引用并显示“已删除记录”，不级联删除其他 Record。

## Attachment

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

## 删除

- Record、Field、Table 和 Attachment 引用默认使用软删除或回收站。
- 正常查询隐藏已删除对象。
- 恢复操作保留原 ID。
- Managed Attachment 的物理文件不能因为一次普通删除立即清理。
- 物理清理需要引用计数、保留期和可恢复备份策略。

