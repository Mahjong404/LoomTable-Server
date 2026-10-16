# R0决策登记与R1实施分片

所有“推荐”都不是批准。本轮产物仅设计；确认设计后才可启动R1，生产发布仍另授权。

## 已由用户明确的方向

U01：Go拥有业务状态/Registry/权限/CAS协调/幂等/Operation/Audit/Order/Scope/Query语义/Convert/批事务；PG拥有持久化、事务、锁、约束、SQL执行/索引/条件CAS。U02：消除复杂领域Trigger双重规则；基础设施例外必须书面理由。U03：不为未发布内部设计永久背负债务，但真实生产数据安全、Git历史/恢复追踪必须保留。U04：R0仅审计/设计，不改业务、不部署，不与Git Governance争改AGENTS/CI。

## 方案与需裁决项

| ID | 决策 | 推荐与依据 | 备选/影响 | 状态 |
|---|---|---|---|---|
| D01 | 当前数据/迁移lineage | 保留全部现有数据；冻结001–011，新增Forward012退役旧规则。同一目标覆盖006和011起点，既有证据不作废 | 新独立Baseline+影子库完整保全迁移更干净但实施/发布/恢复面更大；不提供原库清空捷径 | **待用户确认路线A或B** |
| D02 | 旧Server降级边界 | 新Writer contract之后不再允许G6/旧G2–G5直接写新库；旧Client HTTP兼容仍保，回退用新contract兼容构建/停写恢复 | 保旧binary写必须保其SQL产品保障或桥接，不符合一次权威目标；不能同时承诺两者 | **待用户确认运维代价** |
| D03 | 最小基础设施Trigger例外 | 倾向批准一个仅验证当事务UOW envelope的fail-closed guard，挡偶然裸DML/旧程序；10旧函数/12旧绑定全部退役 | 零自定义Trigger：依赖权限/封闭代码入口/架构测试与事后抽样，无法保证任意DB写都被实时识别；两方案均不防恶意Owner伪造上下文 | **待用户选择guard例外或零Trigger** |
| D04 | Record值模型 | 保Canonical JSONB+同步Projection，热点/未来Relation按需规范化 | 完整EAV在100k×50下数百万Cell行，多条件JOIN/重组复杂；无证据不强换 | 推荐，随整体架构确认 |
| D05 | View Order模型 | 保有界JSONB完整数组+独立Revision；Go生命周期与Reorder同模型 | 独立关系有FK好处但不能独自证明active全排列，100上限下新增复杂性较大 | 推荐，随整体架构确认 |
| D06 | Registry与查询 | 编译期类型Registry+有类型Query IR+SQL执行，不搞动态插件/任意脚本 | 继续每层各自switch会增加新类型改动面；把查询搬内存不可接受 | 推荐，随整体架构确认 |
| D07 | Audit与版本 | Go同事务Fact，暂保v0/v1/legacy Reader及原ID格式；持久Operation关联与统一Redactor | 紧凑payload v2只有在量测或独立产品决定要求时修改合同/Schema；不在0.8悄悄改enum/值含义 | 推荐；v2不预先实施 |
| D08 | 并发与性能 | R1先Table写门锁证明所有Writer/Scope；Actor粗锁起点后可测每Key事务锁，合理SQL约束/索引保留 | 无锁/内存锁/先拆跨事务可能丢一致性；一开始全面细锁使证明面爆炸 | 推荐，需满足性能Gate |
| D09 | 权限“立即” | 以撤销Commit线性化：已取得屏障请求在撤销前完成，后续不可继续；Token与资源授权区分 | 无法追回已发送字节；若要求取消所有在途响应，需要额外产品/通道协议，不能口头承诺 | 明确推荐边界，整体确认 |

D01–D03须在R1规划前明确；D09若用户要求更强在途取消也须先裁决。普通包名/方法名/内部测试命名不是额外产品审批项。R0没有更改现有API Gate合同，它们是R1行为oracle；发现协议语义确须变更时单独修订批准。

## R1分片顺序与退出标准

| 切片 / 本地TODO | 内容 | 先决 / 可独立审查验收 |
|---|---|---|
| R1.0 / SR-R1-001 | 当前基线与失败夹具、20k/100k基准、全Writer测试索引 | 架构确认；F03 Search投影与F04 forever GC运行复现，先看到失败/风险证据 |
| R1.1 / SR-R1-002 | UOW/写入口/Access/锁序/Receipt/Guard/条件CAS基础 | 一个代表Record与Catalog路径打通，已提交/未知结果/回滚、架构禁止绕路；不急删全部Trigger |
| R1.2 / SR-R1-003 | Type Registry/Query IR/Projection、Choice/Convert归Go领域 | 当前10类型契约测试、G1/G6/G2、新旧请求hash；分块读取仍单事务、Audit流式同Tx接口就绪 |
| R1.3 / SR-R1-004 | Go ViewOrder与所有Lifecycle Writer | 新专用Schema中撤008对应旧Trigger，Reorder/Restore/Delete/Default竞争、完整List snapshot与No-op |
| R1.4 / SR-R1-005 | Scope统一影响集/Access撤销/独立GC | 所有实际Writer矩阵、跨实例、10k/100k、跨页与Sort/Display；原009撤销职责移交 |
| R1.5 / SR-R1-006 | Go Fact Builder/Operation/Change关联/Redactor、所有剩余Writer | 对旧oracle差分+新完整Schema、故障注入；不双写正式Audit，不依赖GUC产品语义 |
| R1.6 / SR-R1-007 | 目标Forward迁移、约束/角色、最终Trigger退役与恢复 | 006/011/新空库路径终态一致，checksum/单Migrator，DB+附件恢复，旧Client，D03策略 |
| R1.7 / SR-R1-008 | 全矩阵性能/安全/兼容/正常Review/Plugin handoff | 所有预算和阻断项完成；只交Source Ready，不自动生产发布 |

中间片使用云端自有合成库/目标Schema演练；最终生产迁移文件在R1.6收敛，不能把每个探索Schema发到生产。应用与数据库切换受同一Writer contract gate限制；不能让过渡代码在缺少其保障时报告ready。每片保持可审查提交和真实Not Verified，不以“分片完成”假称整个新架构可发布。

完整Table Rollback、Bulk Mutation产品API、Team ACL、Relation/Computed执行器、新Field类型、实时协作均不在R1当前范围。设计只保留接口/锁资源闭包/数据关系扩展位置。

## R0完成证据与停止

检查文档覆盖10函数/12绑定、Go/SQL责任、约束/CAS/锁/Writer/维护、模型成本、数据保全、预算/故障矩阵、D01–D03；链接/Markdown/差异卫生验证；本地TODO稳定ID更新。只读生产证明001–006/非空/高权限角色，不是R1业务QA。

R0设计PR保持待用户确认，不自动合并为已采用架构、不改AGENTS/CI/Release设置或清理分支。不启动R1，不执行生产DDL/DML/发布。历史Guard/恢复要求仍有效；用户决定新路线后再形成正式采纳ADR及实施合同。
