# P0 不引入 PostGIS

P0 将 WGS 84 Location 保存在 PostgreSQL JSONB 中，通过安全的数值提取完成矩形视口查询，并在 Server 应用层执行有界聚类；先以 20k Records 基准验证，再按热点添加表达式或投影索引。这样避免 Personal 部署提前承担 PostGIS 扩展依赖，同时保留在 `geoWithin`、Polygon 等更完整空间查询出现时重新引入 PostGIS 的路径。
