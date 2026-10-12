# G1 后端交付 — 2026-10-12

G1 已审查、合并并部署。固定源码提交：`47fc985bd9376dc761469b40fe40dcfd1ceb483e`；main 合并提交：`173c2f031dd4acf0957a40afcb07e94125e8305f`；[PR #10](https://github.com/Mahjong404/LoomTable-Server/pull/10)。

- 合同：[G1 Choice Palette](../contracts/g1-choice-palette.md)。Select/MultiSelect 12 色严格 writer；Record 值仍为 Option ID；distinctOptions 仍轮播原九色。
- OpenAPI：固定提交的 docs/api/openapi.yaml，0.3.0，SelectOptionColor 增加 lime/indigo/brown；路由与响应结构不变。生成 Client 的 color union 扩为 12，现有 string reader 保值。
- Capability：实际 /v1/meta 已包含 choicePalette12；API v1，minPluginVersion 0.1.0，serverVersion v0.3.0+47fc985bd937。
- 发布：2026-10-12 22:07（Asia/Shanghai），固定提交镜像；镜像 SHA-256 `00613b7b9941be4429e108109805c466d441bc9d01cd2dfdd4a7cc0ec4e409e3`。生产 checkout 仍为此前版本，运行事实由固定镜像、Compose override 和 Meta 确认。未执行 migration；六个既有 migration 无差异。
- 备份：停止应用写入后备份 PostgreSQL + Managed Attachments；归档校验通过，SHA-256 `f1b72c2f55071aa900ce5e6c30e52c45fb1fd8559cfda197d1836e98ef7184a7`。在无公网端口的隔离恢复实例完成 pg_restore，schema/Record/Field/Attachment 数量一致；附件归档独立恢复成功。私有备份位置与旧配置留在运维环境，不写入本仓。
- 回滚：原镜像及 Compose override 已保留，失败恢复流程已配置；发布前新增色数量为零。以后若新色已写入，长期降级前按 G1 §4.5 通过新 writer 修正颜色；不自动改写数据。当前读路径兼容，旧 writer 对含新色 options PATCH 会返回 422。
- 验证：专用 tmpfs PostgreSQL/私有网络/无 host ports；完整 go test -count=1 ./... 与 go vet ./... 通过，覆盖 HTTP required/format、全色往返、replay/key reuse、Option tombstone/restore、Field restore、Distinct/History 和混合色并发 CAS。完整九色轮播及第十项 wraparound 通过；测试资源清理完成。
- CI：[Go tests and vet](https://github.com/Mahjong404/LoomTable-Server/actions/runs/38260110660/job/114848131415)、[Docker/Compose](https://github.com/Mahjong404/LoomTable-Server/actions/runs/38260110660/job/114848130955) 均成功。两个审查轴未发现运行时/规范违例，三项验证缺口已补齐。
- 运行：内网和 HTTPS 的 /healthz、/readyz、/v1/meta 均 200；migrationRequired=false、bootstrapState=complete、attachments 保持启用。未在生产创建 QA Record。

Plugin 规划工作线收到固定提交后安排 api:sync → api:generate，审查 enum diff、Capability 评估/失效、旧新版本/未知色保值回归，再接入真实消费者。本 Server 工作线未修改 Plugin 代码、pin 或状态表，也未启用新增三色 UI。跨仓库状态仍由 Plugin 唯一状态表维护。
