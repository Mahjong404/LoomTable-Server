# R1验收矩阵与性能预算

本页建立可检验目标，不是R0性能PASS。R0未运行DB/HTTP/并发/迁移/benchmark；只进行了生产READ ONLY元数据检查。本轮无测试容器/卷/网络需清理。所有未来实际运行仍遵守 [云端 SOP](../../agent-ops/server-cloud-sop.md)。

## 基线和计量纪律

复用 [现有预算/20k脚本](../../performance-budget.md) 的5次预热、至少30次有效测量、p50/p95与响应bytes；正式R1报告同时给raw样本/误差，必要时扩大样本，不能靠一次最快值。既有脚本默认端口可能撞生产，先在云端隔离项目指定已确认空闲端口/网络/卷，禁止在开发者本机运行。

固定参考4vCPU/8GiB（历史目标规格；云端达不到则记录实际规格并做同环境成对比较，不声称绝对预算通过）、PG16/Go版本、CPU内存/连接限制、索引/扩展/热冷缓存/并发/数据分布。R0不保证当前云端主机正好有该额度。

A基线：固定G6 f3980b1b4126cdfce026c0a94779c8c8261b3997，用于已部署合同/旧Client兼容。B基线：R0固定 3e7c58a372a4b230033f096611355cb7c25cd780 的完整G2–G5，实现现有Trigger；用于同功能新旧架构比较。二者不能混为一谈：G6无Audit等成本，不能用Go新功能性能和空旧路径做虚假对比。当前功能代码与e83242b一致。

历史G5的10k Audit/100页Race合成读写已在delivery记录，但不是20k/100k HTTP p95，也不是R1基线。没有可复核原样本时先重测B，不从旧PASS臆造数值。

## 提议阻断预算（待设计确认，R1实测）

| 场景 | 20k参考p95上限 | 同环境B回归限 | 备注 |
|---|---|---|---|
| 首屏Query | 500ms | 不超过1.20×B | 空/NULL/长文本/多选/Location混合 |
| Search/Filter+Sort | 750ms | 不超过1.20×B | 同一Filter+Search语义，记录真实计划 |
| Map viewport / Summary | 750ms / 1.5s | 不超过1.20×B | bounded features，禁止截断 |
| 单Record Mutation（含Audit） | 250ms | 不超过1.30×B | 非Race release构建；含DB往返/Commit |
| 100-command Mutation | 1.5s | 不超过1.30×B | 事务原子，不拆100次请求 |
| Scope Acquire 10k / 100k | 1s / 5s | 不超过1.20×B | 当前硬deadline10s；100001拒绝且不泄漏成员 |
| Scope Resolve（≤500probe） | 250ms | 不超过1.20×B | 不全量扫描；Exclusions≤5000 |
| Audit category/filter连续页 | 500ms/页 | 不超过1.20×B | count/高水位/Retention generation一致 |
| 100k普通Query /复杂Query | 1s / 2s（研究目标） | 必须报告，超限给索引/成本决策 | 不是已有产品SLA |
| 100k宽Field Convert | 先测B再定绝对上限 | wall≤1.20×B、peak RSS≤0.70×B的目标 | 原子性不因分块取消；目标未达不得靠跨Tx提交 |

相对预算和绝对预算都报告，不能只挑容易的一项。若新纯Go实现不可达，回到索引/批写/投影/Audit大小的成本分析；不未经批准把产品规则搬回复杂Trigger，也不把损失Audit/Scope正确性当优化。RSS/WAL/锁等待没有B样本时标Not Verified；不在R0伪造观测数字。

测试集：20k×50字段为基本，100k×50为压力，10/200字段为宽度抽样，500字段上限作功能边界；稀疏/满填、长文本、Select/MultiSelect/Attachment引用分布记录。不是所有组合的无目标全笛卡尔积。记录DB表/索引大小、WAL、heap/GC、rows examined、lock wait/hold、deadlock/timeout、Audit bytes/op、Receipt/Guard增长及Scope成员存储。使用EXPLAIN ANALYZE只在合成库，不能无评估地跑在生产。

## 事务、并发和协议验收

| 场景 | 必须验证的结果 |
|---|---|
| G2同名Option、多Field Draft、12色、deleted Option | 一真实active身份；Field/Record双CAS；失败无Option/Record/Audit/Receipt/Guard半效果 |
| CAS与幂等 | 同时旧revision只一成功；Replay-before-CAS；不同Body复用拒绝；Receipt过期Guard终止；unchanged原结果 |
| Unknown Outcome | Commit成功后丢HTTP；Commit返回未知；进程中断；原身份重放不重复业务/事件 |
| G3 Reorder/Create/复制Create/Delete/Restore/Default | 完整active permutation；strict CAS；tail append-once；Default/config/No-op不改变Order；同RR List |
| G4 10k且只加载100、100k上限 | count覆盖完整Filter+Search；多页无漏重；禁止loaded page冒充全选；100001完整rollback |
| G4成员迁移 | 新非成员进入、成员离开/删除/restore导致成员变则失效；普通值变但成员不变保留；Sort/Display保留Scope |
| G4所有Writer/两实例 | 注册表逐项覆盖Field/Convert/Choice/View/祖先/维护；Acquire与Writer交错，检查新数据+有效旧Scope不可能 |
| Permission撤销 | Token401、资源撤销不可续页/写；恢复不复活；验证撤销Commit线性化与在途请求边界 |
| Scope TTL/GC | 5min边界、重启/共享key跨实例、晚响应、两个Retention=forever也能清理；TTL过期永远不可用 |
| G5 Fact | Go event-time rename/color/tombstone/转换old/new、multiField、精确Change关联、No-op/Replay不重复、完整Schema |
| Privacy /未知历史 | 嵌套attachment坐标不泄露、redactedProperties、v0缺metadata诚实、读恢复旧payload也保护 |
| Retention | bounded cleanup、水位/generation+410、Count/cursor正确、Guard与必要Operation身份不清理、无假30天Rollback |
| Query/G6/Map | Filter-only/Search-only/组合/null与casefold、Aggregate同域、projection schema生命周期与重建；权限在limit/count前 |
| 故障注入 | 每个Apply/Projection/Scope/Audit/Change/Receipt/Guard阶段报错，确认完全rollback；Audit不能异步补成功 |
| SQL权限/绕过 | 最小Runtime DDL失败、旧binary/裸DML的D03策略、注册Writer缺Effect与非法Finalize拒绝 |
| Migration/Restore | 006→011→012、已有011→012、新库一致；重复apply/并行Migrator拒绝、checksum差异、DB+附件恢复、旧Client/新Server |
| 序列/时间 | 大于单Table的全局sequence有间隙正常；commit前隐藏事件；ancestor fanout按Table锁；时间相等不丢事件 |

每条R1交付记录源码/镜像40位SHA或digest、命令/退出码、环境隔离证据、完整日志哈希、raw benchmark样本/计划、资源清理与Not Verified。用正常外部PR Review核查事务/CAS/迁移；不启动重复审查Subagent。

## R1最终上线Gate

零正确性/隐私/数据丢失阻塞；所有Writer进入受控UOW并有矩阵测试；旧12个Trigger依赖退役且唯一Go规则；D03例外若采纳只有基础设施职责；全部合理约束和身份保留；同环境性能预算/资源增长可接受；恢复与旧Client验证；正常CI/外部Review；Plugin固定pin/能力/Queue/未知结果handoff。通过这些仍只代表Source Ready，生产发布另行授权。
