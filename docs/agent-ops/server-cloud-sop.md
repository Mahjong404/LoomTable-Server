# Server 云端运行与维护 SOP

实际 Server、PostgreSQL、Docker 服务栈及 HTTP/SQL/并发/迁移/恢复验收全部在云端。开发者本机仅源码阅读、格式、编译/静态与不启动服务的纯逻辑测试。生产发布与研发 QA 分开授权。

## 1. 云端连接前

每次访问或操作云端前重新读取私有文件 C:\Personal folder\Server\rainyunMahjong404\server.md，按其中当前入口、用户、连接方式和安全步骤操作。不可读时暂停，不靠旧 Session 猜地址/端口。

核对只读、隔离 QA、Cleanup 或明确生产发布授权；记录源码 SHA、工作树、Owner、拟创建资源和退出方案。核对生产/共享容器、固定镜像、网络、持久卷、附件和备份保护清单。只读任务不启动/停止容器、不创建数据库、不执行迁移/清理。

私人地址、SSH 别名/凭据、Token、完整 Env/Command 和业务备份只留私有上下文。Git/PR 只记录脱敏结构、版本、SHA 和结果。连接/审批失败走正常授权路径，不换工具绕开。

完成标准：当前私有说明可读，边界/保护清单明确，连接资料未泄露。

## 2. 固定源码和现有工具

本地用 git rev-parse HEAD、git status --short、git diff --check 核对40位 SHA。源码归档/实际运行日志用 Get-FileHash -Algorithm SHA256（Windows）或 sha256sum（云端）记录哈希；同时记录工具版本、实际命令和退出码。归档、日志、业务备份不进 Git。

| 已有脚本 | 真实入口（从当前源码核对） | 限制 |
|---|---|---|
| scripts/operations/integration.sh | bash scripts/operations/integration.sh，无参数 | 新私有网络、tmpfs PG、无 host ports，不接收已有 URL/卷；当前仅 go test -count=1 ./... 与 go vet |
| backup.sh | bash scripts/operations/backup.sh OUTPUT.tar.gz | 在已核对 Compose 项目备份 DB+附件+manifest+校验和，拒绝覆盖；生产备份按发布授权 |
| validate-backup.sh | bash scripts/operations/validate-backup.sh BACKUP.tar.gz | 验结构/哈希，不证明能恢复 |
| restore.sh | bash scripts/operations/restore.sh BACKUP.tar.gz --confirm | 拒绝 Server 运行时恢复；实际 pg_restore --clean 并清空附件，是破坏性动作 |
| acceptance.sh / benchmark.sh | 先逐行读源码与 Compose 项目 | 启动服务、down -v、删除临时资料；benchmark 默认端口可能与正式服务冲突 |

表内参数经过源码核对；Gate 的实际历史云端运行见各 delivery。本轮 SOP 文档任务没有重新执行服务栈。私有临时 QA 脚本可能依赖固定旧镜像/路径/归档，先读后复用，不当作通用 CLI。

## 3. 可销毁 PostgreSQL 与真实隔离

集成测试可能执行 **DROP SCHEMA public CASCADE**。LOOMTABLE_TEST_DATABASE_DISPOSABLE=true 只是声明，不是生产安全的充分证据；名字带 test、空库、共享测试容器或 localhost 均不证明可销毁。

只用本轮新建资源和合成数据。现有 integration.sh 使用随机项目名、源码 build stage、docker network create --internal、postgres:16-alpine、--tmpfs /var/lib/postgresql/data，不发布 -p、不挂长期卷/宿主数据；测试 URL 从这个新容器生成。trust 认证只限当次内网 tmpfs 合成环境。

SQL 重置前保存实际 inspect，不只记录脚本意图。下列 qa_postgres/qa_project 变量必须来自本次创建的精确资源清单：

~~~bash
docker inspect "$qa_postgres" --format '{{json .Mounts}} {{json .HostConfig.Tmpfs}} {{json .HostConfig.PortBindings}}'
docker network inspect "$qa_project"
~~~

验收 PG 数据目录实际 tmpfs、无长期 Bind/Volume、无发布 PG 端口、Internal=true 且网络成员属本轮、URL 对应此 PG；记录创建时间/Owner。脚本本身不额外输出完整 inspect，若需要独立物理证据，用已审阅的分阶段 runner 在测试前取证。任何项不明暂停 SQL。持续运行的共享 test PG 禁止 reset。

## 4. Race、HTTP、OpenAPI

隔离证明成立后才测试。G5 已实际验证的完整云端命令：

~~~bash
go test -race -p 1 -count=1 -v ./...
go vet ./...
~~~

Race 需要 CGO 和 C 工具链。仓库 build stage 是 Alpine；G5 采用仅测试的派生镜像安装 gcc/musl-dev，不修改业务 Dockerfile。不能仅在原镜像附加 -race 就报成功。runner 仅获得新 PG URL/disposable 声明。

HTTP/Contract 验完整 Response Schema，覆盖错误详情、公开枚举及额外字段，不只状态码。CAS、竞争/锁序、失败零部分效果、No-op、Receipt/原身份重放都须有真实数据库覆盖。SKIP 明确说明；实际 Server 重启、旧 binary probes 另行运行，CI PASS 不替代云端 QA。

## 5. Migration 前向、重试、恢复与旧 binary

1. 同时核对源码 migration 与目标实际 schema_migrations。迁移器按已登记版本跳过文件，没有自动内容 checksum/并行锁；固定各文件哈希、单迁移进程、预期版本和锁/超时计划。
2. 隔离库验新建前向 apply、正常再次 apply、必要 raw SQL retry、既有版本升级、事务失败。已应用原010→011必须正常 ApplyMigrations；编辑010不会自动升级已登记库。
3. pg_dump -Fc 后创建新的隔离恢复库，pg_restore --exit-on-error --no-owner --no-privileges 还原；每个 public Table 比行数与稳定排序 JSON 摘要，附件同时验证。哈希相等不替代实际还原/应用探针。
4. 恢复库运行固定 SHA/image digest 的旧与新 binary，验真实 HTTP、CAS、Receipt/原身份、Unknown Outcome 和旧 Writer；实际 stop/start 验跨重启 cursor/receipt。不得借用生产进程。
5. **007 Guard、008 Order、009 Scope、010 Audit、011 Redaction helper/trigger不是垃圾。** Guard 维持 Receipt 过期后的身份保证；011 对 Audit 坐标删减不可逆，原业务值/旧 Changes 保留。降级旧 Writer 会有 instrumentation gap，不代表连续完整 Audit。

完成标准：源码/备份哈希、前向/重试/升级、实际恢复摘要、旧新 binary 验证及限制可复核；数据库恢复不等于 Table Rollback。

## 6. 盘点与精确清理

只读盘点记录路径/资源 ID、逻辑 bytes、分配 bytes（未测写未知）、文件数、时间、Owner/引用、唯一性、可重建和清理条件。Top目录嵌套不能相加；镜像共享层不能重复计容量。默认 Go 缓存可能共享，显式工程 GOCACHE 单列。

A=无活跃引用且可重建；B=归档/替代/唯一性确认后清理；C=保留至里程碑；D=关键资产；E=归属未明。大胆列旧 QA backup 候选，但生产备份/附件/Guard不因名字旧就变A。原始日志不能仅因有摘要就当已被完整替代。

只读 Docker 盘点用 ps -a、network ls、volume ls、images、system df -v 与字段白名单 inspect；不打印完整 Env/Command/任意敏感 labels。共享 Build Cache 先确认 Owner/活跃构建，不全局 system/volume/builder prune。

真实清理只在获授权 Cleanup/QA 收尾任务：先保存必要唯一日志/恢复证据，再停止本轮 runner/PG；按精确 ID 清理自有容器/匿名测试卷、网络、构建镜像，保留共享 postgres base image、生产/回滚镜像、附件/备份。解析并验证绝对临时目录与归属，禁宽泛 glob。之后复核容器/网络/镜像/卷/PID、路径不存在、释放量/保留项。trap只是机制，失败/中断仍要现场复核；容器清理成功也可能留下 Build Cache/目录。

## 7. 只读生产状态和发布门槛

从当次 server.md 获取生产读入口，在私有 shell 中设置 ops_url（仅操作变量，不是Server配置）；下列是已存在的真实 Endpoint：

~~~bash
curl --silent --show-error --fail --max-time 10 "$ops_url/healthz"
curl --silent --show-error --fail --max-time 10 "$ops_url/readyz"
curl --silent --show-error --fail --max-time 10 "$ops_url/v1/meta"
~~~

Meta的migrationRequired只反映运行binary需求，不能证明更新源码的迁移已执行。未做只读SQL登记核对时写Not Verified，不用旧版本false推断007–011已经执行。

单独发布批准涵盖窗口/停写、当前immutable image+配置、完整DB+附件备份与隔离恢复、按实际缺失项批准007→008→009→010→011、锁/耗时与不可逆redaction、固定新镜像、失败时单一旧binary+原配置并保留Guard/Order/Audit/Redaction、Queue/Scope reconciliation、health/ready/Meta和真实Gate probes、恢复写入条件。未经验证不混合旧新Writer，源码Merged不默认启用Plugin。

## 8. 环境失败与跨 Agent 收尾

连接/审批/隔离证明失败先停依赖步骤，保留阶段、退出码、脱敏错误与唯一恢复证据；按正常授权通道排障。测试编排收尾失败不能伪装测试PASS；确认实际日志/摘要/资源后分别报告。磁盘不足交Cleanup定位Owner，不全局prune。

下一Agent读入口、相关合同、最新delivery/本地报告和工作树，按明确活跃路径/文件所有权继续，不重做无目标架构审计。每次云端访问仍重读私有说明。
