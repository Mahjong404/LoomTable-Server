# Git 治理审计：2026-10-16

本页记录本次只读核查和实施边界，不替代产品状态表。审计 main：`3e7c58a372a4b230033f096611355cb7c25cd780`。数据来自 GitHub connector 的 branches、全部分页 PR、commits、Git refs/tags、releases、rulesets 与 repository metadata，以及本机只读 Git/Worktree 检查。两个仓库当前均无独立 Issue；Plugin 仅 #116 开放，Server 无开放 PR（治理 PR 创建前）。

## 分支台账

A：已完成、成果保全、当前未发现有效依赖；B：等待开放 PR；C：先保全/裁决；D：活跃或 main。所有建议删除在实际执行前必须重查远端 HEAD 和本机依赖。下方保留删除前的分类与映射；实施结果见执行记录。

| 分支                                                | HEAD SHA                                   | 最后提交 UTC         | HEAD 在 main | PR 状态                                                              | 合入方式证据                            | 未合并独有修改                 | Tag/Release 保全 | 本机 Worktree                 | 类别 |
| --------------------------------------------------- | ------------------------------------------ | -------------------- | ------------ | -------------------------------------------------------------------- | --------------------------------------- | ------------------------------ | ---------------- | ----------------------------- | ---- |
| docs/correct-development-log-main-baseline-20260830 | `ef97cd8fbc937a7e601804222a784821e95685cf` | 2026-08-29T17:09:11Z | 否           | [#9](https://github.com/Mahjong404/LoomTable-Server/pull/9) Merged   | Squash 相符：1 父，tree 与 PR HEAD 相同 | PR 全部内容已合入；无后续 HEAD | 无 HEAD 引用     | 无同名 Worktree               | A    |
| docs/g6-release-evidence                            | `0da4dd5c2a0084e8da92f69374f68e7985c3434f` | 2026-10-12T14:36:21Z | 是           | [#12](https://github.com/Mahjong404/LoomTable-Server/pull/12) Merged | Merge Commit                            | 无                             | 无 HEAD 引用     | 无同名 Worktree               | A    |
| docs/release-status-audit                           | `44089b65a90935175ff4a949280deb2619a3bea6` | 2026-08-17T22:11:33Z | 是           | [#7](https://github.com/Mahjong404/LoomTable-Server/pull/7) Merged   | Merge Commit                            | 无                             | 无 HEAD 引用     | 无同名 Worktree               | A    |
| docs/server-development-log                         | `588dcfd2c071ce3503a14e655bec8e9bd04863ea` | 2026-08-29T14:58:09Z | 是           | [#8](https://github.com/Mahjong404/LoomTable-Server/pull/8) Merged   | Merge Commit                            | 无                             | 无 HEAD 引用     | 无同名 Worktree               | A    |
| docs/v0.1.0-release-status                          | `826e56003575ddb275501e4caff7578c9cbd144c` | 2026-08-17T21:56:40Z | 是           | [#6](https://github.com/Mahjong404/LoomTable-Server/pull/6) Merged   | Merge Commit                            | 无                             | 无 HEAD 引用     | 无同名 Worktree               | A    |
| docs/wave1.5-g1-contract                            | `c99c3fb96f4e9610abcb72ece19fb76a3534c8f8` | 2026-10-12T10:56:01Z | 是           | 无                                                                   | —                                       | 无                             | 无 HEAD 引用     | 无同名 Worktree               | A    |
| feature/attachment-p1                               | `52dc89f47ec3477f6517a729fce0f5add77307cf` | 2026-08-14T05:44:00Z | 否           | [#3](https://github.com/Mahjong404/LoomTable-Server/pull/3) Merged   | Squash 相符：1 父，tree 与 PR HEAD 相同 | PR 全部内容已合入；无后续 HEAD | 无 HEAD 引用     | 无同名 Worktree               | A    |
| feature/g1-choice-palette12                         | `47fc985bd9376dc761469b40fe40dcfd1ceb483e` | 2026-10-12T14:03:11Z | 是           | [#10](https://github.com/Mahjong404/LoomTable-Server/pull/10) Merged | Merge Commit                            | 无                             | 无 HEAD 引用     | 无同名 Worktree               | A    |
| feature/g6-aggregate-search                         | `f3980b1b4126cdfce026c0a94779c8c8261b3997` | 2026-10-12T14:28:07Z | 是           | [#11](https://github.com/Mahjong404/LoomTable-Server/pull/11) Merged | Merge Commit                            | 无                             | 无 HEAD 引用     | 无同名 Worktree               | A    |
| feature/server-p0                                   | `d05d13c8dc101b1d182232db52010752d62e49e2` | 2026-08-13T18:44:37Z | 否           | [#2](https://github.com/Mahjong404/LoomTable-Server/pull/2) Merged   | Squash 相符：1 父，tree 与 PR HEAD 相同 | PR 全部内容已合入；无后续 HEAD | 无 HEAD 引用     | 无同名 Worktree               | A    |
| fix/map-broken-field-ids                            | `36562816f38e0db9a3668e1a01ecab005e6cf10f` | 2026-08-15T19:25:52Z | 是           | [#5](https://github.com/Mahjong404/LoomTable-Server/pull/5) Merged   | Merge Commit                            | 无                             | v0.1.0 直接指向  | 无同名 Worktree               | A    |
| fix/mutation-409-openapi                            | `0faef837647338e16907a4fda955837cbea50cef` | 2026-08-15T17:58:17Z | 是           | [#4](https://github.com/Mahjong404/LoomTable-Server/pull/4) Merged   | Merge Commit                            | 无                             | v0.1.0 可达      | 无同名 Worktree               | A    |
| main                                                | `3e7c58a372a4b230033f096611355cb7c25cd780` | 2026-10-15T09:23:30Z | 自身         | 无                                                                   | —                                       | —                              | 无 HEAD 引用     | 主 checkout（审计初始 clean） | D    |

“Squash 相符”根据 PR 已 Merged、合入 commit 为单父、合入 tree 与最终 HEAD tree 完全相等、HEAD 后无新增提交得出；REST PR 不单独返回 merge_method。未把 Squash 的独有提交 SHA 误判为未合入代码。一般 Merge Commit 通过双父且包含 PR HEAD 核实。

| 分支                                                | 删除前 HEAD                                | PR  | 合入 SHA                                   |
| --------------------------------------------------- | ------------------------------------------ | --- | ------------------------------------------ |
| docs/correct-development-log-main-baseline-20260830 | `ef97cd8fbc937a7e601804222a784821e95685cf` | #9  | `ab949d59c37680d53b4109e1502f8478b24cc655` |
| docs/g6-release-evidence                            | `0da4dd5c2a0084e8da92f69374f68e7985c3434f` | #12 | `bb00e6ad3d100a6e5e0879d1e8c1fb25ab51bfec` |
| docs/release-status-audit                           | `44089b65a90935175ff4a949280deb2619a3bea6` | #7  | `e02f055fecddc0852085dc5a71b4eb136860774a` |
| docs/server-development-log                         | `588dcfd2c071ce3503a14e655bec8e9bd04863ea` | #8  | `41a403cae63e75e5b523a1a10c2318e43834082a` |
| docs/v0.1.0-release-status                          | `826e56003575ddb275501e4caff7578c9cbd144c` | #6  | `9eec4440e5b8e76771299bac58f7ab4f949cb60c` |
| feature/attachment-p1                               | `52dc89f47ec3477f6517a729fce0f5add77307cf` | #3  | `2988f7dd4f87d811e0191a84b9e6b28f8c1f2471` |
| feature/g1-choice-palette12                         | `47fc985bd9376dc761469b40fe40dcfd1ceb483e` | #10 | `173c2f031dd4acf0957a40afcb07e94125e8305f` |
| feature/g6-aggregate-search                         | `f3980b1b4126cdfce026c0a94779c8c8261b3997` | #11 | `bf6237b7450af3ff3ca1ba9d0155c19096a352fb` |
| feature/server-p0                                   | `d05d13c8dc101b1d182232db52010752d62e49e2` | #2  | `76c8ec377258ac9eeffe0a241b9976f03afe46df` |
| fix/map-broken-field-ids                            | `36562816f38e0db9a3668e1a01ecab005e6cf10f` | #5  | `ef0c6bd751642f4a604fe1bf88980f64e39dd992` |
| fix/mutation-409-openapi                            | `0faef837647338e16907a4fda955837cbea50cef` | #4  | `0bd67e910e9abf2b60100c7bb0366ca40fa6212b` |

没有远端 C 类分支。feature/server-p0、feature/attachment-p1、docs/correct-development-log-main-baseline-20260830 的独有提交因单父合入而不成为 main 祖先，但整个最终 tree 与合入 tree 相同，PR 交付无独有内容待合入。

## 本机保全与未推送内容

审计初始只有 main Worktree，clean，HEAD `e83242be0a4f340b182561d8d2f7dd7f04bd5270`；真实远端 main 多 1 个只改 AGENTS 的提交。后续发现本地 refactor/server-architecture-rebaseline 指向真实远端 main，说明有其他工作在准备；本次不切换、不清理任何本地分支/Worktree。

本地 feature/g1-choice-palette12 (`00eb533fc842496eb82476b46d4afe5075f8bd8f`) 是远端 HEAD 的直接父，落后 1，没有领先提交；docs/wave1.5-g1-contract 同名本地与远端一致。g2/g3/g4 等仅本地历史分支不在当前远端列表，不能把缓存 tracking refs 当远端分支。它们保留，不在本次删除范围。

上述检查只覆盖当前主机；其他设备、远程实现者的未推送内容无法从 GitHub 读取。删除候选若有新 Owner/依赖出现，降为 D/C 后重审。未清理任何未确认 ignored 快照。

## Tag、Release 与身份

- v0.1.0 → `ef0c6bd751642f4a604fe1bf88980f64e39dd992` (commit)
- [v0.1.0 Release](https://github.com/Mahjong404/LoomTable-Server/releases/tag/v0.1.0)，target `ef0c6bd751642f4a604fe1bf88980f64e39dd992`，发布于 2026-08-17T17:49:19Z。

本地两仓有效 user.name/user.email 均继承用户全局合法身份；connector 当前账户为 Mahjong404，profile 邮箱与有效本地邮箱一致。历史 main 还存在另一个关联同一 Mahjong404 的个人邮箱，不需要因此统一改写。PR Author 与 Commit Author 分别查询，当前相关 PR 的 author.login 均为 Mahjong404。个人 Emails 设置是否 verified 未通过受限 connector 核实；API 账户映射已核对。

当前 main 的完整 77 个可达提交 Author 均为 Mahjong404，使用两个已由 GitHub 映射到同一用户的历史邮箱；没有 Co-authored-by 工具尾注。部分平台合并提交由 GitHub/web-flow 作为 Committer，这是平台合并记录，不是另一位产品作者。不改写历史作者或 SHA。

## Settings 与执行限制

两仓默认分支 main；allow_squash_merge、allow_merge_commit、allow_rebase_merge 均 true；delete_branch_on_merge 均 false；rulesets 返回空，branch list 的 protected=false。读取 main/protection 返回 **403 Resource not accessible by integration**；用户 metadata 显示 admin=true 不等于 GitHub App 有 Administration 权限。用户明确授权改用已登录 GitHub 网页后，两仓 Settings → Branches 均显示 Classic branch protections have not been configured；rulesets 为空，当前没有 Required Checks 保护配置。本次未新增保护规则，以免改变在途产品 PR 的合并约束；建议治理 PR 检查通过后再按 SOP 配置 main 防强推/删除及适用检查。

当前 connector 没有删除 ref 或修改 repository Settings 的写入接口；tags/contributors 直接列表 URL 不在 fetch 白名单（400），Tag 已通过允许的 git/refs/tags 核实。用户在本 Session 明确批准改用网页执行安全清理和设置；未使用 git/gh 远端写入。

已执行：网页启用 Automatically delete head branches；Squash 默认消息改为 PR title + description。随后通过 connector 重新读取，确认 delete_branch_on_merge=true、squash_merge_commit_title=PR_TITLE、squash_merge_commit_message=PR_BODY。Merge/Rebase 保留供已说明的例外，未改变当前 PR 状态。如需复核，打开 General → Pull Requests；分支保护入口为 Settings → Branches / Rulessets。

必需检查保持现有名称：Go tests and vet、Docker build and Compose validation。先让本治理 PR 的检查实际出现并通过，再在 UI 核对 Required Checks；不以新 job 名代替原门禁、不关闭全部 CI。

## CI 差异与第三阶段

原工作流所有 PR/main 均启动 PostgreSQL、Go tests/vet 和 Docker/Compose。提案新增正文白名单、范围回归及稳定 Go 汇总门禁；正文不启动数据库，其他变更保留完整 Go/HTTP/Schema/PostgreSQL。只有测试文件的 PR 省 Docker，代码/依赖/迁移/镜像/未知路径仍构建；main 全量。范围或测试失败不可通过 skip 绕过。

原来没有非 main push 触发，不新增它；PR 验证拟合入内容，main 验证实际合入内容，各有目的。新提交仅取消同 PR 旧运行；main 使用 run_id，避免 pending main 被取消。不新增 Matrix。

治理前远端 **13** 条；安全清理后 **1** 条。已逐条删除 A 类 **12** 条，删除前 HEAD 与审计一致，网页每行显示 Deleted / Restore；随后 connector 重新列举确认删除结果。Tag/Release 与 main SHA 均未改动。D：main。治理 PR 会新增一个短期 chore/git-governance 分支，合并后按同一流程退役。

第三阶段：待产品 PR/活跃 Worktree 的 Owner 确认交付后，重查 HEAD、批准、独有提交、dirty/untracked/ignored 与下游 PR，再退役剩余历史名称。无需追求分支数量恒为 1。治理 PR 自身合并后同样退役。

## 简明交接

从最新 main 开始；任务语义短分支；使用用户合法身份、不加工具共同作者；一个目标一个主要 PR，CI/行为/Schema 必须 PR；普通小任务不强制公开 Issue；默认 Squash，重要历史例外需说明；Tag/Release 独立版本；保留其他 Owner 文件与 Worktree；失败用 Revert PR；清理先核实并记录 SHA。完整规则见 [Git 工作流](git-workflow.md)。本 Session 使用 0 个 Subagent。

## 本次验证

本地不启动 Server/数据库/Docker，仅验证 CI Python、YAML、门禁失败/跳过状态及文档链接。Go 全量、隔离 PostgreSQL 与 Docker 由治理 PR Actions 执行；实际状态查看 PR 最新 checks，不以本地静态验证冒充云端通过。

- 22 个范围场景通过，包含 prose、Go tests、Schema、Migration、Docker/依赖、未知路径和 main。
- Go 汇总门禁验证 scope 失败、测试失败/取消、错误 skip 均失败；合法 prose skip 和完整测试成功通过。
