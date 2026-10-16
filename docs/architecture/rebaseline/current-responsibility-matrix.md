# 当前 Go/SQL 职责审计

事实基线见 [R0入口](README.md)。源码位置均相对此仓；列出的风险是源代码审查结论/推断，不是本轮新增故障测试PASS。领域规则、查询执行、约束、事务机制、性能优化和运维分别标注。

## 职责矩阵

| 模块 / 类别 | 当前Go行为与证据 | 当前SQL行为与证据 | 目标责任方 | 改造必要性 | 风险 |
|---|---|---|---|---|---|
| Transport / 传输 | internal/httpapi严格解码、认证、错误与Schema、Audit header共享context | 无Transport | HTTP只编解码/Principal/Result映射 | 保留；去除可变context结果通道 | 提交后编码/签名失败是Unknown，不假称回滚 |
| Catalog / 领域 | catalog/service.go生成ID、normalize Config、生命周期输入；大Store接口 | postgres/catalog/field/view/table.go重复开事务、权限/预算/状态/CAS/Change | Application编排，Domain决策，Repository执行 | 高；不是把Store方法再套空壳 | 行为继续隐藏在Repository将无法独立测试 |
| Field类型 / 领域 | domain支持类型，catalog normalize config，record/value.go与query.go多个switch，catalog/convert.go模式表 | JSONB保存config/values，类型在Go解释 | 一个编译期Type Registry及有类型语义计划 | 高 | 文档称Registry，实际尚非统一可扩展Registry |
| Record Mutation / 领域+事务 | Service验证命令/hash；postgres/record.go执行normalize、状态、Diff、Attachment校验 | BeginTx，Actor→Table→Field/Record锁，UPDATE，Change/Receipt，commitAudit | Application UOW + Domain mutation plan；SQL仅原子执行 | 高 | 不可丢掉500命令单事务与逐命令结果 |
| G2 / 领域 | postgres/choice.go含canonical name复用、Option创建、预算、双CAS与Delta | Field IDs排序锁、Record锁、config写入、007唯一Guard | Choice Domain Service+同一UOW；保留SQL身份唯一性 | 高 | Select/MultiSelect/tombstone/Replay-before-CAS不可退化 |
| G3 / 领域 | catalog/view_order.go验证，postgres/view_order.go排列/CAS/No-op；普通View Writer | 008 maintain_view_order承担Create/Delete/Restore成员和revision | 单一Go ViewOrder模型 | 必须 | 直接删Trigger会丢成员/回滚/旧binary能力 |
| G4 / 领域+查询 | record/selection.go规范化语义；postgres/selection.go配额、物化、增量失效 | 持久Scope/成员、SQL谓词执行、全局allocation advisory；009祖先删除Trigger | Go Scope Service决定生命周期，Repository做集合比较/SQL执行 | 高 | 当前依赖每Writer显式调用及部分Trigger；不能换普通DataVersion失效 |
| G5 / 领域 | beginAudit把Actor/op放SET LOCAL，commitAudit从GUC取结果；Go只读投影 | 010推导kind/before/after/typed metadata/Change归属，011递归redaction，读路径调用函数 | Go事件Builder/Redactor + UOW原子Append | 必须 | 隐式SQL推导、Actor缺失检测与转换override耦合 |
| Query / 查询执行 | record/query.go校验/规范化、SchemaHash与cursor；postgres/query.go编译SQL | 过滤、排序、count、keyset、每页只读RR | Go语义IR/规划，SQL执行 | 保留并收敛类型分支 | 禁止把全量记录拉到Go再Filter/Sort |
| G6 Aggregate/Distinct / 查询 | record/values.go规范化 | postgres/values.go共用buildRecordWhere与投影、聚合SQL | 同一QueryPlan/编译器 | 保留 | Search与Filter范围不可分叉 |
| Map / 查询+优化 | Go有界聚类/坐标语义 | postgres/map.go视口/summary、RR、SQL数值抽取 | 保留现有执行分工 | 按基准决定索引 | 不因去PL/pgSQL就把视口/聚合搬内存 |
| Query Projection / 派生数据 | record/value.go在值更新生成query_values/search_text；convert.go重建 | 同事务列保存；当前主要Table/position B-tree，无通用动态字段索引 | Type Registry+Projection Service，SQL同步存/索引 | 高，统一Schema影响判断 | 删除/恢复Text Field可能留下陈旧Search键 |
| Field Convert / 领域+事务 | catalog/convert.go回调plan.Scan/Verify/BuildConfig；postgres/convert.go加载整个Table/重写 | Table→Field→Records锁；SET LOCAL audit_conversion；Record revision不增 | Go转换计划/迭代器，UOW整表原子publish | 高 | O(N×Record宽度)内存、长Table锁、Audit写放大 |
| Order/Default / 领域 | Move/Duplicate/SetDefault在Repository含规则 | position中点写、唯一default索引 | Go Domain；保留单行position/SQL约束 | 中 | Move不增RecordRevision；Default不是Reorder |
| Permission / 领域+约束 | 逐请求Authenticate；Personal继承Actor ownership；无Team ACL | JOIN祖先可见性、Actor FK；Token撤销DML | Application AccessPolicy + DB权限/锁 | 高，先定义撤销线性化 | 现有请求开始认证不保证已在途请求撤销后立刻停止 |
| Attachment / 领域+基础设施 | Service文件临时写/rename、pending/ready；Repository引用元数据校验 | Actor FK、source/status/path CHECK，Record引用时锁元数据 | 保留可恢复文件协议，Access/Refs走UOW | 中 | 文件系统不是DB事务，不能宣称两阶段提交 |
| Retention / 运维+查询 | maintenance.Runner启动/24h，forever门控 | advisory会话锁、DB clock、bounded DELETE、watermark/generation；Scope级联清理 | 独立Scope GC与History Retention；SQL删除/水位同事务 | 中 | 两Retention都forever时Runner不启动Scope清理 |
| Bootstrap/Key / 基础设施 | Go随机Token/key/哈希，持久签名key | serializable bootstrap+advisory锁、唯一Token/name、server_secrets | 保留 | 必需基础设施 | 不因移除业务Trigger误删锁/Secret |
| Migration / 运维 | postgres/postgres.go按文件版本逐文件事务 | schema_migrations；无checksum/全局迁移锁 | 专用Migrator、版本哈希与排他执行 | 高 | 不是已有Migration多实例安全或改文件自动升级 |

## 完整函数与Trigger目录

001–007无自定义函数/Trigger。008：maintain_view_order；009：revoke_ancestor_selection_scopes；010：loomtable_audit_operation、loomtable_audit_cell、loomtable_capture_audit、loomtable_associate_audit_change、loomtable_capture_ancestor_audit；011：loomtable_audit_safe_value、loomtable_audit_safe_payload、loomtable_sanitize_audit_payload。总计10函数，无CREATE PROCEDURE。12个Trigger的逐绑定/行号见 [退役表](trigger-retirement-plan.md)。FK内部系统Trigger不属于这12个，不应删除。

## 约束、唯一性与完整性

| 位置 | 现状（保留） | 缺口 / 目标 |
|---|---|---|
| 001 | Actor→Workspace→Base→Table；Field/View/Record/Change→Table、Token/Receipt/Change→Actor真实FK | Table.primary_field_id只FK Field.id，未同时证明同Table；推荐复合FK，保留deferred启动循环 |
| 001 | PK/ID唯一；Token hash唯一；active Actor+Token name唯一；每Table一个is_primary索引 | “恰有一个active primary”等跨行产品规则仍Go；不用取消FK解决 |
| 001 | revision/schema_version正数、position_index非负、secret32bytes、NOT NULL | 动态Field值/Option与软删除状态由Go，不加成百类型CHECK |
| 002 | Attachment Actor FK、source/status/path互斥、size/width/height/hash域CHECK | JSONB内Attachment引用不是FK，需批量验证真实归属/ready；文件不可用仍要拒绝 |
| 005/006 | Record position非空+索引；active default每Table最多一个唯一索引 | 最少一个Default/具体切换语义Go；普通config不动order |
| 007 | Guard(actor_id,client_mutation_id)复合PK+Actor FK | 永久身份非Receipt缓存，不随Audit/Receipt清理；legacy请求未凭空拥有永久保证 |
| 008 | Order JSONB array/长度≤100、order_revision≥1 | CHECK不验证完整active permutation、成员唯一/归属；由单一Go模型在锁下验证 |
| 009 | Scope Actor/Table FK、request唯一、TTL顺序、count≤100k、成员PK与scope FK cascade | record_id当前无FK；建议R1添加稳定Record FK，跨Table由受控INSERT/业务校验保证 |
| 010 | Event sequence PK、event ID unique、Table/Actor FK、category/payload_version CHECK、legacyChange unique、coverage/watermark | Change关联不是强FK，因独立历史保留期限需要保留已消失的来源ID；Operation已知项可加FK |
| 索引 | lifecycle/parent/order/Change/Retention；Audit category/record/op/GIN changeIds | 无“JSONB万能索引”；动态Filter/Sort/Search需专门量测与热点索引 |

## CAS与锁/事务实况

当前CAS主要是在Table/子对象行锁后由Go比较revision，然后SQL WHERE id更新；Record更新、G3 Order写并非普遍 WHERE revision=expected。现有受控Writer锁下可正确，不能写成“已是所有SQL条件CAS”。目标在保留锁的同时补原子revision/状态条件，0行明确Conflict。

事务在多个Repository顶层方法重复Begin/Commit；UOW尚不存在。Record/G3等Receipt Writer还取Actor FOR NO KEY UPDATE（避免FK KEY SHARE互锁），不同Table同Actor写被整体串行。Field/View非Receipt Writer走Table→子对象；Choice Field/Record ID排序；Convert全Table FOR UPDATE未显式ID排序，但Table门锁先序列化。Ancestor Audit还在SQL里按Table ID取锁。读Query/Map/Aggregate/Audit/List Order为短RR；普通Cursor不持长事务。

Advisory只有auth bootstrap事务锁、selection allocation事务锁和retention独占连接上的会话锁（内部SQL语义，不是Go内存互斥）。没有理由一起搬出DB。Attachment行锁当前逐引用迭代，R1需预收集、稳定排序，避免未来降低粗锁后死锁。详见 [事务方案](transaction-and-uow.md)。

## 审计发现与验证等级

| ID | 结论/证据 | 等级与R1处理 |
|---|---|---|
| F01 | Audit规则实际以SQL为主；Go依赖GUC和conversion override | 源码确认；R1同事务Go事件替代，差分oracle验证 |
| F02 | Writer事务/幂等/权限/Scope hook分散，无统一不可绕过Finalize | 源码确认；建Writer覆盖表与UOW封闭入口 |
| F03 | Field Delete/Restore仅改Field/Change/Scope；BuildQueryProjection排除deleted Field，而Search SQL直接strpos(search_text,term) | 静态证据支持陈旧Search风险，运行复现Not Verified；R1先补失败夹具及投影一致性维护 |
| F04 | Runner.Start/RunOnce在两个age=0时返回，Scope GC藏在CleanupRetention内 | 源码确认可跳过周期GC；Scope有效性TTL仍拒绝过期，新Acquire仍清理；不是无限可用Scope |
| F05 | Runtime连接匹配生产bootstrap DB role，权限过大 | 只读现场确认，未打印连接信息；R1最小权限+单独获批发布 |
| F06 | Convert持锁加载全部values到pending，再逐行重投影；Audit又保存before/after | 源码确认复杂度；没有100k新实测，先量测，禁止先宣称Go更快 |
| F07 | Readiness枚举只含001/002与007–011，迁移器无文件checksum/并发锁 | 源码确认；目标manifest精确版本/哈希与独占Migrator，旧001–006不可改登记 |
| F08 | 文档“Registry”与散落switch实现不一致；元数据/Query语义有重复分支 | 源码确认；编译期Registry收敛，非动态可执行插件 |

## 001–011静态约束逐表补录

| 表/迁移 | PK/Unique | FK | Check/Not Null要点 |
|---|---|---|---|
| schema_migrations/001 | version PK | 无 | applied_at非空 |
| actors/001 | id PK | 无 | created_at非空 |
| auth_tokens/001 | id PK、token_hash UNIQUE、active(actor_id,name_key) Unique Index | actor_id→actors | 名称/哈希/时间非空，撤销nullable |
| server_secrets/001 | name PK | 无 | secret非空且32bytes |
| workspaces/001 | id PK | actor→actors | revision>0 |
| bases/001 | id PK | workspace→workspaces | revision>0 |
| tables/001、008 | id PK | base→bases、primary_field→fields（deferred） | revision>0、order_revision≥1、Order array且≤100 |
| fields/001、004 | id PK、每Table is_primary Unique Index | table→tables | position_index≥0、schema_version/revision>0；description nullable |
| views/001、006 | id PK、active default per Table Unique Index | table→tables | revision>0；default非空 |
| records/001、005 | id PK | table→tables | revision>0；values/query_values/search_text/position非空 |
| changes/001、003 | change_sequence PK、id UNIQUE | Table/Actor | kind/revision非空；fields/primary text nullable |
| idempotency_keys/001、010 | (actor,key) PK | actor→actors | hash/response/time非空、operation_id nullable |
| change_retention_watermarks/001 | table PK | table→tables | expired_through≥0 |
| attachments/002 | id PK | actor→actors | source/status enum、size非负、width/height/revision正、managed/vault路径互斥、SHA域；非空元数据 |
| mutation_identity_guards/007 | (actor,key) PK | actor→actors | hash非空 |
| selection_scopes/009 | id PK、(actor,request) UNIQUE | Actor/Table | count0–100k、expires>created、Query/hash/状态非空 |
| selection_scope_members/009 | (scope,record) PK | scope→scopes ON DELETE CASCADE | record ID非空、当前无Record FK |
| audit_coverage/010 | singleton PK | 无 | CHECK singleton=true、captured_since非空 |
| audit_instrumentation_gaps/010 | table PK | table→tables | first_observed非空 |
| audit_events/010 | sequence PK、id UNIQUE、legacy_change_id UNIQUE（允许NULL） | Table/Actor | category枚举、payload_version0/1、payload等非空；历史身份允许NULL |
| audit_retention_watermarks/010 | table PK | table→tables | expired seq/generation/time非空 |

此为声明式SQL盘点，不把查询JOIN/索引或内部FK Trigger算作复杂Stored Procedure。表间软删除可见性、必须存在Default/Primary、JSONB内合法Choice等仍是领域规则。生产只读catalog与001–006核对；007–011属源码，不冒充线上约束。

FK/Unique/Check/延迟约束的基础能力参考 [PG16 Constraints](https://www.postgresql.org/docs/16/ddl-constraints.html)；具体跨行/软删除不变量分工由本报告提出，不能从数据库支持这些约束推导出完整领域正确性。
