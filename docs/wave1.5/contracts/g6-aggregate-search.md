# G6 Contract — Aggregate Filter + Search

2026-10-12。按用户已批准的 Wave 1.5 G6 语义与 Server 工程授权实施。范围仅为既有 Aggregate 加入 Record Query 同域 Search；不依赖 G2–G5，不新增聚合引擎或 endpoint。

## 领域与传输合同

既有 `POST /v1/tables/{tableId}/records/aggregate` 的 `AggregateRequest` 增加可选 `search: string`，OpenAPI 从 0.3.0 升至 0.4.0；`additionalProperties: false`、fieldIds/fns/filter 和响应结构不变。

- 省略、空串或纯 Unicode 外围空白表示无 Search，保留旧行为。null、数字、布尔、数组、对象仍是 422 VALIDATION_ERROR（/search, type）；字符串按既有 `normalizeSearch` 处理：Unicode Trim → NFC → locale-neutral Default Case Folding，NFC/Trim 后最多 500 Unicode 码点，超限 422 VALIDATION_ERROR（/search, limit）。
- OpenAPI 使用 `x-loomtable-normalized-max-length: 500` 与描述表达规范化后的上限，不以原始字符串的 JSON Schema maxLength 限制合法输入；外围空白或 decomposed Unicode 可使原始字符串超过 500 码点。既有 Query schema 的原始 maxLength 声明有同一历史偏差，本 Gate 不扩大修改范围，Client 应沿用真实 Query 规范化语义。
- Search 与 Record Query 使用相同 `buildRecordWhere` / `strpos(records.search_text, normalizedSearch) > 0`，Filter 与 Search 取交集。百分号、下划线是普通字符；大小写折叠且 accent-sensitive。
- 搜索投影保持当前合同：Primary/Text/LongText/URL 的文本值；不按请求 fieldIds、显示列、排序或已加载页面收窄。Field 名称和 Select/MultiSelect Option 名称不属于当前搜索投影，改名不新增匹配；本 Gate 不扩展 Choice 搜索。
- 仅当前 Actor 可访问、未删除 Workspace/Base/Table 内的 active Records；deleted Records 排除。继承既有 Filter 校验、外来/删除 Field 拒绝和 50 Field 上限，不修改权限。
- count 为非空值的 Record 数，零有效值返回 0；sum/avg 仅 Number，min/max 仅 Number/Date；没有有效值或不支持的函数返回 null；0 是有效 Number，不与 null/unset 混淆。
- 每次 Aggregate 内所有请求 Field 及 changeCursor 共用既有只读 REPEATABLE READ 事务快照。独立 Query 与 Aggregate 请求可能在并发写入之间观察不同快照；本 Gate 保证相同成员条件与每次响应内一致性，不承诺跨请求固定快照。Query selection snapshot 由 G4 单独定义。
- 只读请求不创建 receipt，不改变 Revision/CAS、Idempotency-Key、Durable Mutation Queue 或 replay 协议。

## Capability、兼容与回滚

实现二进制 `/v1/meta` 无条件声明 `aggregateSearch`，与 Attachment 开关无关。旧 Client 省略 search，结果不变；未知 capability 由现有 string-array decoder 接受。新 Client 必须观察该 capability 才能提交带 search 的 Aggregate；旧 Server 会拒绝未知 search 字段，不能把 filter-only 或 loaded-page 结果显示为 Search 全范围聚合。空 Search 时可继续既有请求。

Plugin 按固定交付 SHA 同步 OpenAPI/生成可选 string，接入 HTTP/InMemory/Controller 的当前 Search 与 generation/cache guards；late Aggregate 不得覆盖新查询。具体 UI 与请求生命周期由 Plugin 规划工作线实施。

零 migration/回填，继承既有 search_text/query_values。回滚代码与镜像时 capability 消失；新 Client 应将 Search 下聚合标为 unavailable，保留数据与旧的无 Search 聚合。G1 的新色降级限制仍适用，回滚 G6 优先退至已交付 G1 镜像。

## 实施与验收

1. OpenAPI / DTO / HTTP optional string → 服务复用 normalizeSearch → AggregatePlan.Search → storage 复用 QueryPlan.Search。
2. 单元验证 omission/Trim/NFC/Folding/500 码点/超限；HTTP 验证 typed decoding/422/Meta；合同校验包含 Search 规范与 capability。
3. 专用 disposable PostgreSQL 验证 Filter+Search、仅 Search、空 Search、Unicode/URL/LongText/字面 %_、Options/Field rename 边界、active/deleted、完整 Query membership 超过一页、所有函数的 number/date/null/0 与权限。
4. 并发事务同时更新两个 Number Field，聚合必须始终观察同一值，且 changeCursor 对应该值的真实 Mutation；不只测试 HTTP happy path。
5. 完整 Go/HTTP/合同/数据库测试、vet、Docker/Compose CI、独立 PR 审查 → 安全发布 → 固定 SHA/Meta/证据交接 Plugin。生产不写合成 QA 数据。

Server 交付事实维护在本 Gate delivery 文档与 PR；跨仓库状态仍由 Plugin 唯一 Wave 状态表维护。
