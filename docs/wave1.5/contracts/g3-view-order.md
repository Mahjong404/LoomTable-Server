# G3 — Server-owned View Order CAS

2026-10-14。基于main 01dd2d84107f36c3c35300f39ce7d50aad8b6f18 / OpenAPI0.5.0，用户正式授权G3合同与实现。Plugin API Boundary G3为产品依据。本轮不修改Plugin、不启动G4/G5、不生产执行007/008。Subagent=0；主Session合同一致性检查/自审，正常PR Review为外部独立流程。

## 领域与持久不变量

Table持有独立 view_order_revision（>=1）和完整active View ID有序数组，008新增tables.view_order(jsonb array)。不能使用Plugin本地顺序。每个active View恰好一次，无deleted/foreign/missing成员，最多100；既有每Table 100总View（含deleted）预算不变。顺序revision与Table普通revision、View config revision、default独立。

008初始化所有既有Table为revision1，active IDs按created_at ASC,id ASC稳定排序；时间相等按ID，deleted排除，空Table为[]。新Table先空revision1，初始View的insert追加后revision2；删除最后一项得到[]但revision继续递增，未来first View追加。无需连续position或人为复用历史revision。

数据库AFTER View INSERT/UPDATE deleted_at/DELETE触发器是生命周期成员维护的唯一owner：active创建追加；active→deleted移除保持survivors相对顺序；deleted→active追加一次；active→active/deleted→deleted/config/default不变；事务回滚连带恢复array/revision。支持旧binary writer在保留008的数据库上维护成员。新writer统一Table-before-View；不在Service再重复append。Reorder只修改Table Order。

## API选择与Schema（OpenAPI0.6.0）

既有 GET /v1/tables/{tableId}/views 扩展可选单值 includeOrder=true|false；省略/false保持仅{items}。true返回{items,order:{tableId,revision,viewIds}}，读取采用同一REPEATABLE READ只读transaction，Table可见性、active membership与View对象来自同snapshot。默认lifecycle=active的items按Order排列；all/deleted保持既有createdAt/id显示顺序，order始终完整active数组，不以filtered items猜成员。无分页。

新增 PUT /v1/tables/{tableId}/views/order 是本Gate定义的新协议（此前不存在），不复用普通Table PATCH revision或View config PATCH。必需typed Idempotency-Key(mut_...)；strict JSON body {expectedOrderRevision,viewIds}，revision>=1，viewIds必需非null完整active permutation（0–100），IDs为view_类型、不能重复。成功200 {clientMutationId,status:applied|unchanged,order}。

先权限/activeTable，再Order CAS，再实际完整membership检查。格式/重复/缺失/多余/deleted/foreign/unknown ID为422 ValidationError（foreign成员不泄露其他Table）；Table不可访问404。stale expectedOrderRevision为409 CONFLICT标准envelope/details:{tableId,expectedOrderRevision,currentOrder}，完整当前order帮助显式refresh/rebase。请求body shape优先由strict decoder/service验证；合法旧revision无论当前permutation是否过时都409。相同排列只有CAS匹配才unchanged，不增revision、不写Change。实际reorder一次revision+1，View config/default/Table普通revision不变。

## Lifecycle、锁序与并发

Create/复制后Create使用现有POST Views：真实新ID追加一次；CreateTable初始View按同一insert规则。当前没有DuplicateView endpoint；消费者已有源config快照后Create，G3保证追加及幂等，未新增原子source-config-copy协议。

Delete移除一次但保留既有View CAS与重复删除错误；Restore旧createdAt不变、普通恢复在尾部且isDefault=false，原View CAS与active恢复422保持。重复原Restore请求不会再次append（可能409/422，GET reconcile），不伪称原Restore已有Receipt。SetDefault/rename/config不改变Order；SetDefault可改变各View配置revision但不改变Orderrevision。其deleted错误action使用既有update枚举。

新idempotent Order操作：Actor FOR NO KEY UPDATE→sharedReceipt/guard→activeTable FOR UPDATE→CAS/membership→TableOrder/Change/Receipt/guard→COMMIT，不锁View行。所有新View Update/Delete/Restore/SetDefault为Table→View；Create沿Actor→Table。生命周期触发器在同transaction更新Table。两个Reorder只能一个成功；Reorder与lifecycle串行：life先提交使旧Order CAS409，Order先提交则后续life按新尾部/删除规则递增；default与Reorder各自独立成功，无顺序覆写。事务失败零部分效果。

实际Reorder沿现有schemaChanged事件，objectId=Table ID、revision=Order revision；现有Schema允许Table对象，不引入旧Client无法解码的新ChangeKind。noop/replay不新发Change。生命周期仍发原viewChanged事件，不增加伪造View config revision。仅是失效通知，G5完整审计/可逆历史不属于G3。

## 幂等、Unknown与历史边界

Actor+Idempotency-Key共享已有metadata/Record namespace。hash包含PUT/path/原expectedOrderRevision/原ordered viewIds；不得发送前排序或改revision。Receipt replay先于权限/CAS/lifecycle检查返回原status/order；改body409 IDEMPOTENCY_KEY_REUSED。成功包括unchanged在同transaction保存007永久最小identity guard；不改G2 receipt协议或回填旧操作。Receipt过期相同hash410 MUTATION_RECEIPT_EXPIRED(operationCommitted:true)，不同hash409；禁止自动换key执行，读取Order并显式reconcile。response丢失/commit error是unknown，保持Actor/Table/key/body重试。

现有History只有metadata事实，没有历史Order snapshot或Table Rollback接口。普通Restore不可承担历史位置恢复。未来历史恢复必须提供真实checkpoint/inverse中的active permutation并在单事务恢复，不能按旧createdAt或调用普通tail Restore推断；008只保证当前确定性初始Order，不宣称恢复迁移前任意历史顺序。此既有Future边界不需要在G3实现回滚引擎，无未决G3产品决策。

## Migration、兼容与交付

008_view_order在007之后加两列/约束、确定性backfill与生命周期函数/trigger，已有业务/配置revision不变。ApplyMigrations整体事务+版本记录支持顺序重复apply；不支持多个migration进程竞争。readiness新增008要求，capability viewOrderCAS独立于attachments；consumer需ready、migrationRequired=false及capability。

旧Client省略includeOrder/新writer请求：响应DTO不增加成员，旧create/delete/restore会维护Order。新Client需Client/DTO/parser、ordered List/intent CAS coordinator、完整permutation与unknown/error处理；不得localstorage fallback。旧Server无viewOrderCAS，暂停reorder；oldbinary在保留008DB上可ready，createdAt list没有G3呈现保证，trigger仍维护Order供再升级。禁止mixed binary writers/滚动混跑（旧binary仍View-first），停写后切换单一writer。保留008列/trigger/007guard，禁止自动DROP/down。长期guard增长/备份完整性沿G2要求。

生产当前G6仅001–006，007未部署；G3不默默捆绑发布。未来若直接发布G3，需用户单独确认包含007+008的明确迁移计划/风险/窗口，完整DB+attachments备份与隔离恢复；本轮仅测试forward、reapply、restore及旧G2/G6降级。PR固定SHA、CI/正常独立Review、真实未部署状态与Pluginhandoff分开记录。一条主要PR，集中Push，不自动G4/G5。
