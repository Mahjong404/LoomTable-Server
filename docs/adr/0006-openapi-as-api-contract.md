# OpenAPI 是 API 合同唯一来源

LoomTable Server 仓库维护 OpenAPI 文件，Plugin 根据版本化合同生成或固定客户端类型，并将同一文件导入 Apifox。接口以 REST 资源、游标分页、Revision、Mutation ID 和统一错误码为核心，不让 Apifox、Server 和 Plugin 各自维护互相漂移的接口定义。

