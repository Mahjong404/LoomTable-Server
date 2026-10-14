# G4 — Whole-query RowSelection Scope

2026-10-14。产品决定：用户选择固定范围、实际成员变化即失效，并明确值变化但成员不变不得失效。基于 main `f43ecb1a7a70c6fbf109f094ceb1e22a16a7425b` / OpenAPI 0.6.0。以下为本 Gate 的实施合同；具体源码、QA、PR、部署事实在交付证据中单独记录。Subagent=0。禁止 Plugin 修改、Bulk Mutation、G5 和生产部署。

## 产品决定与不变量

Scope 固定创建时实际匹配的完整 active Record ID 集合 S。Filter 与 Search 取交集，不以加载页、普通 Cursor 或查询条件替代 S。不固定 Record 的旧值；读取显示当前事务可见的值。Scope 有效时，全选表达为 S 减去 Excluded IDs E，selectedCount=scope.count-|E|。

比较过的方案：固定范围不会把后来匹配的 Record 偷偷加入选择；成员变化要求重新全选，需保存成员并检测变化。Live 范围存储成本较低，但新增匹配记录自动入选、离开记录自动退出、Count 改变；这改变用户已选集合，用户已否决。固定范围也不意味着保存整个 Table 的历史值或允许失效范围继续使用。

创建后任一已提交写入导致实际成员集合不同，原 Scope 永久失效，即使后续写入又把成员恢复。失败事务和 no-op 不失效。值改变但仍匹配、非成员改变但仍不匹配、非匹配新建/删除均不因数据变化失效。Sort、Display、列宽、Record Position、View Order、Default、View rename 不属于成员语义。

Schema/Query Context 是独立失效原因：Filter 引用 Field 不再可用或类型改变、Search 可搜索 Field 集合/类型改变、继承的 saved View Filter 改变、源 View 被删除使上下文失效。Field/Option 的名字、颜色和纯显示配置不能仅因普通 Revision 增加而失效。Query 的 Filter/Search 改变必须清除旧 Scope；不依据整个 View Revision 判断成员变化。

## 成员快照、检测与客户端提示

服务端保存 S，拥有唯一 Scope ID、Actor/Table 绑定、创建时间、固定过期时间及失效标记。有效性检测和 S 是不同对象，检测从不改写 S 或原 count。Client 负责及时验证、拒绝过期/迟到结果、清除全选状态和提示重新全选；Server 不声称请求结束后仍能即时知道所有未来变化。

采用 writer-time 增量检测。Scope 创建与 Record Writer 在现有 Table 锁下串行；创建一次使用现有 buildRecordWhere 的 Filter+Search SQL 物化 S。实际 Record Mutation 和 Duplicate 在提交前只检查本次受影响的 Record IDs：比较各 ID 在 S 中与在相同 Filter+Search SQL 中的成员资格，有差异则在同事务永久标记 Scope 失效。这也检测尚未加载的 ID、新进入记录以及两次提交间离开又返回；不靠当前 Count 相同推断相同集合。每个 Table 有限个有效 Scope，成本按 affected IDs × active Scopes 有界，普通 resolve 不扫描全部 Records。

Field/View 语义 writer 在同事务复查相关 Scope Context；Convert 同时处理 Schema Context 和实际改写的 Record IDs。必须覆盖所有现有生产 Record Writer；Move 不改变成员。不新增另一套 Filter Engine，校验与 SQL 均复用既有实现。此设计不依赖 30 天 Change 保留、普通 changeCursor、Record Revision 或粗粒度 Data Version，避免日志删除/提交次序造成误判。

验证和 scoped page 使用一个短期 REPEATABLE READ 事务，Scope 状态、权限、有效 Query Context、Exclusions、页与 Count 同 Snapshot。若并发写入在其 Snapshot 之后提交，本次读取可以成功且描述读取时的范围；下一次验证必定拒绝。任何需要最新完整成员资格的未来操作必须在其自己的事务重新验证，G4 不授予这种写操作。

锁序：创建配额锁 → Actor → ancestor shared locks → Table → Scope；既有 writer Actor（如适用）→Table→Field/Record/View→Scope。增量检测不反向锁 Table 或 Record。只读 resolve/page 不取 writer row locks。失效和业务变更一同提交/回滚，不改 Writer 的 CAS、Mutation Receipt、Replay 或 Queue 协议。

## 标准化与身份

Selection Query 只含可选 viewId/filter/search。viewId 的 saved Filter 为默认值；显式 filter（包括 null）覆盖，Search 默认为空。只选 active Records。继承 View 的 Sort/Projection/Display 不参与成员解析；普通 Query 的这些参数仍控制页面展示。

复用既有 Filter 验证（深度8、最多100节点、typed Field/Option 引用、自然空值规则和 normalized rule value）及 Search Unicode Trim→NFC→最多500码点→case folding。AND/OR 子节点 canonical 排序、重复子节点去重用于身份，不改变 SQL 结果；不承诺求解任意逻辑等价式。

membershipFingerprint 为版本化 canonical Table/active/normalized Filter/Search 的 SHA-256，不含 limit/cursor/sort/projection/View Revision。schemaContextHash 只包含查询语义引用：Filter Field ID/type/可用性；Search 的 active text/longText/url Field ID/type 集合。scope.semanticDataVersion 是创建时 S 的稳定成员摘要与 Schema Context 的不可解释字符串；不是可排序 revision，不随着普通 Record 值或 Change Cursor 增加。成员摘要在创建时计算，绝不每次重扫来保持版本。

Scope Token 为按用途 `row-selection` 隔离的 HMAC-SHA256 envelope，签名 Scope ID、Actor、Table、expiry；复用 DB 的持久化 signing key。Token 不含完整 ID 集合，不替代认证，不赋予权限。不记录 Token/解码载荷。服务重启与共享同一 DB/key 的多个实例保留有效 Scope。

## API 选择（新增合同，非现有端点）

独立创建/验证 read scope，扩展既有 records/query 以读取其成员页面；不扩展 records/mutate。计划 OpenAPI 0.7.0，Capability `querySelection`；使用前必须 ready、migrationRequired=false 及 capability。此版本号与能力在代码实施后才算 Source Ready。

`POST /v1/tables/{tableId}/records/selection-scope`

```json
{"requestId":"req_<typed ID>","query":{"viewId":"view_<typed ID>","filter":null,"search":"alpha"}}
```

requestId 必需，query 必需（可为空对象）；strict schema。Actor+requestId 是本短期只读分配的重试身份，与 mut_ Receipt/Identity Guard 分离。相同 canonical 输入重试返回原 Scope，但仍检查当前权限/失效/期限；不同输入复用身份 409 IDEMPOTENCY_KEY_REUSED。Response 丢失可原身份重试，不重复存储 S。原身份在失效后仍保留到固定TTL，重试410；不是永久Mutation Guard。Client不得在首次请求发出5分钟之后继续重试旧requestId，须明确重新全选使用新身份。TTL 清理后相同 requestId 可能再次分配，消费者不得把已过期的身份当持续选择。

`POST /v1/tables/{tableId}/records/selection-scope/resolve`

```json
{"scopeToken":"<opaque>","query":{"filter":null,"search":"alpha"},"excludedIds":[],"recordIds":[]}
```

query 必需，对当前有效上下文重新解析并比对；不接受客户端提交 count/version。excludedIds 默认[]，必须唯一且全部属于 S；recordIds 是有限个待验证 ID，不是完整成员列表。unknown/foreign/nonmember probe 均 inScope=false，不泄露其是否存在。

两个端点成功200，响应结构一致：

```json
{
  "scope":{"id":"sel_<typed ID>","token":"<opaque>","tableId":"tbl_<typed ID>",
    "membershipFingerprint":"<sha256>","semanticDataVersion":"<sha256>",
    "count":10000,"createdAt":"<UTC>","expiresAt":"<UTC>"},
  "excludedIds":[],"excludedCount":0,"selectedCount":10000,
  "membership":[{"recordId":"rec_<typed ID>","inScope":true,"selected":true}]
}
```

创建 membership=[]。Exclusions 是调用方传入的表达，Server 校验并规范排序，不以其他 Client 的 Exclusions 覆盖本 Client。每个 Client 可有独立 Scope/Exclusions；不能把 token 用于另一 Table/Actor。

现有 `POST .../records/query` 可选 `selectionScopeToken` 与 `selectionExcludedIds`。不带 token 保持旧 DTO/游标/行为；带 token 必须 active Query，与 Scope 成员 Fingerprint 匹配，响应附 `selection`（上面同结构，membership 仅覆盖当前页），页始终属于 S。Cursor 另绑定 Scope ID，并仍绑定原页面排序/投影参数；Sort/Display 改变需重新开始页面 Cursor，却保留 Scope。scoped Cursor另绑定前页同事务的Change tail：分页期间任何Table Change导致410 CURSOR_EXPIRED，重新获取第一页仍保留有效Scope。这仅保护页面排序/值的连续性，绝不据此判断S失效，避免排序值变化导致漏页/重复。QUERY_SNAPSHOT_EXPIRED才清Scope；普通无Scope Cursor保持既有live行为。不能给普通 Cursor 加 token 后把旧页冒充 snapshot。

## TTL、权限、预算和错误

TTL 固定5分钟，从创建计，不因 resolve/replay/page 滑动续期。Server 时间为准。到期/永久失效/已清理 Scope 返回410 QUERY_SNAPSHOT_EXPIRED；不返回可继续用的旧 Count/成员页。Client 定时到期先清除全选；offline/验证失败禁用依赖完整成员资格的操作；不能从本地缓存恢复有效证明。

每次 Authenticate 当前 Access Token，按 Actor→Workspace→Base→Table 的 active ownership 验证，源 View（若使用）也验证。无认证/撤销 Token401，资源不可访问404，不泄露其他 Actor 的 Scope 信息；签名无效/用途错误/跨 Actor/Table400 INVALID_CURSOR。权限不是创建时永久授权。权限恢复也不得自动恢复已清除的 Client 全选。

预算：每 Scope 最多100,000成员；每 Actor8、每 Table4、全实例64个有效 Scope；另限制含失效身份在内的未过期请求行：Actor64、实例256。总持久成员最多1,000,000。失效成员可在下次创建时清理，但请求身份保留到原TTL，防止 Response Lost 的旧请求自动分配新的成员集。不是 Table Record 总量限制，超预算使全选 unavailable，普通浏览仍可用。Exclusions最多5,000，probe最多500，page现有1–500；strict JSON仍受8MiB上限。canonical Selection Query 最多64KiB（仅存储scope的预算，不缩小普通Query）；case folding可能扩展码点，保存Trim/NFC后的pre-fold Search再验证，身份使用folded Search。请求有界 deadline，创建全量物化有独立超时；全局创建配额串行，不在普通 resolve 扫描总资源。过期清理级联删除本 scope 成员，采用有界批次，在新 Scope 创建和维护阶段清理；不删除业务 Records。

422 VALIDATION_ERROR：request/query shape、typed IDs、重复/超过数组预算、非成员 Exclusion、token缺失；复用现有 ValidationIssue enum，不造非法 resource/action。成员/总量/Scope配额422 VALIDATION_ERROR，path区分 /scopeMembers、/scopeBudget、/totalScopeMembers，code=limit；不把Scope配额伪称为业务Record容量。暂时依赖/预算超时503 DEPENDENCY_UNAVAILABLE，不降为 loaded-page count。Schema Context/成员失效统一410；View无效 acquire保留既有404/422，resolve拒绝旧Scope。所有错误验证 envelope/requestId/details，而非只测状态码。

## Header 和 Client 状态

有效 count>0 且 E=[] 时 Checked。部分取消选择或普通显式局部选择为 Unchecked，不能推导 Mixed/Indeterminate；没有获批准的第三态。count=0 时 Unchecked+Disabled。Scope未获得、expired/revoked/unavailable/offline 时 Disabled并说明原因，不以 loaded IDs 代替全Query。

单行选中 = 已验证 inScope 且 ID 不在 E。Header Unchecked 不清除其他仍已选行；用户明确再次全选可清 E，用户取消全选清该选择模型。Current Cell/局部 rectangle/Ctrl+A 独立，不因本协议重定义。可缓存并复用当前query的仍有效Scope，改变E不重新创建Scope；清除UI全选不等于自动续期/获取新的范围。任何 Scope Acquisition/Resolve/page 的迟到 success/error 都必须带 Client Query generation、Table/Actor/Scope identity 比较；query变化先清 scope，不让旧响应重新选中。Sort/Display 不增加 membership generation，但页面请求 generation 仍需更新。

## Migration、兼容、发布与回滚

009 在已合并007/008后增加 ephemeral selection_scopes 与不可变 selection_scope_members，Actor/requestId唯一、scope/member唯一、expiry索引、级联清理；不迁移业务值、不回填既有 Scope。ApplyMigrations整体事务，重复按版本跳过。旧 Client 不带新参数保持原行为；旧 Server 没有 querySelection，不发送新请求，不自动 fallback 伪全选。

G4 的 writer-time 检测要求所有业务 writer 为支持G4的新binary，不允许新旧 writer 混跑。降级必须停写、禁用新 Scope、删除/使所有 ephemeral Scope失效，再切换单一旧binary；保留009结构以及007 Guard/008trigger。旧binary写入不维护G4有效性，旧token不得跨该降级/再升级继续使用。备份恢复必须保留 signing key/Scope一致性，恢复后使全部短期 Scope失效再开放（不沿用恢复前客户端token）；测试恢复同时验证新 acquire 正常。无自动 destructive down migration。

生产实测仍 `v0.4.0+f3980b1b4126`，health/ready200，能力不含G2/G3/G4；本轮不执行007/008/009。未来发布单独确认版本/窗口/007Guard与008锁序及009风险、完整DB+attachments备份与隔离恢复，停旧writers、顺序迁移、验证健康/Meta和真实Scope，再开放消费者。本轮 Source/Merged/Deployed/Capability Verified 分别报告。

## 实施验收

- 10,000匹配/只加载100，Count=10,000；不返回全部成员ID；Filter-only/Search-only/两者交集/empty，普通多页与scope多页区分。
- 同名值/数值/Unicode Search/自然空值与Query/Aggregate一致；规范化指纹，Sort/Display/列宽/Move/Default不失效。
- 值变更仍匹配、非成员仍不匹配保留；相同Count不同ID、未加载成员离开、新记录进入、Delete/Restore、Duplicate和离开后返回使原scope永久失效。
- Acquire与Writer、跨页与Writer、两个Client、多实例重启；读取中写入提交不能产生混合Snapshot。业务失败/receipt失败回滚不失效scope，Replay不重复快照/业务变化。
- Schema/Search字段变化/View Filter、Token篡改/Actor/Table/权限/TTL、exclusion非成员/重复/预算、已清理Scope、Response lost、query变化的迟到响应身份。
- SQL/资源成本证据：创建一次物化，writer候选ID索引检查，resolve不扫描全表；10k耗时与EXPLAIN。超额与清理有界，不关闭测试。
- 009前向/重复/备份恢复、旧binary+保留结构验证，G1/G6/G2/G3回归，gofmt/test/vet/race/HTTP完整Schema/隔离DB/CI/正常PR Review。

验收条目是要求，不是已运行 PASS。实现和测试证据完成后才提供固定40位源码 SHA、OpenAPI diff、主要PR与Plugin接入合同；未部署时绝不称G4 UI可用。
