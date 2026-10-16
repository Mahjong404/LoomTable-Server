# 迁移策略与生产数据保护

## 已核实事实

2026-10-16只读Meta、运行镜像和schema_migrations一致：生产v0.4.0+f3980b1b4126，只应用001–006；public没有用户自定义函数/Trigger。生产非空，不能按“尚未正式大规模使用”丢弃。未读取业务Cell值；精确计数/部署/角色证据仅在被忽略本地r0目录，不提交Git。

源码含001–011、OpenAPI0.8.0，007–011只在开发/QA验证及源码合并，尚未生产。Plugin pin仍f3980b1b4126cdfce026c0a94779c8c8261b3997。因此必须保0.4实际API/身份/数据；未部署Gate可以重写内部实现，不需要为旧SQL设计永久加适配层，但不能假设所有内部测试库都不存在。

## 两种路线

| 路线 | 优点 | 成本 / 风险 | 推荐 |
|---|---|---|---|
| A：冻结001–011，新增Forward 012（名称/内容R1再定） | 不改登记历史；同时支持001–006与已到011的QA库；同一目标Schema可回放；保现有ID/Receipt/Guard/Audit | 生产停写后可能先依次应用007–011再012，旧函数短暂安装后退役；全部步骤必须演练，不能中间启动旧新Writer | **默认推荐**。历史DDL仅作可追踪迁移，不等于保留活跃技术债 |
| B：独立新Baseline/新数据库，受控保全迁移 | 新安装不回放旧复杂Trigger；目标Schema干净 | 新lineage/完整迁移器/双库切换、身份/Secrets/序列/旧Receipt全面复制验证；不是直接清空重建；发布/恢复操作面更大 | 若用户确认维护新lineage值得成本，单独批准；本轮不采用 |

不推荐直接修改已用007–011的SQL内容/版本登记、把未执行迁移标为已执行、重置已有DB、删除Git历史。可对**全新空环境**提供目标Schema snapshot用于快装，但snapshot须由固定完整Forward链生成、终态/约束摘要一致，不能成为第二套手工权威DDL。

A路线短暂历史Trigger在应用停写、迁移专用进程期间存在，不让中间版本接业务流量。当前迁移器逐文件事务，不是整条链一个事务；新增排他迁移锁/checksum/manifest与失败恢复后才可发布。若不愿生产经过007–011，B或另行设计单一受控前向路径都要明确测试，不偷偷修改当前枚举器来跳版本。

## 012目标与G2–G5兼容

- 目标Schema包含永久Guard、Order当前状态、Scope表、Audit/Retention、必要Operation/Projection元数据与合理FK/Unique；按清单先验证所有旧ID/关联再添加强化约束，失败停机处理，不能丢“坏行”过检查。
- Go-only Writer全覆盖后，在新迁移中原子撤销12个旧业务/隐私Trigger与其10函数依赖；改Audit读路径、conversion GUC、begin/commitAudit依赖一起切换。可选Envelope guard以D03为准，不伪造旧函数保留理由。
- G2双CAS、request hash/Replay、Receipt/Guard保持。已存在G2/G3 Guard永不删；生产006起点没有旧G2保证，不回填所有旧Receipt为永久Guard。
- G3保当前完整Order与revision；001–006来者按稳定created_at/id初始化一次。不能在升级已有008时重新排序覆盖用户意图；新Go Writer对Create/Restore独立增量维护。
- G4 Scope是短期资源，切换前暂停/禁用旧Scope消费；R1可显式invalid所有旧Scope并保request identity至原TTL，旧请求返回失效、不重分配。不要删除业务Records或换HMAC key“自然过期”。已有固定成员身份若不保留须明确消费恢复窗口。
- G5保已存在Event/Operation/Change ID与水位；未知legacy metadata仍未知。011已删减的敏感坐标不逆向恢复。Go Redactor接管新写/旧读。012不会把仅剩30d Change重建成完整历史。
- 保Canonical values、Field/Option tombstone、Record/元数据revision、position、Default、Actor/Token hash、server_secrets、已返回Receipt/Change序列；序列与cursor连续性核对。投影可以按原Canonical/schema重建，不能重生成业务ID或据新状态伪造旧Audit。

## 独立Baseline的最低保全要求（若选B）

影子目标库只由旧库只读快照/全备份复制，不接写；逐表数量/有序内容摘要、Canonical/投影语义、ID/外键、Token认证/持久签名key、Receipt replay、Guard过期行为、Change/Audit水位/序列下一值、附件manifest+内容全部校对。Scope按上节明确失效。旧库/旧卷/旧镜像和完整备份保留，不清库；最终切换需停写/最终差额核对，避免双写丢失。

没有行数或“看起来是测试数据”的默认删除通道。任何丢数据/重发ID/重置身份选项必须用户逐项批准，R0不推荐。

## 角色与禁止绕过DML

只读核验发现运行Role与bootstrap高权限Role一致。目标分Owner（Schema）、Migrator（显式DDL/数据修复）、Runtime（最小表/序列权限，无DDL/Trigger管理/superuser）。签名key管理与Admin CLI按所需权限单独列出，不给客户端/普通脚本DB写Secret；生产凭据不得进报告/PR。

R1在隔离环境证明新Runtime可正常服务、不可DROP/ALTER/禁Trigger、非UOW旧binary/裸DML按D03策略拒绝。权限削减本身也是生产配置变更，需发布授权与恢复方案；本轮未执行GRANT/REVOKE或修改生产账户。

## 发布与回滚是另一任务

R1设计/源码合并不授权发布。未来窗口明确：停止所有Writer/队列与恢复条件；固定源/镜像/配置/文件哈希；全DB+附件备份并在新隔离环境实际恢复；校验原001–006、顺序迁移/012、角色/约束/锁耗时、真实Meta/readiness/API/旧Client和Unknown Outcome；通过后才开放写入。

退役旧领域Trigger后，不能默认G6旧binary仍可安全写Order/Scope/Audit。优先同新Writer contract的兼容构建/前向修复；需恢复旧库时停写并审查新写入差额/备份RPO，不能把“切镜像”称无损回滚。007 Guard和历史证据保留，禁止自动down或drop guard。新Baseline影子库也不能在验证失败时覆盖原库。

R0状态：只读核验已做；Forward012、Baseline复制、权限收敛、恢复、负载和切换均**Not Implemented / Not Verified**。等待设计裁决，不执行任何生产动作。
