# Server/API — Codex 工程执行入口

Repository: https://github.com/Mahjong404/LoomTable-Server.git

2026-10-12。用户已授权 Codex 接管 Server/API 的合同、实现、集成验证、部署与维护，在已批准产品语义内连续推进 G1–G6；Devin 负责合同与设计明确后的 Plugin 实施。本仓库维护合同与 Server 交付事实，跨仓库唯一状态表仍由 Plugin 规划工作线维护。

## 必读输入与当前边界

1. 本仓库 CONTRIBUTING.md、[docs 索引](../README.md)、[CONTEXT](../../CONTEXT.md)、[Field Types](../field-types.md)、[OpenAPI](../api/openapi.yaml)、[架构](../architecture/overview.md)/[存储](../architecture/storage-model.md)/[同步](../architecture/sync-model.md)。这些定义当前领域与传输事实。
2. 整个 Wave 的审批/Gate 与 Plugin 接入边界见 [Plugin API Boundary](https://github.com/Mahjong404/LoomTable-Obsidian-Plugin/blob/main/docs/wave1.5/api-boundary.md)，整体进度只在 [Plugin Wave status](https://github.com/Mahjong404/LoomTable-Obsidian-Plugin/blob/main/docs/wave1.5/status.md)。handoff 固定相应文档 commit，不能依赖聊天或忽略目录。
3. 检查实际 branch/HEAD、远端 main、status/diff、用户已有改动、go.mod 与授权 Gate；阅读相关 domain/service/HTTP/storage/测试。固定交付基线见各 Gate 合同与 delivery 的 SHA，执行前重查。

DatePicker、当前 Choice ID、Query Panels、Display、Grid 几何、Date消费者和当前读/Undo竞态属于 Plugin 的既有合同 UI 工作，不需要新的 Picker endpoint。Time/DateTime、通用 Required、Table rollback、bulk Mutation 和 View 回收站没有从设计批准自动获得实现权限。

## API-first：一个 Gate 一个可审查合同

1. **合同先行**：明确获授权 Gate，创建该 Gate 的正式合同评审文档（例如 docs/wave1.5/contracts/gX-contract.md，实施时才新增）。先冻结领域、请求/响应/错误、权限/预算、revision/幂等/未知结果、兼容与 rollout；提出并审查 OpenAPI 变更。候选名字不能当现存 endpoint。
2. **批准后实现 Server**：按合同改 domain/model/validation/service/HTTP/SQL 与必要 migration；保模块化单体与现有事务/receipt seam，不整体重构。审查锁序和所有相关 writer，不只测新增 happy path。
3. **隔离测试**：合同/HTTP/unit/并发与专用可销毁 DB integration；验证旧新 client/Server、replay/CAS/no-op/权限/expiry与回滚。无隔离环境则停在未验证，不碰真实数据。
4. **审查与交付**：按明确授权进行 PR/提交/发布，交付完整40位 Server commit SHA、OpenAPI/合同路径、测试、必要迁移/回滚和 capability/版本范围。合并与部署分别记录，未部署不能声称能力可用。
5. **Plugin 才接入**：Plugin 以批准 SHA显式 api:sync/生成/decoder/client/Queue/消费者；由其集成切片把 Server PR/commit/能力证据同步到唯一 Wave status。Server 不自行改 Plugin 代码或 pin，也不要求另一个 Session 消息作为依赖。

本仓库的合同文件/PR保留 Server 实施与验证证据，整体状态只引用它们；不要再建立第二份全 Wave 完成表。一次交付一个明确 Gate；用户本轮已授权六 Gate 的 Server 工程工作，按依赖顺序逐项交付，真正缺失的产品决策、不兼容协议或危险迁移仍须裁决。

已交付 G1/G6 的固定 SHA、CI、备份/恢复、部署及 Plugin 接入条件见 [G1 交付](deliveries/g1-choice-palette.md) / [G6 交付](deliveries/g6-aggregate-search.md)；G6 精确语义见 [Aggregate Search 合同](contracts/g6-aggregate-search.md)。G2 合同与事务实现、隔离验证已形成候选交付，见 [G2 交付](deliveries/g2-atomic-choice.md) / [Atomic Choice 合同](contracts/g2-atomic-choice.md)；尚未合并或部署，独立实现审查待完成。本次授权到 G2 停止，不自动启动 G3。

## 六条合同 Gate 的 Server 任务边界

这些产品目标已获本轮 Server 工程授权；具体 Schema 仍需逐 Gate 合同审查，当前实现事实以源码和固定交付 SHA 为准。所有 Gate 共用上述 API-first、兼容/安全验证和交付清单。

| Gate                               | 目标 / 批准时缺口                                                                      | Server 修改入口                                                                                                                                    | 核心验收 / Plugin 交付                                                                                                                                                                                                |
| ---------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| G1 12 色 writer                    | 当前9色writer，新增lime/indigo/brown；reader未知色安全保值                             | internal/catalog/service.go、domain/model.go、Field规范、OpenAPI；检查真实schema，不假定加色需要DB迁移                                             | enum/保存/未知色resave、旧新矩阵、capability/reader-first；Plugin收到新writer合同后才启用新色                                                                                                                         |
| G2 Atomic Choice Existing/Draft    | 当前Field PATCH与Record赋值分开；需要create/reuse+assign或outerCreate单事务all-or-none | internal/record/model.go/service.go、catalog normalization、postgres record.go/field.go/catalog.go/convert.go、httpapi/receipt；必要writer锁序协调 | 双revision/权限/labelcanonical/budget、同名race/convert死锁、failure零部分效果、replay-before-CAS/applied-noop、bounded Field delta、immutable body/id；交付Queue persisted schema/unknown恢复所需协议，不假ID/提前写 |
| G3 View order CAS                  | 当前按createdAt列表，没有Table orderRevision；新建/复制/普通restore尾部一次            | domain/catalog/postgres view.go/table.go/httpapi、必要order migration/backfill、OpenAPI                                                            | 同snapshot list+revision、完整active permutation、并发/409/no-op/replay/delete/default、跨Table拒绝、旧client与migration回滚；不自动新增View Trash UI                                                                 |
| G4 Query RowSelection              | 当前普通cursor/loaded IDs不能代表完整Filter+Search snapshot                            | record/query/model/service、storage query、httpapi及正式scope/expiry机制                                                                           | count/membership/token/exclusions/fingerprint、snapshot vs live、权限/TTL/latequery、sort/display不变成员、空disabled/partialunchecked；选择不授权bulk写入                                                            |
| G5 Table Audit / Record projection | 当前事件覆盖/metadata不足；需要分页前Recordcategory投影及event-time事实                | record/change/history/model、Field/View/convert等writer、postgres change/history/retention、httpapi及versioned event schema                        | 所有已授权writer、typed presence/label/color、category先于limit/cursor、stableoperation/change身份/applied-noop、retention/真实availability与旧reader兼容；先只读投影，不先重建完整rollbackengine                     |
| G6 Aggregate Search                | AggregateRequest当前fields/filter，无Search；需与Recordquery同域                       | record/values.go/query.go、service/requestvalidation、postgres values.go/query.go、httpapi、OpenAPI                                                | Filter+Search/仅Search/空值/trim、权限/active/snapshot、Count/Sum/Avg/Min/Max/null0、搜索投影与旧Serverfallback；独立正确性补齐，P1.6 API例外先批准                                                                   |

本轮用户已授权六 Gate 的 Server 工程，执行顺序与理由见下文。G2/G3/G4 的精确合同和并发风险更高，各自评审后实施。G1与G2、G4与G6没有人为互锁；共享domain/OpenAPI/SQL writer一次一个集成owner。

## Future Gate

- **Time/DateTime**：先决定Time-only/instant/wall-time+zone、序列化/精度/跨日/DST/nullable/unset/config/Required来源、查询/转换/审计与兼容。HH:mm分钟值只是候选。之后Server字段和值校验/查询/存储→OpenAPI→固定commit→Plugin TimeDraft/Clock/Wheel/AMPM/消费者。12h仍Future，无Figma Motion/工具开发任务。
- **整 Table 回滚**：inclusive回到目标操作前，覆盖Record/Field/options/config/View/default/order；需versioned inverse、全writer head/checkpoint/依赖、真实earliestRestorable、preview/headCAS/权限/expiry、atomic publish/unknown。30d日志可读不代表30d可恢复，localUndo不替代。独立合同/阶段，不阻断当前Record投影。
- Avatar/Profile/Cascader、细粒度转换审计及其他Deferred能力没有当前任务。

## 测试、环境与完成定义

实际命令以 go.mod、CONTRIBUTING 和 CI为准。源码切片通常运行 targeted go test、go test ./...、go vet ./...、修改Go文件的gofmt检查与git diff --check；涉及部署文件时才验证Compose。HTTP/OpenAPI合同测试位于 internal/httpapi，domain/service在各模块 *_test.go，事务/并发在 internal/storage/postgres/integration_test.go。

集成测试读取 LOOMTABLE_TEST_DATABASE_URL，会执行 DROP SCHEMA public CASCADE；连接前要求 LOOMTABLE_TEST_DATABASE_DISPOSABLE=true。安全入口为 bash scripts/operations/integration.sh：新建私有网络和 tmpfs PostgreSQL，不发布端口，不接受已有 URL/容器/卷，退出清理本次资源。仅用专用可销毁CI/test数据库和合成数据；名字含test不代表可重置长期共享DB。不要默认启动本机Docker/PostgreSQL，不运行migration/管理命令指向生产，不重置远端部署。没有该环境时 go test 跳过DB测试，必须明确写未验证，不能报integration通过。

DoD：批准合同与OpenAPI一致；真实实现/权限/事务/兼容测试完成；必要迁移/rollout/回滚审查；没有凭据/真实数据/临时产物；PR/合同/40位SHA/实际部署能力/测试限制可交给单独Plugin环境。新consumer不得依赖内部SQL或未公布endpoint。

## 推荐执行顺序与持续工作入口

顺序维持 G1 → G6 → G2 → G3 → G4 → G5。G6 复用现有 Query Search normalization、search_text 与只读快照，零迁移，独立修正聚合口径；G2 需先冻结双 CAS、receipt 与 writer 锁序；G3 有独立 order migration；G4 需真实 Query snapshot/expiry 决策；G5 覆盖所有已交付 writer，因此安排在 G2/G3 之后。若 Plugin 需求改变优先级，由合同影响说明调整，不修改产品语义。

每个 Gate 的合同与交付证据留在 contracts/ 和 deliveries/；不新建全 Wave 第二份状态表。Server 不改 Plugin 源码、API pin 或状态表。

## 可直接提供给后续 Server Session 的启动输入

> 在本Server仓库读取docs/wave1.5/README.md、CONTRIBUTING、领域/架构与当前OpenAPI。处理本次明确授权的Gx Gate，先产出并审查领域与OpenAPI合同，再在合同批准后实现Server及隔离测试。保持Future关闭，保留用户改动。交付合同/PR/完整Server commit、capability与兼容矩阵、真实测试和回滚信息，供Plugin之后固定pin接入。不修改Plugin/Figma；按既有用户授权完成审查、合并和安全发布，生产数据安全与必要裁决要求持续适用。

正式handoff必须替换Gx、指定合同评审还是已批准实现、提供文档commit/审批与隔离环境信息。若仅授权文档整理，就停在文档，不执行Server切片。
