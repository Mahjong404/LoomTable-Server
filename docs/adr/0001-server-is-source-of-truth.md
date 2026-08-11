# Server 是数据事实来源

LoomTable 采用服务端作为 Workspace、Base、Table、Field、View、Record 和 Managed Attachment 元数据的事实来源。Obsidian Plugin 不直接访问数据库，也不把每条 Record 拆成独立 Markdown 文件；这样 Personal 的本地 Docker、远程 Personal 和未来 Team 可以共用同一套数据模型。

