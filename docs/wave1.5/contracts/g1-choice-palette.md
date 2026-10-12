# G1 Contract Proposal — 12-Color Choice Palette Server Writer

**Status: PROPOSAL / PENDING APPROVAL。** 本文是 Gate G1 的正式合同评审草案，不是已生效的生产 API 合同。在批准、实现、固定 commit 与部署验证完成前，权威传输合同仍以 `docs/api/openapi.yaml`(v0.2.0,9 色 enum)为准。批准本文不等于授权实现；实现按单独指令进行。

- Gate:G1 — 12-color write compatibility(Server writer)
- Date:2026-10-12
- Review baseline:Server `5d430cd641e5a210eab3371c9864919837cd69a9`(main,文档入口已发布);OpenAPI v0.2.0
- 上游设计:Plugin `docs/ui/design-system.md` 批准色板;Plugin `docs/wave1.5/api-boundary.md` G1 节
- 范围：仅 Server writer 侧接受三个新增语义色 Token。不改 Record 值模型、选项生命周期、排序、Distinct、History 或任何其他 Gate。

## 1. 数据与接口语义

### 1.1 完整 12 色 Token

以 Plugin Design System 批准表为准，Token 名称与顺序:

`gray, red, orange, yellow, lime, green, cyan, blue, indigo, purple, pink, brown`

新增三个:`lime`、`indigo`、`brown`。不允许 CSS 色值、其他字符串或大小写变体;writer 保持大小写敏感的精确匹配(与现行 `validOptionColor` 一致)。颜色是必填字段,无默认值语义——`color` 缺失仍返回 `422 VALIDATION_ERROR`(`required`),非法值返回 `422`(`format` / `unsupported option color`)。

### 1.2 适用对象

- `select` 与 `multiSelect` 两种 Field Type 共用同一 enum,不加区分。
- `Active Option` 与 `Deleted Option`(tombstone)共用 `SelectOptionColor`。删除不重置颜色;tombstone 原样保留写入时的 Token,供历史 Record 引用与恢复显示。
- Record 值只引用 Option ID,从不携带颜色;Record Mutation、Filter、Sort(option rank)、Distinct Values 的语义不变。

### 1.3 受影响的 writer 路径(全部枚举)

| 路径 | 当前行为 | G1 后行为 |
| --- | --- | --- |
| `POST /v1/tables/{tableId}/fields`(create select/multiSelect,`config.options[].color`) | `normalizeNewSelectConfig` → `validOptionColor` 9 色 | 同一校验点接受 12 色 |
| `PATCH /v1/fields/{fieldId}`(`config.options[]` 期望 Active 列表:无 ID 创建、有 ID 改名/换色/重排、Deleted ID 恢复、遗漏 Active ID 软删) | `normalizeUpdatedSelectConfig` → `validOptionColor` 9 色 | 同一校验点接受 12 色;恢复 Deleted Option 时提交的 `color` 同样走新 enum |
| `POST .../convert`(text→select `distinctOptions` 自动建选项) | `convert.go` 内置 9 色轮播 palette 自动赋色 | **建议保持 9 色轮播不变**(见 §8 裁决点 D1);新色仅经显式 writer 写入 |
| Record Mutation / move / duplicate | 不含颜色 | 不变 |
| Idempotent replay / stored response | 原样返回首次规范化结果 | 不变;含新色的已存响应 replay 原样返回新色字符串 |

### 1.4 Server Validation 语义

改动面收敛为单点:`validOptionColor`(`internal/catalog/service.go:934`)的允许集合从 9 扩至 12。建议同时把允许集合提为单一常量表(create/update/convert 共用引用),避免 Go 与 OpenAPI 双源漂移;实现细节在实施阶段确定。`color` 仍不参与 name 唯一性、不参与 no-op 判定之外的规范化;`normalizedInput` 继续回显规范化后的 name+color 进入幂等 Hash(机制不变)。

### 1.5 既有 9 色数据

零语义变化。存量 `fields.config` JSONB 中的 9 色值全部保持合法;读取、编辑、删除、恢复路径不变。

## 2. OpenAPI 变更草案(未应用)

仅 enum 扩展与 capability 声明,无 endpoint、形状或错误码变化:

```diff
 info:
   title: LoomTable Server API
-  version: 0.2.0
+  version: 0.3.0            # 建议值,见裁决点 D3

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

引用面:`SelectOptionColor` 同时被请求(`SelectOptionInput.color`)与响应(`SelectOption.color`、`DeletedSelectOption.color`)schema 引用,一次 enum 扩展同时放宽写入与标注读取。生成 Client 的变化仅是 union literal 从 9 个扩到 12 个(TypeScript 为类型级约束;JSON 线上仍是字符串)。

## 3. 存储与 Migration 判断

**结论:不需要 Migration,无数据回填。** 依据:

1. `fields.config` 为 `JSONB NOT NULL DEFAULT '{}'`(`migrations/001_initial.sql:65`);View `config` 同为 JSONB。
2. 全部 6 个 migration(`001`–`006`)不含任何 color/option 相关 CHECK、PostgreSQL ENUM 或查找表——色板枚举从未进入数据库层(对 `migrations/` 全目录 grep `color|option` 零命中)。
3. 唯一运行时门禁是 Go 层 `validOptionColor`(`internal/catalog/service.go:934`),HTTP 解码(`internal/httpapi/metadata.go:501-505`)只验必填字符串,storage/postgres 各文件无 color 断言,直接 marshal `SelectFieldConfig` 落盘。
4. 读路径(`domain.SelectOption.Color` / `DeletedSelectOption.Color` 均为 `string`)对色值原样透传;Deleted tombstone、Change/History 快照、幂等 `response` JSONB 均按字符串保存,新色无需转换。
5. `convert.go` 对 select 系 config 只做 JSON marshal 透传(line 630-651),不重验颜色。

因此 Server 侧 G1 是纯代码+合同变更:`validOptionColor` 允许集合、OpenAPI enum、capability 声明、测试夹具。回滚也不需要任何数据操作(见 §4.5 的降级风险)。

## 4. 向后兼容分析

不因"数据库存字符串"直接断言全兼容;按方向逐项分析:

| 场景 | 行为 | 判定 |
| --- | --- | --- |
| 新 Server + 旧 Plugin(9 色) | 旧 Plugin 不发送新色;存量数据全是 9 色,行为完全不变。若数据已被其他新 client 写入新色:读路径按 string 透传(需 Plugin 侧确认 reader 不 enum-assert,api-boundary 已记录 "CurrentReader 字符串事实");旧 Plugin 对该 Field 做 options PATCH 时若原样回显新色,新 Server 接受;若 UI 强制改选 9 色则属正常编辑 | 兼容,依赖 reader 保值事实(验证项 V1) |
| 旧 Server + 新 Plugin(12 色) | 新 Plugin 在未检测到 capability(§4.4)前不得发送新色;若仍发送,旧 Server 返回 `422 VALIDATION_ERROR`/`format`,不静默截断 | 兼容,由 capability 门控 |
| 旧生成 Client 读新色 | TS union 是类型级约束;运行时 JSON 为字符串,严格 enum decoder 可能抛错——Plugin 生成/decoder 须保持 string 语义(验证项 V1) | 有前提条件兼容 |
| 未知前瞻色(第 13 色)写新 Server | 仍 `422`;Server 保持 strict-writer,前瞻兼容边界不放宽 | 边界不变 |
| 新色 Deleted Option 的 resave/恢复 | tombstone 原样保存新色;恢复经 options PATCH,新 Server 接受;旧 Server 不存在该数据 | 兼容 |
| convert 产物 | `distinctOptions` 仍产 9 色(D1);含新色的 select→select 系转换 config 透传,新色保留 | 兼容 |

### 4.4 发布顺序与能力检测

1. **Reader-first 已满足**:当前 Plugin reader 对未知 color 按 string 保值并安全显示(api-boundary 记录事实,实施时复核)。
2. Server 合并 G1、部署后,`GET /v1/meta` 的 `capabilities` 增加 `choicePalette12`(名字见 D2)。`capabilities` 是无约束 string 数组,旧 reader 应忽略未知项(Plugin 侧复核项)。
3. Plugin 在批准 SHA 上 `api:sync` → 生成 client,检测到 `choicePalette12` 才启用三色 writer;未检测到时 Picker 只提供 9 色(现状)。
4. `minPluginVersion` **不提高**:旧 Plugin 对新 Server 完全可用,不应被强制升级。
5. `apiVersion` 保持 `v1`(enum 扩展为 v1 内 additive 变更);OpenAPI `info.version` 是否升 0.3.0 见 D3。

### 4.5 回滚到旧 Server 的风险

DB 无需回滚;但代码降级后:含新色的 Field config 读路径仍正常(string 透传);**任何对该 Field 的 options PATCH 会因新色 `422` 被拒**——新色 Field 在旧 Server 上变为 config 只读。Record 值、Distinct、History 不受影响。缓解:回滚预案中声明该限制;如需可逆,提供一次性"新色→gray"离线重写脚本(不在本合同范围),或避免在生产写入新色直到部署稳定。该风险与"严格 writer"立场一致,不视为阻断项。

## 5. 测试与发布策略(实施期执行,本轮不运行)

环境约束:集成测试读 `LOOMTABLE_TEST_DATABASE_URL` 且会 `DROP SCHEMA public CASCADE`;只用专用可销毁合成库,无环境则如实标"integration 未验证";不默认本机起 Docker/PostgreSQL;不碰生产/共享库。

| 层 | 用例 |
| --- | --- |
| unit(catalog) | `validOptionColor` 表驱动:12 色全接受;`lime`/`indigo`/`brown` 大小写变体、hex、空串、未知 token 拒绝 |
| unit(catalog) | create/update 路径分别提交三色;Deleted ID 恢复携带新色;500/5000 上限与 duplicate 逻辑不受色影响 |
| writer 往返 | create 含 lime → GET 回显 lime;PATCH 改色→tombstone 保旧色→恢复 |
| 旧数据兼容 | 9 色 fixture 读/改/存不变;resave 后 config 字节级不变 |
| convert | `distinctOptions` 仍产 9 色轮播(若 D1 通过);含新色 select 的转换 config 透传 |
| 幂等 | 同 `Idempotency-Key` 不同 color → `409 IDEMPOTENCY_KEY_REUSED`;replay 原样返回新色 |
| httpapi | 严格解码;color 缺失 `required`、非法值 `format`;OpenAPI 合同测试断言 enum 与 Go 常量一致 |
| integration(隔离库) | 全 writer 往返 + distinct/history 透传;多 client 混合色并发 PATCH 的 CAS/409 行为 |
| Plugin 侧(其交付验证) | 9 色旧 Plugin × 新 Server、12 色新 Plugin × 旧 Server、未知色 reader 保值 resave |

发布检查沿用 CONTRIBUTING:`gofmt`、`go test ./...`、`go vet ./...`、`git diff --check`;改动仅 Go+OpenAPI+测试,`docker compose config --quiet` 不适用。

## 6. Plugin 交付合同(Server 实现完成并批准后提供)

1. 固定交付 commit:完整 40 位 Server SHA(含 OpenAPI enum 与代码同 commit)。
2. 合同引用:本文档路径 + 批准后 `docs/api/openapi.yaml` 的实际 diff/version。
3. 能力信号:`/v1/meta` `capabilities` 含 `choicePalette12`(以最终裁决名为准);Plugin 仅在观察到该 token 时启用三色 writer。
4. 兼容说明:reader-first 事实复核结果;9↔12 旧新矩阵结论;`minPluginVersion` 不变。
5. 同步时点:Plugin 以批准 SHA 显式 `api:sync` → `api:generate`,审查 `openapi/` 与 `src/generated/transport.ts` diff 后接入。
6. 验证证据:unit/integration 实际命令与结果、隔离库证明、回滚限制声明;无证据不声称 capability 可用。
7. 在以上全部交付前,Plugin 保持 9 色 writer,不提前发送 `lime`/`indigo`/`brown`。

## 7. 不变量与排除项

- Record 值、Option ID 生命周期、排序 rank、Distinct/History/Map、幂等与 receipt 语义全部不变。
- 不引入第 13 色、别名、每用户调色板或 CSS 值;不放宽 strict-writer 边界。
- 不解锁 G2(Atomic Choice):Field options PATCH 与 Record 赋值仍是两个请求;G1 只扩展合法色集合。
- 不涉及 Time/DateTime、bulk mutation、View trash、Table rollback。

## 8. 待裁决事项

| ID | 事项 | 建议 | 备选 |
| --- | --- | --- | --- |
| D1 | `convert.go` `distinctOptions` 轮播 palette | **保持 9 色**(确定性、最小行为面;新色只经显式 writer) | 扩为 12 色轮播(自动转换产物更"新",但改变既有输出且不可预期) |
| D2 | capability token 名 | `choicePalette12` | 仅以生成 client union 宽化为信号(不显式);或 `apiVersion` 升级(过重) |
| D3 | OpenAPI `info.version` | 升 `0.3.0`(additive enum 扩展,minor 级) | 保持 `0.2.0`(认为 enum 值是数据而非合同形状) |
| D4 | enum 顺序 | 按 Design System 表序(yellow 后插 lime,blue 后插 indigo,brown 末尾) | 追加到末尾(对既有顺序 diff 最小,但与设计表不一致) |
| D5 | Go 侧允许集合载体 | 单一常量表供 `validOptionColor` 引用,合同测试与 OpenAPI 对账 | 维持 switch 字面量(漂移风险) |

## 9. G6 附带影响说明(不展开设计)

`AggregateRequest` 当前仅 `filter`/`fieldIds`/`fns`,无 `search`;Footer 聚合口径与 Grid Filter+Search 可见范围不一致是已知正确性缺口。G6 是独立的窄正确性合同,与 G1 无互锁;在获授权前,Plugin 不得用 loaded rows 或 filter-only 结果冒充 Search 范围聚合,行为保持 Gated。详见 api-boundary G6 节。

---

**G1 Contract Proposal — Pending Approval.** 批准前不修改 `docs/api/openapi.yaml`、Go 实现、数据库或部署。
