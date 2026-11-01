# loom-oi 文档索引

本分支为 `alpha` 架构探索，基于内部聊天修订现有文档。**所有接口、对象命名、传输优先级和里程碑均未冻结；没有新增正式决议。**

| 文档 | 用途 |
| --- | --- |
| [分布式能力架构](distributed-architecture-conclusions.md) | 阅读当前边界、候选技术设计与验收建议 |
| [讨论蒸馏](alpha-discussion.md) | 追溯团队原意、分歧与 AI 推演，决定下一轮讨论方向 |
| [方案与 Issue 规划](alpha-issues.md) | 进入 #1–#9，D1–D6 与具体方案同单讨论 |
| [完整方案 v2](proposals/alpha-solution-design.md) | 8项推荐机制、备选、31个纸面场景与待决策取舍 |
| [统一 prompt](prompts/architecture-partner.md) / [skill](../.agents/skills/architecture-partner/SKILL.md) | 完整工作契约及项目执行入口 |
| [KB](../kb/README.md) | 来源、事实、决策与工作经验的持续沉淀 |
| [Issue 管理规范](issue-management.md) | 统一分类、评分、状态、来源与 AI 协作；含两份 alpha 模板入口 |

## 架构导航

| 主题 | 入口 |
| --- | --- |
| 目标、三方责任与总体图 | [职责](distributed-architecture-conclusions.md#2-职责与总体结构) |
| Node / Workload 争议与对象图 | [对象模型](distributed-architecture-conclusions.md#3-对象模型与命名) |
| 透明通道、协议适配、直连及中转 | [通信边界](distributed-architecture-conclusions.md#4-通信与适配边界) |
| 会话、连接、业务资源与隔离 | [状态归属](distributed-architecture-conclusions.md#5-状态与隔离) |
| 候选元数据与调用契约 | [契约](distributed-architecture-conclusions.md#6-候选契约) |
| 调用时序与失败语义 | [调用链路](distributed-architecture-conclusions.md#7-调用与故障处理) |
| 小机器、外部服务、虚拟手机 | [部署与场景](distributed-architecture-conclusions.md#8-部署与场景映射) |
| Loom 源码核对及集成图 | [集成边界](distributed-architecture-conclusions.md#9-与-loom-及已有服务的集成) |
| 实验与验收建议 | [验证](distributed-architecture-conclusions.md#10-验证顺序与验收建议) |
| 待决策问题与合并条件 | [决策清单](alpha-discussion.md#6-待决策清单) |

## 图示维护

现行四图使用架构正文中的 Mermaid 源码：总体职责、对象关系、调用时序、Loom 集成。图题分别说明哪些是共同方向、哪些是 AI 建议或特定路径示例。图中的组件拆分不代表已实现。

旧 PNG 中的 Session 管理、全局 Scheduler、联邦和 P2P 优先等容易被误读为团队决议，已从 `alpha` 移除；可在 [原始基线](https://github.com/wyw-ai/loom-oi/tree/63c1db9b22e608ec1e05ea246dc0698dfbb5f6f1/docs/assets/distributed-architecture) 查看。

本轮原始聊天含缺失媒体引用和与架构无关的内部信息，不整体提交。蒸馏文档保留必要的时间、说话人标签、行号和摘要，供持有原始附件者复核。

问题的实时状态在 GitHub Issues；本分支维护详细提案、证据与决策映射。默认分支尚未启用 alpha 模板，标签也未部署；分类/优先级/状态以 Issue 正文为准。
