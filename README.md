# loom-oi

**让控制方发现、连接并使用分散在不同环境中的受控能力。**

能力提供方拥有业务逻辑与状态；OI 关注登记、发现和受控连接，普通脚本、应用或 Agent 都能成为消费者。仓库目前是 `alpha` 架构探索，无 OIS/OIC 实现。

用户已明确：本阶段不接入 Loom，OI 不以 Loom 为必需依赖；未来由 Loom 端适配。Go/Rust 尚未选定，需要结合功能需求比较。其他产品边界、对象模型、路径和协议仍待人类取舍。

当前工作先从完整消费链推导职责与依赖，再组织 Issue。三条主线是对外能力边界（#3）、对象与生命周期（#2）、连接的可达/信任/失败（#4）；#5/#6/#7 保留为关联专项。

- [整体架构推导与候选方案](docs/proposals/alpha-solution-design.md)：来源、端到端闭环、完整备选、推导链与反例。
- [当前架构](docs/distributed-architecture-conclusions.md)：已确认边界和未确认设计。
- [Issue 规划](docs/alpha-issues.md) / [GitHub 总入口 #1](https://github.com/wyw-ai/loom-oi/issues/1)：已回复项跟进、旧单整合与讨论次序。
- [Go/Rust 功能与部署比较](docs/proposals/oic-language-tradeoffs.md)：官方机制、工程推导、同条件验证设计；无性能实测。
- [讨论蒸馏](docs/alpha-discussion.md)：原始意图与争议。
- [统一 prompt](docs/prompts/architecture-partner.md)、[项目 skill](.agents/skills/architecture-partner/SKILL.md)、[KB](kb/README.md)：脑暴、问题求解、证据和反馈持续联动。
- [管理规范](docs/issue-management.md) / [文档索引](docs/README.md)。

本轮只维护用户指定的 alpha，没有新增探索分支或合并 main。旧架构和旧规划保留在 Git 历史；整合 Issue 不代表关闭或接受设计。
