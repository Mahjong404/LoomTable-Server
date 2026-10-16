# R0：上线前 Server 架构重基线

状态：**设计提案，等待用户确认；不是 R1 实施批准。** 2026-10-16（Asia/Shanghai）。审计固定源码 3e7c58a372a4b230033f096611355cb7c25cd780，业务源码与 G5/SOP 合并基线一致；本轮未修改 Go、SQL Migration、OpenAPI、AGENTS、CI 或生产配置。Subagent=0。

## 推荐结论

采用 Go 模块化单体，Application/Unit of Work 统一事务，纯 Go Domain/Field Type Registry 产生业务效果；PostgreSQL 保留约束、原子条件写、锁、SQL Query Execution 和持久化。保留 Canonical JSONB + 同事务可重建投影，不全面改 EAV。View Order 继续用有界完整数组，但生命周期与 Reorder 均由一个 Go Order 模型维护。旧的10个领域/隐私函数、12个 Trigger 分片退役；是否保留一个仅检查写入 Envelope 的基础设施防绕过机制，须用户裁决。

G2原子 Choice/双CAS/Receipt/永久Guard，G3严格OrderCAS，G4固定成员集合及增量检测，G5同步事务Audit均保留。R1不实现未来字段、Team ACL、Bulk API、Outbox投递或完整Table Rollback。

## 现场与兼容边界

只读生产核验：v0.4.0+f3980b1b4126，health/ready正常，schema_migrations实际只有001–006；public无用户函数/Trigger。生产**非空**，身份、Record、Change、Receipt、Cursor签名材料均须保护；精确计数、角色与部署元数据仅留被忽略的本地证据。运行连接权限过大，应在未来获批迁移中拆分Owner/Migrator/Runtime角色，本轮未调整。

源码OpenAPI0.8.0，G2–G5已合并而未部署；Meta无atomicChoice/viewOrderCAS/querySelection/tableAudit。Plugin只读pin仍为 f3980b1b4126cdfce026c0a94779c8c8261b3997，不能按源码默认启用能力。已有001–006/0.4协议是实际兼容底线；未发布0.5–0.8的内部实现可替换，但更改公开Schema/已批准业务须另行明确。

## 阅读路径

1. [当前职责与风险证据](current-responsibility-matrix.md)：Go/SQL、约束、CAS、Writer、锁和维护。
2. [目标架构与Field Type扩展](target-architecture.md)：职责、依赖与最小接口。
3. [事务与Unit of Work](transaction-and-uow.md)：G2–G5、并发、权限、Unknown Outcome的闭合路径。
4. [Schema方案](schema-options.md)：JSONB/EAV/辅助关系、Order、查询/投影成本。
5. [Trigger逐项退役](trigger-retirement-plan.md)：10个函数、12个绑定及切换条件。
6. [迁移与数据保护](migration-strategy.md)：001–006/007–011、Forward与新Baseline比较。
7. [性能与R1验收预算](performance-and-test-budget.md)：已知/未测、20k/100k、故障注入。
8. [决策与R1分片](decision-register.md)：推荐、待裁决、顺序、停止条件。

本地证据：docs/local/maintenance/r0/production-readonly.json 与 schema-integrity-readonly.json，仅元数据且未查询业务值；不进入Git。R0只进行了源码/约束审计、只读现场核验与文档静态检查；没有新建云端QA资源、没有新性能/HTTP集成结果，不把历史PASS当R1验证。既有 [SOP](../../agent-ops/README.md)、[合同](../../wave1.5/contracts)、[ADR](../../adr)、[性能预算](../../performance-budget.md)仍是历史/现行依据，R0不偷偷修改它们。

R0设计文档进入正常聚焦PR供确认；设计确认前停止，不进入R1，也不以文档合并授权生产发布。
