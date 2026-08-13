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
├── values JSONB
├── query_values JSONB（内部派生，不进入 API）
└── search_text TEXT（内部派生，不进入 API）
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

`query_values` 和 `search_text` 是由 Go Field Type Registry 从 Canonical `values` 生成的可重建查询投影，并与 Record Mutation 在同一事务内更新。它们保存统一 Unicode Case-Fold、类型化和坐标查询键，不成为新的事实来源，也不在 API 中返回。Select Option Rank 从当前 Field Config 取得，Option 重排无需重写全部 Record。

`values` 对象的键必须是稳定的 Field ID，而不是 Field 名称。Field 改名只改变 Field Definition 的名称，不需要重写 Record 值。

缺失的 Field ID 表示 Unset Cell；显式 `null`、空字符串、空数组等仍是已经写入的值，并按对应 Field Type 的规则校验。更新 Record 时，`set` 写入或替换值，`unsetFieldIds` 删除键；请求中未出现的 Field 保持不变。

创建 Table 时，Server 在同一事务中创建 Table、可重命名的 `text` Primary Field 和初始 Grid View，并将 Field ID 保存为 `table.primary_field_id`。初始 View 只包含 Primary Field，使用 `standard` 行高且无 Filter/Sort。P0 的 Table 始终有 Primary Field。

P0 的 `date` 值是 `YYYY-MM-DD` 形式的纯日期，不携带时区；日期时间值属于后续字段能力。

P0 不执行 Field Type 迁移。类型变更必须等迁移预览和错误集合同步定义后再开放。

## Personal Actor 和 Token

Personal Server 的稳定 Actor 与 Access Token 哈希都保存在 PostgreSQL。显式 Bootstrap 或管理命令在事务中创建 Actor 和 Token 记录；Token 使用稳定的 `tok_...` ID 和必填名称。运行时认证以未撤销的数据库 Token 记录为唯一事实来源。一个 Actor 可以拥有多个具名 Token，每个 Token 可以单独撤销；P0 不设置自动过期时间。更换或撤销 Token 不改变 Actor ID，Server 启动也不会隐式创建或轮换 Token。

Token Secret 默认由 32 字节 CSPRNG 生成并编码为 `ltp_` 加 Base64URL；数据库只保存 SHA-256，不保存明文。管理命令是 `loomtable-admin auth bootstrap/create/list/revoke`，明文仅在创建时显示一次。

Token 名称保存规范化后的值及用于 Active 唯一性的 locale-neutral Unicode case-fold key；活动 Token 名称在同一 Actor 内唯一。Bootstrap 只负责首次 Actor 和首个 Token，后续 Token 由 Create 命令生成，撤销最后一个 Token 也是合法操作。

Cursor/Query Snapshot Token 的 32 字节 CSPRNG HMAC Key 保存于 PostgreSQL `server_secrets`，随数据库备份恢复。Token 使用按用途隔离的版本化 HMAC-SHA256，Secret 不进入日志、API 或 Manifest 明文。

## 查询索引和空间边界

普通 Record Query 使用 30 分钟 TTL 的无状态 Keyset Cursor；数据库不为分页持有长事务或成员快照。每页使用一个短期只读 Repeatable Read Transaction，使该页 Records、`changeCursor` 和首页面 `totalCount` 一致。稳定 Sort 总是追加 `record.id ASC`。具体 Filter/Sort 使用参数化 SQL，查询投影热点按实际基准增加表达式或投影索引。

P0 不安装 PostGIS。Location 的 WGS 84 `lat`/`lng` 继续存入 JSONB，矩形视口查询通过安全的数值提取完成，Server 应用层执行最多 500 Feature 的自适应聚类。`geoWithin`、Polygon 等后续空间条件出现时再重新评估 PostGIS。

## Relation（后续能力）

Relation 的值由目标 Table 和一个或多个 Record ID 组成。为了支持反向查询和未来的 Lookup/Rollup，Server 可以维护规范化的 `relation_link` 查询索引；该索引必须与 Record Mutation 在同一事务中更新。

Relation 不属于 P0 的九种字段。启用后只允许同一 Base 内的 Relation；被引用 Record 软删除后，Relation 保留引用并显示“已删除记录”，不级联删除其他 Record。

#�n��h��춻�q�^t
��H�H[����KT�\�Y]�U\�H	\�HSY]��]U[Y[�]�X���]S�[��]\���B��]��\�T�Y\T�X�ۙ�B�B�B�[����KQ���\�
	���\��I�	�����	��\��\��B����	��\��\�Y���X��YH�XYK�B���H\�S��][ۈ	�\�\��[����KQ���\�
	���\��I�	�\	�	�Y	�	���ܙ\��B�[����KQ���\�
	���\��I�	�K\�ٚ[I�	����	ܝ[��	�K\�I�	�KX�Z[	�	�ZYܘ]I�	�Y\��	��\�ZYܘ][ۜ��B�	�����\^H
	����\���\��HK\�ٚ[H���[�K\�HYZ[�]]�����\K[�[YHX��\[��H�]T��[��B�Y�
	T�VU��H[�H
H����	�]][�X�][ۈ�����\�Z[Y��B�	�����\H	�����\^�۝�\����KR��ۂ�	��[�H	�����\���[���Xܙ]�Y�
[��	��[�H����	Л����\Y���]\��H��[��Xܙ]��B��[����KQ���\�
	���\��I�	�\	�	�Y	�	��\��\��B��Z]S��UX�T�XYB�	XY\��H]]ܚ^�][ۈH��X\�\�	��[���	�Y[\�[��KR�^I�H	�]]�	B�I�ܚ��X�T�\]Y\�HBU\�HH����L�ˌ��N�
	[�����UP�W����ԕ
K݌K��ܚ��X�\Ȃ�BSY]�H	���	BRXY\��H	XY\�BP�۝[�\HH	�\X�][ۋڜ�ۉBP��HH	�ț�[YH���X��\[��H�ܚ��X�H�I_B�I�ܚ��X�HH[����KT�\�Y]��ܚ��X�T�\]Y\��Y�
[��	�ܚ��X�K�Y
H����	��ܚ��X�HܙX][ۈY���]\��[�Q��B��	�
��[�T]	�ܚ\\�	ؘX��\��I�HS�]]]	\��]�B�	�
��[�T]	�ܚ\\�	ݘ[Y]KX�X��\��I�HP\��]�T]	\��]�B�[����KQ���\�
	���\��I�	��ۉ�	�]��	�K\�[[ݙK[ܜ[���B�[����KQ���\�
	���\��I�	�\	�	�Y	�	���ܙ\��B�	�
��[�T]	�ܚ\\�	ܙ\�ܙK��I�HP\��]�T]	\��]�HP�ۙ�\�B�[����KQ���\�
	���\��I�	�\	�	�Y	�	��\��\��B��Z]S��UX�T�XYB��	\�XY\��H�]]ܚ^�][ۈH��X\�\�	��[��B�	�\�ܙYH[����KT�\�Y]�U\�H����L�ˌ��N�
	[�����UP�W����ԕ
K݌K��ܚ��X�\ȈSY]��]RXY\��	\�XY\�Y�
	�\�ܙY�][\˚Y[���۝Z[��	�ܚ��X�K�Y
H���	ԙ\�ܙY�\��\�Y���]\��HX��\[��H�ܚ��X�K�B�ܚ]KR��	���UX�H��\��H�X��\ܙ\�ܙHX��\[��H\��Y�B��[�[HY�

�]S��][ۊK�]Y\H	�\�\�H��S��][ۈB�\�S��][ۈ	�\�\���H�	����\���\��H�ۈ]�K\�[[ݙK[ܜ[�����[�]S�[H�]��B��S��][ۂ��[[ݙKR][HS]\�[]	�ܚ�\�T�X�\��HQ�ܘ�HQ\��ܐX�[ۈ�[[�P�۝[�YB��ܙXX�
	�[YH[�	�]�Y[��\�ۛY[���^\�H�[��\�ۛY[�N���][��\�ۛY[��\�XX�J	�[YK	�]�Y[��\�ۛY[���[YWK	����\���B�B�B