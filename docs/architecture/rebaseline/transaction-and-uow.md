# Unit of Work、事务与G2–G5一致性

## Interface与强制Finalize

推荐一个Application WriteSession管理一个DB事务。Domain只收到有类型对象/依赖事实，返回Effect；Tx-bound Repository只接受受控写计划，不把db/Tx/Commit暴露给Transport、Domain或CLI。不是把现有各Store.Mutate方法在外面再包一层事务。

提案伪码（不是现有Go Interface/可运行源码）：

~~~text
Application.Execute(command, Principal)
  validate shape + exact legacy request fingerprint
  UOW.Execute(writerKind, requestIdentity?, principal, func(session):
    authenticate current Principal / credential
    replay actor-scoped Receipt or expired Guard BEFORE resource visibility/CAS
    authorize normal execution + lock declared resource scope
    load authoritative before states + event-time definitions
    domain.plan(command, before) -> effects/results
    session.apply(effects)                 # no nested transaction
    return proposed result
  )
  UOW central Finalize:
    verify all canonical/query/auxiliary and Audit/Change stages complete
    flush any pending facts under the same transaction
    validate/invalidate affected selection scopes
    persist exact replayable result + required identity guard
    commit once
  return committed result, or Unknown Outcome
~~~

所有字段/Record/元数据修改、Order、Scope失效、Audit/Change、Receipt/Guard在同事务。不存在业务提交后再调用另一个Service补Audit/Option。只读Query/Aggregate/Audit走独立短RR，不套写UOW。

G3现行Receipt允许在资源已变化/不可见时先重放，仍必须当前凭据有效、Actor作用域匹配；不能在重构时把资源权限检查移到其前并声称协议未变。未来Team撤销与完整Receipt快照可见性的取舍须另立合同，不能在此提前实现ACL。Scope/read请求继续按当前资源权限验证。

转换/大量Record时，UOW的Apply管道按有界chunk同步维护canonical/projection并构造、redact、Append Audit/Change（均未Commit），只累积受影响ID/摘要和完成标记，不在Go保留全Table before/after/Event列表。Finalize校验所有已登记Effect阶段完成并做Scope/Receipt/Guard。任何chunk失败回滚全部；流式并不等于异步Audit或跨事务分块提交，因而能实际降低Convert峰值内存。

Repository seam建议按本Use Case的实际需要分小Interface：CatalogTx提供锁定/条件更新元数据，RecordTx提供有界加载与条件Apply（canonical+projection），QueryExecutor执行统一IR与候选成员比较，AuditTx只Append已构造/脱敏Fact，Receipt/Guard端口按Actor+原Key读写。它们由一个Tx绑定工厂创建，不能返回裸SQL字符串供Application执行，也不能由Repository再调用Catalog/Record Application Service或隐式开启子事务。测试用受控fake验证用例顺序，用真实Postgres验证约束/隔离，不建设泛化的跨数据库框架。

Convert分块必须在同一Table门锁/同一事务中按稳定ID keyset取有界Record chunk，关闭Rows后再Apply该chunk，避免同一DB连接保留查询游标时再次执行写入的conn-busy问题。不能使用OFFSET在改写中跳行，也不能把所有Audit Event缓冲在Finalize。R1保留deleted Records转换覆盖和原RecordRevision语义；Missing/Null等边界先用批准合同+fixture确认，不借重构暗改转换损失规则。

请求身份可先按Actor+原Key获取事务级每Key锁（哈希碰撞最多额外串行，实际唯一性仍靠PK）；保留已有actor粗锁作为R1.1起点，按基准证明后再切每Key方案，避免同时改全部锁协议。非幂等旧API不会凭空要求新Key。Replay保原OperationID/Status，不生成新Event；Guard到期规则保G2/G3，legacy不回填虚假永久保证。请求hash/legacy字段缺失的序列化要字节/语义固定fixture验证。

Commit成功后才公开applied/operation header；Commit error/网络断开是Unknown，禁止自动换Key执行。可以确定回滚的deadlock/serialization failure才在严格有界策略中重试完整幂等用例；不对未知Commit重跑。Receipt可序列化内容在Commit前验证，跨进程HMAC key持久不更换。

## 锁与多实例证明

起点保留Table写门锁（真实PG row lock），不是Go mutex。一个Table内所有成员/Schema/Order Writer与Scope Acquire共用门锁；不同实例连接同DB也相同。先正确再按热点测量细化，协议不依赖单进程内存。

建议全局顺序：请求身份/配额advisory（如需要）→权限/祖先行→Table IDs升序→Field IDs升序→View IDs升序→Record IDs升序→Attachment IDs升序→Scope IDs升序。只有实际需要的层才锁；所有多Table命令预声明完整闭包，锁后不得发现更小ID再补锁，不能中途反转。

- 祖先名称/生命周期影响后代Table时，在祖先锁下列出并按ID锁全部Table；Create子资源先取同祖先保护锁，避免新增Table逃过投影/权限失效。此路径未来大扇出要预算，不能换成无保护多次事务。
- Scope创建沿全局allocation quota锁→Access/祖先共享锁→Table门锁→物化；预算计数/插入同事务。普通业务Writer不取allocation锁。过期清理从分配用例拆为有界维护步骤，避免持一个Table锁时做全库大清理。
- 无需Actor身份变化的FK关系用合适锁模式，保留当前NO KEY UPDATE避免KEY SHARE死锁的原因。Token/Attachment并发、SQL外键隐式锁也进死锁测试。
- CAS是Domain检查后SQL WHERE id/table_id/revision/required lifecycle RETURNING的原子条件；0行区分Conflict/不可访问。Order CAS与Record/Field CAS独立；Table锁不能代替公开CAS。
- RR分页在同snapshot校验权限/Scope/Schema并读取count/page；不跨HTTP持事务。Scope仍稳定不代表排序值/页面Cursor不会过期，保留现有Change-tail页面失效语义。

未来Relation/计算依赖可能跨Table，需在独立Gate冻结资源闭包/并发策略；R1不先实现分布式锁、CRDT或通用任意事务脚本。

## G2：Choice与Draft

锁Table、Choice Fields升序，再目标Records升序；任何写前完成Field与Record双CAS。纯Go Choice resolver按active canonical name复用，deleted不暗恢复；真实OptionID服务端产生。每Field一次config revision/Delta，Select/MultiSelect输入顺序/预算、New Draft取消零持久化都保留。一个UOW保存Option+Record+Audit/Change+Receipt+Guard，注入任何阶段失败全部回滚；同名并发不留两个active身份。Convert与Choice共享同一Table→Field顺序。

## G3：Go View Order

一个纯Order模型处理Create（含复制后Create/初始View）、Restore append-once、Delete remove并保其余相对顺序、Reorder完整active permutation。相同排列也先strict CAS；No-op不增OrderRevision、不造Audit。Default/config改变不触发Order；List+Order在同RR。

没有DuplicateView endpoint，不新增原子复制API来“修架构”。普通Restore仍受原CAS/错误合同，不凭空增加Receipt；幂等Create/Order的Receipt重放不重复append。CreateTable初始Order revision1→初始View追加→2，保既有G3语义。恢复历史位置未来必须有真实permutation/checkpoint，不调用普通tail Restore冒充。

## G4：固定成员、Go决策与SQL集合执行

Scope持久固定Record ID集合S，count/semanticDataVersion只属于创建边界；TTL5min、100k成员上限、Exclusions/探针/预算沿合同。普通值变化不是失效理由；Sort/Display/列宽不进MembershipFingerprint。

Finalize对每个未过期Scope：Go规范化原Filter+Search和依赖上下文；真正Schema/权限/原View语义改变按合同失效；否则Repository对本事务ChangedRecordIDs执行“原S包含ID XOR 当前同Query匹配ID”的SQL集合比较，Go据结果设置invalidated。候选由PK定位，不重新扫描全Table；最多每Table4有效Scope，因此普通写成本约O(4×changed IDs×predicate cost)。非成员Record可能进入，必须也检查，不只检查已选成员。一个事务的最终集合才对外可见，不因事务内暂态或普通data version增量误失效。

Acquire在Table门锁下物化S；所有成员Writer先持同门锁并Finalize。两种序列：Acquire先完成→Writer比较S并失效，Writer先完成→Acquire看到新集合。成员写与失效同Commit，RR Reader只能看到前一套或后一套状态，不会读到新Record+旧有效Scope的裂缝。SQL执行谓词属于Query执行，不是另一个Go/SQL业务规则引擎。

| Writer | 必需影响登记 |
|---|---|
| Record create/update/delete/restore | 所有实际改变的Record ID，包括非成员与生命周期 |
| Record Duplicate | 新Record ID；Move只有排序变化，Scope保留，页面Cursor可失效 |
| G2 Choice | Field Option语义影响+所有Record ID，同一Finalize |
| Field create/update/delete/restore | 依赖语义/active searchable集合变化；纯改名/颜色/展示保持；Search投影维护不可漏 |
| Field Convert | 目标类型/Config语义及实际转换Record集合，保RecordRevision合同 |
| View create/update/delete/restore/default/order | 原Scope View的Filter/可访问性变化失效；Sort/Display/Default/Order本身不失效 |
| Table/祖先/权限更改 | Table/Actor资源范围权限或active上下文撤销持久失效，Restore不自动复活旧Scope |
| 导入/修复/恢复/管理员写 | 必须受控维护入口、明确影响集/epoch；绕过入口不承诺Scope正确 |

权限“立即”定义为撤销Commit的线性化点：之后开始/续页不能继续旧授权；已在途请求若已取得共享权限屏障，其完成先于撤销Commit。不能承诺已发往网络的字节能追回。Token撤销阻断该凭据401，不随意使同Actor其他有效Token下的所有Scope失效；实际资源权限撤销则invalid并绑定权限epoch。多实例使用DB锁/epoch，不靠内存缓存。R1只落当前Personal所需屏障/接口；Team政策另Gate。

## G5：Go Fact，不是异步补写

UOW分配OperationID、预分配ChangeID/EventID，Domain Effect显式关联它们；一个操作多Effect、一个Change可关联多个必要Audit事实，不能靠时间/object近似UPDATE拼归属。Operation/Event身份独立，保现有Opaque格式/Replay。No-op不造Audit，Receipt重放不再Append；多步业务有真实Effect时不因最终某值相等就抹去过程。

Builder从事务内before/after对象和完整Field/Option定义快照构造typed cell：Missing/Null/Empty/Value、Field类型/名称、Option名称/color/删除状态，转换携带明确old/new定义，不再SET LOCAL conversion override。旧资料缺metadata保持unknown。FieldDefinition加载需包含名称/Config/tombstone，不能复用当前只有Type/Config的窄投影然后从“最新定义”补历史。

Redactor为Go单一递归策略，保存和读取都调用同一实现；省略storageKey/vaultPath并保redactedProperties。保payloadVersion0/1、legacy未记录事实、category-before-limit/count、RR+high-sequence+Retention generation+410。现有before/after体积若预算不通过，先评审v2 Delta合同，不直接丢掉旧事实或改enum。

必须同步原子保存Audit Fact。未来Outbox只允许在同事务追加供异步通知/搜索/物化；通知失败不丢Audit，不把派生系统当事实。Operation identity与Audit availability保留历史可扩展性，但不实现完整Inverse/Checkpoint。

## 防遗漏与未仪器化Writer的诚实边界

WriterKind注册表列全部现有Use Case/维护路径；每种声明资源锁、所需before context、Effect类别、Projection/Scope/Audit策略和测试。UOW只接受注册种类，中央Finalize缺必要上下文失败；Repository的Apply是受控Effect入口，无公开任意Exec/独立Commit。建立静态SQL写路径清单/架构import检查与对每个Writer故障注入，不靠人工记得调用hook。

最小权限：Owner/Migrator专用，Runtime无superuser/CREATE/ALTER/TRIGGER/BYPASSRLS权限，凭据不提供给普通CLI/Plugin。管理员repair也走显式维护入口。仅靠Go封装与权限无法证明一个拥有Runtime SQL权限的错误程序没有绕过；不能谎称无Trigger还可自动发现任意DML。

建议用户批准一个仅检查UOW Envelope的基础设施guard：验证写入带当事务有效operation/writer envelope，缺失直接拒绝，不推导事件/成员/生命周期、不做redaction、不查询Fields。它能挡旧binary/偶然裸DML，不证明一个已获Envelope的恶意或错误Writer生成了完整Audit；完整性仍由统一Apply/Finalize和测试保证。若用户要求零Trigger，采用纯Application封闭入口+定期一致性抽样，但覆盖gap检测为best-effort；原始未知历史不能事后伪造。维护/恢复epoch已知gap显式标记，旧SQL fallback不继续制造“完整”历史。

### D03基础设施guard的具体范围

若批准：业务表的BEFORE写入只核验当前事务携带的operation ID能匹配当事务创建的write envelope（例如持久Operation行中的xid8与当前PG事务ID），以及允许的Writer contract标识；缺失即拒绝。operation row与SET LOCAL由UOW建立，池化连接不遗留上下文。检查不读Field/Option/Record内容，不推导事件或权限，不把函数当作完整Audit正确性证明。实际DDL/表范围、Bootstrap actor为空与受控维护的上下文协议在R1.1先测试，再纳入012；不在R0提供可直接生产执行的SQL。Owner/恢复管理员始终能绕过，必须用已知maintenance epoch标记覆盖缺口。若无需此例外，就不实现它，不能悄悄保留旧Capture代用。

PostgreSQL行锁/事务级与会话级advisory lock的区别以 [PG16 Locking](https://www.postgresql.org/docs/16/explicit-locking.html) 为依据；上述资源顺序与撤销协议是本项目推荐设计，不是文档声称自动保证的产品语义。
