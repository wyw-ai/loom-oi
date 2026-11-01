# loom-oi 文档索引

当前 alpha.4：#10为产品前置，#9按授权关闭；Go统一核心是待确认推荐。已有机制fixture与原始数据，无OIS/OIC产品实现/语言性能实测。

| 阅读顺序 | 入口 |
| --- | --- |
| 产品与节点/监测场景 | [前置分析](proposals/consumer-scenarios.md) / [#10](https://github.com/wyw-ai/loom-oi/issues/10) |\n| 隔离实验与原始证据 | [实验室](../experiments/README.md) / [首轮报告](evidence/alpha-lab-round1.md) |\n| 每轮图文联动 | [产物映射](architecture-artifacts.md) |\n| 当前边界与结构 | [架构正文](distributed-architecture-conclusions.md) |
| 需求/证据 → 完整方案 → 依赖影响 | [整体提案](proposals/alpha-solution-design.md) |
| 最新问题结构、回复跟进、旧单整合 | [Issue 规划](alpha-issues.md) / [总入口 #1](https://github.com/wyw-ai/loom-oi/issues/1) |
| 语言机制与部署功能的对应 | [Go/Rust 比较](proposals/oic-language-tradeoffs.md) |
| 原讨论、分歧和历史变化 | [讨论蒸馏](alpha-discussion.md) |
| 人类确认与证据 | [决策台账](../kb/decisions.md) / [证据台账](../kb/evidence.md) |
| 工作方法与反思 | [统一 prompt](prompts/architecture-partner.md) / [项目 skill](../.agents/skills/architecture-partner/SKILL.md) / [经验](../kb/lessons.md) |
| 分类、状态、量化与模板 | [Issue 管理规范](issue-management.md) |

主线 #3/#2/#4 承载端到端决策；#5/#6/#7 为条件专项，避免重复冻结。#8 讨论语言，#9已关闭且只记录边界；独立验收归#10/#3/#4。问题实时状态以 GitHub 为准。

图示直接以文档 Mermaid 源码维护，不把候选组件画成已确定模块。v4新增分层状态、响应丢失时序与候选部署图；v3已删除会让人误以为当前要做Loom bridge的图；旧图仍在 Git 历史。原聊天不整体提交，证据保留必要行号与原意。

alpha 模板尚未进入默认分支，标签未部署；分类和工作状态以 Issue 正文为准。GitHub open 不等于活跃的独立设计任务，关系字段会明确专项归属。
