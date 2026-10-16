# Schema与存储方案比较

以下是推荐与成本推导，不是已测得性能。事实见001–011、ADR0003/0010及当前 [存储模型](../storage-model.md)。

## Canonical值方案

| 方案 | 读/查询与索引 | 写放大/迁移成本 | 扩展与限制 | R0建议 |
|---|---|---|---|---|
| 保持values JSONB + query_values JSONB + search_text | 已有共用SQL编译器；Table/position B-tree；热点typed expression/GIN按操作选择 | 单Cell修改仍更新Record/派生列，Audit前后态/WAL可能更贵；投影须可重建 | 动态类型/稀疏行容易；无JSON内部FK | **R1基底，补投影版本/一致性与索引，不全换** |
| JSONB + 少量规范化辅助关系 | Relation反向、引用/类型热点可用真实FK/复合索引 | 同事务维护额外行/索引；需要重建来源定义 | 只在已测热点或获批Relation启用；不能有两份Canonical事实 | **推荐可扩展方向，按需实施** |
| 完整typed EAV | 每个typed cell易建B-tree，按字段聚合方便；多条件需多个JOIN/EXISTS，Sort后取完整Record重组 | 100k×50×50%填充约250万Cell行，满填500万，另加索引/历史；不是100k行原地替换 | Schema/type convert仍要大规模改行；MultiSelect子表再放大；权限/跨字段查询更复杂 | 没有实测证明前不采用 |
| 每Table动态DDL/列 | 热点SQL自然，强typed列/索引 | 每次增字段/类型变化做DDL与锁，版本/租户/恢复复杂 | 与动态Field/schema变更频率矛盾 | 不作为R1总模型 |

成本模型：每次写入约L(canonical)+L(projection)+L(search)+L(audit)+index/WAL/旧版本，不是“用了jsonb_set就只写那一个Cell”。Type Registry在Go做验证不意味着Go执行全量Filter。JSONB与关系可以共存，Postgres更新仍取整行锁；这是保留行级CAS的现实成本。[PG16 JSONB说明](https://www.postgresql.org/docs/16/datatype-json.html)支持索引/混合设计，不承诺所有JSON路径查询都自动高效。

## 推荐物理职责与约束

保留actors/tokens/workspaces/bases/tables/fields/views/records/attachments和001–006身份。Field ID/Record ID不可重建；软删除保留引用。Registry验证动态JSONB值/Option预算/生命周期，数据库保真实FK、NOT NULL、基本域CHECK和重要Unique。

- 建议补基础JSON形状CHECK（例如Canonical values/query_values必须为object），添加前核对既有数据；不是把每个动态Field/Option的验证编成SQL。Go提前验证与数据库静态域兜底可以重复边界检查，不等于Go和Trigger各自实现领域状态机；稳定上限由合同测试校核，变更需要相应迁移。
- 保留全部合理现有FK/Unique/Check。补强Table主Field同表关系：fields(table_id,id)唯一键，tables(id,primary_field_id)复合FK，保留DEFERRABLE来支持CreateTable循环；不是把“active且text Primary”等产品状态写进约束。
- Record稳定归属由table_id FK兜底；Scope成员可加Record FK（软删除保留），scope成员PK/ON DELETE CASCADE保留。跨Table物化由受控INSERT SELECT/校验保证；如将来物理清理Record，必须先定义Scope/历史引用保留，不随意cascade。
- Guard(actor,key)永久PK保留；Receipt可清理，Guard不能。Legacy非G2/G3请求不因此被虚构永久保证。
- Audit已知Operation可FK operation ledger；legacy NULL保留。Change ID是历史来源身份，允许Change先过期，不加阻断Retention的强FK。Event中的历史Field/Option/Record身份不是当前active对象的强制引用。
- 拟增operation ledger（id、actor可空仅Bootstrap上下文、kind、writer_version、事务标识/结果关联），支持同步Audit/Receipt引用与审计覆盖。它不是业务事件溯源数据库。清理只在无Audit/Receipt/Guard引用后进行；永久Guard引用导致最小Operation身份也持久，需统计体积。
- 拟增records.projection_version/必要的schema投影epoch，用于验证派生数据来源。版本只用于投影有效性，不作为Scope成员变化的替代判断。

## View Order：完整数组还是规范化关系

推荐R1保留tables.view_order完整数组及独立view_order_revision，最大100成员；Go Order模型在同一Table门锁下保证完整active permutation。顺序/Default/config/普通Table revision仍独立，不迁移成position浮点而丢全序CAS。

替代是table_view_order(table_id,view_id,ordinal)、PK/复合FK/Unique(table_id,ordinal)，另Order头revision。优势是成员FK与关系查询；代价是reorder至多100行写/延迟唯一约束或重编号、仍不能仅靠FK证明“包含所有active View一次”，生命周期与CAS仍需Go。当前边界下收益不足，不为形式规范化新增第二套Order事实。未来若移除100上限再评估，不能上线后偷偷改变上限。

## Query、索引与权限

普通Record Cursor继续绑定原View/Field Revision合同；Scope有自己的成员语义指纹，不混用。Sort/Display变化重启页面Cursor但保Scope，Table Change可能使scoped分页410但不意味着成员Scope失效。

统一typed Query IR供Record/Map/Distinct/Aggregate/Scope使用。Filter、Sort、Aggregate、JOIN、Keyset在SQL中；Scope transition只给受影响ID集合，不拉全表到Go。projection只影响响应列，不改变匹配集合。

当前Search使用strpos(search_text,foldedTerm)>0，普通B-tree不解决任意substring。R1用EXPLAIN (ANALYZE,BUFFERS)在隔离合成库评估热点：number/date/location表达式B-tree、MultiSelect存在性GIN、长文本substring的pg_trgm。不能把strpos直接替成LIKE而忽略%/_转义、短词、Unicode和NULL语义，也不能只CREATE GIN就声称原表达式可用该索引。索引预算/扩展安装须独立说明。[PG16 pg_trgm](https://www.postgresql.org/docs/16/pgtrgm.html)是能力依据，是否适用必须计划验证。

Field rename只改定义；Option rename/color/order不重写无关Record。Field删除/恢复改变searchable集合，投影服务须在事务内重建相关search贡献或采用按active字段贡献查询的经测辅助结构；不能保留陈旧search_text。转换更新所有受影响canonical/投影并保原Record revision合同；辅助索引可重建但不能先供客户端读到半成品。

R1默认同步重建，按ID分块读取降低Go峰值内存，但**分块不跨事务提交**。若100k宽行预算不通过，再单独评审shadow projection generation+原子切换；未验证的异步投影不参与当前查询事实。

未来Relation的规范化表只在获批类型实施时加入，源/目标稳定实体优先真实FK；反向索引和依赖图由同一UOW维护。Computed值与用户canonical值区分；权限必须先进入SQL计划，再count/aggregate/分页，不能先LIMIT后在Go隐藏数据。

## 投影一致性与恢复

同一事务写Canonical与Projection，保存codec版本。提供隔离/维护用可重复验证器：按原schema和Registry版本重建，比较query_values/search_text语义哈希；不修改canonical来迁就派生值。修复派生列不增加Record业务Revision、不制造Audit；若修复会纠正查询成员或跨越语义epoch，显式失效旧Scope/页面上下文，不声称成员永远未变。

导入、恢复、Schema/Convert、Duplicate、旧数据升级均需同一投影策略。Dump恢复同时保留schema/key/ID/Change/Receipt/Guard；覆盖验证见 [迁移策略](migration-strategy.md)。全表投影/O(N)转换与JSONB Audit写放大要实测，不用架构口号替代。
