# Attachment 支持 Managed 和 Vault 两种来源

Attachment 元数据存储在 PostgreSQL，文件内容不直接存入数据库。用户可以上传到 LoomTable 管理的文件存储，也可以引用 Obsidian Vault 文件；Managed Attachment 负责跨设备可用性，Vault Attachment 保留本地文件的快速访问，但不承诺由 LoomTable 负责跨设备同步。

