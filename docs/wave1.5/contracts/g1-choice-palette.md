# G1 Contract — 12-Color Choice Palette Server Writer

**Status: APPROVED FOR IMPLEMENTATION(2026-10-12)。** 用户已批准本文的 G1 合同方向并授权 Server 实现(实现分支 `feature/g1-choice-palette12`)。批准范围限 D1–D5 记录的决策;**实现 ≠ 合并、部署或 Plugin 启用**——PR 合并、生产部署与 Plugin 三色 writer 仍为三个独立待授权动作。在实现交付前,权威传输合同以 `docs/api/openapi.yaml`(v0.2.0,9 色 enum)为准;同分支实现提交落地 v0.3.0/12 色后,以该分支 OpenAPI 为准。

- Gate:G1 — 12-color write compatibility(Server writer)
- Date:2026-10-12(R2 复核增补:Plugin 兼容代码证据、Rollback/Capability 合同、Storage 复核;R3 批准基线)
- Review baseline:Server `5d430cd641e5a210eab3371c9864919837cd69a9`(main,文档入口已发布);OpenAPI v0.2.0;Plugin `bde37b9`(W15-02 后工作树);Proposal `c99c3fb96f4e9610abcb72ece19fb76a3534c8f8`
- 上游设计:Plugin `docs/ui/design-system.md` 批准色板;Plugin `docs/wave1.5/api-boundary.md` G1 节
- 范围:仅 Server writer 侧接受三个新增语义色 Token。不改 Record 值模型、选项生命周期、排序、Distinct、History 或任何其他 Gate。

## 1. 数据与接口语义

### 1.1 完整 12 色 Token(D4 已定)

以 Plugin Design System 批准表为准,Token 名称与顺序:

`gray, red, orange, yellow, lime, green, cyan, blue, indigo, purple, pink, brown`

新增三个:`lime`、`indigo`、`brown`。不允许 CSS 色值、其他字符串或大小写变体;writer 保持大小写敏感的精确匹配(与现行 `validOptionColor` 一致)。`color` 是必填字段,无默认值语义——缺失仍 `422 VALIDATION_ERROR`(`required`),非法值 `422`(`format` / `unsupported option color`)。

### 1.2 适用对象

- `select` 与 `multiSelect` 两种 Field Type 共用同一 enum,不加区分。
- `Active Option` 与 `Deleted Option`(tombstone)共用 `SelectOptionColor`。删除不重置颜色;tombstone 原样保留写入时的 Token,供历史 Record 引用与恢复显示。
- Record 值只引用 Option ID,从不携带颜色;Record Mutation、Filter、Sort(option rank)、Distinct Values 语义不变。

### 1.3 受影响的 writer 路径(全部枚举)

| 路径 | 当前行为 | G1 后行为 |
| --- | --- | --- |
| `POST /v1/tables/{tableId}/fields`(create select/multiSelect,`config.options[].color`) | `normalizeNewSelectConfig` → `validOptionColor` 9 色(`internal/catalog/service.go:742`) | 同一校验点接受 12 色 |
| `PATCH /v1/fields/{fieldId}`(`config.options[]` 期望 Active 列表:无 ID 创建、有 ID 改名/换色/重排、Deleted ID 恢复、遗漏 Active ID 软删) | `normalizeUpdatedSelectConfig` → `validOptionColor` 9 色(`service.go:866`) | 同一校验点接受 12 色;恢复 Deleted Option 提交的 `color` 同样走新 enum |
| `POST .../convert`(text→select `distinctOptions` 自动建选项) | `convert.go:641` 内置 9 色轮播 palette 自动赋色 | **D1 已定:保持 9 色轮播不变**;新色仅经显式 writer 写入 |
| Record Mutation / move / duplicate | 不含颜色 | 不变 |
| Idempotent replay / stored response | 原样返回首次规范化结果(`idempotency_keys.response` JSONB) | 不变;含新色的已存响应 replay 原样返回 |

### 1.4 Server Validation 语义(D5 已定)

改动面收敛为单点:`validOptionColor`(`internal/catalog/service.go:934`)的允许集合从 9 扩至 12。Go 侧采用**单一允许集合**(常量表)供 create/update 两处调用点引用,并新增合同测试与 OpenAPI `SelectOptionColor` enum 对账,消除双源漂移(当前 `openapi_contract_test.go` 不含此项对账,属新增)。`color` 不参与 name 唯一性;`normalizedInput` 继续回显规范化后的 name+color 进入幂等 Hash,机制不变。

### 1.5 既有 9 色数据

零语义变化。存量 `fields.config` JSONB 中的 9 色值全部保持合法;读取、编辑、删除、恢复路径不变。

## 2. OpenAPI 变更草案(未应用)

仅 enum 扩展与 capability 声明,无 endpoint、形状或错误码变化:

```diff
 info:
   title: LoomTable Server API
-  version: 0.2.0
+  version: 0.3.0            # D3 已定;API 路由保持 v1

     SelectOptionColor:
       type: string
-      enum: [gray, red, orange, yellow, green, cyan, blue, purple, pink]
+      enum: [gray, red, orange, yellow, lime, green, cyan, blue, indigo, purple, pink, brown]
       description: Required Server-defined semantic palette token. Clients map the token to their active theme; CSS color values are never stored.

     ServerMeta.properties.capabilities:
       type: array
-      description: Enabled server capabilities. P1 may include attachments when the Attachment module and its file volume are available.
+      description: Enabled server capabilities. P1 may include attachments when the Attachment module and its file volume are available. A server that accepts the extended 12-color SelectOptionColor writer declares `choicePalette12`.
       items:
         type: string
```

引用面:`SelectOptionColor` 同时被请求(`SelectOptionInput.color`,`openapi.yaml:2852`)与响应(`SelectOption.color` :2881、`DeletedSelectOption.color` :2891)schema 引用,一次 enum 扩展同时放宽写入与标注读取。生成 Client 的变化仅是 union literal 从 9 扩到 12;TypeScript union 是类型级约束,线上 JSON 仍是字符串。

## 3. 存储与 Migration 判断(R2 复核,维持原判)

**结论:不需要 Migration,无数据回填。** 逐项源码依据:

1. `fields.config` 为 `JSONB NOT NULL DEFAULT '{}'`(`migrations/001_initial.sql:65`);View `config` 同为 JSONB。全部 6 个 migration(`001`–`006`)grep `color|option` 零命中——色板枚举从未进入数据库层,无 CHECK、无 PostgreSQL ENUM、无查找表。
2. 唯一运行时门禁是 Go 层 `validOptionColor`(`internal/catalog/service.go:934`),HTTP 解码(`internal/httpapi/metadata.go:501-505`)只验必填字符串。
3. **写入路径复核**:`internal/storage/postgres/field.go:103,160` 对 `proposed.Config`/`target.Config` 只做 `json.Marshal` → `config = $N::jsonb`,无任何颜色断言;`storage/postgres` 全部 18 文件 grep `color` 仅命中测试夹具字面量。
4. **读取路径复核**:`postgres/field.go:298-308` `json.Unmarshal` 到 `domain.SelectFieldConfig`,`Color` 字段为 `string`(`internal/domain/model.go:139,145`),无 enum 收窄;`OPTIONS`/`deletedOptions` 只做 nil 归一化。
5. **Tombstone 复核**:删除时原样复制 color(`service.go:918-923` 写入 `DeletedSelectOption{ID, Name, Color, DeletedAt}`);读回同样 string 透传。
6. **History/Change 复核**:`internal/record/history.go` 无 config 快照;Change 载荷只含 Record/Schema 标识、revision、actor、时间与 `recordUpdated` 的字段级值 diff(Option ID,不含颜色)。
7. **Replay 复核**:`idempotency_keys.response` JSONB 原样存取首次规范化响应(migrations/001:113),含新色的响应 replay 时原样返回,无重校验。
8. **Convert 复核**:`convert.go:630-651` select 系 config 只做 JSON marshal 透传,不重验颜色。

因此 G1 是纯代码+合同变更:`validOptionColor` 允许集合、OpenAPI enum、capability 声明、测试夹具。回滚也不需要任何数据操作(见 §5 的降级合同)。

## 4. 向后兼容分析(R2:Plugin 代码证据已落实)

### 4.1 旧 Plugin 读新色 — 代码验证通过

| 环节 | 证据 | 结论 |
| --- | --- | --- |
| HTTP 解码 | `decodeSelectOption` 只验 `typeof color === 'string'`(`src/client/http-loomtable-client.ts:1850-1860`);`decodeDeletedSelectOption` 复用同一函数 | **无 enum gate,新色原样进领域层** |
| 领域模型 | `SelectOption.color: string`;`DeletedSelectOption` 继承(`src/client/loomtable-client.ts:144-152`) | string 透传 |
| 渲染 | chip/swatch 走 `dataset.color` 属性(`field-renderer-registry.ts:480`);未知值无 `[data-color]` CSS 规则,落回 `.loom-field-value-chip` 基线中性样式(`styles.css:1421-1431`,有明确 border/bg/text) | **安全降级为中性 chip,不映射为某个 9 色** |
| i18n | `field.color.*` 仅 9 键(`messages.ts:204-212`),但逐色 label 只在 palette 内部使用,而 palette 只渲染 `SELECT_OPTION_COLORS`(9 色)——未知色不存在 missing-key 调用路径 | 无缺失键风险 |

### 4.2 旧 Plugin 编辑其他属性并 resave — 代码验证通过(保值)

| 环节 | 证据 | 结论 |
| --- | --- | --- |
| Draft 初始化 | `initialOptionDrafts` 原样复制 `option.color`,仅 `as SelectOptionColor` 类型断言(`field-editor-panel.ts:465-470`)——运行时无映射 | 新色进 draft 不被改写 |
| Submit | `color: draft.color` 原样回送(:357);`fieldConfigFromInput`/`fieldUpdateConfigFromInput` 只 trim name、透传 color(`grid-view-controller.ts:300-307,340-348`),无 `includes()` 或默认值重映射 | **resave 完整保留新色** |
| Field 复制 | 复制 Field 时 `color: option.color as SelectOptionColor` 同样透传(`readonly-grid-renderer.ts:3547-3549`) | 复制保留新色 |
| Tombstone 隔离 | editor 只 map `config.options`(active),`deletedOptions` 不进 draft——resave 不会复活或改色 tombstone;删 active 项由 Server 软删并保留原色 | tombstone 安全 |

**唯一残留类型级事项**:`OptionDraft.color`、`SelectOptionInput.color` 声明为 9-literal `SelectOptionColor`(`loomtable-client.ts:241-248`、`field-editor-panel.ts:63`)。这是 TS-only 约束,运行时 JSON 仍是 string,不阻断任何行为;但合同记录:**Plugin 不得把现有的 `as` 透传改为运行时 enum 收窄(如 `includes()` 校验回退),那会引入数据降级**;`api:generate` 后 union 自然扩至 12。

### 4.3 全矩阵结论

| 场景 | 判定 |
| --- | --- |
| 新 Server + 旧 Plugin(读) | 兼容(4.1 代码证据;reader-first 事实落实) |
| 新 Server + 旧 Plugin(编辑/复制/resave) | 兼容(4.2 代码证据;新色全路径保值) |
| 旧 Server + 新 Plugin | 由 §4.4 capability 门控;误发新色 → `422 VALIDATION_ERROR`,不静默截断 |
| 旧生成 Client(9-literal union)读新色 | 兼容:union 为类型级,decoder 无 enum 断言(4.1) |
| 未知前瞻色(第 13 色)写新 Server | 仍 `422`;Server 保持 strict-writer,前瞻边界不放宽 |
| 新色 tombstone resave/恢复 | 新 Server 接受;旧 Plugin 无法复活 tombstone(4.2),语义安全 |

### 4.4 Capability 合同(`choicePalette12`,D2 已定)

**Server 实现事实**:capabilities 由 `internal/config/config.go:26-28` 生成(`["grid","map"]`,启用 Attachment 模块追加 `"attachments"`);`internal/httpapi/server.go:141` 在 attachment 禁用时剔除该 token。G1 合同规定:

1. **声明条件**:实现 G1 的 Server 二进制在 `GET /v1/meta` 的 `capabilities` 中无条件声明 `choicePalette12`(不依赖配置开关)。token 由代码决定,**降级自动消失、升级自动出现**,不存在 DB/环境漂移。
2. **缺失情形**:所有旧版本 Server(含当前部署)以及任何 G1 前 build 均不声明;Plugin 必须把缺失视为"无 12 色 writer 能力"。
3. **旧客户端容忍性(代码证据)**:`decodeServerMeta` 只验 `isStringArray(capabilities)`(`http-loomtable-client.ts:2162`);`determineIncompatibility` 不检查 capabilities(:2123-2152)——未知 token 透明通过,旧 Plugin 兼容。
4. **新 Plugin 门控**:12 色 writer 入口(选项色板、复制、粘贴式配置)仅在观察到 `choicePalette12` 时开放;未观察到时保持 9 色 UI(现状)。**注意:Plugin 当前没有任何运行时 capability 消费点或缓存**(`getMeta` 仅被 Settings `checkConnection` 调用)——G1 将引入首个运行时消费者,须定义评估点(建议:进入 Choice/Field 编辑会话时按连接 profile 求值并缓存至该会话结束;断线重连或切换 profile 即失效重取)。
5. **Stale capability 处置**:无持久缓存,不存在跨进程陈旧问题;会话内缓存的陈旧后果是有界的——升级未察觉 = 仅入口不显示(保守、可重取恢复),降级未察觉 = 新色写入被旧 Server `422` 拒绝(硬错,不腐蚀数据,Plugin 应将 422 color format 错误如实呈现而非重试)。Server strict-writer 是最终安全网。

### 4.5 Server Rollback 运维合同

G1 部署声明:新色一旦写入生产数据,降级前须先完成 §4.5.4 的数据修正;不自动执行任何颜色重写。

1. **停止新写入**:旧 Server 本身即双重停止信号——`capabilities` 不再含 `choicePalette12`(代码级,无需额外配置),且新色写入直接 `422`。规范回滚序列:① 先停用/回滚所有已启用新色的 Plugin(或将新 Server 置于"只读观察期"再降级);② 部署旧 Server;③ 未回滚的新 Plugin 在下一次能力评估(会话级重取或新会话)自动失能。
2. **已写新色数据的读取**:全部读路径 string 透传——Field GET、Record 查询、Distinct、History、View 加载、Map、Backup 均正常;tombstone 保留新色。**旧 Server 上数据无损可读。**
3. **被拒绝的操作(精确边界)**:仅"提交 `config.options` 且列表中含新色 Option 的 `PATCH /v1/fields/{fieldId}`"返回 `422 VALIDATION_ERROR`(issue `format`)。不影响:Field 改名(不携 config 的 PATCH)、`DELETE`/restore(只验 expectedRevision)、Record 全部读写、Table/View/其他 Field 操作、convert(select→text 不读 options color;text→select 只产 9 色)。
4. **恢复完全编辑能力**:把受影响 Field 中残留的新色 Option 改回 9 色集。途径按序:① 在降级前由新 Server 上的 client 完成(推荐,写入路径正常);② 降级后由 DBA 对 `fields.config` JSONB 做受控修正(运维操作,**不在 G1 范围,需单独审批与审计**);③ 无自动 migration/gray 映射。
5. **回滚完成判定**:`GET /v1/meta` 返回的 `serverVersion` 回落且 `capabilities` 无 `choicePalette12`;抽样 GET 至少一个含新色 Field 确认读正常;对不含新色的 Field 做一次 options PATCH 确认 200(证明编辑面整体可用)。
6. **长期降级评估**:若新色已写生产且长期回退旧 Server,受影响 Field 的 options 管理(增/删/改/恢复/重排/换色)持续不可用——属**不可接受的功能降级**,故降级方案必须前置数据修正。若新色从未写入,旧 Server 运行无任何降级。
7. **可选缓解项(可审查,不扩大本合同)**:实施期可评估一个小型"rollback-compatible writer"补丁——旧 Server 端在 options PATCH 中把未知色降级为 `gray` 并记录 warning,换取 config 可编辑性。该方案引入静默数据改写,**默认不采用**;仅当运维认定新色数据已大规模落地且无法先修数据时再评审。

## 5. 测试与发布策略(实施期执行,本轮不运行)

环境约束:集成测试读 `LOOMTABLE_TEST_DATABASE_URL` 且会 `DROP SCHEMA public CASCADE`;只用专用可销毁合成库,无环境则如实标"integration 未验证";不默认本机起 Docker/PostgreSQL;不碰生产/共享库。

| 层 | 用例 |
| --- | --- |
| unit(catalog) | `validOptionColor` 表驱动:12 色全接受;大小写变体、hex、空串、未知 token 拒绝 |
| 合同对账(D5) | 新增测试断言 Go 允许集合 == OpenAPI `SelectOptionColor` enum(当前无此项) |
| unit(catalog) | create/update 路径分别提交三色;Deleted ID 恢复携带新色;500/5000 上限与 duplicate 逻辑不受色影响 |
| writer 往返 | create 含 lime → GET 回显 lime;PATCH 改色→tombstone 保旧色→恢复 |
| 旧数据兼容 | 9 色 fixture 读/改/存不变;resave 后 config 无 diff |
| convert | `distinctOptions` 仍产 9 色轮播(D1);含新色 select 转换 config 透传 |
| 幂等 | 同 `Idempotency-Key` 不同 color → `409 IDEMPOTENCY_KEY_REUSED`;replay 原样返回新色 |
| meta/capability | G1 build 的 `/v1/meta` 含 `choicePalette12`;禁用 attachments 时不影响该 token |
| httpapi | 严格解码;color 缺失 `required`、非法值 `format`;openapi_contract_test 覆盖 |
| integration(隔离库) | 全 writer 往返 + distinct/history 透传;多 client 混合色并发 PATCH 的 CAS/409 |
| Plugin 侧(其交付验证) | 9 色旧 Plugin × 新 Server resave 保值(§4.2 已有代码证据,做回归);12 色新 Plugin × 旧 Server 门控+422;未知色 reader 不映射默认值 |

发布检查沿用 CONTRIBUTING:`gofmt`、`go test ./...`、`go vet ./...`、`git diff --check`;改动仅 Go+OpenAPI+测试,`docker compose config --quiet` 不适用。

## 6. Plugin 交付合同(Server 实现完成并批准后提供)

1. 固定交付 commit:完整 40 位 Server SHA(含 OpenAPI enum 与代码同 commit)。
2. 合同引用:本文档路径 + 批准后 `docs/api/openapi.yaml` 的实际 diff/version(0.3.0)。
3. 能力信号:`/v1/meta` `capabilities` 含 `choicePalette12`;Plugin 仅在评估点观察到该 token 时启用三色 writer(评估点与失效语义见 §4.4.4)。
4. 兼容说明:本提案 §4.1/§4.2 的代码证据已由 Server Session 复核提交,Plugin 接入切片回归确认;9↔12 旧新矩阵结论;`minPluginVersion` 不变。
5. 同步时点:Plugin 以批准 SHA 显式 `api:sync` → `api:generate`,审查 `openapi/` 与 `src/generated/transport.ts` diff 后接入。
6. 验证证据:unit/integration 实际命令与结果、隔离库证明、回滚限制声明(§4.5);无证据不声称 capability 可用。
7. 在以上全部交付前,Plugin 保持 9 色 writer,不提前发送 `lime`/`indigo`/`brown`。

## 7. 不变量与排除项

- Record 值、Option ID 生命周期、排序 rank、Distinct/History/Map、幂等与 receipt 语义全部不变。
- 不引入第 13 色、别名、每用户调色板或 CSS 值;不放宽 strict-writer 边界。
- 不解锁 G2(Atomic Choice):Field options PATCH 与 Record 赋值仍是两个请求;G1 只扩展合法色集合。
- 不涉及 Time/DateTime、bulk mutation、View trash、Table rollback。
- 不自动执行颜色重写/gray 迁移;rollback 数据修正为独立审批运维项。

## 8. 决策记录(D1–D5 已批准)与待裁决项

已批准基线(2026-10-12 用户正式批准):

| ID | 决策 | 结论 |
| --- | --- | --- |
| D1 | `distinctOptions` 自动赋色 | 保持 9 色轮播,新色仅经显式 writer |
| D2 | capability token | `choicePalette12` |
| D3 | OpenAPI `info.version` | 升 `0.3.0`;路由保持 v1 |
| D4 | enum 顺序 | 按 Design System 表序 |
| D5 | Go 允许集合 | 单一常量表 + OpenAPI 合同对账测试(新增) |

已授权:G1 Server 实现(Go writer 校验、OpenAPI 更新、`choicePalette12` capability、测试)、实现分支提交与 Review PR。

待用户裁决(真正开放项):

| ID | 事项 | 说明 |
| --- | --- | --- |
| R1 | 实现 PR 审查与合并 | 合同批准不覆盖合并;Review PR 由用户审查决定 |
| R2 | 生产部署 | 未授权;部署后才存在 Deployed Capability |
| R3 | §4.5.7 rollback-writer 缓解补丁 | 默认不采用;仅运维场景需要时再评审 |
| R4 | Plugin 侧能力评估点的最终形态 | 建议会话级缓存(§4.4.4),细节留给 Plugin 接入切片 |
| R5 | Plugin `api:sync`/三色 writer 启用 | 需 Server 合并 + 部署证据后的单独授权 |

## 9. G6 附带影响说明(不展开设计)

`AggregateRequest` 当前仅 `filter`/`fieldIds`/`fns`,无 `search`;Footer 聚合口径与 Grid Filter+Search 可见范围不一致是已知正确性缺口。G6 是独立的窄正确性合同,与 G1 无互锁;获授权前 Plugin 不得用 loaded rows 或 filter-only 结果冒充 Search 范围聚合,行为保持 Gated。详见 api-boundary G6 节。

---

**G1 Contract — Approved for Implementation.** 实现限本分支;合并、部署与 Plugin 启用均需单独授权。
