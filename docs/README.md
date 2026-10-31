# loom-oi 文档索引

## 分布式能力架构结论

[分布式能力架构与 Magpie 接入结论](distributed-architecture-conclusions.md) 收录《对比AI分布式架构与Magpie【备份】》所关联的正式合并文档，提供结论摘要、完整架构正文、协议示例、实施路线、最小版本验收场景及 4 张原架构图。

该文档保留源文件 Draft v0.2 的设计内容，已合并“续篇”衔接、删除重复的 Phase 3 开头，并恢复 JSON / YAML 示例的缩进。来源文件名和 SHA-256 记录在正文中。

## 既有文档及适用范围

| 文档 | 内容与状态 |
| --- | --- |
| [分布式能力架构与 Magpie 接入结论](distributed-architecture-conclusions.md) | 备份方案；以 Node / Workload / Capability / Lease / Session 描述分布式能力网络，包含调度、远程执行与联邦设计。 |
| [总体架构](architecture.md) | 既有 OIC/OIS 提议稿；聚焦受控端管理、扩展与平台集成。 |
| [OI 改造技术评审](oi-technical-review.md) | 既有 OIC/OIS 方案的工程评审、迁移风险与分阶段验收建议。 |
| [Loom 集成与迁移方案](integrations/loom.md) | 既有 OIC/OIS 方案与 loom-server、Machine、daemon 的职责映射及迁移方向。 |

这些文档描述的是设计方案，并非实现进度。新增的备份方案范围更广，不能与既有 OIC/OIS 方案视为同一版规格。

后续落地时，需要先明确 Node 与 Endpoint、Lease 与 Binding、Execution Session 与 Loom Task / Run 的关系，以及调度、事件路由和执行状态分别由哪个组件负责。本文档整理保留既有提议稿，供上述差异核对使用。
