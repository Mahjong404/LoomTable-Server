# API Gate 工程与消费者交接 SOP

一次处理一个明确获授权 Gate。正式合同与本仓事实分别在 [contracts](../wave1.5/contracts) 和 [deliveries](../wave1.5/deliveries)。本页固化过程，不改变已批准业务合同。

## 合同先行

读AGENTS/CONTEXT/Wave、Plugin API Boundary（只读）、固定OpenAPI及源码。明确不变量、全部相关Writer、权限/预算、完整错误Schema、CAS/Receipt/Replay/Unknown Outcome、锁序/失败原子性、旧新矩阵、Migration/降级/Capability版本。先判断现有Endpoint/Mutation seam能否承载；规划候选名不是接口事实。

G1检查reader/writer颜色兼容；G6复用Filter+Search范围；G2跨Field/Record同事务与双CAS；G3独立OrderRevision；G4固定实际ID集合及增量失效；G5精确身份、event-time事实与分页前投影。不能用loaded IDs、filter-only聚合、两次HTTP或最新metadata假装正确。

实质产品分歧、不兼容协议或危险迁移给出具体选项/影响/推荐交用户裁决，暂停争议代码；内部命名普通风格直接决定。完成标准是合同/OpenAPI/兼容/迁移均可审查，未决项明确。

## 最小实施与身份一致

Domain→Service→Postgres→HTTP沿既有层和事务seam；业务与必需Audit同事务，失败零部分效果。核对跨Catalog/Record锁序与所有生命周期Writer，保Revision/CAS、G4成员检测和G6查询语义。

先Replay再CAS；ClientMutationId/Key按合同Actor作用域。发送前持久完整原Payload/身份，丢Response为Unknown Outcome，原Body/Key恢复；不能换ID盲重试。验不同Body复用ID、Receipt过期Guard、Applied/Unchanged；no-op/replay不造事件。扩展字段缺失时保旧JSON/hash/重放，错误遵守公开Schema。

已登记迁移不能靠编辑原文件完成升级；新编号验证Forward/Retry/既有版本升级/BackupRestore/旧binary兼容。实践见 [云端 SOP](server-cloud-sop.md)。

## QA、正常Review与合并

本地无服务静态/纯逻辑，云端新可销毁环境完整HTTP/OpenAPI/PG事务/并发/Race、Migration/Restore、旧新binary与重启/丢Response。固定测试源码SHA、证据哈希、隔离环境、真实命令/退出码、SKIP/Not Verified，CI不替代集成。

按CONTRIBUTING检查diff/ignored/untracked并精确stage；私有报告/日志/备份不入Git。成熟后集中push一条主PR，不微改反复CI、不关门禁、不恢复旧GitHub MCP。默认0 Subagent；独立性由正常外部Review提供，作者自审不冒充独立PASS。修具体finding、核最新HEAD的CI/Review，按用户授权与仓库规则合并。Merge与生产Deploy分别记录。

## Readiness与Plugin handoff

状态分别为Source Ready、Merged、Deployed、Capability Verified。运行版本/实际迁移/Meta/readiness从现场获得；Capability仍需真实Endpoint验证。迁移缺失/Unavailable/Expired/权限撤销按合同门控，不静默回退改变业务。

交消费者：Gate业务与Approved Contract；固定40位Server SHA/PR；OpenAPI版本/Diff/新增Schema；Client生成与decoder影响；Queue持久schema/原身份恢复；Revision/CAS/Receipt/Delta/Event identity；权限/失败；Capability/旧新矩阵；测试与限制；实际部署；兼容/回滚；接入顺序/验收。

Server不改Plugin源码/API pin/跨仓状态表。Plugin由实现Agent按批准SHA显式api:sync/生成并开发真实consumer，生产使用等待已发布且实际验证能力。旧Queue/Change decoder按合同保留；晚响应、410、未知历史metadata不能造假状态。

## Future与停止

Gate验收完即停止。G4选择不授权Bulk写；G5Audit不等于30天完整回滚；Time/DateTime、Inverse/Checkpoint/DependencyClosure/完整Table Rollback要独立合同与授权。新任务从实际缺口继续，不重复全Gate审计或另建Wave进度表。
