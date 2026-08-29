# LoomTable Server 开发日志

本日志记录可由公开仓库提交、Pull Request、GitHub Actions 和 Release 核验的 Server 开发阶段与稳定支持边界。它不是 API 规范；API 以 [OpenAPI](./api/openapi.yaml) 为唯一权威。

## 当前基线

- 当前 `main`（PR #8 docs-only merge 后的文档主线）：[`41a403ca`](https://github.com/Mahjong404/LoomTable-Server/commit/41a403cae63e75e5b523a1a10c2318e43834082a)。PR #8 是 docs-only merge；runtime/API/OpenAPI/数据库/部署均未变。
- v0.1.0 Release：[`v0.1.0`](https://github.com/Mahjong404/LoomTable-Server/releases/tag/v0.1.0)，发布目标为 `ef0c6bd751642f4a604fe1bf88980f64e39dd992`。
- OpenAPI source：[`ef0c6bd`](https://github.com/Mahjong404/LoomTable-Server/commit/ef0c6bd751642f4a604fe1bf88980f64e39dd992)；当前 main 与该 source 的 `docs/api/openapi.yaml` blob SHA 均为 `92b416993ce6be4664d8bee783f2dcaed36e05b5`。
- PR #8 新增并索引本开发日志：新增 `docs/development-log.md`，并在 `docs/README.md` 建立索引；PR #8 仅变更 Markdown，OpenAPI blob 未变。
- PR #8 CI：[run 33259007295](https://github.com/Mahjong404/LoomTable-Server/actions/runs/33259007295) 成功；PR #8 合并后的 main CI：[run 33259077170](https://github.com/Mahjong404/LoomTable-Server/actions/runs/33259077170) 成功。

## 阶段记录

### P0 Server

- [PR #2](https://github.com/Mahjong404/LoomTable-Server/pull/2) 已合并，merge commit 为 `76c8ec377258ac9eeffe0a241b9976f03afe46df`。
- PR 记录的范围包括 Field/View 生命周期、Record Query/Change、Map 查询、PostgreSQL 认证管理、保留期清理、备份/校验/恢复入口及 20k Query/Map benchmark。
- PR 描述记录了本地与远程 Compose acceptance、Go test/vet、OpenAPI route contract、Docker/脚本检查和隔离 benchmark 结果。当前 Actions 历史没有与 PR #2 head 单独关联的 workflow run，因此本日志不把它写成独立 CI run 证据。
- P0 的发布状态以后续 main、Release 和 CI 记录为准，不以 PR 标题或本地 checkout 状态为准。

### Attachment P1

- [PR #3](https://github.com/Mahjong404/LoomTable-Server/pull/3) 已合并，merge commit 为 `2988f7dd4f87d811e0191a84b9e6b28f8c1f2471`。
- 范围包括 managed attachment、Attachment Field 配置与引用校验、ownership checks、migration 002 及 Attachment P1 OpenAPI/部署配置。
- PR CI：[run 31773943559](https://github.com/Mahjong404/LoomTable-Server/actions/runs/31773943559) 成功；合并后的 main CI：[run 31774583992](https://github.com/Mahjong404/LoomTable-Server/actions/runs/31774583992) 成功。

### Mutation/Conflict 合同澄清

- [PR #4](https://github.com/Mahjong404/LoomTable-Server/pull/4) 已合并，merge commit 为 `0bd67e910e9abf2b60100c7bb0366ca40fa6212b`。
- 该 PR 补齐 Mutation 的两种 409 响应形状、`CONFLICT` 与 `IDEMPOTENCY_KEY_REUSED` 映射，以及 PostgreSQL stale revision、幂等重放、key reuse 和 atomic rollback 的测试语义。
- PR 明确说明 runtime business code 与 routes 未改变。
- PR CI：[run 31899841947](https://github.com/Mahjong404/LoomTable-Server/actions/runs/31899841947) 成功；合并后的 main CI：[run 31899892754](https://github.com/Mahjong404/LoomTable-Server/actions/runs/31899892754) 成功。

### Map wire-field 修复与 v0.1.0 发布记录

- [PR #5](https://github.com/Mahjong404/LoomTable-Server/pull/5) 已合并，merge commit 为 OpenAPI source `ef0c6bd751642f4a604fe1bf88980f64e39dd992`。
- 该 PR 将 `VIEW_CONFIGURATION_REQUIRED` 的公开字段对齐为 `brokenFieldIds`，并增加 wire-shape 测试；PR 记录明确没有其他公开 wire drift。
- PR CI：[run 31903906320](https://github.com/Mahjong404/LoomTable-Server/actions/runs/31903906320) 成功；合并后的 main CI：[run 31903957339](https://github.com/Mahjong404/LoomTable-Server/actions/runs/31903957339) 成功。
- [PR #6](https://github.com/Mahjong404/LoomTable-Server/pull/6) 合并为 `9eec4440e5b8e76771299bac58f7ab4f949cb60c`，记录 v0.1.0 部署验收；PR CI [run 32073685593](https://github.com/Mahjong404/LoomTable-Server/actions/runs/32073685593) 与 main CI [run 32073757640](https://github.com/Mahjong404/LoomTable-Server/actions/runs/32073757640) 均成功。
- [PR #7](https://github.com/Mahjong404/LoomTable-Server/pull/7) 合并提交为 `e02f055fecddc0852085dc5a71b4eb136860774a`，当时成为 main，澄清 P0 release gates 为历史验收记录；随后 PR #8 docs-only merge 形成当前 main `41a403cae63e75e5b523a1a10c2318e43834082a`。PR CI [run 32074877745](https://github.com/Mahjong404/LoomTable-Server/actions/runs/32074877745) 与当时的 main CI [run 32074948440](https://github.com/Mahjong404/LoomTable-Server/actions/runs/32074948440) 均成功。

### P1.5 Server contract audit / freeze

- P1.5 的 Server 工作是合同核验与 stable-support 决策，不是新的 Server 实现交付。
- 核验基线为 release-gate/runtime stable-support freeze 的 `e02f055fecddc0852085dc5a71b4eb136860774a` 与 OpenAPI source `ef0c6bd751642f4a604fe1bf88980f64e39dd992`；`e02f055` 是 freeze 基线，不是当前 docs main；当前 main 为 PR #8 docs-only merge 后的 `41a403cae63e75e5b523a1a10c2318e43834082a`。当前 OpenAPI blob 未漂移。
- 已发布的 `records/query`、`records/mutate`、`changes`、Mutation/Conflict、幂等、revision、value validation 与 opaque change cursor 合同继续有效。
- P1.5 没有 Server 代码、API、OpenAPI、数据库或部署行为变更，也没有对应的 Server 实现 PR。Server 保持冻结，Plugin 只消费已发布合同。
- 该条目不应被解读为新增能力、接口承诺或 Server PR 完成。

## Stable-support 边界

- Server 当前只维护已发布 v0.1.0 合同；不为 Plugin 的 UI、scheduler、error split、queue 展示或 Map/Location wiring 新增接口。
- Plugin 应以 OpenAPI、响应中的 `requestId`、Record revision、MutationResult 和 opaque change cursor 为准；不得从本日志推导未发布字段或行为。
- 部署仍须完成显式 migration、认证配置和 readiness 检查；远程暴露须遵循仓库的 Personal deployment 文档。[部署文档](./operations/personal-deployment.md)

## 重新开启 Server 修改所需的 HTTP 证据

只有在提供最小、可脱敏、可复现的 HTTP 证据后，才重新审计 Server：

1. 实际 Server URL、HTTP method、完整 path 和请求体；不要提供 token、密码或其他凭据。
2. `/healthz`、`/readyz`、`/v1/meta` 的 status 与响应，尤其是 `serverVersion`、`apiVersion`、`migrationRequired`。
3. 失败业务请求的 HTTP status、响应 JSON 的 `error.code`、`message`、`requestId`。
4. 实际部署的 Server commit、是否经过反向代理，以及 timeout 属于连接、TLS 还是 HTTP 响应阶段。
5. 证明现象无法由 URL、认证、路径、请求体、代理或客户端重试/超时处理解释。

在证据满足前，Server 继续以 `e02f055` / `ef0c6bd` 作为 release-gate/runtime stable-support freeze 基线；该 freeze 基线不是当前 docs main，当前 main（PR #8 docs-only merge 后）为 `41a403ca`。
