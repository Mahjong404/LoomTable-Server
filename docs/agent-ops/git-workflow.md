# Git 工作流

本仓库采用轻量 Trunk-Based Development。开始 Git 交付、创建分支/Worktree、Review、合并、发布或清理前读本页。产品范围仍由原阶段合同与唯一状态表决定。

## 恢复上下文与文件所有权

1. 核对真实远端 main、任务分支 HEAD、PR 状态、Tag/Release；本地 origin/main 只是缓存。读取 AGENTS、CONTRIBUTING、对应阶段合同、唯一状态和 gitignored 本地 TODO。
2. 本地只读核对 `git status --short --branch`、`git worktree list --porcelain`、`git log -5 --oneline` 和完整 diff。记录 40 位基线 SHA、Owner、可改文件、验收与停止条件。
3. 多个实现者优先各用一个任务 Worktree；共享文件先明确一个写入 Owner。状态表、锁文件、生成合同与 CI 也属于共享文件。遇到已有修改保留现场，先确认归属再继续，不用 reset/clean/stash/强制切换消除冲突。
4. 交接只记录任务/Issue、分支/PR、基线与交付 SHA、Owner/活跃路径、验证及限制、下一步；延用原状态表，不靠跨 Session 消息恢复，不另建产品进度台账。缺少私有 TODO 时说明缺口，不上传私有内容。

## Trunk 与任务分支

唯一长期开发分支是 `main`。每个独立目标从最新 main 开始，尽早交付一个可验证切片，完成后退役任务分支。不同版本由各自仓库的 SemVer Tag/Release 管理。

只创建以下任务前缀，后缀用简短 kebab-case：

| 前缀      | 用途           | 示例                       |
| --------- | -------------- | -------------------------- |
| feat/     | 功能           | feat/boolean-cell          |
| fix/      | 缺陷或紧急修复 | fix/grid-frozen-overlap    |
| refactor/ | 行为不变的重构 | refactor/audit-transaction |
| docs/     | 纯文档         | docs/git-workflow          |
| test/     | 测试           | test/query-conflicts       |
| chore/    | CI、依赖与维护 | chore/git-governance       |

不创建长期集成、发布、预发或版本维护分支，不以个人或执行工具命名分支。已有历史命名是迁移例外：保留当前 PR、Worktree 与交接，任务结束后退役，不为改名重开 PR 或改写历史。Worktree 目录也使用任务名称。

## 作者与 Commit

用户本人授权并拥有的工作使用用户自己的合法提交身份；核对有效的 `git config --show-origin --get-regexp '^user\.'` 和当前 GitHub 账户。姓名/邮箱必须归属本人，邮箱应关联其 GitHub 账户。当前环境已有正确身份时不重复改配置；不能读取账户设置时明确验证限制，不猜 noreply 地址。

Author、Committer、PR Author、Co-author 是不同字段。工具不是独立项目贡献者：未来不自动附加工具作者或 Co-authored-by；保留真实外部人员应有署名，不把他人贡献改归用户。提交前检查完整 message/footer；Squash 前也检查最终 message，避免把 WIP 中的自动尾注重新带入 main。平台生成的 Committer 记录不等同于产品作者。

标题使用简洁 Conventional Commits，例如 `fix(grid): preserve frozen offsets`、`chore(ci): scope pull request checks`。分支内可多次 WIP Commit；关键验证完成后集中推送。精确选择本任务文件，检查完整 diff、ignored/untracked 和秘密，不提交依赖、构建产物、Vault、Token、日志或私有运维资料。

本执行环境远端查询/写入默认走已授权 GitHub connector，本地 git 仅作工作树/diff/分支/历史读取。连接器失败记录具体错误、保存可审阅成果，不自动降级 CLI；CLI 例外需用户明确授权。若必须保留本地 commit 历史而 connector 无法等价完成，先说明限制，再请求真实 Windows 用户上下文。其他运行环境沿其已获授权工具规则，作者和分支规范一致。

## 选择 Issue、PR 或直接 main

| 场景                                                                                | 入口与门槛                                                  |
| ----------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| 极小文档、注释/错别字、行为不变的简单维护                                           | 必要本地验证完成且符合保护/权限时可直接 main；不满足则走 PR |
| 产品行为、UI、多文件重构、跨模块 Bug、CI、OpenAPI/Schema、Migration、权限/认证/事务 | 必须使用聚焦 PR                                             |
| 产品裁决、多切片、多实现者/跨仓、复杂 Bug、分阶段架构                               | 建立 Issue；需要时 PR 关联原 Issue，避免重复台账            |
| 早期建议或简单维护                                                                  | 先进入 gitignored 本地 TODO；无需全部同步为公开 Issue       |

PR 描述交代具体问题、结果、相关需求/Issue、验证和限制；一个 PR 聚焦一个目标。草稿 PR 可用于早期审阅，准备好后核对最新 HEAD 的必要检查与 Review。自审不能冒充独立 Review；需要用户视觉或合同批准的任务等待对应批准，不以绿色 CI 代替。

## Review、合并与回退

默认 Squash and Merge。需要保留重要分步历史或迁移证明时，在 PR 中解释例外再选择 Merge Commit 等方式。合并前核对目标 main、最新 HEAD、完整 diff、CI、Review、产品批准及最终标题/作者/尾注。发布和部署另有授权，合并不意味着生产部署。

仓库 Settings → General → Pull Requests 启用 Automatically delete head branches。此设置只处理完成合并的 head，不主动合并/关闭 PR，也不清理本地 Worktree；它不是历史分支审计的替代品。启用前检查是否有其他开放 PR 以该 head 为 base，或仍有依赖它的工作；存在依赖时先完成交接，再启用设置或单独保留该 ref。

需要撤回已合入改动时，创建 `fix/*` 或 `chore/*` Revert PR，说明被回退 SHA、原因和验证。Migration、数据写入、发布包与部署分别评估补偿/兼容，不把源码 Revert 当数据库回滚。不重写 main，不 force push，不移动已发布 Tag；历史作者修正必须单独取得用户明确批准。

## Release 与跨仓兼容

Plugin 与 Server 独立演进版本。正式发布从获批准且验证通过的固定 main SHA 创建 SemVer Tag 和 GitHub Release；记录源码 SHA、变更、构建/测试证据、兼容要求与回滚条件。已发布 Tag/资产保持可追溯，修复发布用新版本；不创建长期 release 分支。

跨仓兼容依靠固定 40 位 Server/OpenAPI SHA、Capabilities 与正式发布记录。Source Ready、Merged、Released、Deployed、Capability Verified 分开记录；不从相同版本号或 PR 合并推断兼容/可用性。

## CI 与通知

PR 和 main push 均验证；不为任务分支额外增加重复 push 工作流。PR 同一编号的新提交取消旧运行；main 使用 run_id 独立分组，避免排队中的 main 运行也被替换。Release/部署使用自己的策略，不沿用 PR 取消组。不增加无明确兼容需求的 OS/运行时 Matrix。

Workflow 级不设置 paths/paths-ignore 过滤。范围检测采用明确正文白名单，未知路径运行完整检查；删除和重命名的旧/新路径都要计算。必需检查保留稳定名称，即使仅文档也产生结果；范围检测失败必须使最终门禁失败，不能通过 skip 掩盖。

个人通知无法由仓库 YAML 控制。在 [个人 Notifications](https://github.com/settings/notifications) 的 System → Actions 中，保留需要的 Email/On GitHub，选择 Only notify for failed workflows 并 Save；这会减少成功/取消运行邮件，不减少检查。另查仓库 Watch → Custom/Participating and @mentions、单个 Issue/PR 的 Subscribe，以及个人 Review requests 通知偏好。Watch、参与讨论、被请求 Review 和 Actions 运行通知是不同来源，按邮件主题/通知原因分别定位；不要为了减少邮件移除必要 Reviewer 或关闭质量门禁。

GitHub 将被触发运行的完成状态通知给订阅用户；订阅和个人投递偏好由账户持有人管理。YAML 仅减少无价值重复运行、取消过时 PR 和按范围节省检查。具体见 [Actions 通知](https://docs.github.com/en/subscriptions-and-notifications/how-tos/managing-github-actions-notifications)、[Workflow 通知来源](https://docs.github.com/en/actions/concepts/workflows-and-actions/notifications-for-workflow-runs) 与 [Watch/参与通知](https://docs.github.com/en/subscriptions-and-notifications/get-started/configuring-notifications)。

## 分支与 Worktree 清理

删除前重新查询真实远端，不从旧 Session 摘要或分支名决定。记录 Branch → 40 位 HEAD SHA → PR/合入 SHA → Tag/Release，核对所有本机 Worktree、同名本地分支的未推送提交及已提交/未提交/未跟踪内容。当前主机之外的环境无法核实时说明边界；有有效依赖则保留。

| 分类 | 判据                                                                 | 操作                                     |
| ---- | -------------------------------------------------------------------- | ---------------------------------------- |
| A    | 已完成；祖先可达或 Squash 内容等价有证据；无有效依赖，成果有可靠保全 | 复查 HEAD 未变化后删除远端；记录实际结果 |
| B    | 仍关联开放 PR                                                        | 等 PR 和批准结束后重审                   |
| C    | 有未合并成果、唯一历史或保全/归属不明确                              | 先保全并由用户决定去向                   |
| D    | 活跃工作、Worktree 或交接依赖                                        | 保留；Owner 结束后重审                   |

Squash 后 ancestry 不相等不代表丢失代码；比较 PR 最终 HEAD、合入 commit 的 tree/patch 和 main 可达性，并确认合并后无新增工作。发布 Tag 可保全历史；仅“API 还能按 SHA 打开”不足以保全唯一成果。远端删分支不会改变已有 main Contributors。

Owner 确认任务结束后，本地同步 main，再核对 dirty/untracked/ignored 与独有提交，保全有价值的私有快照，再清理已退役 Worktree 和本地分支。当前环境需本地 Git 写入时先按上节取得例外授权；不批量删除他人 Worktree、不递归清除未确认快照。误删 ref 时用台账 SHA 经已授权接口恢复分支；保留 Tag/Release、PR 和原历史。

## Server 验证范围

`Go tests and vet` 保留为稳定最终门禁，汇总范围检测与实际 Go job；`Docker build and Compose validation` 名称保持不变。纯 Markdown 正文仅验证差异空白；API Schema、Go、Migration、构建/依赖/脚本/CI 及未知路径运行全量 `go test ./...` 和 `go vet ./...`，使用 GitHub 托管 runner 独立 PostgreSQL 16 服务及专用可销毁测试库，保留 HTTP/Schema、事务、并发和迁移覆盖。

Docker/Compose 在 main 始终验证；PR 除纯正文和仅 Go 测试文件外运行镜像构建及 Compose 配置验证。测试文件本身仍运行完整 Go 测试。生产镜像或部署不由本 CI 自动发布。

本地禁止启动 Server/数据库/Docker，CI 的云端隔离服务不属于开发者本机；真实云端 QA 与发布仍按云端 SOP 独立授权。CI 成功不冒充实际生产部署或云端完整验收。
