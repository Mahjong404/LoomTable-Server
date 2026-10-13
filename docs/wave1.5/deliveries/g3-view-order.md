# G3 后端交付 — 2026-10-14（生产未部署）

固定实现/QA源码：`a64a596e46d858bcfaa15a19dfeb64a79a15f27f`；基线main：`01dd2d84107f36c3c35300f39ce7d50aad8b6f18`。后续文档提交不改实现。G3产品不变量由已批准Plugin Boundary和本轮授权确定；[精确合同](../contracts/g3-view-order.md)经主Session一致性检查，无未决产品选择。主Session实现与自审不算独立审查，实际PR/CI/外部Review作为最终集成事实。本轮Subagent=0，模型等级/委派产出不适用，不启动G4/G5。

## API与领域事实

OpenAPI0.6.0。Table独立Order={tableId,revision,viewIds}是active View完整排列（0–100）；View config/default、Table普通revision独立。新增PUT /v1/tables/{tableId}/views/order，Idempotency-Key必需，body={expectedOrderRevision,viewIds}。旧Revision409带currentOrder；缺失/重复/foreign/deleted/invalid permutation422；CAS匹配的相同排列unchanged，无revision/Change增加。Actor/Table继承访问边界，外来Table404。

GET现有Views显式includeOrder=true返回{items,order}，同一REPEATABLE READ snapshot；旧请求只{items}，View/Table DTO不增加排序字段。active items按Server Order；all/deleted仍createdAt/id，order始终完整active成员。现有Duplicate消费者复制config后Create复用既有POST，不新增假Duplicate endpoint；并不承诺读取源config与复制之间的原子源快照。

008生命周期trigger唯一维护append/remove：Create/复制Create/普通Restore尾部一次，Delete保持survivors，default/config无Order变化；Restore保留旧createdAt且isDefault=false。新Table空revision1，初始View后revision2；既有Table backfill revision1。Restore重试沿旧View CAS409/active422，无二次append，未伪造它有完整Receipt。

锁序Actor NO KEY UPDATE→Receipt/guard→Table FOR UPDATE；Reorder只锁Table，无View行锁。新View writer统一Table→View。Order、schemaChanged(Table object/Order revision)、Receipt与007永久identity guard同transaction；fail零部分效果。成功包括unchanged永久guard，原key/body replay先于CAS返回原Order，不排序/改expectedRevision后重试；expired Receipt同hash410 operationCommitted:true、改hash409，terminal reconciliation。无新ChangeKind，旧Change decoder仍兼容。

History现在没有历史Order checkpoint/inverse；本Gate只维护当前Order，不能用普通tail Restore假装历史位置恢复。未来历史恢复必须单独恢复真实历史permutation，不回填或宣称已具备完整Table rollback。

## 自审与实际验证

- 本地Go Test/Vet、gofmt、diff检查通过；本地数据库/外部legacy probe因无隔离runner配置跳过，实际DB/旧binary证据来自下述私有runner。完整OpenAPI YAML解析通过。HTTP路由测试先实际404 red，再实现green；编译阶段初始GridView DTO误当generic View的夹具错误已修复，不将其当业务red证据。
- 固定源码专用PG16：新internal network/tmpfs/no host ports/只合成数据。完整go test -race -p1 -count1 ./... + Vet通过（postgres14.874s）；专跑G3 fixture5.792s。所有G1/G6/G2既有tests保持通过。
- 真实HTTP完整Schema覆盖200 Order/opt-in List/legacy DTO、409结构化Order conflict/key reuse、422 invalid permutation与deleted default、410expired guard。strict decoder/extra/null/missing/Query参数、Service budget与typed IDs另有单元验证。
- 两Reorders一成功一冲突，同key并发Receipt一致；Reorder与Create/复制Create/Delete/Restore/Default并发无丢失成员、重复ID、静默覆盖或死锁，revision计数符合提交顺序；default/rename/config/Table revision独立。旧Create replay、旧Restore重试不再次append。actual HTTP response write失败后原key重放只执行一次。
- 受控snapshot验证：阻塞Views读取，使List已读Table Order之后并发Reorder提交；解除阻塞后仍返回原Orderrevision和原排列/items，下一List才看到新revision。并发goroutines均有deadline并join/cleanup。
- 真实PG trigger拒绝Receipt写入后Order/Change/Receipt/guard全部回滚，移除故障后原key可正常提交。invalid permutation和CAS失败无receipt/guard。
- 008 deterministic backfill（createdAt相等按ID、deleted排除）、未008 readiness失败、重复Apply、旧View配置revision不变与随后追加验证通过。G2的prior-six fixture在专用库移除008再验证007+008升级，保留G2业务验证，不针对生产DROP。
- pg_dump/restore后15public表行数/有序内容摘要一致，包含Order arrays/revisions、12identity guards和8migrations。新建restore DB与原测试DB均在本次tmpfs PG容器。
- 实际旧G2 binary由固定01dd2d8源码构建（imageSHA256 `8c5fea985098dda363c07c4aaa77d44cdd68fe9cc734d8709073dd2f7c6d6af1`），旧G6 imageSHA256 `e5d81341be8152a868ec9aa48eeb7e3696a34e4f25f42f64f5651f6f6ca7c7c8`；分别在恢复008库上health/ready/meta通过、无viewOrderCAS，实际旧HTTP Create/replay/Delete/Restore维持正确Order、Restore重试无重复。两项legacy probe在race下分别1.148s/1.150s，不仅是SQL模拟。Probe只由独立runner提供专用URL/DB，不能用数据库名字代替真实隔离证明。
- 最终G3_FINAL_QA_PASS、无FAIL/SQL ERROR/DATA RACE，独立命令复核摘要相等及退出清理。未读写生产业务数据；本次容器/network/临时image tag已清理，既有生产镜像保留。

源码归档SHA256：`8e96b64450ef37745886d5a572f134d72d5153c2f9cbcdd136ad8811c778acda`；final log：`1f18543039c0e642136cf11e3604faae25aa5cc0b2b2ee85c713e5466a26520c`；restore digest：`187caa7fbde03b00e3fdb6cd8b47ee21732bc5ccc1e515ee2a54e72b1604dec0`。归档/log/dump/Token/私有部署坐标不进入Git。

自审检查了全Diff、事务、CAS、反向FK锁、shared key namespace、No-op、生命周期trigger、guard与backfill、old DTO、snapshot及错误Schema；没有未解决发现。这是作者自审。正常PR外部Review需针对这些实际风险完成，不伪称内部独立Standards/Spec PASS，Subagent保持0。

## 发布与Rollback（本轮不执行）

008仅加Table两列/约束、当前Order backfill、function/trigger，不改普通Table/View/Record revisions。007 guards必须保留。现有迁移器没有并行lock/checksum，未来核对固定001–008文件，单进程显式迁移；DDL/backfill持锁，停写窗口、超时、完整DB+Attachment备份和隔离恢复是发布前置。事务失败回滚，不DROP长期卷。

当前生产仍G6 `v0.4.0+f3980b1b4126`，只有已部署001–006，无atomicChoice/viewOrderCAS。本轮没有执行007或008。G3发布不能默默捆绑G2：未来若从G6直接升G3，须用户另行确认包含007+008的明确迁移范围/持久guard风险与维护窗口；如先发G2再G3，两次分别按固定源/备份发布。

回滚只切换单一旧G2或G6 binary/原配置，保留008列/trigger/007guards，暂停G3 pending intents、不换key或降级为localstorage事实源。已实测oldbinary仍可read/create/delete/restore并维护Order，但旧createdAt List不承诺G3呈现，缺cap时新Client禁用reorder。禁止旧View-first与新Table-first binary混合写入/滚动混跑；停写切换一个writer版本。无自动down/drop，避免丢排序或重复身份保证。永久guard增长仍需监测，不使用TTL删除。普通binary回滚不覆盖备份抹掉发布后数据。

## Plugin 接入与停止

固定已合并/审查SHA后api:sync/api:generate → DTO/HTTP decoder/InMemory → complete list/Order coordinator/intent persistence →真实消费者。保存原Actor/连接/Table/key/expectedOrderRevision/完整viewIds，再发送；unknown原请求重试，409显式refresh/rebase并形成新业务操作，410terminal reconcile，不把PUT Order伪装Record Mutation command。late response与缓存按连接/Table/generation/Orderrevision守护，历史Receipt不能覆盖更新的当前Order。旧请求/旧ViewDTO继续兼容。

需观察ready、migrationRequired=false、viewOrderCAS才能开放G3；生产未部署时不能宣称UI可用。复制仍复用Create；default/config不排序，普通restore尾部，历史恢复另Gate。Server不改Plugin代码/API pin/唯一Wave status。本轮只G3，G4/G5待后续独立授权任务。
