# LoomTable Server — Agent 工作规范

工作前阅读 `CONTRIBUTING.md`、`CONTEXT.md`、`docs/wave1.5/README.md`、目标 Gate 合同/交付记录与 `docs/api/openapi.yaml`。核实真实 Git HEAD、当前部署、Migration、capability、权限与测试证据；不得以“源码已合并”冒充“生产已部署”。

## Server 云端运行和测试（强制）

- **不得在开发者本机启动或运行 LoomTable Server、PostgreSQL、Docker/Compose 服务栈或服务端数据库集成测试。** HTTP/DB/并发/迁移/备份恢复/降级等测试在云端专用隔离环境执行。本地只可做不启动服务的源码阅读、纯逻辑单测、编译、格式与静态检查。
- **每次访问或操作云端前必须读取** `C:\Personal folder\Server\rainyunMahjong404\server.md`，遵循其中最新部署入口、连接和安全步骤；文件不可访问时暂停相关云端操作，不猜测主机/端口/Token。严禁将私人运维文档内容、凭据、生产地址、日志或备份存入 Git/PR。
- 及时在云端完成真实集成 QA，不以本地 Mock、静态分析或 CI 成功替代真实 HTTP/PostgreSQL 证据；记录源码 SHA、隔离方案、运行命令/退出码、事务/并发/恢复证据和限制。
- 仓库的数据库集成测试可能执行 **`DROP SCHEMA public CASCADE`**：只准对当次创建、可销毁、隔离于生产与共享服务的云端测试数据库执行；不得将生产或共享数据库的 URL 传入测试。优先用现有安全隔离脚本，确认其测试资源完全独立。
- 云端创建临时容器、网络、镜像、数据库、卷及合成数据前明确资源归属；每轮结束核对并清理**仅属于本轮的**临时资源，检查无遗留进程/网络/无用容器。不得清除生产容器、长期持久卷、附件或备份及其他任务创建的资源。
- **部署与研发分开授权**：生产 Migration、写入停机、镜像切换或有风险的运维动作必须获得明确发布批准，先确认停写窗口、全量数据库+附件备份、隔离恢复、固定镜像、迁移顺序、健康和回滚。尚未发布的 007/008/009 等迁移不得在开发 QA 名义下对生产执行。

## 跨项目工具、合同和 Git

- Server 一次实施一个明确授权的 API Gate，先冻结领域/OpenAPI/并发/回滚合同。Server 不修改 Plugin 源码、Figma 设计或 Plugin API pin。确需直接读取 Figma 时**只允许已有 Figwright plugin**；**禁止官方 Figma MCP**；不可用时不猜测设计事实。
- 按 `CONTRIBUTING.md` 使用正常 git/gh 与聚焦的 PR；优先本地完成静态检查、云端完成隔离 QA 后集中推送。不得恢复旧 Codex GitHub MCP，不关闭必需 CI，也不反复制造 PR/重跑工作流。
- 测试必须覆盖实际权限、事务、CAS/Receipt/Idempotency/Unknown Outcome、协议兼容与 Migration；破坏性验证只有云端专用隔离环境可执行。

## Codex Subagent 使用限制

- 默认 **0 个 Subagent**；主 Session 完成主体工作，不例行组织 Standards、Spec、Implementation 多 Agent 重复审查。
- 单轮任务最多 **1 个**必要且有独立可验证输出的 Subagent，必须明确使用**严格低于主 Session 等级的模型**，降低同模型 Thinking Effort 不算。无法确认或指定更低模型时禁止使用；Subagent 不得再创建 Subagent。
- 主 Session 对最终代码/合同/测试负责；独立性必要时采用常规外部 PR Review，不将作者自审冒充独立 PASS。报告委派次数与结果。
