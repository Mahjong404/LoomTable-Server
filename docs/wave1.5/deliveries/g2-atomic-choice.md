# G2 后端候选交付 — 2026-10-13

G2 实现与隔离验证完成，进入 PR Review Ready；尚未合并或部署，独立实现审查仍待完成。固定验证源码 SHA：`9fa266ab95868640dc0b3e261bd263706ef924df`；基线 main：`bb00e6ad3d100a6e5e0879d1e8c1fb25ab51bfec`。后续交付文档提交不改变此实现。分支 feature/g2-atomic-choice；本轮到 G2 停止，不启动 G3–G5。

## 合同与真实事务

[Approved engineering contract](../contracts/g2-atomic-choice.md) 基于已授权 Wave 1.5 G2 产品语义，经双轴合同审查，无未解决的产品决策。OpenAPI 0.5.0 在既有 POST /v1/tables/{tableId}/records/mutate 的 createRecord/updateRecord 增加可选 choices；没有新增 endpoint。

- Existing Record：expectedRevision + 每个 Choice Field 的 expectedFieldRevision，一次选择既有 Option 或按名称创建/复用并赋值。New Draft：仅在提交 createRecord 时生成真实 Record/Option ID，多 Field、普通 values 与 Choice 同事务；未提交 Draft 无持久写入。
- 名称 Trim→NFC→Default Case Folding、accent-sensitive；复用 active Option 的真实 ID/原名称/颜色/顺序，deleted name 不隐式恢复。Select 恰好一项，MultiSelect 0–100 项为完整最终选择；重复解析身份拒绝。支持 G1 全 12 色。
- Field CAS 优先于 Record CAS，全部在写入前检查；冲突/权限/预算/SQL 失败整体回滚。每 Field 有新 Option 才一次 revision+1/schemaChanged；仅复用无 Field revision 变化，Record applied/unchanged 沿用原 writer。
- 锁序：Actor FOR NO KEY UPDATE → Receipt/identity → Table UPDATE → Choice Fields 按 ID UPDATE → 其他 Fields SHARE → Records 按 ID → 写入。Catalog Update/Delete/Restore/Convert 统一 Table-before-Field；Actor lock 保留串行幂等 writer，同时允许 metadata Actor FK KEY SHARE，避免反向等待。
- Option/Record/Change/Receipt/identity guard 在同一 PostgreSQL transaction；不组合两个 Service 的独立事务。响应 choiceFields 提供 bounded resolvedOptions/createdOptions 与 beforeRevision/revision，真实生成 ID 和 delta 原样持久化/replay。
- 原 Actor + clientMutationId + Table/path + 原 command body 是重试身份；name/color/revisions 不在 hash 前改写。response 丢失/timeout/commit error 不换 key、不自动 rebase。Receipt replay 先于 CAS；过期 guard 同 hash 返回 410 MUTATION_RECEIPT_EXPIRED、operationCommitted=true，改 body 409 IDEMPOTENCY_KEY_REUSED。

## 验证证据

固定源码归档 SHA-256：`056d2b2786243b7db140566a3e5762c4eb4dfdd5654bf877c1db349367439016`。只归档 Git 跟踪的固定源码，不带 .env、凭据、用户数据或 Plugin 文件。

- 本地 go test ./...、go vet ./...、修改文件 gofmt、git diff --check 通过；本地数据库测试因无 URL 跳过，真实 DB 证据来自下列隔离 runner。完整 OpenAPI YAML parse 通过（3.1.0 / 0.5.0，154 schemas / 34 paths）。
- PostgreSQL 16 disposable：新 internal Docker network、tmpfs PG data、无 host ports、独立合成数据。完整 `CGO_ENABLED=1 go test -race -p 1 -count=1 ./...`、`go vet ./...` 与专门再跑 G2 transaction fixture 通过；无 DATA RACE、SQL error 或 test failure。完整 postgres suite 5.225s，G2 fixture 2.845s。
- 实际 authenticated HTTP + DB 覆盖 Existing/Draft/多 Field、Select/MultiSelect、Unicode canonicalization、同名并发严格 CAS、Record CAS 竞争、并发相同 key 的唯一 Draft、Field/Record Delete/Restore、deleted Option 保留/显式恢复/新身份、active/total 预算、权限/foreign ID、全 12 色、unchanged、历史 replay、caller payload 不变。
- 故障与 unknown：前一 command 已写入后后一 command validation 失败回滚；真实 PG trigger 在 Receipt 写入阶段拒绝后 Option/Record/Change/Receipt/guard 零部分效果；移除故障后原 key 重试仅一次。HTTP ResponseWriter 在 commit 后写失败，原 body/key replay 身份和结果稳定。实际 retention 清理过期 Receipt 后 guard 返回 410，其他入口同 key 改 hash 409。
- Convert 并发测试人为锁住第二 Field，确认 Convert 持有 Table 后等待 Field、G2 持有 Actor 后等待 Table，再释放：Convert 无 Actor FK 死锁提交，G2 Field CAS 失败且零写入。测试取消并 join 所有 writer goroutines。
- Migration：隔离 legacy 001–006 fixture → 007，未迁移时 readiness/MigrationRequired 正确；两次 apply 成功，原 Field/Record 不变、不为旧操作虚构 guards，新 G2 可执行。
- 恢复：pg_dump custom format → 新独立 restore DB；全部 15 个 public table 行数与有序内容摘要一致，17 guards / 7 migrations 保留。恢复摘要 SHA-256：`3f75013b2931d944ed9090962a9e2dd4153a1762d45c48a45715be6c6e7169c3`。
- 降级：已交付 G6 immutable image（revision `f3980b1b4126cdfce026c0a94779c8c8261b3997`，image SHA-256 `e5d81341be8152a868ec9aa48eeb7e3696a34e4f25f42f64f5651f6f6ca7c7c8`）在恢复的 007 test DB 上 healthz/readyz/meta 成功；migrationRequired=false，choicePalette12/aggregateSearch 保留，atomicChoice 缺失。全部 QA 容器/network/tag 退出清理；生产数据库没有参与。
- 最终 QA log SHA-256：`ab7394ca56da0476ff346fce8e8857411739c4d214ac001dc96d117a61fca408`，明确 G2_FINAL_QA_PASS，独立复核无错误且恢复摘要相等。源码归档、日志/恢复数据只保留在本地/私有运维证据目录，不提交。

开发失败证据：实现前合法 choices HTTP 被旧 decoder 422 拒绝；随后 HTTP 支持但 Storage 未实现时返回 unchanged/无 Option delta，真实 DB 测试失败，驱动事务实现。扩展 expiry 测试发现原 retention defer 在注册时执行 QueryRowContext 导致 conn busy，已只修正为退出时闭包执行，不改清理策略，最终通过。辅助 QA 的 SQL转义在执行摘要前修正；SSH 收尾 exit 转义曾返回非零，之后独立检查成功标记/无错误/全部摘要与清理完成，未将编排退出码伪装测试成功。

## 审查、Migration 与部署边界

合同双轴审查无未解决发现。实现 Spec 审查返回一项 capability 描述错误，已在固定源码中修正；独立 Standards/Spec 实现审查随后因 workspace credits 错误中断，不能记为通过。当前手动复核与全部自动化验证完成，适合 review；Merge Ready 仍需完成独立代码审查和 PR CI。CI 结果以主要 PR 检查为准，不关闭质量门禁。

007 仅新增 mutation_identity_guards 小表/复合主键/Actor FK，无 backfill、无既有数据改写。只保存成功 G2 Actor/key/hash（含 unchanged），不保存业务值，不随 Receipt retention 删除；必要性是防止过期未知结果重复创建。生产迁移尚未执行。

发布需固定已审查 SHA、当前版本/备份、隔离恢复证据和 migration 001–007 核查，应用暂停写入后执行已验证 migration，再启动 G2，实际检查 healthz/readyz/meta/atomicChoice；失败恢复 G6 binary 和原配置，保留 007/guards。不能 DROP guard 或自动 down migration，删除将丢失幂等身份保证。生产依旧 `v0.4.0+f3980b1b4126` / OpenAPI 0.4.0，无 atomicChoice；本轮未改生产运行配置。

## Plugin Handoff 与验收

1. 固定上述 40 位源码 SHA 执行 api:sync/api:generate，审查 OpenAPI 0.4.0→0.5.0 diff：ChoiceAssignments/ChoiceAssignment/ChoiceOptionReference、可选 create/update choices、MutationResult.choiceFields、409 fieldConflicts 和 410 ErrorDetails。
2. Client/decoder/InMemory 覆盖真实 ID delta、Field/Record 双 CAS、权限404/lifecycle422/预算422/冲突409/过期410；旧请求省略 choices 的 fingerprint/response 不变。旧 Server 拒绝新 schema，禁止 Field PATCH + Record mutation 降级。
3. Durable Queue v2 strict parser 需独立 v3 持久 schema：发送前落盘完整原 choices/name/color/optionId/expectedFieldRevision 与原 key/body；v1/v2 无损迁移，不重写已有操作。sending/unknown 用原身份恢复，存储失败不发；410 terminal reconciliation。
4. choiceFields 按 revision 单调合并，缺少 beforeRevision 基础时 refetch，late response 受连接/Table/generation 保护。assignment Undo 不声称撤回新 Options。
5. G2 消费者只在真实 Server ready、migrationRequired=false、atomicChoice 存在后开放。验收并发同名、双 CAS、取消未提交 Draft、失败无部分数据、响应丢失和重启 Queue、deleted Option、多 Field、12 色、旧 Server/降级 Queue 暂停。

本工作线未改 Plugin 代码、API pin 或唯一跨仓状态表。没有待用户裁决的新增产品事项；剩余为独立代码审查、CI、合并与单独安全发布，当前 G2 UI 不可宣称可用。
