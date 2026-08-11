# Plugin 与 Server 分仓库，Server 使用 Go

LoomTable Obsidian Plugin 和 LoomTable Server 使用两个独立仓库。Plugin 使用 TypeScript，Server 使用 Go 并以模块化单体部署；OpenAPI 是两个仓库之间的合同。这样可以分别发布和部署，同时避免在一个进程中混合两套后端运行时。

