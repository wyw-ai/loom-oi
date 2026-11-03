# 文档导航

本页按阅读任务导航，不复制设计或状态。返回[项目入口](../README.md)；需要找具体文件时查看[完整目录](catalog.md)。

## 理解系统

| 内容 | 入口 | 维护范围 |
| --- | --- | --- |
| 概念 | [loom-oi](concept/loom-oi.md) / [OIC](concept/oic.md) / [OIS](concept/ois.md) | 项目主体、价值与共同承诺；组件职责由此推导 |
| 当前架构 | [架构v0.7](architecture/alpha-v0.7.md) / [图册](architecture/diagrams/README.md) | 共同对象、运行路径、部署与失败契约 |
| 模块 | [OIC](modules/oic/README.md) / [OIS](modules/ois/README.md) / [客户端](modules/client/README.md) | 模块责任和上游契约定位 |
| 应用 | [应用路径](designs/application-paths.md) / [持续案例](proposals/consumer-scenarios.md#7-持续案例台账) | 场景的具体部署、输入结果与反例 |

## 评审与追溯

| 内容 | 入口 | 维护范围 |
| --- | --- | --- |
| 当前问题 | [Issue导航](alpha-issues.md) | 独立确认目标、真正依赖与验证归属 |
| 阶段经验 | [血缘索引](../kb/lineage.md) / [原文快照](issue-history/README.md) | 前序、证据、停止/重启条件与历史缺口 |
| 讨论变更 | [变更索引](changes/README.md) | 来源、需求、差异、任务/验证与确认范围 |
| 人类决定 | [决议索引](decisions/README.md) / [工作授权与决定台账](../kb/decisions.md) | 原回复、准确采用范围与后果 |
| 证据 | [KB](../kb/README.md) / [报告索引](evidence/README.md) / [实验室](../experiments/README.md) | 原始来源、窄结论及可复现记录 |
| 通道主问题 | [端到端通道](designs/ois-control-relay.md) | 路径策略→实际负载/故障→部署；完整取舍与验证 |
| 候选分析 | [提案索引](proposals/README.md) | 当前专题分析与历史推导，角色明确分开 |

## 维护规则

[Issue规范](issue-management.md)定义六节正文；[结构规范](structure.md)定义内容归位和引用；[SDD](sdd/README.md)定义确认/实施链；[产物联动](architecture-artifacts.md)定义跨产物更新和绘图规则。执行从[AGENTS](../AGENTS.md)进入，不从历史报告提取当前任务权限。
