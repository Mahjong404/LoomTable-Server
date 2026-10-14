# G4 — Fixed whole-query RowSelection delivery

2026-10-14。用户正式选择固定实际 Record ID 集合、真实成员变化即永久失效；值改变但仍属于原集合保留 Scope。正式合同见 [g4-query-selection](../contracts/g4-query-selection.md)。本次工程 Subagent=0；主 Session 自审不是独立 Review，独立性由正常 PR Review 提供。

## 固定源码与交付边界

- Base main：`f43ecb1a7a70c6fbf109f094ceb1e22a16a7425b`（G3 merge）。
- 合同初始提交：`743bc9b3ba147e00f95fe3a533d45516757a4855`；最终语义以本 PR 合同为准。
- 最终 Server 源码/测试：`571a559c8553871b4da3f82272c4c5aa42273d6f`。
- 完整隔离 Race 的实现基线：`a2d9f0d426cbdc292a58b47e5c5de90593505cbb`。571 与之仅两处合成 Token 名称不同；生产 Go/OpenAPI/migration 完全相同，修正后的运行时 fixture 单独实际补测。
- OpenAPI `0.6.0 → 0.7.0`，Capability `querySelection`，新增 migration `009_selection_scopes`。
- 分支 `feature/g4-query-selection`，一条主要 PR；实际 PR/CI/正常独立 Review/merge 状态以 GitHub 为准，不能从此文档的源码 SHA 推断已合并或部署。
- 生产只读 Meta 实测仍 `v0.4.0+f3980b1b4126`，无 atomicChoice/viewOrderCAS/querySelection。本轮未执行生产007/008/009，未部署，未修改 Plugin/Pin/跨仓状态表，未启动G5或Bulk Mutation。

## 实现和不变量

`internal/record/selection.go` 定义 Snapshot、Exclusions、opaque membership version、canonical query、签名 Token 和资源验证；`internal/storage/postgres/selection.go` 物化完整成员ID、actor/request重试身份、同事务增量失效及同快照读取；`internal/httpapi/selection.go` 为 strict JSON 获取/验证 API。Query 的既有验证与 `buildRecordWhere` 是唯一 Filter+Search 引擎。

009创建selection_scopes/selection_scope_members，成员PK唯一，actor/request唯一，expiry与Table索引、ancestor软删除撤销触发器；无业务值Backfill。固定范围不是动态条件或普通30分钟Cursor，未向Client传输全部IDs，也不保存整个Record的历史值。

Scope创建：分配预算锁→Actor→ancestor shared locks→Table→Scope，成员稳定后DB clock开始5分钟TTL。Writer沿已有Actor/Table/Field或Record锁序，在提交前检查本次实际受影响IDs与不可变成员快照的差异；Scope失效、业务变化、Receipt同事务提交/回滚。包含普通Mutation/Create/Delete/Restore、Duplicate、Atomic Choice和Field Convert；Move和纯显示变化不改成员。Field/View相关writer只在查询语义Context改变时失效，不以全局Revision判断。

读Scope/scoped page为REPEATABLE READ，状态、当前权限、Query Context、Exclusions/Count/当前页同Snapshot。并发写入在其Snapshot之后提交时，该响应描述之前的合法快照；下一次请求拒绝原Scope。将来任何完整范围操作必须在自己的事务再次验证，当前没有Bulk授权。

scoped Cursor与Scope独立：排序/显示/普通值变化可导致CURSOR_EXPIRED重启页面，却保留仍有效Scope；只有QUERY_SNAPSHOT_EXPIRED清除该Scope。分页Cursor绑定Scope与Change tail，防止排序值变化导致静默漏页/重复；普通Cursor的既有live行为不变。

## API / OpenAPI diff

新增POST `.../records/selection-scope`（typed req_ requestId + SelectionQuery）与POST `.../records/selection-scope/resolve`（scopeToken + 当前query + bounded exclusions/probes）。新增SelectionQuery/SelectionScopeRequest/SelectionResolveRequest/SelectionScope/SelectionResult Schema；成功返回Scope ID、Token、完整count、固定expiry、fingerprint/version、excludedCount/selectedCount及最多500个请求ID的成员证明。

现有POST `.../records/query`只扩展可选selectionScopeToken/selectionExcludedIds，以及可选selection响应。无token时DTO、Cursor和Query语义保留；records/mutate的CAS、G2 Receipt/Identity Guard、Durable Mutation Queue均不变。

签名用途row-selection，与query/map/change隔离，绑定Actor/Table/Scope/微秒级expiry，DB持久key支持多实例/重启。每次重新认证和检查当前可见性；Token不是权限。invalid/mismatched400、无认证/撤销Token401、不可访问404、过期/永久失效410、输入/预算422、不可用503，完整Error Schema验证。不会将新操作名塞入旧Action enum。

预算：Scope≤100,000成员；有效Scope Actor8/Table4/实例64；含失效请求身份的未过期行Actor64/实例256；总成员≤1,000,000；canonical Query≤64KiB；Exclusions≤5,000，probe/page≤500。失效请求身份保留到原TTL，成员可清理；不能因旧请求重试而静默重选。物理清理为有界请求/维护清理，不承诺恰在expiry时立即删除存储行。

## 实际验证证据

本地 `go test -count=1 ./...`、`go vet ./...`、修改Go文件gofmt及diff检查通过。本地没有配置DB，数据库PASS来自以下实际隔离运行，未把本地skip记作DB通过。

隔离环境实际新建internal Docker network、PostgreSQL16 tmpfs、无host ports/现有Volume、合成数据，任务后清理本次容器/network/images。无生产URL或共享DB。标准可复现DB入口 `bash scripts/operations/integration.sh`；定向核心 `go test -race -count=1 -run '^TestWholeQuerySelection$' ./internal/storage/postgres` 必须由专用runner提供隔离URL和disposable flag。

| 实际检查 | 结果与界限 |
|---|---|
| 单独完整G4 Fixture | PASS，22.279s；16组场景，包括10000/100loaded、全100页、Filter/Search/intersection/empty、非成员Exclusion、固定版本、成员离开再返回、永久失效、no-op、retry/response lost、rollback、Actor/Table binding、TTL、permissions、Display、Convert竞争、Palette12/G2、预算和Unicode扩展 |
| 全量隔离 `go test -race -p 1 -count=1 ./...` + vet | PASS，Postgres package36.272s；G1/G6/G2/G3回归全部保留；runtime/legacy fixture需要专门环境，另见下行，不伪称默认套件自动覆盖 |
| 受控跨页并发 | Record表lock屏障+pg_stat_activity确认reader已建立快照；并发成员变化后旧页仍内部一致、下一次scope410；成员未变但排序值改变时仅cursor410，scope仍有效 |
| 创建与父级撤销竞争 | 祖先shared-lock屏障，撤销/恢复不复活原Scope；当前MVP没有Base删除公开endpoint，fixture使用专用DB的合成生命周期更新，不虚构产品接口 |
| 10,000-member创建 | 553.327ms（Service+PG、Race环境，不是生产HTTP p95） |
| 100页Scope Query遍历 | 2.27765s，完整10,000 IDs、无重复/丢失，不返回全ID集合给单个请求 |
| 单个受影响ID SQL EXPLAIN | 0.304ms，Record候选使用索引，无全Table Seq Scan；resolve不通过重扫所有Record维持Scope |
| 009前向/重放 | 从008专用fixture移除009结构后重新Apply两次；9版本、原业务Record值/revision、007 Guard不变，新scope可用 |
| pg_dump / pg_restore | 自建restore DB，17个public Table的count及ordered row JSON digest逐一一致；11个Guard、9个migration，signing key一并恢复；恢复后清scope再验证新请求 |
| 实际新Server stop/start | 同私有DB/key，seed/verify两个进程运行，原Token/ID/version/成员count保持；当前新Server Meta声明querySelection，仅QA Capability Verified |
| 实际旧G3/G6 binary | 基于固定G3 main与固定G6 image；保留009、先清Scope，真实legacy Record mutate/query200且无selection DTO，新参数422、新Scope route404；health/ready成功，无querySelection |

备份恢复/兼容步骤只在owned disposable DB执行。runtime/legacy Go fixture拒绝无disposable flags或非runner restore DB；不接受生产环境复用。实际restart使用QA专用proof表，不改server_secrets的32字节约束。

审查曾发现并修复：首次响应与PG回放时间精度差异；500码点Search经fold扩展后重复验证错误；Scope创建与祖先撤销竞态；失效request身份必须保留到TTL。运行时补测还修复合成Token名称冲突，并纠正把G2专属“无G3 capability”断言用于旧G3的runner错误。原失败日志保留，不将失败阶段标成整体PASS。

全量日志含旧runtime fixture名称冲突失败，但其完整Race/vet/restore阶段实际PASS；修正后的restart在补测日志PASS，该补测末端另有旧G3断言runner失败；最终legacy-only两旧binary实际PASS。legacy-only沿用helper的一条G3 lifecycle echo不代表本轮跑了G3 legacy fixture，本轮实际仅TestSelectionLegacyBinaryCompatibility，不以echo扩大证据。生产Go/OpenAPI/migration在这些阶段相同。

本地private证据不提交（只记录SHA256）：源码571归档`8cd74398684451b2c080f51e8a9a0a7e8d37db8764732c822da86610001afe94`；full log`cdff034bd5356a85058d94e89435e041971aa2abef716cdc9abb00789989ed25`；restart log`a4dde8e7013c0c0ac7a9dc9c797796c5cc1d77fb6b14967bd43f9a4a7aacd725`；最终legacy log`62b87096f6f4e86830911a9708d8d1d1b1e918008fef16a30f509bb27dd67d09`；restore digest`f37ed51ae103b67c513a7c8156d66f05a1df4c03011bad41403f3bd84b6d939d`。不记录Token、真实业务数据、备份内容或私人部署路径。

## Plugin handoff 和未验证边界

Plugin负责人之后显式pin最终PR固定SHA、api:sync/生成/decoder/client；capability+ready+noMigration三条件满足才启用Header全Query。新RowSelection Scope/E是独立短期读状态，不进入Durable Mutation Queue，不生成Record/Option假身份，也不改变Current/rectangle/Ctrl+A。

获取/验证/page迟到success/error以Actor/Table/Query generation、Scope ID和expiry守卫；Filter/Search/可用Schema Context变化清范围，Sort/Display保留范围并重启页。Table Change只是触发验证或页重载，不等于真实成员变化。expired/offline/revoked/unavailable禁用完整范围操作，明确重新全选；empty为Unchecked+Disabled，部分Unchecked没有Mixed第三态。无capability没有loadedSet/all-ID-fetch fallback。

Client需区分CURSOR_EXPIRED与QUERY_SNAPSHOT_EXPIRED，并按Server验证的E计算selectedCount。Header/row checkbox、迟到响应处置、离线/宿主/a11y为后续Plugin切片，当前Not Verified，不能宣称UI已可用。没有尚未裁决的G4产品选择；正常独立PR Review尚未完成前不能声称独立PASS。

## 发布 / 回滚

代码合并与生产发布分开。本轮0.4.0生产不变，007/008/009均未执行。未来发布窗口须单独确认明确包含三项未部署迁移的版本与风险，完整DB+attachments备份、隔离恢复验证、单migration operator、停旧writer后顺序Apply、health/ready/meta与真实scope冒烟，再允许Plugin消费者。

风险是新Scope分配持有Table锁（最长请求预算10s），会员ID物化/索引成本、所有writer必须支持G4；不允许混跑旧writer，也不承诺任意外部SQL被监测。任何绕过writer的编辑/导入/恢复先清所有Scope。恢复/降级必须暂停Scope消费者并清理短期Scope，以免恢复前token继续使用；保留009结构以及007永久Guard、008Order trigger，禁止自动down/DROP或删除业务数据。旧版本能力关闭，随后新版本需重新全选。G4不授予Bulk写权限，不启动G5。
