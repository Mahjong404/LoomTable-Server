# PostgreSQL 与 JSONB 作为第一阶段存储

第一阶段只支持 PostgreSQL。Record 的普通字段值使用 JSONB，Field Definition、View、Revision 和查询所需元数据单独保存；Relation 可以维护规范化查询索引。该选择让字段扩展不依赖频繁修改数据库列，同时保留对热点查询建立专用索引的空间。

