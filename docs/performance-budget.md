# 性能预算与测试计划

## 基线

第一阶段以以下数据集作为基线：

| 级别 | Record | Field | 用途 |
|---|---:|---:|---|
| S | 100 | 10 | 功能测试 |
| M | 1,000 | 20 | 普通交互 |
| L | 10,000 | 30 | 性能回归 |
| XL | 20,000 | 50 | 产品基线 |
| Stress | 50,000+ | 50 | 压力测试 |

测试值需要包含空值、长文本、多选、Location 和不同长度的主字段；Attachment 引用作为后续 capability 的扩展夹具。

## 临时目标

这些数字是工程目标，不是未经验证的产品承诺：

- XL 数据集打开后先显示结构和首个可见窗口。
- 本地 Docker 服务的常规筛选和排序目标为数百毫秒级。
- 单次 Cell Mutation 不重新加载完整 Table。
- Grid DOM 行数量保持在可控的视口窗口内，而不是等于 Record 总数。
- 快速滚动不产生持续的长任务。
- 50k 数据集允许降级，但不能导致 Plugin 崩溃或无限增长内存。

## 测量维度

### Server

- SQL 查询耗时。
- API 总耗时。
- 响应体大小。
- p50/p95 延迟。
- PostgreSQL 连接和锁等待。
- 批量 Mutation 耗时。

### Plugin

- 首次可见时间。
- 页面切换时间。
- Grid 滚动长任务。
- DOM 节点数量。
- JS 堆内存。
- Cell 编辑到确认的延迟。
- Map Marker 和聚类数量。

## 优化原则

- Filter、Sort、Group 在 Server 执行。
- 使用游标分页。
- 只缓存当前窗口和相邻页面。
- Grid 行虚拟化。
- 启用 Attachment capability 后使用缩略图、懒加载和缓存。
- Map View 使用服务端视口查询返回最多 500 个 Map Point/Map Cluster；Server 自适应聚类并完整代表视口内结果，不能下载完整匹配集、静默截断或一次创建所有复杂 Popup。
- Map Point 只携带 Record ID、坐标和 Primary Field 文本；详情按需直查。全局计数和 Data Bounds 由独立 Summary 查询聚合，只在首次打开、Filter 变化或显式“适配全部”时请求；普通平移/缩放不重复全局聚合，也不把完整 Record 集传给 Plugin。
- P0 不依赖 PostGIS；Location 视口查询从 JSONB 安全提取 WGS 84 数值并在应用层聚类。先用 XL 20k 基准验证，再为已测出的热点增加表达式或投影索引，而不是为所有 Field 盲目建索引。

## 回归场景

1. 打开 XL Table。
2. 快速上下滚动。
3. 横向滚动并操作固定列。
4. 连续编辑 100 个 Cell。
5. 切换 Filter 和 Sort。
6. 同时打开 Grid 和 Map View。
7. Server 重启后恢复 View。
8. 模拟服务不可用和恢复。
9. 模拟 Revision Conflict。
10. 在窄桌面 Pane、平板和手机布局下运行。
11. 验证 Filter 深度 8/节点 100、10 个 Sort、500 个 Projection 和 8 MiB Body 的边界与拒绝路径。
12. 对 20k Records 的 Grid Query、Location 矩形视口查询、Map Summary 和应用层聚类记录 p50/p95、查询计划、响应大小及峰值内存。

## P0 验收门槛

20k Query/Map 基准是 Server P0 PR 转为 Ready 的必要条件，不是可延后的优化项。还必须通过 OpenAPI Contract、PostgreSQL Integration、Docker Compose Smoke、完整 Backup/Restore、Go Test/Vet；未达到任一门槛时 `agent/server-p0` 保持 Draft，不合并到 `main`。
