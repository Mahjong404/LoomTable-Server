# LoomTable 同步模型

## 事实来源

LoomTable Server 是 Workspace、Base、Table、Field、View、Record 和 Managed Attachment 元数据的事实来源。Obsidian Plugin 是客户端，不把本地缓存视为新的事实来源。

## Personal 阶段目标

第一阶段不实现实时协作，但支持：

- 多个客户端连接同一个 Server。
- 手动刷新。
- 活动 View 的低频轮询。
- Record Revision。
- Change Cursor。
- 幂等 Mutation。
- 可解释的 Conflict。

第一阶段不支持离线写入。服务不可用时，Plugin 可以读取最近缓存，但进入只读或不可编辑状态。

## Mutation

```json
{
  "clientMutationId": "mut_01H...",
  "commands": [
    {
      "kind": "updateRecord",
      "recordId": "rec_01H...",
      "expectedRevision": 7,
      "set": {
        "fld_01STATUS": "server-generated-option-id-in-progress",
        "fld_01NOTE": null
      },
      "unsetFieldIds": ["fld_01FOLLOW_UP"]
    }
  ]
}
```

`set` 中的 `null`、空字符串或空数组是显式值；`unsetFieldIds` 才把对应键从 `Record.values` 中移除。未出现在两者中的 Field 保持不变；同一 Field 同时出现在两者中属于无效 Mutation。

Server 校验 `expectedRevision`：

```text
expectedRevision == currentRevision
    ├── 是：应用 Mutation，Revision + 1
    └── 否：返回 Conflict，不覆盖当前值
```

一个 `MutationRequest` 中的多个 Command 采用全有或全无语义：所有 Command 在同一个 PostgreSQL Transaction 中校验和提交；任一 Command 失败时整个请求回滚，不返回部分持久化成功的结果。`clientMutationId` 用于请求级幂等，重复请求返回第一次应用结果，不再次增加 Revision 或 Change。

Workspace、Base、Table、Field 和 View 的创建请求使用必填的 `Idempotency-Key: mut_...` Header。Key 在 Actor 范围内全局唯一；相同 Method、Path 和规范化 Body 的重试返回第一次 `201` 结果，不重复创建对象。不同请求复用同一 Key 返回 `409 IDEMPOTENCY_KEY_REUSED`。元数据创建与 Record Mutation 共用 Server 声明的幂等保留策略。

规范化 Body 使用通过严格 Schema 校验后的领域输入，而不是原始 JSON 字节：未知属性先被拒绝，资源名称完成 Unicode Trim/NFC 后再进入幂等 Hash。CreateField/CreateView 的父级 Table ID 只取自路径参数并参与 Hash，Body 不重复提交该 ID。

Mutation 的失败原因必须可区分。至少包括输入无效、未认证、无权访问、资源不存在、能力未启用、Revision Conflict 和 Server 内部错误；HTTP 状态与 `ErrorBody.code` 必须保持稳定映射。

Record、Field、Table 和 View 都使用软删除和 Revision。Record 的删除/恢复，以及 Field、Table、View 的更新/删除，都必须带有 `expectedRevision`；P0 不提供硬删除。元数据冲突与 Record 冲突一样返回 `409 CONFLICT`。

P0 的 Cursor 是不透明且与 Query 参数绑定的值。Query 参数或排序不匹配时返回 `400 INVALID_CURSOR`；服务端无法再接受过期 Cursor 时返回 `410 CURSOR_EXPIRED`。

Table、Field、View List 与 Record Query 默认只返回 `active` 对象；调用方可显式请求 `deleted` 或 `all` 以发现回收站内容。Record Lifecycle Scope 是 Cursor 绑定的一部分，不能在续页时改变。

Change Cursor 按 Table 作用域保存。当前版本的 Change 保留期为 30 天；不带 Cursor 拉取时返回当前尾部位置和空的 ChangePage，而不是返回全部历史 Change。后续版本可以让 Personal Server 选择 `30d`、`90d`、`365d` 或 `forever`，并在 Server Meta 中声明实际保留策略。

## Change Cursor

Change Cursor 是单调前进的服务端位置，不等同于 Record Revision。Record Revision 用于单条记录的并发控制；Change Cursor 用于客户端发现一段时间内发生过哪些变化。

```text
Query → 返回 changeCursor
Pull → 提交 cursor，返回 ChangePage 和 nextCursor
```

Map Query 同样返回查询快照的 `changeCursor`。活动 Map View 发现 Record、Field 或 View Change 后保留当前临时 Map Viewport 并重新查询；Change 本身不被客户端解释成增量 Marker 补丁，因为 Filter、聚类、计数和 Data Bounds 都可能联动变化。

Map 全局 Summary 使用独立查询，不随每次相机移动重复计算。Cluster Record Query Token 有效 5 分钟并绑定原 Map Query、View Revision 与 Table Change Cursor；超时或相关 Change 后返回 `410 QUERY_SNAPSHOT_EXPIRED`，不提供漂移的实时分页。

Change 至少包含：

- Change ID。
- Table ID。
- Record ID 或 Schema Object ID。
- Change 类型。
- 新 Revision。
- Actor ID（Personal 模式也保留字段）。
- 创建时间。

## Conflict

Conflict 响应应包含：

- 客户端提交的旧 Revision。
- 服务端当前 Revision。
- 客户端变更。
- 当前服务端值。
- 可安全合并的字段提示。

第一阶段 UI 支持：

- 放弃本地修改。
- 使用当前服务端值。
- 覆盖服务端值。
- 逐字段选择。

覆盖操作必须是一次新的、明确的 Mutation，不能由普通重试隐式完成。

## Vault Attachment

Vault Attachment 不由 LoomTable Server 负责跨设备同步。Plugin 可以检测当前 Vault 中的文件是否存在；如果用户使用某种 Vault 同步工具，LoomTable 不假设同步已经开始或完成。

未来可以提供可选的 `VaultSyncProvider` Adapter，但其结果只能表示“已请求”“已知成功”或“不支持”，不能替代文件存在性检查。
