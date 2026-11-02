# loom-oi

**让控制方发现、连接并使用分散在不同环境中的受控能力。**

能力提供方拥有业务逻辑与状态；OI负责登记、发现和受控连接，普通脚本、应用或Agent都能成为消费者。当前在已有`alpha`分支进行架构探索，尚无OIS/OIC产品实现；仓库已有局部机制实验与原始数据。

## 从这里开始

1. [当前架构与实现方案 v0.7](docs/architecture/alpha-v0.7.md)：端到端路径、职责、协议、存储、恢复及实施阶段。
2. [当前六张架构图](docs/architecture/diagrams/README.md)：总体、建连、准入、恢复、部署与实施依赖。
3. [Issue规划与剩余决策](docs/alpha-issues.md)：#3产品与准入、#4信任与恢复、#8语言；[#11](https://github.com/wyw-ai/loom-oi/issues/11)跟踪实验条件和阶段门槛。
4. [技术结论C01–C11](kb/conclusions.md) / [最新反馈分析ITER-002](docs/evidence/alpha-feedback-002.md)：证据、推论与人类采用分别记录。

已确认本阶段不接入Loom，由业务方管理功能实例；R中继路线及默认推荐PostgreSQL、支持显式SQLite部署的要求已承接。A的消费/登记细节、K2准入、Go选型与具体存储/恢复契约仍按各自决策状态评审。阶段门槛尚未通过，局部实验不能证明产品或生产HA。

## 文档分层与本轮确认

[概念：OI](docs/concept/loom-oi.md) / [OIC](docs/concept/oic.md) / [OIS](docs/concept/ois.md) → [顶层架构](docs/architecture/alpha-v0.7.md) → [OIC模块](docs/modules/oic/README.md) / [OIS模块](docs/modules/ois/README.md) / [消费接入](docs/modules/client/README.md)。模块页为Review草案，公共协议仍由顶层架构统一维护。

[DISC-001讨论融合](docs/changes/DISC-001.md)补入Mac/ECS工具消费、设备/Pod案例和外置适配参考契约；[SDD规范](docs/sdd/README.md)要求每轮实际提交相关Issue供开发者确认。方案与实验采用状态分别记录。

## 主目录

| 目录 / 入口 | 用途 |
| --- | --- |
| [docs/](docs/README.md) | 文档总索引、当前架构、案例、候选方案和Issue规范 |
| [kb/](kb/README.md) | 证据、技术结论、人类决策和协作经验 |
| [experiments/](experiments/README.md) | 隔离实验、原始数据、复现工具和[G0–G7门槛](experiments/GATES.md) |
| [AGENTS.md](AGENTS.md) | 仓库协作入口；连接项目skill、[统一prompt](docs/prompts/architecture-partner.md)与产物联动规则 |

方案推导见[完整候选比较](docs/proposals/alpha-solution-design.md)、[持续案例台账](docs/proposals/consumer-scenarios.md)及[Go/Rust比较](docs/proposals/oic-language-tradeoffs.md)；原始意图见[讨论蒸馏](docs/alpha-discussion.md)。

已替代的完整架构和图册从当前目录移除，由Git保存历史；[v0.6历史快照](https://github.com/wyw-ai/loom-oi/tree/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture)可追溯。仍用于结论解释和实验复现的证据、原始数据与源码版本继续保留。当前工作分支为`alpha`，未合并`main`。
