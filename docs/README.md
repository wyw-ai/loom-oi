# loom-oi 文档索引

当前 alpha v0.7：按既有案例收束A/R/Go的明确推荐实现路线，包含协议、存储、恢复及分阶段验证。持续案例已迁入台账，活跃决策仅#3/#4/#8，#9关闭边界不变；无OIS/OIC产品实现/生产HA实测。

| 阅读顺序 | 入口 |
| --- | --- |
| 概念定位与职责 | [loom-oi](concept/loom-oi.md) / [OIC](concept/oic.md) / [OIS](concept/ois.md) |
| 新版架构与实现方案 | [alpha v0.7](architecture/alpha-v0.7.md) / [六张当前架构图](architecture/diagrams/README.md) |
| 结论与最新反馈 | [当前结论C01–C11](../kb/conclusions.md) / [ITER-002](evidence/alpha-feedback-002.md)；首轮推导见[ITER-001](evidence/alpha-synthesis-001.md) |
| 产品与节点/监测场景 | [持续案例台账](proposals/consumer-scenarios.md#7-持续案例台账) / [历史#10](https://github.com/wyw-ai/loom-oi/issues/10) |
| 隔离实验与原始证据 | [实验室](../experiments/README.md) / [首轮报告](evidence/alpha-lab-round1.md) |
| 模块详细设计（Review） | [OIC](modules/oic/README.md) / [OIS](modules/ois/README.md) / [消费接入](modules/client/README.md) |
| 讨论与规格变更 | [DISC-001](changes/DISC-001.md) / [SDD规范](sdd/README.md) |
| 每轮图文联动 | [产物映射](architecture-artifacts.md) |
| 历史锚点兼容入口 | [章节导航](distributed-architecture-conclusions.md)，完整正文统一维护于v0.7 |
| 需求/证据 → 完整方案 → 依赖影响 | [整体提案](proposals/alpha-solution-design.md) |
| 最新问题结构、回复跟进、旧单整合 | [Issue 规划](alpha-issues.md) / [历史总入口 #1](https://github.com/wyw-ai/loom-oi/issues/1) |
| 语言机制与部署功能的对应 | [Go/Rust 比较](proposals/oic-language-tradeoffs.md) |
| 原讨论、分歧和历史变化 | [讨论蒸馏](alpha-discussion.md) |
| 人类确认与证据 | [决策台账](../kb/decisions.md) / [证据台账](../kb/evidence.md) |
| 工作方法与反思 | [统一 prompt](prompts/architecture-partner.md) / [项目 skill](../.agents/skills/architecture-partner/SKILL.md) / [经验](../kb/lessons.md) |
| 分类、状态、量化与模板 | [Issue 管理规范](issue-management.md) |

活跃决策为#3产品与准入、#4信任与恢复、#8语言；另有[实验条件汇总 #11](https://github.com/wyw-ai/loom-oi/issues/11)跟踪LAB-003–006的就绪缺口。其他已收束单按用户授权归档；独立验收、故障、兼容和资源缺口在[验证归属](alpha-issues.md#remaining)继续记录。实时状态以GitHub为准。

本轮六张精确技术图用Graphviz专业矢量布局从DOT源确定连线，并逐张校对；旧ImageGen图已移出当前目录，见[v0.6的Git历史图册](https://github.com/wyw-ai/loom-oi/blob/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture/diagrams/README.md)。当前契约、图源、SVG与哈希一起保存；采用状态和实验结论独立记录。

alpha 模板尚未进入默认分支，标签未部署；分类和工作状态以 Issue 正文为准。GitHub open 不等于活跃的独立设计任务，关系字段会明确专项归属。
