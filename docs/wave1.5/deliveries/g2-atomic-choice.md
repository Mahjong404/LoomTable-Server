# G2 后端交付 — 2026-10-14（生产待发布）

G2 最终错误合同修复、独立实现审查与隔离验证完成。固定最终验证源码 SHA：`8553749b0f0ea514b8a8874e65835a52f6c19820`；基线 main：`bb00e6ad3d100a6e5e0879d1e8c1fb25ab51bfec`。[PR #13](https://github.com/Mahjong404/LoomTable-Server/pull/13) 保留实际 HEAD、CI 与 merge 状态；本地审查与测试已无阻塞，生产尚未部署。后续交付文档提交不改变此实现。本轮到 G2 停止，不启动 G3–G5。

## 合同与真实事务

[Approved engineering contract](../contracts/g2-atomic-choice.md) 基于已授权 Wave 1.5 G2 产品语义，经双轴合同审查，无未解决的产品决策。OpenAPI 0.5.0 在既有 POST /v1/tables/{tableId}/records/mutate 的 createRecord/updateRecord 增加可选 choices；没有新增 endpoint。

- Existing Record：expectedRevision + 每个 Choice Field 的 expectedFieldRevision，一次选择既有 Option 或按名称创建/复用并赋值。New Draft：仅在提交 createRecord 时生成真实 Record/Option ID，多 Field、普通 values 与 Choice 同事务；未提交 Draft 无持久写入。
- 名称 Trim→NFC→Default Case Folding、accent-sensitive；复用 active Option 的真实 ID/原名称/颜色/顺序，deleted name 不隐式恢复。Select 恰好一项，MultiSelect 0–100 项为完整最终选择；重复解析身份拒绝。支持 G1 全 12 色。
- Field CAS 优先于 Record CAS，全部在写入前检查；冲突/权限/预算/SQL 失败整体回滚。每 Field 有新 Option 才一次 revision+1/schemaChanged；仅复用无 Field revision 变化，Record applied/unchanged 沿用原 writer。
- 锁序：Actor FOR NO KEY UPDATE → Receipt/identity → Table UPDATE → Choice Fields 按 ID UPDATE → 其他 Fields SHARE → Records 按 ID → 写入。Catalog Update/Delete/Restore/Convert 统一 Table-before-Field；Actor lock 保留串行幂等 writer，同时允许 metadata Actor FK KEY SHARE，避免反向等待。
- Option/Record/Change/Receipt/identity guard 在同一 PostgreSQL transaction；不组合两个 Service 的独立事务。响应 choiceFields 提供 bounded resolvedOptions/createdOptions 与 beforeRevision/revision，真实生成 ID 和 delta 原样持久化/replay。
- 原 Actor + clientMutationId + Table/path + 原 command body 是重试身份；name/color/revisions 不在 hash 前改写。response 丢失/timeout/commit error 不换 key、不自动 rebase。Receipt replay 先于 CAS；过期 guard 同 hash 返回 410 MUTATION_RECEIPT_EXPIRED、operationCommitted=true，改 body 409 IDEMPOTENCY_KEY_REUSED。

## 2026-10-14 最终修复与独立审查

[Devin finding](https://github.com/Mahjong404/LoomTable-Server/pull/13#discussion_r4251285820) 确认成立：deleted Choice Field 的 InvalidStateTransitionDetails.action 原为 assign choice，超出既有 enum。现使用 update，HTTP422与资源/状态详情保持，无公共 enum 或 OpenAPI版本扩大。其他 G2新错误路径逐一检查：deleted Record使用update，type/limit/duplicate/invalidReference/name/color使用既有ValidationIssue codes，CAS/expiry使用已批准schema。

新增测试直接从正式 OpenAPI 解析完整 operation response Schema，JSON Schema2020-12、递归本地$ref与format校验，拒绝外部加载；CONFLICT另校验完整ConflictResponse，防止409 generic分支掩盖CAS形状错误。测试用jsonschema/yaml库仅在_test.go导入，Go1.22不变，YAML测试间接依赖固定兼容版本。

真实HTTP/PG回归覆盖422 deletedField Existing/Draft、deletedField staleCAS409、RecordCAS409、Select数量/type/color/foreignOption/重复canonicalID/foreignField404、正常200及历史replay、改body409、Receipt过期410、deletedRecord422和旧无choices响应。负向Schema测试证明非法action、缺requestId、非法resource及额外detail都会被拒绝。修复前隔离red实际拒绝完整assign choice响应；新增fixture最初置于旧no-op测试之前，改变了cursor断言的前置状态；已移到原断言后。首次green又因text fixture缺config失败，补EmptyFieldConfig后，最终固定源码全green。

Standards：完整旧候选26文件独立静态审查及1934141→8553749最终补查，0硬违反/0可执行heuristic发现。审查者与实现作者分离，检查模块边界、scope、锁序、guard、legacy DTO/fingerprint与并发测试cleanup；没有宣称自己复跑QA。

Spec：完整合同/最终源码独立审查，既知Action finding修复后无剩余发现；另复核正式Schema与Go1.22依赖。旧候选QA也由Spec独立读证据，新日志最终证据复核见审查报告。该独立审查不等于生产验证：生产迁移/发布仍未执行。

固定8553749的新验证：本地Go Test/Vet/Format/diff通过；隔离完整 go test -race -p1 -count1 ./... + Vet通过（postgres10.187s），专跑G2事务fixture7.456s；001–006→007/reapply、15public表restore内容与行数摘要一致，18 guards/7 migrations，固定旧G6image health/ready/meta成功且无atomicChoice。新network/tmpfs PG/no host ports，仅合成数据，退出资源清理已复核。

最终源码归档SHA-256：`1d7b1c4c0f14c7d10668c561517ebb73f5416e8254d77a533dd9d6acfe90f4c4`；green log：`9b49006008b2a08697588374200df644a755416fa34cf5d51385af5dd21c5fc8`；修复前red log：`f064b9f4a28fc07384587b98688eac7894ce91d1df3b24bc29fa467650ea8189`；恢复摘要：`08f8c4939ba4016f46cd6b5cb4bb60d830f21083634c1ac3894beabfc39424ca`。日志/归档/dump不提交。最终PR CI在集中Push后核对，不复用旧HEAD的CI作为新HEAD通过证据。

## 首次候选验证证据（9fa266a）

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

首次候选的独立实现审查因workspace credits错误中断，当时正确标记未完成。本轮已补齐完整独立Standards/Spec审查与最终源码验证，历史capability及Action finding均已修复；新的PR CI结果以最终HEAD检查为准，不关闭质量门禁。

007 仅新增 mutation_identity_guards 小表/复合主键/Actor FK，无 backfill、无既有数据改写。只保存成功 G2 Actor/key/hash（含 unchanged），不保存业务值，不随 Receipt retention 删除；必要性是防止过期未知结果重复创建。生产迁移尚未执行。

发布需固定已审查 SHA、当前版本/备份、隔离恢复证据和 migration 001–007 核查，应用暂停写入后执行已验证 migration，再启动 G2，实际检查 healthz/readyz/meta/atomicChoice；失败恢复 G6 binary 和原配置，保留 007/guards。不能 DROP guard 或自动 down migration，删除将丢失幂等身份保证。生产依旧 `v0.4.0+f3980b1b4126` / OpenAPI 0.4.0，无 atomicChoice；本轮未改生产运行配置。

## Plugin Handoff 与验收

1. 固定上述 40 位源码 SHA 执行 api:sync/api:generate，审查 OpenAPI 0.4.0→0.5.0 diff：ChoiceAssignments/ChoiceAssignment/ChoiceOptionReference、可选 create/update choices、MutationResult.choiceFields、409 fieldConflicts 和 410 ErrorDetails。
2. Client/decoder/InMemory 覆盖真实 ID delta、Field/Record 双 CAS、权限404/lifecycle422/预算422/冲突409/过期410；旧请求省略 choices 的 fingerprint/response 不变。旧 Server 拒绝新 schema，禁止 Field PATCH + Record mutation 降级。
3. Durable Queue v2 strict parser 需独立 v3 持久 schema：发送前落盘完整原 choices/name/color/optionId/expectedFieldRevision 与原 key/body；v1/v2 无损迁移，不重写已有操作。sending/unknown 用原身份恢复，存储失败不发；410 terminal reconciliation。
4. choiceFields 按 revision 单调合并，缺少 beforeRevision 基础时 refetch，late response 受连接/Table/generation 保护。assignment Undo 不声称撤回新 Options。
5. G2 消费者只在真实 Server ready、migrationRequired=false、atomicChoice 存在后开放。验收并发同名、双 CAS、取消未提交 Draft、失败无部分数据、响应丢失和重启 Queue、deleted Option、多 Field、12 色、旧 Server/降级 Queue 暂停。

本工作线未改 Plugin 代码、API pin 或唯一跨仓状态表。没有待用户裁决的新增产品事项；剩余为独立代码审查、CI、合并与单独安全发布，当前 G2 UI 不可宣称可用。

## 2026-10-14 发布检查表与持久风险

本轮仅合并代码；生产 007 Migration 和切换镜像需用户另行确认发布窗口与以下风险。私有部署说明已授权读取，生产 Meta/healthz/readyz 只读核查正常，仍为 v0.4.0+f3980b1b4126，无 atomicChoice。

1. 固定审查/CI 成功的源码与发布镜像 revision/digest，核对生产当前镜像和原 override，确认只有 migration 007；版本记录只按文件名标记，不自动校验 checksum，所以人工核对既有 001–006 与已交付基线一致。普通启动不执行迁移。
2. 用户确认维护窗口、可接受的写入暂停与恢复责任后停止应用写入；保留 PostgreSQL、Attachment volumes，记录版本/Schema。使用现有 backup.sh / validate-backup.sh 备份并校验数据库 custom dump + Managed Attachment + manifest；包含 Actor/token hash/HMAC/业务值的归档仅存私有运维位置。必须在新隔离 PG/附件目录恢复，核对对象/附件引用及身份 guard，不把生产 URL 传给会 DROP SCHEMA 的集成测试。
3. 只运行一个显式迁移进程，防止既有版本检查器的并行执行竞争。007 的 CREATE TABLE + schema_migrations 版本写入同 transaction，无 backfill 或旧记录 UPDATE；Actor FK 需要短暂 DDL 锁，因此迁移前停止应用写入，避免长事务，设定受控超时。失败整次007回滚；未查证状态不启动 G2。
4. 迁移核查完成后启动固定 G2 镜像，验证 healthz/readyz/meta、migrationRequired=false、atomicChoice 和旧 capabilities、Actor/Token 可用、只读旧 Record/Field；正常 Plugin 不自动改变 Queue/API pin。必要写入验收使用明确隔离的 synthetic QA scope，随后才恢复业务流量。
5. 切换失败优先恢复旧 G6 镜像和原运行配置，保持原数据库/附件卷与007/guards。G6 已在带007的恢复测试库验证 ready；它不接受 choices，Plugin G2 Queue 必须暂停并保留原 body/key。恢复 G2 后才恢复原身份 replay。普通 binary rollback 不恢复旧备份覆盖期间新数据，也不 DROP guard；若确需整体灾难恢复，停写并独立确认目标备份、RPO/新增数据损失和 DB+附件一致性后处理。

Guard 复合主键 Actor/key 阻止并发重复身份；同事务成功才创建，失败没有 guard。Receipt 清理不删 guard。cleanup 与 replay 相遇时，可返回已读到的原 Receipt 或永久guard的410，不能重新执行；same Actor writer lock 与唯一键仍序列化并发请求。成功 unchanged 也保留 guard；没有历史非G2回填。guard 永久增长，部署后监测行数/索引体积/备份与恢复耗时；不通过TTL删除减小表，这会撤销安全保证。Actor FK 还限制未来 Actor 物理删除，当前没有该产品操作，未来必须单独设计。007的加法性质降低即时数据风险，但持久增长和不可自动 down 的约束仍须在发布窗口确认。

代码合并不等于线上能力可用；生产合同仍0.4.0。G2消费者须固定已交付SHA做 api:sync/api:generate、Client/decoder/InMemory/Queue v3改造，保留v1/v2原身份，ready+atomicChoice+无migrationRequired后才接入。Field lifecycle422的action现在使用既有update；409 fieldConflicts/Record conflicts、410 operationCommitted reconciliation、choiceFields revision单调合并等精确合同不变。
