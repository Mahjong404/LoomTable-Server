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
→ QueryResult + hasMore + nextCursor（仅续页存在）+ changeCursor + 首页面 totalCount
```

约束：

- Table、Field、View List 与 Record Query 使用统一 Lifecycle Scope：`active`、`deleted` 或 `all`，默认 `active`；该范围按对象自身的软删除状态筛选。P0 不删除 Workspace/Base，因此其 List 不接受该参数。
- Workspace、Base、Table 和 View List 的稳定默认顺序是 `createdAt ASC, id ASC`；Field List 使用 `position ASC, id ASC`。名称不参与默认排序。
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
- PostgreSQL 中未撤销的 Token 哈希是认证事实来源。最终 P0 的 Bootstrap/Create 都只生成随机 Secret，不接受调用者通过参数、stdin 或环境变量指定 Secret；普通启动不把环境变量哈希当作旁路，也不隐式创建或轮换 Token。
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

所有 P0 Request、Command、Filter 和 Config 都拒绝未声明属性。Field/View 创建的父级身份只来自嵌套路由中的 `tableId`，Body 不重复提交。资源名称先去除首尾 Unicode 空白并规范化为 NFC，再拒绝控制字符、空名称和超过 200 个 Unicode 码点的名称；同一父对象内允许同名，身份始终由 ID 决定。

Conflict 使用 `409`；认证、授权、输入校验、资源不存在、能力未启用和内部故障必须分别使用可识别的 HTTP 状态和错误码，不能全部折叠为同一个默认错误。

### 健康、认证和备份

- `/healthz` 是无需认证的进程存活检查。
- `/readyz` 是无需认证的可接收请求检查，至少检查数据库连接和迁移状态。
- 业务 API 继续要求 Bearer Token。
- P0 的初始 Token 由显式 Bootstrap/管理命令随机创建；调用者不能指定 Secret。Server 只保存 Token 哈希，不提供公开的 Token 生成 API，也不对 localhost 免认证。同一稳定 Actor 可以拥有多个具名 Active Token，各自独立撤销；P0 Token 不设置到期时间。
- P0 的备份/恢复通过 PowerShell/Bash Docker Compose 脚本完成，必须同时覆盖 PostgreSQL custom-format dump、Managed Attachment 文件卷、版本清单和校验和；转为 Ready 前必须在真实 Docker/PostgreSQL 环境完成脚本验收。

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
| 410 | Cursor 或短期查询快照已过期/失效 | `CURSOR_EXPIRED`, `QUERY_SNAPSHOT_EXPIRED` |
| 413 | JSON 请求体超过 8 MiB | `PAYLOAD_TOO_LARGE` |
| 415 | 请求媒体类型或 Content-Encoding 不受支持 | `UNSUPPORTED_MEDIA_TYPE` |
| 422 | 字段值、Filter、Operator、View 配置或其他领域语义无效 | `VALIDATION_ERROR`, `UNSUPPORTED_OPERATOR`, `VIEW_CONFIGURATION_REQUIRED` |
| 501 | 能力存在于合同但当前 Server 未启用 | `CAPABILITY_NOT_ENABLED` |
| 503 | Server 尚未就绪或依赖不可用 | `MIGRATION_REQUIRED`, `DEPENDENCY_UNAVAILABLE` |
| 500 | 未预期的服务端错误 | `INTERNAL_ERROR` |

错误码、HTTP 状态和 `requestId` 是 Client Adapter 的稳定映射依据；Server 不把上述情况折叠为单一默认错误。

### Personal 认证

P0 不提供公开 Token 生成 API，也不因使用 localhost 而免认证。一个 Personal Server 有一个持久化的稳定 Actor；显式 Bootstrap/管理命令创建或管理与其关联的多个具名 Token，数据库保存 `tok_...` ID、名称、不可逆哈希及撤销状态。每个 Token 可独立撤销，P0 不设置到期时间。Token 更换、重启和恢复不改变 Actor ID，普通启动不会隐式轮换凭据。P0 不实现用户登录、Workspace 邀请、角色或细粒度权限。

### Query、Filter 和 Cursor

- P0 Filter 严格按照 Field Type Registry 校验 Operator 和值类型。
- `null`、缺失值和空字符串保持区分；文本匹配使用 NFC 和 locale-neutral Unicode Default Case Folding，不区分大小写但区分重音。
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
- `Field`、`CreateFieldRequest` 和 `UpdateFieldRequest` 使用 `type` 判别的严格联合；更新请求回显不可变 `type`，每种 Config 拒绝未声明属性。Select Option 使用期望 Active 列表完成创建、更新、软删除与恢复，OpenAPI 不再保留通用 Config 过渡 Schema。
- Text、LongText、Number、Checkbox、Date、URL 和 Location 的 P0 Config 固定为严格空对象 `{}`；Select/MultiSelect Config 只包含 Option 生命周期数据，不接受显示或校验扩展属性。
- Select/MultiSelect 更新提交期望的 Active Option 列表：无 ID 表示新建，当前 Field 的 Active ID 表示更新，Deleted ID 表示恢复，遗漏的 Active Option 软删除。响应分别返回 Active 与 Deleted Option；Active 数组顺序就是显示顺序。Option ID 由 Server 生成，其他 Field 的 ID 或未知 ID 返回 `422 VALIDATION_ERROR`。
- Grid View 的 P0 配置包含 `projection`、`columnOrder`、`columnWidths`、`frozenFieldIds`、`rowHeight`、`filter` 和 `sort`；Group 只预留，不在 P0 实现。
- 初始 Grid View 仅投影并排列 Primary Field，`columnWidths` 和 `frozenFieldIds` 为空、无 Filter、Sort 为空、`rowHeight` 为 `standard`。行高只允许 `compact`、`standard`、`comfortable`；显式列宽是 80–1000 CSS px。
- Map View 配置必须包含同一 Table 内有效的 `locationFieldId`，还可包含 `filter` 和 Default Camera `center + zoom`；瓦片提供方和 Credential 仅属于客户端本地配置。创建时没有 Location Field 则不能创建；配置的 Field 被软删除或不再可用时，Map Query 返回 `422 VIEW_CONFIGURATION_REQUIRED`，不自动改选其他 Field。
- Map View 使用 `POST /v1/views/{viewId}/map/query`。请求只提交临时 Map Viewport（一个或两个不跨反经线的 WGS 84 Box）、Zoom 和 CSS Pixel 尺寸；Server 校验 Map View 并应用已保存的 Location Field 与 Filter，不接受临时覆盖。
- Map Query 最多返回 500 个 Map Point/Map Cluster，并自适应聚类直到满足上限；Feature 必须完整代表视口内全部可渲染匹配 Record，不能静默截断。聚类算法属于内部实现，不成为 API 兼容合同。
- Map Point 只返回 Record ID、坐标和服务端格式化的 Primary Field 文本；用户打开详情时再调用 `GET /v1/records/{recordId}`。Map Cluster 返回中心、范围、数量、可选展开 Zoom 和短期 Record Query Token。
- 可展开 Cluster 点击后适配其范围；到达最大 Zoom、坐标重合或没有可用展开 Zoom 时，使用 `POST /v1/views/{viewId}/map/cluster-records/query` 分页显示完整 Record。Cluster ID 和 Token 都不可持久化。
- Map 全局 Summary 对已保存 Filter 的全部 Active Record 返回精确且互斥的匹配、可渲染、未定位和不可渲染数量，并返回全部可渲染匹配 Record 的 Data Bounds；Viewport Query 单独返回的视口可渲染数量是其子集。跨反经线范围使用两个不跨越 Box。
- 全局精确 Summary 和 Data Bounds 由独立的 `POST /v1/views/{viewId}/map/summary` 返回，只在首次打开、保存的 Filter 变化或用户显式“适配全部”时请求。普通 Map Viewport Query 只返回当前视口 Feature、视口可渲染数量、View Revision 和 Change Cursor，不在每次平移/缩放时重复全局聚合。
- Map Cluster Record Query Token 有效 5 分钟，并绑定 View Revision、Location Field、保存的 Filter、原 Map Query 和当时的 Table Change Cursor。任何相关变化或超时都返回 `410 QUERY_SNAPSHOT_EXPIRED`；客户端刷新视口。P0 不保存大型 Record ID 快照，也不在翻页时实时漂移成员。
- Location 仍按 `lat -90..90`、`lng -180..180` 保存；超出 EPSG:3857 纬度 `±85.0511287798066` 的合法 WGS 84 坐标保留原值但归入不可渲染数量。无效或缺失坐标归入未定位数量。

### Filter、Search 和 Operator

- FilterGroup 支持递归嵌套 `AND`/`OR`，每个 Group 至少一个子节点，最大嵌套深度为 8。空 Group 或超深结构返回 `422 VALIDATION_ERROR`。
- P0 Operator 严格按 Field Type 矩阵校验。Text/LongText/URL 支持等于、不等于、包含、不包含、前缀、后缀、为空、不为空；Number/Date 支持等于、不等于、大于、大于等于、小于、小于等于、为空、不为空；Checkbox 支持等于、不等于；Select 支持等于、不等于、为空、不为空；MultiSelect 支持包含、不包含、为空、不为空；Location 只支持为空、不为空。
- Search 只在 Primary Field、Text、LongText 和 URL 中执行服务端包含匹配；输入去除首尾 Unicode 空白，规范化后为空表示不搜索。匹配使用 NFC 和 locale-neutral Unicode Default Case Folding，区分重音。Field Filter 的文本 Operand 不自动 trim。

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
- Unknown、属于其他 Table 或 Deleted Field 不能写入，返回 `422 VALIDATION_ERROR`。Deleted Option 不得被新引入，但同一 Record 已保存的 Deleted Option 引用可以原样保留；删除 Field/Option 不清除历史值，恢复后重新可用。Server 不静默删除无效输入。
- Text 最多 10,000、LongText 最多 100,000 个 Unicode 码点；URL 最多 2,048 个字符且必须是绝对 HTTP/HTTPS URL；Number 必须是有限 IEEE-754 数；Date 必须是有效 Gregorian `YYYY-MM-DD`；MultiSelect 最多 100 个互异 Option ID。Location 的 `label`、`address`、`provider` 分别最多 500、2,000、100 个 Unicode 码点。
- Table、Field、View List 与 Record Query 的 Lifecycle Scope 是 `active`、`deleted` 或 `all`，默认 `active`；Record Query 会把它纳入 Cursor 绑定。`deleted` 按对象自身的软删除状态筛选，不把仅因祖先删除而暂时不可见的子对象改写为已删除。
- Personal Actor、Token 哈希与撤销状态持久化在 PostgreSQL。首次 Bootstrap 或后续管理命令显式创建/管理多个具名 Token；各 Token 独立撤销，P0 不设置到期时间。普通 Server 启动不会隐式创建、替换或轮换。运行时认证只查询数据库中未撤销的 Token，Actor ID 在 Token 更换、重启和备份恢复后保持稳定。

## 14. Q136–Q148 已确认的实现边界

- CreateField/CreateView Body 不重复 `tableId`；嵌套路由参数是唯一父级身份来源。
- 所有 P0 Request、Command、Filter 和 Config 对额外属性关闭；错误拼写不得被忽略。
- 名称执行 Unicode Trim + NFC + 控制字符/空值/200 码点校验；同一父对象内允许同名。
- P0 Field 使用严格 `type` 判别联合；七种无配置 Field 使用 `{}`，Select/MultiSelect 使用完整 Option 生命周期合同。
- `isEmpty` 匹配 Unset、`null` 及该类型的自然空值，`isNotEmpty` 是其反集；存储层仍保留三者差异。
- 初始 Grid、Deleted Field/Option 写入限制、Cell 值上限和元数据 List 顺序按本章前述规则执行。
- 本机管理程序提供 `loomtable-admin auth bootstrap/create/list/revoke`。Token Secret 默认使用 `ltp_` 加 32 字节 CSPRNG Base64URL，明文仅显示一次，数据库保存 SHA-256；`tok_...` 是元数据 ID，不是 Secret。
- Map 全局 Summary 与 Viewport Query 分离；Cluster Token 使用 5 分钟、View/Table Change 绑定的无状态失效合同。

## 15. Q149–Q170 已确认的实现边界

### Select、Location 与 Field 顺序

- Select Option 的必填语义颜色固定为 `gray`、`red`、`orange`、`yellow`、`green`、`cyan`、`blue`、`purple`、`pink`；客户端按当前主题映射，Server 不保存 CSS 颜色。
- Option 名称执行 Unicode Trim、NFC、控制字符拒绝和 100 码点上限。Active Option 名称按 locale-neutral Unicode Default Case Folding 后唯一；Deleted Option 可以与 Active Option 同名。单个 Field 最多 500 个 Active Option、5,000 个 Active + Deleted Option；Deleted Option 按 `deletedAt ASC, id ASC` 返回。
- Location 的 `label`、`address`、`provider` 分别限制为 500、2,000、100 个 Unicode 码点，并执行 Unicode Trim、NFC 和控制字符拒绝。规范化后为空的成员被省略；省略后必须至少保留 `label`、`address`、`provider` 或完整经纬度对之一，`precision` 不能单独构成有效 Location。
- 新 Field 的 `position` 为当前最大位置加一；删除不回收位置，恢复沿用原位置。P0 不提供 Schema Field 重排 API；Grid `columnOrder` 只改变该 View 的视觉顺序。

### 元数据、直接读取和容量

- P0 元数据列表保持完整、不分页并继续使用稳定顺序，同时执行硬上限：每 Actor 100 Workspace、每 Workspace 500 Base、每 Base 500 Table、每 Table 500 Field、100 View。达到上限后创建返回 `422`，不会静默截断 List。
- Workspace 和 Base 增加直接 GET 与 PATCH rename，响应包含 `revision`，PATCH 必须携带 `expectedRevision`。P0 不为二者提供删除和恢复。
- 直接 GET Table、View、Record 时，只要祖先仍 Active 且可访问，就可以返回目标自身的 Recycle State 及 `deletedAt`；Deleted 祖先隐藏后代并返回 `404`。Restore API 可以直接访问待恢复目标。
- 格式错误的类型化 ID 返回 `400`；格式正确但不存在、属于其他 Actor、不可访问或被祖先隐藏的 ID 返回 `404`。`403` 只表示明确授权策略拒绝，不用于泄漏资源存在性。

### Record Query、Filter 和 Sort

- 普通 Record Query 使用有效期 30 分钟的无状态 Keyset Cursor，绑定完整等价 Query 和 API 版本。它不是 Query Snapshot，也不维持数据库快照；并发写入可能影响后续页，客户端通过 `changeCursor` 判断是否失效并在需要一致性时重新开始。结构不匹配返回 `400 INVALID_CURSOR`，过期返回 `410 CURSOR_EXPIRED`。
- `QueryResult.hasMore` 必填；只有 `hasMore=true` 才返回 `nextCursor`。`totalEstimate` 被精确的 `totalCount` 取代，并且只在未提交 Cursor 的第一页返回。
- Filter 最大深度 8、总节点数 100；Sort 最多 10 个互异 Field；Projection 最多 500 个互异 Field；Search 最长 500 个 Unicode 码点；JSON 请求体最大 8 MiB，超过时返回 `413 PAYLOAD_TOO_LARGE`。Record 页大小仍为 1–500。
- Text、LongText、URL 以 NFC + Unicode Default Case Folding 后的 locale-neutral、区分重音键排序；Number 按数值，Date 按日期，Checkbox 为 `false < true`；Select 按 Active Option 显示顺序，Deleted Option 排在其后并以稳定 ID 排序。MultiSelect 和 Location 在 P0 不可排序，返回 `422`。Unset 与 `null` 共用指定 null bucket，自然空值仍是真实可排序值；最终总是追加 `Record.id ASC` 作为稳定 Tie-breaker。
- 保存的 View 中 projection/filter/sort/location 等查询语义引用指向 Deleted 或不可用 Field 时，Query 返回 `422 VIEW_CONFIGURATION_REQUIRED` 并列出失效 Field ID，不自动修复 View。纯展示元数据中的过时引用可以保留但忽略；恢复 Field 后查询配置重新有效。Ad-hoc Query 的 Foreign/Deleted Field 引用返回 `422 VALIDATION_ERROR`。

### Mutation 状态合同

- 一个 MutationRequest 不得包含多个指向同一已有 Record 的 Command；违反时整个请求返回 `422`。每个成功 Command 都返回完整操作后 Record，Delete 返回带 `deletedAt` 的 Tombstone；MutationResult 必须返回最终 `changeCursor`。
- `expectedRevision` 始终先校验。规范化后的目标状态与当前状态相同则返回 `unchanged`，不增加 Revision，也不写 Change。对已删除对象再次 Delete、对 Active 对象 Restore 均是 `422` 非法状态转换；重试语义由幂等键保证。

### 认证、部署、空间查询与运维

- `loomtable-admin auth bootstrap/create/list/revoke` 的 Token 名称执行 Unicode Trim、NFC、控制字符拒绝和 100 码点上限；同一 Actor 的 Active Token 名称按 Unicode case-fold 唯一。`bootstrap --name` 只在尚未初始化时创建 Actor 和首个 Token，已初始化时只报告状态且不显示 Secret；`create --name` 始终生成 `ltp_` 加 32 字节 CSPRNG Base64URL Secret，不接受调用者提供的 Secret；`list` 只返回元数据；`revoke` 使用 `tok_...` ID，并允许撤销最后一个 Token。
- 原生 Server 默认只监听 `127.0.0.1:31201`；Compose 容器监听 `:31201`，只发布 `127.0.0.1:31201:31201`。局域网或远程访问必须显式覆盖监听地址并使用 TLS 反向代理或可信内网；Obsidian Adapter 不依赖浏览器 CORS。
- P0 不引入 PostGIS。Location 使用 WGS 84 JSONB，通过数值提取完成矩形视口查询和应用层聚类；以 20k Record 基准决定是否添加表达式或投影索引。`geoWithin`/Polygon 等后续范围能力再重新评估 PostGIS。
- P0 提供 PowerShell 与 Bash Docker Compose 备份/恢复脚本。一个版本化归档包含 PostgreSQL custom-format dump、Attachment Volume、Server/Schema/时间 Manifest 和校验和；验证脚本检查归档，恢复要求 Server 停止并显式确认；转为 Ready 前必须完成真实环境验收。
- Server 启动时及其后每 24 小时执行有界后台清理，移除超过配置保留期的 Change 和幂等记录；`forever` 禁用清理，不依赖外部 Scheduler。
- Map Cluster Record Query 返回完整 Record，并固定按 `createdAt ASC, id ASC`；Cluster Token 和后续 Cursor 绑定该顺序。

### P0 合并门槛

Server P0 PR 仅在没有待定合同标记，全部 P0 路由和业务模块、Migration、认证管理、备份恢复、OpenAPI Contract Test、PostgreSQL 集成测试、20k Query/Map 基准、Docker Smoke/Backup Restore、Go Test/Vet 全部通过后转为 Ready。随后通过 GitHub PR 合并到 `main` 并删除开发分支；在此之前保持 Draft，不提前合并设计骨架。

## 16. Q171–Q185 已确认的实现边界

### 元数据刷新、所有权和 No-op

- P0 不增加 Actor 级 Metadata Change Stream，也不把 Workspace/Base 变化扇出到后代 Table Change。Mutation 发起端使用响应更新缓存；其他客户端在进入导航、手动刷新或导航可见时低频重新拉取已有硬上限的元数据列表。
- Workspace、Base、Table、Field、View PATCH 与 Record Update 都先校验 `expectedRevision`。规范化结果与当前状态相同时，返回当前对象、Revision 不变且不写 Change；Record Command 的状态为 `unchanged`。Metadata PATCH 不增加额外 Wrapper 或 Header。
- Personal 中 Workspace 由创建它的 Actor 持有，Base、Table、Field、View 和 Record 继承祖先访问边界。P0 不实现资源 ACL 或跨 Actor 移动；格式正确但属于其他 Actor 的对象统一返回 `404`。未来 Team 再迁移到 Membership 模型。

### 错误和 JSON 解码

- `VALIDATION_ERROR.details.issues` 使用按 JSON Pointer 排序的 `{ path, code, message? }`，稳定 Issue Code 为 `required`、`type`、`format`、`unknownProperty`、`duplicate`、`limit`、`invalidReference`。
- 容量上限使用 `422 RESOURCE_LIMIT_EXCEEDED`，Details 为资源类型、父级类型/ID 和 Limit；重复 Delete/Restore 等非法动作使用 `422 INVALID_STATE_TRANSITION`，Details 为资源类型/ID、动作和当前状态。
- 不支持的排序使用 `422 UNSUPPORTED_SORT`；不支持的 Filter Operator 继续使用 `422 UNSUPPORTED_OPERATOR`。`VIEW_CONFIGURATION_REQUIRED` 返回 View ID 和按 ID 排序的失效 Field ID；`PAYLOAD_TOO_LARGE` 返回固定 `limitBytes: 8388608`。Cursor 错误不返回解码后的 Token 内容。
- JSON Endpoint 只接受 UTF-8 `application/json`，Charset 只能省略或为 UTF-8；P0 只接受 Identity Content-Encoding。Server 在解码前限制原始 Body 为 8 MiB，拒绝重复 Object Key、多个顶层 JSON 值和非 JSON Media Type；不支持的媒体类型或压缩返回 `415 UNSUPPORTED_MEDIA_TYPE`。

### Query Cursor、读取一致性和排序

- 普通 Query Cursor 与 Map Cluster Token 使用数据库持久化的 32 字节 CSPRNG Key、版本化 Base64URL Envelope 和按用途隔离的 HMAC-SHA256。载荷绑定 Actor、Route、Query 指纹、位置、签发/到期时间等所需状态；Key 随数据库备份恢复，不通过 API 暴露。
- 普通 Cursor 还绑定 View Revision、被引用 Field Revision 和 Query Schema Fingerprint。Record 数据变化不会直接使其失效；查询语义 Schema/View 变化返回 `410 CURSOR_EXPIRED`。格式、签名或调用方提交的等价 Query 不匹配返回 `400 INVALID_CURSOR`。
- 每一页使用一个短期 Read-only Repeatable Read Transaction，令 Records 与该响应的 `changeCursor` 来自同一数据库快照；第一页在同一事务内取得精确 `totalCount`。事务不会跨 HTTP 请求存活，因此普通 Cursor 仍不是 Query Snapshot。
- Select Sort 先应用 `nulls` 桶；非空值中 Active Option 始终在 Deleted Option 之前。`asc/desc` 只改变 Active 和 Deleted 各自桶内顺序，不把 Deleted Option 提到 Active 之前。
- Canonical `Record.values` 之外，Repository 在同一 Mutation Transaction 中维护可重建的内部 `query_values JSONB` 与 `search_text`。Go 生成 Unicode Case-Fold、类型化及坐标查询键；Select 排序从当前 Field Config 解析 Option Rank，不因重排而改写全部 Record。派生投影损坏时可从 Canonical Values 重建，且不出现在 API Response。

### Bootstrap、保留期、备份和 Migration

- `/readyz` 只表示数据库、Migration 和必要存储是否可服务，不因尚未 Bootstrap 而失败。公开 `/v1/meta` 必须返回 `bootstrapState: required | complete | unknown`，不公开 Active Token 数量；无 Actor 为 `required`，已有 Actor 即使没有 Active Token 也是 `complete`，数据库状态无法确定时为 `unknown`。
- 保留期清理使用 PostgreSQL Advisory Lock 和数据库时钟计算 Cutoff。启动时及每 24 小时，每种数据最多执行 10 批、每批 10,000 条短事务删除，并记录删除数和耗时；`forever` 不获取 Lock、不执行清理。
- P0 备份归档格式为跨平台 `.tar.gz`，拒绝覆盖已有输出，包含 SHA-256 Manifest。归档默认不加密，但脚本创建受限权限并提示其中包含 Token Hash；Restore 拒绝运行中的 Server 和非空目标，只有显式 `--confirm` 才继续。
- 首个公开 P0 发布前，Schema 调整折叠进 `001_initial.sql`，开发数据库视为可重建。发布后冻结 001，只追加顺序 Forward Migration，普通 Server 启动仍不自动执行 Migration。

### 性能验收环境

- 参考环境为 4 vCPU、8 GiB、本地 Compose 和热缓存；每项 5 次预热后测量 30 次。常规 Query p95 不超过 500 ms，Search/复合 Filter+Sort 与 Map Viewport p95 不超过 750 ms，Map Summary p95 不超过 1.5 s，单条 Mutation p95 不超过 250 ms，100-command Mutation p95 不超过 1.5 s。

### 后续字段扩展

- P0 只实现现有九种字段：Text、LongText、Number、Checkbox、Date、Select、MultiSelect、URL、Location。
- 后续增加独立 `Region` Field Type，使用版本化的多级行政区目录和稳定 Region Code；Region 与 Location 分离。
- 后续增加 `DateTime` 和 `Time` Field Type；两者都支持单值和范围模式。DateTime 保存 UTC，Time 保存不带日期的本地时刻。
- 后续为 Location 增加 `geoWithin` 查询操作，先支持矩形和圆形，Polygon 后置，并允许作为 View Filter 保存。
- 手机号、身份证号等作为 Text 的区域化 Validation Preset；Currency、Percent 作为 Number 格式；Rating、Duration、User 等独立语义类型后续再评估。
