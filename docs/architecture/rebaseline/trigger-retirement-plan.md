# Trigger与Stored Function逐项退役

源码固定基线见入口；当前生产001–006并没有这些自定义函数。此表是R1设计，不是本轮DROP命令。所有现有10函数/12个绑定一一列出，FK内部Trigger、Unique/Check/锁/SQL执行不属于退役范围。

| SQL定义与源码行 | 当前绑定 | Go替代与必须先具备的证明 |
|---|---|---|
| 008:12 maintain_view_order | 008:53 views_maintain_order ON views（1） | Domain ViewOrder + lifecycle UOW；Create/Restore/Delete/Reorder竞争、回滚/No-op、完整permutation、初始revision与旧Client合同 |
| 009:28 revoke_ancestor_selection_scopes | 009:40 tables_revoke_selection_scopes；:42 bases_revoke_selection_scopes；:44 workspaces_revoke_selection_scopes（3） | Access/Ancestor Use Case→Scope影响清单；共享权限屏障+Table锁，revoke/restore不可复活，两个实例与Acquire竞争 |
| 010:31 loomtable_audit_operation | 被下述Capture调用（无独立Trigger） | UOW OperationID/Writer version显式值，Receipt持久化/replay原ID，Bootstrap/维护与No-op身份明确 |
| 010:41 loomtable_audit_cell | Capture内部（无独立Trigger） | Go TypedDelta codec+before/after定义上下文，missing/null/empty、Option tombstone和转换metadata差分 |
| 010:60 loomtable_capture_audit | 010:112 records、:114 fields、:116 views、:118 tables同名capture_audit（4） | 各Writer Domain Effect→中央Audit Builder/Append；错误注入证明业务/Audit/Receipt/Guard全回滚；derived-only写不造事件 |
| 010:92 loomtable_associate_audit_change | 010:120 associate_audit_change ON changes（1） | 预分配明确ChangeID和Effect关联，不再SQL按object/op后查；G2多Field+Record、Convert一个操作多事实 |
| 010:98 loomtable_capture_ancestor_audit | 010:122 bases、:124 workspaces同名capture_ancestor_audit（2） | Go祖先用例在一致锁序下显式fanout到后代Table，Create并发不会漏；分页sequence与commit可见性证明 |
| 011:2 loomtable_audit_safe_value | 被下述函数递归调用 | Go单一Redactor保存/读取复用；嵌套数组/对象/保留attachment身份、原DTO不变 |
| 011:15 loomtable_audit_safe_payload | 读Audit SQL+下述Trigger调用 | Go Redactor保redactedProperties，覆盖恢复的旧未删减payload、legacy importer；不能只改新Writer |
| 011:28 loomtable_sanitize_audit_payload | 011:34 sanitize_audit_payload ON audit_events（1） | Audit Store只接受已脱敏Fact，read defensive使用同一Go策略；Go-only后裸写防线由UOW/权限或下述guard承担 |

合计：008 1绑定 +009 3 +010 7 +011 1=12。Migration010仍含legacy import SQL，导入/筛选/批写本身不是必须搬离SQL；历史分类/unknown语义由明确Importer Go计划决定。011旧历史redaction已经完成的数据库不能“回滚恢复秘密坐标”。

## 迁移节奏与防双写

R1可在隔离库用冻结旧版本作oracle，对同种合成操作比较事件语义，忽略独立生成ID/时钟；另验证真实新ID/关联唯一性。不能在同一业务库同时让旧Trigger与Go Audit都写正式事实，会重复事件/Order revision。对现有007–011测试库，阶段切换以一个新迁移原子禁用该组旧领域Trigger并启用Go Writer contract；只运行一个获准writer版本。R0不编辑或DROP现有迁移。

G3→G4→G5每片验收后，在专用环境演练组合切换，未完成Go对应Writer之前不得拆除其数据库保障。所有12绑定都要用pg_trigger非internal清单复核；函数退役前先去读路径/其他函数依赖（尤其Audit reader safe_payload、conversion GUC），再在新迁移中显式处理，不CASCADE误删。

## 允许保留的基础设施

FK实现Trigger、基本Unique/Check、行锁/advisory、数据库序列与SQL Query都保留。R0没有发现001–011中另一个不可替代的基础设施自定义Trigger；旧12个均计划退役，不用“降级兼容”永久保留复杂产品语义。

可选新例外（待用户D03确认）：一个非常小的“require UOW envelope”检查，限定当事务有效operation/writer版本，缺上下文拒绝DML。允许读取事务Envelope/角色，不允许Field/Record值diff、Option、Order、Scope、Audit推导或递归JSON规则。更安全的理由：Go封装无法阻止持有DB写凭据的旧程序/误操作；fail-closed比事后抽样更可靠。代价：新增一个明确基础设施函数及绑定、Bootstrap/恢复路径需显式适配、旧binary不能无条件降级写。若拒绝此例外，目标为零自定义Trigger，接受 [事务方案](transaction-and-uow.md) 中gap检测的限制。此决策不是本轮保留旧Capture的理由。

## 旧binary兼容终点

旧Client通过新API仍兼容；旧Server直接写目标新库不再承诺。008原来为旧binary维护Order的保障，在新Go-only schema上撤除后已不存在；不能同时声称触发器退役且G6旧binary仍安全执行View生命周期。降级只可能是支持同一新Writer contract的已验证兼容构建，或停写恢复独立备份并明确数据差额；不许把旧binary ready当作领域写兼容。这个运维代价须设计确认、发布单独批准。
