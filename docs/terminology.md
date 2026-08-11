# LoomTable 术语对照表

本表是 LoomTable 的中英文术语约束。代码、数据库字段、OpenAPI 和日志使用 Canonical ID；用户界面使用中文或英文翻译。除非另有说明，不得为同一概念创造新的正式名称。

## 领域术语

| Canonical ID | English | 简体中文 | Avoid | 说明 |
|---|---|---|---|---|
| workspace | Workspace | 工作空间 | 账户、项目空间 | 用户或团队的数据容器 |
| base | Base | Base（多维表） | 数据库、数据源 | 包含多张数据表的逻辑空间 |
| table | Table | 数据表 | 表格、页面 | 由字段和记录组成的数据集合 |
| view | View | 视图 | 页面、副本 | 同一数据表的查询和展示配置 |
| grid-view | Grid View | 表格视图 | 网格页面 | 电子表格式记录视图 |
| map-view | Map View | 地图视图 | GIS 页面 | 将带坐标记录显示在地图上的视图 |
| field | Field | 字段 | 属性、列配置 | 一种值的定义 |
| field-type | Field Type | 字段类型 | 数据格式、控件类型 | 字段的语义和值行为 |
| record | Record | 记录 | 行、数据项 | 数据表中的独立数据项 |
| cell | Cell | 单元格 | 字段值槽 | 记录与字段交叉位置的值 |
| primary-field | Primary Field | 主字段 | ID 字段、标题列 | 用于识别记录的用户可见字段 |
| relation | Relation | 关联 | 外键字段、链接文本 | 对其他数据表记录的引用 |
| computed-field | Computed Field | 计算字段 | 公式列 | 由其他数据派生且只读的字段 |
| location | Location | 地点 | GeoPoint 字段、地点文本 | 可包含名称、地址和坐标的地点值 |
| geopoint | GeoPoint | 地理坐标 | 地点字段 | Location 内部的经纬度值 |
| attachment | Attachment | 附件 | 文件路径、二进制字段 | 对文件内容的引用 |
| managed-attachment | Managed Attachment | 托管附件 | 服务器文件 | 由 LoomTable 存储管理的附件 |
| vault-attachment | Vault Attachment | Vault 附件 | 本地路径附件 | 引用 Obsidian Vault 文件的附件 |

## 变化和服务术语

| Canonical ID | English | 简体中文 | Avoid | 说明 |
|---|---|---|---|---|
| mutation | Mutation | 变更请求 | 写事件、数据库更新 | 客户端请求的数据或结构修改 |
| revision | Revision | 记录版本 | 时间戳、同步版本 | 用于并发控制的记录版本 |
| change | Change | 变更 | 请求、操作 | 已持久化的数据变化事实 |
| change-cursor | Change Cursor | 变更游标 | 页码、同步令牌 | 拉取后续变更的位置 |
| conflict | Conflict | 冲突 | 合并错误、覆盖警告 | 基于过期版本的变更被拒绝 |
| server | Server | 服务端 | 后端程序 | LoomTable Server |
| plugin | Plugin | 插件 | 客户端程序 | LoomTable Obsidian Plugin |
| personal | Personal | Personal 模式 | 纯本地模式 | 单用户、无实时协作的部署配置 |
| team | Team | Team 模式 | 共享 Personal | 多用户协作部署配置 |

## UI 文案规则

1. 首次出现的 `Base` 在中文界面显示为 `Base（多维表）`。
2. `Table` 显示为“数据表”，不使用“表格”作为领域对象名称；“表格”只用于 `Grid View` 的用户描述。
3. `Location` 显示为“地点”；只有在坐标输入或技术说明中使用“地理坐标”。
4. `Tag` 不是字段类型；标签表现由 `Select` 或 `MultiSelect` 提供。
5. 错误信息优先使用用户可操作的中文说明，同时提供可复制的错误码和诊断 ID。

