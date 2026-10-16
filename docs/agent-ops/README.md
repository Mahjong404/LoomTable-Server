# Server 工程与云端运维入口

Codex、DSH、Devin 或其他 Agent 开始 Server 任务时先读本页，再按任务读取 SOP。这里描述工程过程，不替代领域合同、OpenAPI 或私有配置。

## 按任务进入

| 任务                                                 | 必读                                                | 完成标准                                                                         |
| ---------------------------------------------------- | --------------------------------------------------- | -------------------------------------------------------------------------------- |
| 分支、身份、Issue/PR、Review、合并、发布、CI 或清理  | [Git 工作流](git-workflow.md)                       | 任务语义分支、合法作者、适用门禁和可追溯交付                                     |
| 云端只读检查、QA、迁移验证、恢复演练、清理或获批发布 | [云端 SOP](server-cloud-sop.md)                     | 固定源码、实际隔离证据、结果和资源收尾；生产动作另有授权                         |
| API Gate 合同、实现、Review、交付或 Plugin handoff   | [API Gate SOP](api-gate-sop.md) + 该 Gate 合同/交付 | 合同与代码一致、真实 QA、外部 Review/CI、Source/Merge/Deploy/Capability 分开记录 |
| 存储盘点与清理交接                                   | 云端 SOP 的盘点/收尾节                              | 验证 docs/local 忽略，记录字节、唯一性、归属和清理条件                           |

同时读取根 AGENTS.md、CONTRIBUTING.md、CONTEXT.md、[Wave 入口](../wave1.5/README.md)，核对真实 HEAD/分支/工作树与当前授权；保留用户改动。现有 [部署文档](../operations/personal-deployment.md) 的历史版本文字不是当前现场事实。

正式 Gate 合同和本仓事实分别留在 docs/wave1.5/contracts 与 deliveries；跨仓唯一 Wave 进度由 Plugin 工作线维护。盘点、容量明细、原始日志/备份和秘密配置留在被忽略的 docs/local 或独立私有目录，禁止进入 PR。Maintenance 台账不替代 Wave 进度。

## 跨 Agent 交接

携带授权范围、Owner/活跃路径、40位源码 SHA、分支/PR、工作树、合同/OpenAPI/Capability、测试及限制、迁移/恢复约束、实际部署状态、临时资源归属和清理证明。沿固定交付继续，不重复初始化所有 Gate。未运行项写 Not Verified；作者自审不称独立 PASS。
