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
| map-point | Map Point | 地图点 | Marker Record、GeoPoint | Map View 中一条已定位 Record 的呈现 |
| map-cluster | Map Cluster | 地图聚类 | 分组 Record、聚合 Record | 当前比例尺下多个密集 Map Point 的汇总呈现 |
| map-summary | Map Summary | 地图汇总 | 视口数量、地图聚类 | 与临时视口无关、针对已保存 Map View 匹配集的精确全局计数与地理范围 |
| default-camera | Default Camera | 默认相机 | 当前视口、上次平移位置 | Map View 共享保存的初始中心点和缩放级别 |
| map-viewport | Map Viewport | 地图视口 | 默认相机、Map View 配置 | 一个 Map View 实例当前可见、不会自动保存的地理范围 |
| unlocated-record | Unlocated Record | 未定位记录 | 隐藏记录、不可渲染地点 | Map View 命中但缺少合法 WGS 84 坐标的 Record |
| unrenderable-location | Unrenderable Location | 不可渲染地点 | 无效地点、未定位记录 | 坐标合法但超出 P0 EPSG:3857 可渲染纬度范围的 Location |
| tile-provider | Tile Provider | 瓦片提供方 | 地图源、LoomTable 地图服务 | 向 Map View 提供可替换底图瓦片的外部服务 |
| tile-provider-profile | Tile Provider Profile | 瓦片配置档 | Map View 配置、瓦片 URL 字段 | 客户端使用某个瓦片提供方的具名非密钥配置 |
| field | Field | 字段 | 属性、列配置 | 一种值的定义 |
| field-type | Field Type | 字段类型 | 数据格式、控件类型 | 字段的语义和值行为 |
| record | Record | 记录 | 行、数据项 | 数据表中的独立数据项 |
| cell | Cell | 单元格 | 字段值槽 | 记录与字段交叉位置的值 |
| unset-cell | Unset Cell | 未设置单元格 | null、空字符串 | 尚未提供值、与显式空值不同的单元格 |
| natural-empty-value | Natural Empty Value | 自然空值 | 未设置、null | 已存储且非 null、但该 Field Type 在空值筛选中视为空的值 |
| primary-field | Primary Field | 主字段 | ID 字段、标题列 | 用于识别记录的用户可见字段 |
| select-option | Select Option | 选项 | 标签文本、显示名称 | Select/MultiSelect 中由 Server 持有稳定 ID 的可选值 |
| active-option | Active Option | 活动选项 | 启用标签、未删除名称 | 当前可供新 Record 引用并参与显示顺序的 Select Option |
| deleted-option | Deleted Option | 已删除选项 | 无效选项、移除字符串 | 不再接受新引用、但为历史值和恢复保留的 Select Option |
| relation | Relation | 关联 | 外键字段、链接文本 | 对其他数据表记录的引用 |
| computed-field | Computed Field | 计算字段 | 公式列 | 由其他数据派生且只读的字段 |
| location | Location | 地点 | GeoPoint 字段、地点文本 | 可包含名称、地址和坐标的地点值 |
| geopoint | GeoPoint | 地理坐标 | 地点字段 | Location 内部的经纬度值 |
| region | Region | 地区 | 地点、行政区文本 | 从版本化层级目录选择的行政区域值 |
| date-time | DateTime | 日期时间 | 本地时间字符串 | 表示明确时间基准的日期和时间值 |
| time | Time | 时间 | 时长、日期时间 | 不带日期的每日时刻值 |
| geo-within | GeoWithin | 范围内 | 地图临时框选 | 判断 Location 坐标是否位于指定空间范围内的查询条件 |
| attachment | Attachment | 附件 | 文件路径、二进制字段 | 对文件内容的引用 |
| managed-attachment | Managed Attachment | 托管附件 | 服务器文件 | 由 LoomTable 存储管理的附件 |
| vault-attachment | Vault Attachment | Vault 附件 | 本地路径附件 | 引用 Obsidian Vault 文件的附件 |

## 变化和服务术语

| Canonical ID | English | 简体中文 | Avoid | 说明 |
|---|---|---|---|---|
| mutation | Mutation | 变更请求 | 写事件、数据库更新 | 客户端请求的数据或结构修改 |
| revision | Revision | 对象版本 | 时间戳、同步版本 | 用于 Record 或可变元数据对象并发控制的版本 |
| change | Change | 变更 | 请求、操作 | 已持久化的数据变化事实 |
| change-cursor | Change Cursor | 变更游标 | 页码、同步令牌 | 拉取后续变更的位置 |
| conflict | Conflict | 冲突 | 合并错误、覆盖警告 | expected Revision 与对象当前 Revision 不相等的变更被拒绝 |
| lifecycle-scope | Lifecycle Scope | 生命周期范围 | 祖先可见性、权限范围 | 按对象自身状态选择 Active、Recycle 或两者的查询范围 |
| query-snapshot | Query Snapshot | 查询快照 | 保存视图、实时分页 | Continuation Token 使用期间成员必须稳定的短期绑定查询上下文 |
| record-query-cursor | Record Query Cursor | 记录查询游标 | 查询快照、页码 | 绑定等价 Query 与排序、但允许并发 Change 影响成员的短期续页位置 |
| recycle-state | Recycle State | 回收状态 | 硬删除、回收站副本 | 可发现并恢复的软删除对象状态 |
| actor | Actor | 操作者身份 | Token、会话、用户账户 | 变更归属的稳定认证身份 |
| access-token | Access Token | 访问令牌 | Actor ID、用户身份、密码 | 具名、可独立撤销且不改变 Actor 身份的秘密凭据 |
| backup-archive | Backup Archive | 备份归档 | 数据库转储、卷副本 | 包含数据库、托管附件和兼容性清单的版本化校验恢复单元 |
| validation-preset | Validation Preset | 校验预设 | 特殊数字类型 | 针对 Text 等字段的区域化格式校验规则 |
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
