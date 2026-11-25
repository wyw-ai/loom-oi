# 创造者与目标 Host Skill 的封板记录

2026-11-24，用户确认按两仓复述的目标和配置封板，本轮讨论结束。OI 基线为 `codex/service-goals-beta@212b970`，需求维护在 `codex/creator-managed-workspaces` 分支。

- 管理 App 收敛为三项 Host 能力查询和四项 Service 管理操作。创造者获授管理 Service 后可以把固定来源发布给业务 Agent。Host 接入和 Channel 配置沿用部署流程。
- Skill 随文件所在的 Host 提供。包的发现和读取分别通过 `list_skills`、`read_skill` 暴露，消费者先按简介选择，再打开包及其材料。过滤规则和同名包识别已纳入验收。
- OI 已有的 Service beta 负责发布名、固定来源、访问策略、REST 和 MCP 入口。本期跨仓验收走 MCP。自动同步 Toolset、模型命名、Agent 授权和完整 WebUI 的目标由 harness 仓库维护。
- 封板覆盖两款 App 的行为、配置归属、权限及验收。具体嵌套 YAML 字段名在实现时确定。两款 App 开发及与 harness 联验待开展。

[封板设计](design.md) · [当前共识](../../../current/creator-workspace/design.md) · [需求与验收](../../../../spec/goals/creator-workspace.md)
