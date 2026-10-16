# 推荐目标架构（待确认）

## 模块与依赖

~~~mermaid
flowchart TD
    HTTP["HTTP / API / DTO"] --> APP["Application Use Cases"]
    APP --> UOW["Unit of Work / WriteSession"]
    APP --> DOM["Domain / Field Type Registry / Query IR"]
    UOW --> REPO["Transaction-bound Repositories"]
    REPO --> PG["PostgreSQL: SQL / constraints / locks"]
    APP --> FILES["Attachment content adapter"]
~~~

仍是单一Go服务，不拆微服务。Go模块深度来自小Interface封装完整不变量，不是新增一层仅转发Store方法。Domain不导入database/sql、HTTP、Postgres、文件系统；Application依赖明确Repository/UOW接口，Postgres Adapter隐藏SQL/Tx；Transport不决定领域规则。Query读取不必实例化完整聚合或写UOW，仍走短RR read session。

建议职责位置（名字为提案，非当前存在的package）：domain/fieldtype与domain/query保存纯规则/IR；application/mutation、catalog、selection、audit组织用例；storage/postgres实现事务绑定Repository/QueryExecutor；HTTP保现有入口/DTO。复用当前模块中可提取的纯函数，不为了目录整齐复制整套旧实现。

| 模块Interface | 必须封装的复杂性 | 明确不负责 |
|---|---|---|
| Mutation Application | 请求canonical hash、Access、Replay、CAS协调、批命令、结果/Receipt | 不允许客户端拼两次HTTP充当原子事务 |
| WriteSession/UOW | 一次事务、锁序、Writer登记、Effect清单、Scope/Audit/Receipt Finalize、Commit outcome | 不重试不确定Commit，不发外部网络/文件写 |
| Domain catalog/record/order | 状态转换、预算、Option规则、独立Revision、TypedDelta | 不执行SQL/自行开事务 |
| FieldType Registry | 类型值/Config/Operator/Projection/转换能力与Audit value codec | 不拥有Actor授权、SQL连接、任意脚本Hook |
| Query Planner/Executor | Go规范化Query IR；Adapter参数化SQL/索引/Keyset | 不把全表下载至Go筛选或聚合 |
| Audit Builder/Store | 确定性事件、event-time上下文、隐私与同事务append | 不异步“最终补写”必需Audit |
| Selection Service/Store | 固定集合、上下文/生命周期、批候选成员比较 | 不执行Bulk、不以全局DataVersion替代成员判断 |
| AccessPolicy | Actor/资源可见性、撤销线性化、字段可读写决定 | Registry只声明能力，不自行授权 |
| Attachment Adapter | 可恢复pending/ready、临时文件/原子rename | 文件操作不伪装数据库原子提交 |

Application返回明确的CommittedOperation/Result metadata供HTTP设置Operation header，替换当前可变context指针/GUC回读通道；Domain不感知HTTP header或数据库session设置。Receipt保存原业务结果/Operation身份，Transport不从新状态重算Replay结果。

Readiness按目标Schema manifest/Writer版本判断，Capability只有源码实现、对应Migration与运行配置真正具备时才声明。旧0.4客户端仍可走原DTO；未部署Gate默认不可用。

## 唯一领域权威

一条写入路径：Application → Domain Effect → Repository Apply → UOW Finalize。Effect（本提案术语）是显式的对象前后态/操作原因/类型上下文/Changed IDs，不是SQL行diff或任意事件总线。业务Domain Event由Go形成，同步写Audit Fact；Outbox若未来需要，只转发已提交事实，不能替代同步Audit。R1不引入通用事件总线/动态插件运行时。

同一种行为只由一处规则实现：View生命周期和Reorder共用Order模型；Scope统一影响检测；Audit共用Builder/Redactor；Query/Aggregate/Selection复用同一语义IR。SQL表达式执行已定义的查询和约束不算第二个产品规则引擎。允许保留的基础设施锁/约束与可选Envelope guard见决策表，不能顺势保留旧Capture产品推导。

## Field Type扩展Interface

当前10种（P0九种+Attachment）先迁入一个静态Go Registry，启动时检查重复TypeID、能力/Operator/codec完整性；不用反射扫描、外部代码加载或字符串调用任意SQL。每种类型按能力实现小接口，而非强迫Action实现所有聚合方法。

| 扩展面 | Registry/领域返回 | Application/Storage协作 |
|---|---|---|
| Value表示 | Missing/Null/Value显式区分；Canonical codec | 保存稳定Field ID键，不能把未记录当null |
| Config | version、normalize、validate、兼容策略 | Schema Change在UOW内决策投影影响 |
| Value validation | normalize(previous,value,config)纯结果 | 引用/权限的批量事实由Repository提供，不在类型里查库 |
| Query Operators | 合法operator/type/null语义与typed IR | Adapter白名单编译SQL，无用户SQL |
| Canonical/Projection | canonical value、派生key/search contribution、codec版本 | 同事务存储/重建；Projection不是第二事实源 |
| Writable/Computed | 能力描述，不接受客户端写Computed结果 | Application检查字段权限，统一物化/维护 |
| Mutation影响 | 引用依赖、投影/Scope影响描述、纯Domain Effect | UOW统一应用；不能在任意Hook中写DB |
| Audit | typed value codec与快照引用的Field/Option语义 | Audit Builder用事务内前后上下文，统一redaction |
| API/Render/Editor | TypeID、value/config版本与能力ID的提案 | OpenAPI/Client生成和UI由独立Agent接入，不把Renderer放Server |
| Permission | 类型声明所需动作/引用范围 | AccessPolicy做真实授权、查询前字段可见性筛选 |

未来只留明确扩展位置：Rating/Progress为有界标量；DateTime/Time须先冻结instant/wall-time/精度/范围；Relation用稳定Record引用及同事务辅助关系；Formula/Lookup/Rollup区分依赖图、计算值与canonical写入，按未来独立合同确定一致性。Action/Button是受权命令意图，不是可执行任意脚本的普通值。R0/R1均不实现这些功能。支持版本化能力不意味着旧Client能安全编辑新类型，需明确只读/拒绝策略。

## API与旧客户端

推荐R1先保持0.4已部署合同和现有G2–G5业务/DTO，减少同时迁移架构与协议的变量；不是为旧内部Trigger保适配层。内部Repository、锁实现、Schema物理组织可替换。若性能证据要求Audit payload v2或其他0.5–0.8变更，修改批准合同/OpenAPI/Client计划并明确版本，不能保持原版本号偷换语义。

保留旧Change类型、request fingerprint/Receipt编码、canonical ID格式、Actor/签名key。G5 EventID目前是aud_+32位小写hex，OperationID已有26位ULID或32hex；Go新生成器应匹配现有Schema，而非直接套统一ULID破坏decoder。未来完整Rollback、CRDT/OT、批量API与细权限产品决策均另立Gate。
