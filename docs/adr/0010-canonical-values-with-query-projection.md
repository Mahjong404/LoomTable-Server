# Canonical Record 值与可重建查询投影分离

Record 的 `values` JSONB 继续保存并返回用户提交的规范值，同时在同一事务中维护不对 API 暴露、可从 Canonical Values 重建的 `query_values` 与 `search_text`。查询投影由 Go 生成统一的 Unicode Case-Fold、类型化和坐标键，使 PostgreSQL 能执行已确认的跨端查询语义而不依赖数据库 Locale；这比完整 EAV 模型简单，也避免把派生键混入用户数据，热点索引仍由实际基准决定。
