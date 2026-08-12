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

- Table、Field、View List 与 Record Query 使用统一 Lifecycle Scope：`active`、`deleted` 或 `all`，默认 `active`；该范围按对象自身的软删除状态筛选。P0 不删除 Workspace/Base，因此其 List 不接受该参数。
- Filter 和 Sort 不在 Plugin 重算。
- Cursor 必须绑定 Lifecycle Scope、Search 和稳定排序所需的全部等价 Query 信息。
- 空值和空字符串按领域规则区分。
- 不允许用户输入直接拼接 SQL。
- 复杂或未支持的操作返回明确错误，不静默降级。

## 4. Mutation 流程

```text
MutationRequest
→ clientMutationId 去重检查
→ Command 校验
→ expectedRevision 校验
→ Field Type normalize（`set`）/ Unset 校验（`unsetFieldIds`）
→ PostgreSQL Transaction
   ├── 写入 Record / Schema
   ├── 更新 Relation Index（如有）
   ├── 写入 Change Log
   └── 更新 Revision
→ 返回 per-command result
```

过期 Revision 返回 Conflict，不能自动覆盖。重复的 `clientMutationId` 返回第一次应用结果或明确的幂等结果。

Record 更新使用 `set + unsetFieldIds`：`set` 中出现的 Field 被写入或替换，且可以显式写入合法的 `null`、空字符串或空数组；`unsetFieldIds` 才移除 `Record.values` 中的键。两者均未提及的 Field 保持不变，同一 Field 同时出现于两者返回 `422 VALIDATION_ERROR`。P0 九种 Field 都允许显式 `null`；Text/LongText 的 `""` 与 MultiSelect 的 `[]` 是合法自然空值，空 URL 字符串和空 Location 对象无效。

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
- Field 更新是顶层 PATCH：省略 `name` 或 `config` 表示保留该顶层成员；一旦提供 `config`，它就是完整替换，不是 JSON Merge Patch。Server 校验并规范化后返回完整 Field。
- P0 不开放类型变更；迁移预览、迁移 Token 和无法转换值的错误集合都属于后续能力。
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
- Personal Server 通过显式 Bootstrap/管理命令在 PostgreSQL 中初始化一个稳定 `actor`；同一 Actor 可以拥有多个具名 Active Token，各 Token 可独立撤销。P0 Token 不设置到期时间，默认授予该 Actor 所有 Workspace 权限，不实现 User、Role 和登录体系。
- PostgreSQL 中未撤销的 Token 哈希是认证事实来源。环境变量只可作为显式 Bootstrap 的输入；普通启动不把环境变量哈希当作旁路，也不隐式创建或轮换 Token。
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

## 11. P0 合同决策

### 范围

P0 先完成 Grid + Map 的完整数据闭环。Attachment 保留在 API 和领域模型的扩展合同中，但不作为 P0 的实现内容；Server 通过 capability 声明是否已启用相关能力，Plugin 在能力不可用时不展示对应入口。

P0 实现的字段类型为：

```text
text / longText / number / checkbox / date
select / multiSelect / url / location
```

P0 不实现 Field Type 迁移。Field 只允许改名和修改不改变值语义的配置；类型变更和迁移预览属于后续能力。

### Record 值和 Primary Field

`Record.values` 的键始终是稳定的 Field ID，不使用可变的字段名称。字段改名不会改变既有 Record 值的定位。

创建 Table 时，Server 在同一事务中自动创建一个可重命名的 `text` Primary Field 和一个 Grid View，并将 Field ID 写入 `Table.primaryFieldId`。请求可选传入 `primaryFieldName` 和 `initialViewName`；Plugin 负责提交本地化名称，未传时 Server 分别使用 `Name` 和 `Grid`。成功响应原子返回 `{ table, primaryField, initialView }`，P0 的 Table 不允许没有 Primary Field。

Primary Field 值可以是 Unset、`null` 或空字符串，创建 Record 不要求先填写它。列表、详情和地图需要标签时由 Plugin 使用本地化的“无标题”或 Record ID 回退；Server 不把显示占位文本写入 Record 值。

`date` 在 P0 中表示不带时区的纯日期，规范值为 `YYYY-MM-DD`；日期时间字段不属于 P0。

### Query 和 Mutation

提供 `viewId` 时，View 配置是查询的基础。Request 中显式提供的 Filter、Sort、Projection 和分页参数只对本次请求生效，不回写 View 配置。

没有显式 Sort 时，Query 使用 `createdAt ASC, id ASC` 作为稳定默认排序。Cursor 必须编码该排序所需的位置，不能只使用页码。

一个 Mutation Request 内的所有 Command 使用同一个 PostgreSQL Transaction：全部通过才提交，任一校验失败、权限失败或 Revision Conflict 都回滚整个请求。`clientMutationId` 在事务边界外也必须保持幂等，重试返回第一次应用结果，不重复写入 Change。

Workspace、Base、Table、Field 和 View 的创建请求必须携带 `Idempotency-Key: mut_...`。Key 在 Actor 范围内全局唯一；相同 Method、Path 和规范化 Body 的重试返回第一次 `201` 结果，不重复创建对象；不同请求复用同一 Key 返回 `409 IDEMPOTENCY_KEY_REUSED`。该结果遵循 Server 声明的幂等保留策略。

Conflict 使用 `409`；认证、授权、输入校验、资源不存在、能力未启用和内部故障必须分别使用可识别的 HTTP 状态和错误码，不能全部折叠为同一个默认错误。

### 健康、认证和备份

- `/healthz` 是无需认证的进程存活检查。
- `/readyz` 是无需认证的可接收请求检查，至少检查数据库连接和迁移状态。
- 业务 API 继续要求 Bearer Token。
- P0 的初始 Token 由显式 Bootstrap/管理命令创建；环境变量只能提供给该命令作为一次性输入。Server 只保存 Token 哈希，不提供公开的 Token 生成 API，也不对 localhost 免认证。同一稳定 Actor 可以拥有多个具名 Active Token，各自独立撤销；P0 Token 不设置到期时间。
- P0 的备份/恢复通过 Server 管理命令或运维脚本完成，必须同时覆盖 PostgreSQL、Managed Attachment 文件卷和版本清单。

## 12. Q93–Q102 已确认的实现合同

### 许可证和 ID

- `LoomTable Obsidian Plugin` 使用 MIT License。
- `LoomTable Server` 使用 GPL-3.0。
- 所有服务端生成的对象 ID 使用不透明、带类型前缀的 ULID，例如 `ws_01...`、`tbl_01...`、`rec_01...`。ID 不因重命名或恢复而变化；名称不能作为对象身份。

### HTTP 错误合同

错误响应统一使用：

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "...",
    "requestId": "req_01...",
    "details": {}
  }
}
```

P0 的稳定 HTTP 状态映射为：

| HTTP | 含义 | 典型错误码 |
|---:|---|---|
| 400 | 请求格式、参数结构或 Cursor 不匹配 | `BAD_REQUEST`, `INVALID_CURSOR` |
| 401 | 缺少或无效 Bearer Token | `UNAUTHENTICATED` |
| 403 | Actor 已认证但无权访问 | `FORBIDDEN` |
| 404 | 资源不存在或不属于当前 Actor | `NOT_FOUND` |
| 409 | Revision 或幂等键冲突 | `CONFLICT`, `IDEMPOTENCY_KEY_REUSED` |
| 410 | Cursor 或短期查询令牌已过期 | `CURSOR_EXPIRED` |
| 422 | 字段值、Filter、Operator、View 配置或其他领域语义无效 | `VALIDATION_ERROR`, `UNSUPPORTED_OPERATOR`, `VIEW_CONFIGURATION_REQUIRED` |
| 501 | 能力存在于合同但当前 Server 未启用 | `CAPABILITY_NOT_ENABLED` |
| 503 | Server 尚未就绪或依赖不可用 | `MIGRATION_REQUIRED`, `DEPENDENCY_UNAVAILABLE` |
| 500 | 未预期的服务端错误 | `INTERNAL_ERROR` |

错误码、HTTP 状态和 `requestId` 是 Client Adapter 的稳定映射依据；Server 不把上述情况折叠为单一默认错误。

### Personal 认证

P0 不提供公开 Token 生成 API，也不因使用 localhost 而免认证。一个 Personal Server 有一个持久化的稳定 Actor；显式 Bootstrap/管理命令创建或管理与其关联的多个具名 Token，数据库保存 `tok_...` ID、名称、不可逆哈希及撤销状态。每个 Token 可独立撤销，P0 不设置到期时间。Token 更换、重启和恢复不改变 Actor ID，普通启动不会隐式轮换凭据。P0 不实现用户登录、Workspace 邀请、角色或细粒度权限。

### Query、Filter 和 Cursor

- P0 Filter 严格按照 Field Type Registry 校验 Operator 和值类型。
- `null`、缺失值和空字符串保持区分；文本匹配不区分大小写。
- 不支持的 Operator 返回 `422 UNSUPPORTED_OPERATOR`，不在 Server 或 Plugin 中静默降级。
- Cursor 是不透明值，绑定 Table、View 基础配置、Filter、Sort、Projection、分页大小和 API 版本。
- Cursor 只允许用于生成它的等价 Query；结构不匹配返回 `400 INVALID_CURSOR`，过期返回 `410 CURSOR_EXPIRED`。
- Map Cluster Record Query 的短期 Token 还绑定 View Revision、Location Field、Filter 和原 Map Query 快照；失效后客户端刷新 Map Viewport，不把 Cluster 当作持久领域对象。

### 元数据生命周期和 Revision

Record、Field、Table 和 View 均使用软删除。Record 的删除和恢复需要 `expectedRevision`；Field、Table 和 View 的更新、删除也需要对应的 `expectedRevision`。冲突统一返回 `409 CONFLICT`。P0 不提供硬删除 API。

### Mutation 返回和幂等

一个 Mutation Request 内的 Command 全部在同一事务中执行，任一 Command 失败则全部回滚。成功响应返回完整的 `MutationResult`；Conflict 响应必须包含 `clientMutationId`、失败 Command 索引、当前 Revision 和当前值。重复提交同一 `clientMutationId` 返回第一次结果，不重新应用变更。

### Migration 和 Attachment capability

- Migration 只能通过显式 Server 管理命令执行 forward migration。
- Server 启动发现待迁移时可以存活，但拒绝进入 ready；`/readyz` 返回 `503 MIGRATION_REQUIRED`。
- P0 保留 Attachment API 合同，但不声明 `attachments` capability。
- P0 调用 Attachment API 返回 `501 CAPABILITY_NOT_ENABLED`；Plugin 必须隐藏 Attachment 入口，不把该响应当作普通资源不存在。

## 13. Q103–Q135 已确认的字段和查询合同

### ID、View 和 Select 配置

- 服务端 ID 使用带类型前缀的 Crockford ULID：`ws_`、`base_`、`tbl_`、`fld_`、`view_`、`rec_`、`att_`、`chg_`、`act_`、`tok_`、`opt_`、`mut_`、`req_`。
- `/v1/meta` 与 `/healthz`、`/readyz` 一样无需认证；业务资源接口要求 Bearer Token。
- P0 的 Select/MultiSelect Option 由 Server 生成稳定 ID，至少保存 `id`、`name`、`color`。删除 Option 不复用旧 ID；已有 Record 保留旧引用并显示为已删除。Option 变更消耗 Field Revision。
- 创建 Table 时在同一事务中自动创建 Primary Field 和一个 Grid View；请求可选提交本地化的 `primaryFieldName`、`initialViewName`，未传时 Server 使用 `Name`、`Grid`。响应必须同时返回完整的 Table、Primary Field 和初始 Grid View；Map View 由用户显式创建。
- `View`、`CreateViewRequest` 和 `UpdateViewRequest` 使用 `type` 判别的 `oneOf`，Grid 与 Map 的 Config 不接受任意额外属性。P0 不允许改变已有 View 的 Type。
- 创建 View 时提交完整 Config；更新时携带 `expectedRevision` 并完整替换 Config，Server 校验、规范化后返回完整 View。缺失的可选配置表示其合同规定的未设置/默认状态，不表示沿用旧值。
- Field 更新保留顶层 PATCH 语义；`config` 省略时保留旧配置，一旦提供则完整替换。Server 必须按既有 Field Type 校验、规范化并返回完整 Field；P0 不接受 `migrationToken` 或类型变更。
- `Field`、`CreateFieldRequest` 和 `UpdateFieldRequest` 必须最终使用 `type` 判别的严格联合；更新请求回显不可变 `type`，每种 Config 拒绝未声明属性。Select Option 的创建、删除和恢复输入合同需在下一层设计中先定案，再替换 OpenAPI 当前的过渡通用 Schema。
- Grid View 的 P0 配置包含 `projection`、`columnOrder`、`columnWidths`、`frozenFieldIds`、`rowHeight`、`filter` 和 `sort`；Group 只预留，不在 P0 实现。
- Map View 配置必须包含同一 Table 内有效的 `locationFieldId`，还可包含 `filter` 和 Default Camera `center + zoom`；瓦片提供方和 Credential 仅属于客户端本地配置。创建时没有 Location Field 则不能创建；配置的 Field 被软删除或不再可用时，Map Query 返回 `422 VIEW_CONFIGURATION_REQUIRED`，不自动改选其他 Field。
- Map View 使用 `POST /v1/views/{viewId}/map/query`。请求只提交临时 Map Viewport（一个或两个不跨反经线的 WGS 84 Box）、Zoom 和 CSS Pixel 尺寸；Server 校验 Map View 并应用已保存的 Location Field 与 Filter，不接受临时覆盖。
- Map Query 最多返回 500 个 Map Point/Map Cluster，并自适应聚类直到满足上限；Feature 必须完整代表视口内全部可渲染匹配 Record，不能静默截断。聚类算法属于内部实现，不成为 API 兼容合同。
- Map Point 只返回 Record ID、坐标和服务端格式化的 Primary Field 文本；用户打开详情时再调用 `GET /v1/records/{recordId}`。Map Cluster 返回中心、范围、数量、可选展开 Zoom 和短期 Record Query Token。
- 可展开 Cluster 点击后适配其范围；到达最大 Zoom、坐标重合或没有可用展开 Zoom 时，使用 `POST /v1/views/{viewId}/map/cluster-records/query` 分页显示完整 Record。Cluster ID 和 Token 都不可持久化。
- Map Query Summary 对已保存 Filter 的全部 Active Record 返回精确且互斥的匹配、可渲染、未定位和不可渲染数量，并返回全部可渲染匹配 Record 的 Data Bounds；视口可渲染数量是其子集。跨反经线范围使用两个不跨越 Box。
- Location 仍按 `lat -90..90`、`lng -180..180` 保存；超出 EPSG:3857 纬度 `±85.0511287798066` 的合法 WGS 84 坐标保留原值但归入不可渲染数量。无效或缺失坐标归入未定位数量。

### Filter、Search 和 Operator

- FilterGroup 支持递归嵌套 `AND`/`OR`，每个 Group 至少一个子节点，最大嵌套深度为 8。空 Group 或超深结构返回 `422 VALIDATION_ERROR`。
- P0 Operator 严格按 Field Type 矩阵校验。Text/LongText/URL 支持等于、不等于、包含、不包含、前缀、后缀、为空、不为空；Number/Date 支持等于、不等于、大于、大于等于、小于、小于等于、为空、不为空；Checkbox 支持等于、不等于；Select 支持等于、不等于、为空、不为空；MultiSelect 支持包含、不包含、为空、不为空；Location 只支持为空、不为空。
- Search 只在 Primary Field、Text、LongText 和 URL 中执行服务端不区分大小写的包含匹配。

### 元数据恢复和 Primary Field

- Table、Field 和 View 提供显式 Restore API。删除父对象只软删除父对象；子对象保持自身状态和 Revision，由祖先状态决定可见性；恢复父对象后，未被单独删除的子对象重新可见。
- P0 不允许删除 Primary Field，也不允许把其类型改为非 `text`；只允许重命名和安全配置修改。

### 幂等和 Change 保留

- Record Mutation 的幂等键按 `actorId + clientMutationId` 唯一。
- 相同请求体重复使用该键返回历史结果；不同请求体复用该键返回 `409 IDEMPOTENCY_KEY_REUSED`。
- Workspace、Base、Table、Field 和 View 的创建使用必填 `Idempotency-Key: mut_...` Header，并以 Method、Path 和规范化 Body 判定是否为同一请求；Key 在 Actor 范围内跨这些资源全局唯一。
- 当前版本幂等结果保留 30 天；后续版本允许配置 `30d`、`90d`、`365d` 或 `forever`。
- Change Cursor 按 Table 作用域保存，Change 当前保留 30 天；无 Cursor 时返回当前尾部位置和空 ChangePage。后续版本允许同样的保留策略选项。

### Record 更新、回收站和 Personal 认证

- Record 更新使用 `set + unsetFieldIds`。`set` 可显式写入 Field Type 允许的 `null` 或空值；只有 `unsetFieldIds` 删除键，未提及的 Field 不变。P0 九种 Field 都允许 `null`；Text/LongText 的 `""` 与 MultiSelect 的 `[]` 是自然空值，空 URL 字符串和空 Location 对象无效。Conflict 必须分别回显客户端提交的 Set 与 Unset 意图。
- Primary Field 可以为 Unset、`null` 或 `""`；Server 不因其为空拒绝创建 Record，也不持久化“无标题”占位。Plugin 在需要标签时使用本地化占位或 Record ID 回退。
- Table、Field、View List 与 Record Query 的 Lifecycle Scope 是 `active`、`deleted` 或 `all`，默认 `active`；Record Query 会把它纳入 Cursor 绑定。`deleted` 按对象自身的软删除状态筛选，不把仅因祖先删除而暂时不可见的子对象改写为已删除。
- Personal Actor、Token 哈希与撤销状态持久化在 PostgreSQL。首次 Bootstrap 或后续管理命令显式创建/管理多个具名 Token；各 Token 独立撤销，P0 不设置到期时间。普通 Server 启动不会隐式创建、替换或轮换。运行时认证只查询数据库中未撤销的 Token，Actor ID 在 Token 更换、重启和备份恢复后保持稳定。

### 后续字段扩展

- P0 只实现现有九种字段：Text、LongText、Number、Checkbox、Date、Select、MultiSelect、URL、Location。
- 后续增加独立 `Region` Field Type，使用版本化的多级行政区目录和稳定 Region Code；Region 与 Location 分离。
- 后续增加 `DateTime` 和 `Time` Field Type；两者都支持单值和范围模式。DateTime 保存 UTC，Time 保存不带日期的本地时刻。
- 后续为 Location 增加 `geoWithin` 查询操作，先支持矩形和圆形，Polygon 后置，并允许作为 View Filter 保存。
- 手机号、身份证号等作为 Text 的区域化 Validation Preset；Currency、Percent 作为 Number 格式；Rating、Duration、User 等独立语义类型后续再评估。
