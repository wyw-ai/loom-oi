# loom-oi

**让控制方发现、连接并使用分散在不同环境中的受控能力。**

能力提供方拥有业务逻辑与状态；OI 关注登记、发现和受控连接，普通脚本、应用或 Agent 都能成为消费者。仓库目前是 `alpha` 架构探索，无 OIS/OIC 产品实现；已有局部机制实验代码与原始数据。

用户已明确：本阶段不接入 Loom，OI 不以 Loom 为必需依赖；未来由 Loom 端适配。当前AI推荐Go作为统一核心，Go/Rust采用尚未由人类确定；质量目标与翻转条件已展开。其他产品边界、对象模型、路径和协议仍待人类取舍。

当前已按完整路径收束[架构与实现方案 v0.6](docs/architecture/alpha-v0.6.md)：A/R/Go建议基线，补齐协议、授权、观测、存储与恢复；[六张全量ImageGen架构图](docs/architecture/diagrams/README.md)可独立查看。案例持续纳入台账，按当前材料设计；活跃决策由#3产品与准入、#4信任与恢复、#8语言承接；另按用户要求新增[实验条件汇总 #11](https://github.com/wyw-ai/loom-oi/issues/11)，跟踪LAB-003–006的解阻。其他已收束条目按授权归档。

- [当前综合结论C01–C06](docs/evidence/alpha-synthesis-001.md)：从实验与KB收敛状态/失败契约，并明确未决取舍。
- [产品与多维节点场景](docs/proposals/consumer-scenarios.md)：消费者、探针、监控和八类能力节点的条件。
- [隔离实验室](experiments/README.md) / [证据报告](docs/evidence/alpha-lab-round1.md)：复现、原始数据、独立复核与限制。
- [整体架构推导与候选方案](docs/proposals/alpha-solution-design.md)：来源、端到端闭环、完整备选、推导链与反例。
- [当前架构](docs/distributed-architecture-conclusions.md)：已确认边界和未确认设计。
- [Issue 规划](docs/alpha-issues.md) / [历史总入口 #1](https://github.com/wyw-ai/loom-oi/issues/1)：已回复项跟进、旧单整合与讨论次序。
- [Go/Rust 功能与部署比较](docs/proposals/oic-language-tradeoffs.md)：官方机制、工程推导、同条件验证设计；无性能实测。
- [讨论蒸馏](docs/alpha-discussion.md)：原始意图与争议。
- [统一 prompt](docs/prompts/architecture-partner.md)、[项目 skill](.agents/skills/architecture-partner/SKILL.md)、[KB](kb/README.md)：脑暴、问题求解、证据和反馈持续联动。
- [管理规范](docs/issue-management.md) / [文档索引](docs/README.md)。

本轮只维护用户指定的 alpha，没有新增探索分支或合并 main。旧架构和旧规划保留在 Git 历史；#1/#2/#5/#6/#7/#10已按最新授权归档，#9保持原关闭状态；其他方案没有因此获批。
