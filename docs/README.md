# loom-oi 文档索引

当前 alpha v0.6：按既有案例收束A/R/Go的明确推荐实现路线，包含协议、存储、恢复及分阶段验证。持续案例已迁入台账，活跃决策仅#3/#4/#8，#9关闭边界不变；无OIS/OIC产品实现/生产HA实测。

| 阅读顺序 | 入口 |
| --- | --- |
| 新版架构与实现方案 | [alpha v0.6](architecture/alpha-v0.6.md) / [六张ImageGen架构图](architecture/diagrams/README.md) |
| 实验后综合结论 | [ITER-001](evidence/alpha-synthesis-001.md) / [claim索引](../kb/conclusions.md) |
| 产品与节点/监测场景 | [持续案例台账](proposals/consumer-scenarios.md#7-持续案例台账) / [历史#10](https://github.com/wyw-ai/loom-oi/issues/10) |
| 隔离实验与原始证据 | [实验室](../experiments/README.md) / [首轮报告](evidence/alpha-lab-round1.md) |
| 每轮图文联动 | [产物映射](architecture-artifacts.md) |
| 当前边界与结构 | [架构正文](distributed-architecture-conclusions.md) |
| 需求/证据 → 完整方案 → 依赖影响 | [整体提案](proposals/alpha-solution-design.md) |
| 最新问题结构、回复跟进、旧单整合 | [Issue 规划](alpha-issues.md) / [历史总入口 #1](https://github.com/wyw-ai/loom-oi/issues/1) |
| 语言机制与部署功能的对应 | [Go/Rust 比较](proposals/oic-language-tradeoffs.md) |
| 原讨论、分歧和历史变化 | [讨论蒸馏](alpha-discussion.md) |
| 人类确认与证据 | [决策台账](../kb/decisions.md) / [证据台账](../kb/evidence.md) |
| 工作方法与反思 | [统一 prompt](prompts/architecture-partner.md) / [项目 skill](../.agents/skills/architecture-partner/SKILL.md) / [经验](../kb/lessons.md) |
| 分类、状态、量化与模板 | [Issue 管理规范](issue-management.md) |

活跃决策为#3产品与准入、#4信任与恢复、#8语言；另有[实验条件汇总 #11](https://github.com/wyw-ai/loom-oi/issues/11)跟踪LAB-003–006的就绪缺口。其他已收束单按用户授权归档；独立验收、故障、兼容和资源缺口在[验证归属](alpha-issues.md#remaining)继续记录。实时状态以GitHub为准。

本轮六张架构图全部按用户要求使用ImageGen重新绘制；Mermaid只保留文字化结构参考，旧SVG不再作为当前图册输出。全部图示须核对语义，不把候选组件画成已确定模块。v4新增分层状态、响应丢失时序与候选部署图；v3已删除会让人误以为当前要做Loom bridge的图；旧图仍在Git历史。原聊天不整体提交，证据保留必要行号与原意。

alpha 模板尚未进入默认分支，标签未部署；分类和工作状态以 Issue 正文为准。GitHub open 不等于活跃的独立设计任务，关系字段会明确专项归属。
