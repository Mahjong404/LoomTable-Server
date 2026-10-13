# G2 — Atomic Choice Existing Record / Create Draft

2026-10-13。用户已授权本 Gate 的合同与 Server 实施；产品依据为 Plugin Wave 1.5 API Boundary G2。本合同基于 Server main `bb00e6ad3d100a6e5e0879d1e8c1fb25ab51bfec`，不启动 G3–G5，不修改 Plugin，不声明未部署的 UI 可用。

## 协议选择与请求

复用 `POST /v1/tables/{tableId}/records/mutate`。现有单事务、Actor-scoped clientMutationId、ordered command results、Record CAS、8 MiB body 与 500 commands 已适合 G2；新增 endpoint 或组合两个领域 Service 的事务没有必要。

OpenAPI 0.5.0：`createRecord` 和 `updateRecord` 增加可选非空 `choices` 数组；delete/restore 不接受它。每项为 `{fieldId, expectedFieldRevision, options}`。options 中每项严格二选一：`{optionId}` 或 `{name, color}`；禁止假 ID、额外属性或 null。color 必须属于 G1 全 12 色集。

```json
{
  "clientMutationId": "mut_01K00000000000000000000000",
  "commands": [{
    "kind": "updateRecord",
    "recordId": "rec_01K00000000000000000000000",
    "expectedRevision": 4,
    "choices": [{
      "fieldId": "fld_01K00000000000000000000000",
      "expectedFieldRevision": 7,
      "options": [{"name": "Ready", "color": "lime"}]
    }]
  }]
}
```

Existing Record 使用 updateRecord 与 expectedRevision。New Draft 使用 createRecord、values（允许 `{}`）与同一 choices 数组；Record ID 和新 Option ID 都由 Server 在提交事务中生成。取消或未提交 Draft 不产生持久数据。

choices 定义该 Cell 的完整最终选项集合。Select 恰好一个 Option；清空仍用现有 set:null。MultiSelect 0–100 项，保留输入顺序；解析到重复 ID 返回 422，不偷偷去重。禁止同一 Field 同时出现在 choices 和 values/set/unsetFieldIds；同一 command 的 Choice Field 不能重复。普通非 Choice values/set 可与 choices 同事务提交。

## 名称、身份与资源边界

- name 复用 domain.NormalizeOptionName：Unicode Trim → NFC，非空、拒绝控制字符，规范化后最多 100 码点；lookup 复用 domain.FoldKey 的 locale-neutral Default Case Folding，accent-sensitive。
- 只对 active Options 按 canonical name 查找。找到则复用真实 ID、原名称/颜色/位置，不改名、不改色；仅不存在时创建并追加到 active 列表尾部。相同 name 在同请求多个 command 中解析到同一真实 ID；第一次创建的输入确定名称与颜色。
- deleted Option 不参与 name lookup，也不被隐式恢复；同名 draft 可创建新的 active 身份。显式 optionId 必须属于该 Field；deleted ID 只能在已有 Record 的该 Cell 中原样保留，不能新增引用或用于 New Draft，沿用普通 Record writer 的 tombstone 规则。
- 权限继承当前 Actor/Workspace/Base/Table；外来或不可访问对象 404，已删除 Field/Record 的动作 422 INVALID_STATE_TRANSITION。不新增 ACL 或绕过 Field 类型。
- 每请求最多 50 个不同 Choice Fields、500 个 Option 输入；每 command 最多 50 Choice Fields，每 Field 最多 100 Option 输入。继承每 Field 500 active / 5000 total Options 上限，已有 Record Mutation 的 500 commands / 8 MiB body 不变。预算失败 422，无部分效果。

## 双 CAS、事务和锁序

1. 同一 PostgreSQL transaction：Actor 序列化锁 → Receipt/identity lookup → active Table lock → Choice Fields 按 ID 排序 FOR UPDATE → 其余 Field definitions 的 SHARE locks → existing Records → Option/Record/Change/Receipt/identity 写入 → COMMIT。
2. 同请求对同 Field 的 expectedFieldRevision 必须一致，基于事务开始的 Field Revision。所有 Choice Field CAS 在任何写入前检查；然后检查所有已有目标 Record CAS。任一失败整个请求回滚，无 Option、Record、Change、Receipt 或 identity guard 留下。
3. Field CAS 失败返回既有 409 CONFLICT envelope 的 clientMutationId/failedCommandIndex/conflicts，并增加 fieldConflicts（fieldId/expectedRevision/currentRevision）；field-only conflict 的 conflicts 为 `[]`。Record CAS 使用原 conflicts。两者同时过时时优先返回 Field CAS，Client 刷新后显式形成新的业务请求。
4. 一个 Field 在一次请求中创建任意数量 Option，只增加一次 Field Revision 并写一次 schemaChanged；只选/复用不改变 Field Revision。Record Revision、projection、recordCreated/recordUpdated 与 unchanged 沿用现有 writer。相同最终值与无新 Option 返回 unchanged，不制造 Change；createRecord 始终 applied。
5. Catalog Field Update/Delete/Restore/Convert 共享 Table → Field 锁序，避免原 Field-first Convert 与 Record Table-first 互锁。无需组合 Catalog Service。Actor 的序列化锁用 FOR NO KEY UPDATE：保持同 Actor Receipt writer 互斥，允许 metadata Change 的 Actor FK KEY SHARE 检查，避免 Field/Table writer 的隐式反向锁；Actor ID 不可变。
6. 同名竞争仍遵守严格 Field CAS：若首次创建改变了 Field Revision，另一旧 revision 请求 409、零效果；刷新 Field 并新建明确请求后复用胜出的 Option。不会绕过 CAS 进行“自动重试”。Convert 与创建 Option 的竞争至多一个在相同旧 Field revision 上成功；所有 writer 串行于 Table，保持类型/Option/Record projection 一致。

## Receipt、幂等和未知结果

clientMutationId 不改变形状，仍以 Actor + ID 唯一。fingerprint 包含 Table/path、原始 command payload（含 choices、name/color、两种 revisions）；不先将请求改成生成 ID 或 canonical name 再计算 hash。原 key + 原 payload replay 在 CAS、生命周期、预算检查之前返回第一次完整 StoredMutationResult；改 payload 返回 409 IDEMPOTENCY_KEY_REUSED。

成功响应保留 clientMutationId/results/changeCursor，G2 请求额外返回 `choiceFields`：每不同 Field 一项，fieldId/beforeRevision/revision/resolvedOptions/createdOptions。选项是实际 id/name/color；resolvedOptions 为本请求选择的不同身份，createdOptions 为本请求真正新增的子集。上限受 50 Fields / 500 输入约束，不返回整个 5000 项 Field config。无 choices 的旧请求不增加该成员。此 delta 与所有 command results/sequence 在同一 Receipt 原样持久化和 replay。

稳定 operation identity 就是 Actor-scoped clientMutationId，command identity 是该 ID + result.index；生成 Option/Record ID 与 delta 在 replay 中稳定。changeCursor 是边界，不是事件 ID，不用时间/标题猜关联；完整 Table Audit/inverse/rollback 仍不属于 G2。

HTTP response 丢失、timeout、connection reset、commit error 均可能是 unknown：保持同一 Actor/连接、Table、原 key 与原 body。关闭 UI 不撤回已发操作；不得换 ID、改 CAS 或变成两次请求。确定的 409/422/404 才证明这次没有写入；409 IDEMPOTENCY_KEY_REUSED 不是可自动覆盖的 CAS。

**Receipt expiry 的安全边界**：现有 receipts 可由 retention 清理，单靠它无法阻止过期 key 再次创建。因此新增 migration 007 的 `mutation_identity_guards`，仅保存成功 G2 的 Actor/key/request hash（不保存业务值），同 transaction 写入，不随 Receipt/Change retention 删除。所有已有 idempotent entrypoints 的 replay helper 在 Receipt 缺失时查 guard：同 hash 返回 `410 MUTATION_RECEIPT_EXPIRED`，details `{clientMutationId, operationCommitted:true}`；不同 hash 409。410 证明原 operation 曾提交但完整结果已不可得，必须 reconciliation，禁止原请求重放或自动换 key recreate。G2 unchanged 同样保存 guard；失败没有 guard。既有非 G2 receipts 与其 retention 语义不变。

## Plugin Client / Durable Queue 接入

Server capability `atomicChoice` 表示本协议，独立于 attachments；新 Client 必须同时确认 migrationRequired=false、连接可用与 capability。旧 Server strict decoder 会拒绝 choices，不降级为 Field PATCH + Record Mutation。

Plugin 当前 Queue schema v2 严格拒绝未知 command property，必须单独升级持久 schema（v3）及 parser/DTO/decoder/InMemory/runtime/scheduler。原 v1/v2 项无损迁移，不改变已有 body、key、expectedRevision、状态；G2 的 choices、expectedFieldRevision、每项原 name/color/optionId 在任何发送前完整落盘，storage failure 不发 HTTP。版本不识别时保留原存储并 blocked，不能把 choices 丢掉或把 sending 当成未尝试。

重试不重新生成字段 payload/IDs；Field 与 Record conflict 均支持确切 envelope。410 guard expiry 是 terminal reconciliation，不作为网络错误循环 retry。接收 choiceFields 后按 Field revision 单调合并；cache revision 高于响应不覆盖，缺少 beforeRevision 基础时 GET/refetch；createdOptions 按真实 ID 合并，不先装配假 ID。late response 不越过连接/Table/generation 边界。Queue、UI 与 session Undo 的接入由 Plugin 工作线实现，assignment-only Undo 不能声称撤回 Option delta。

## Migration、兼容与发布

007 为新增小表和复合主键/Actor FK，无既有数据重写或回填；不为历史非 G2 操作伪造永久保证。readiness 必须检查 007；未迁移 binary 不可宣称 ready。隔离测试验证 001–006 → 007、重复 apply、既有数据不变、backup/restore 保留 guard、G6 binary 在保留 007 的 DB 上可 ready/读旧协议。

旧 Client 省略 choices：原 command/response/fingerprint 不变，G1 12 色与 G6 Search 保持兼容。回滚 binary 至 G6，保留 007 与 guard，不 DROP 表；capability 消失，Plugin 暂停 G2 Queue，不能改写待处理 payload。旧 binary 不承诺能恢复 G2 完整 Receipt；恢复 G2 后才用原 body/key replay。guard 删除会永久撤销防重复保证，不作为普通 downgrade，禁止自动 down migration。

本轮形成一条主要 PR：合同 → OpenAPI → Domain/Service/Storage/HTTP → unit/HTTP/contract → 隔离事务/并发/forward migration/restore/downgrade → 两轴审查 → Review/Merge Ready。集中 push，保留 CI 门禁；生产发布单独记录，本轮不因 PR 测试完成自动执行生产 migration 或启用 G2 UI。
