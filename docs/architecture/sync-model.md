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
  "tableId": "tbl_01H...",
  "recordId": "rec_01H...",
  "expectedRevision": 7,
  "changes": {
    "status": "进行中"
  }
}
```

Server 校验 `expectedRevision`：

```text
expectedRevision == currentRevision
    ├── 是：应用 Mutation，Revision + 1
    └── 否：返回 Conflict，不覆盖当前值
```

## Change Cursor

Change Cursor 是单调前进的服务端位置，不等同于 Record Revision。Record Revision 用于单条记录的并发控制；Change Cursor 用于客户端发现一段时间内发生过哪些变化。

```text
Query → 返回 changeCursor
Pull → 提交 cursor，返回 ChangePage 和 nextCursor
```

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

