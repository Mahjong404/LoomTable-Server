# 使用 Obsidian 主题语义而不是绑定特定主题

LoomTable UI 使用自己的 `--loom-*` 语义 Token，并映射到 Obsidian CSS Variables。组件样式限制在 `.loom-*` 命名空间；任何特定 Obsidian 主题只作为可选视觉环境，不成为运行时依赖。这保证 Light、Dark、移动端和第三方主题下的基本可用性。

