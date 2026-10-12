# G6 后端交付 — 2026-10-12

G6 已审查、合并并部署。固定源码提交：`f3980b1b4126cdfce026c0a94779c8c8261b3997`；main 合并提交：`bf6237b7450af3ff3ca1ba9d0155c19096a352fb`；[PR #11](https://github.com/Mahjong404/LoomTable-Server/pull/11)。合并 tree 与固定源码 tree 相同；发布前逐一核对 132 个归档文件的 Git blob SHA，无缺失或额外文件。

## 合同与 Client 接入

- [Approved Contract](../contracts/g6-aggregate-search.md)：既有 Aggregate endpoint 增加可选 search，复用 Record Query 的 Filter + Search 成员条件与文本投影；不受 aggregate fieldIds、显示列或已加载页面限制。一次响应的所有 Field 结果及 changeCursor 共用 REPEATABLE READ 快照；独立 Query/Aggregate 请求不承诺跨请求固定快照。
- OpenAPI 0.4.0：[固定提交 OpenAPI](https://github.com/Mahjong404/LoomTable-Server/blob/f3980b1b4126cdfce026c0a94779c8c8261b3997/docs/api/openapi.yaml)、[PR diff](https://github.com/Mahjong404/LoomTable-Server/pull/11/files)。AggregateRequest.search 为 optional string，响应结构与既有 endpoint 不变；Meta 增加 aggregateSearch 描述。生成 Client 增加可选 Search 参数，HTTP/InMemory/Controller 需同口径传递当前 Search。
- Unicode：外部空白 Trim → NFC → 500 码点校验 → Default Case Folding。OpenAPI 以 x-loomtable-normalized-max-length 表达规范化后的限制；合法原始字符串可超过 500 码点。既有 Query schema 的原始 maxLength 偏差已在合同注明，本 Gate 不修改其历史声明。
- Capability：实现二进制无条件声明 aggregateSearch，与 attachments 无关；实际公开 Meta 已确认。旧 Client 省略 search，行为不变；新 Client 在 Search 非空时必须观察 capability，缺失时显示聚合 unavailable，不能使用 filter-only 或 loaded-page 结果。空 Search 可以继续旧请求。
- 兼容：API v1、minPluginVersion 0.1.0 不变；没有 migration、回填、Revision/CAS、Idempotency/Replay、receipt 或 Durable Mutation Queue 变化。Search 保持 Primary/Text/LongText/URL 投影，Field/Option 名称不新增匹配。

## 验证与发布事实

- 本地 go test ./...、go vet ./...、gofmt 与 diff check 通过。两项独立审查发现（OpenAPI 原始长度约束、并发测试取消/join）已修正并复核关闭，无未解决发现。
- 专用 disposable PostgreSQL 上完整 go test -count=1 ./... 与 go vet ./... 通过，包含 G1 和全部既有数据库集成测试。G6 覆盖 Filter+Search/仅 Search/空 Search、Unicode/字面 %_/URL/LongText、active/deleted/权限、完整 Query membership 超过一页、所有 Number/Date 函数及 null/0、真实 HTTP 类型/长度错误；并发事务验证两个 Field 的结果与真实 Mutation changeCursor 一致。
- 隔离 runner 只创建本次 internal network、无 host ports、tmpfs PostgreSQL 与合成数据；生产/共享数据库未参与破坏性测试。QA 容器与网络退出后已清理。
- CI：[Go tests and vet](https://github.com/Mahjong404/LoomTable-Server/actions/runs/38263219897/job/114858782742)、[Docker build / Compose validation](https://github.com/Mahjong404/LoomTable-Server/actions/runs/38263219897/job/114858782430) 均成功。
- 发布：2026-10-12 22:32（Asia/Shanghai），serverVersion `v0.4.0+f3980b1b4126`，固定提交镜像，SHA-256 `e5d81341be8152a868ec9aa48eeb7e3696a34e4f25f42f64f5651f6f6ca7c7c8`；运行镜像 revision label 为固定源码 SHA。生产 checkout 保留原版本，实际运行事实以固定镜像、Compose override 和 Meta 为准。
- 备份：暂停应用写入后备份 PostgreSQL + Managed Attachments；归档校验通过，SHA-256 `7e5bf69f23c5dea417d97abb1164ff46a9c94b0c0498665afef107f639d85c2e`。隔离 tmpfs PostgreSQL 完成 pg_restore，schema/Record/Field/Attachment 数量一致；附件归档独立恢复成功。私有备份位置、恢复日志及原配置保留在运维环境，不写入本仓。
- 回滚：已保留 G1 固定镜像与 override；发布失败自动恢复原配置并启动 G1。正常回滚无需数据操作或 migration；新 Client 观察 aggregateSearch 消失后使 Search 聚合 unavailable。保留 G1 的 choicePalette12，避免跨过 G1 的新色降级限制。
- 实际运行：内部与公开 HTTPS 的 /healthz、/readyz、/v1/meta 全部 200；Meta capabilities 为 grid/map/choicePalette12/aggregateSearch/attachments，migrationRequired=false、bootstrapState=complete；六个既有 migration 未变。未在生产创建 QA Record。

## Plugin Handoff

Plugin 规划工作线以固定源码 SHA 执行 api:sync → api:generate，审查 optional Search 与 Capability diff，然后对 HTTP/InMemory/Controller 接入同一 Search、generation/cache guards 与 late-response 保护。验收至少覆盖超过一页的成员范围、Filter 与 Search 交集、Unicode/null/0、旧 Server capability 缺失及切换连接后的能力失效。最新固定提交也包含已交付 G1，按其合同单独协调色板消费者。

本 Server 工作线未修改 Plugin 代码、API pin、状态表或开启新增三色 UI。后续工程顺序为 G2 → G3 → G4 → G5，理由见 Server 执行入口；各 Gate 仍按独立合同、实现、审查和安全发布交付。跨仓库唯一进度表由 Plugin 规划工作线维护。
