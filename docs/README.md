# loom-oi 文档索引

[loom-oi 分布式能力架构](distributed-architecture-conclusions.md) 是当前架构设计基线。项目以 Node / Workload / Capability / Lease / Session 和 Communication Fabric 描述分布式能力的管理、发现、连接、授权与执行。

仓库目前处于设计阶段。正文中的目标、候选技术和验收标准用于指导实现；附录 C 中标记为待决策的细节仍需技术验证。

## 按主题阅读

| 主题 | 入口 |
| --- | --- |
| 项目定位与架构组成 | [项目定位](distributed-architecture-conclusions.md#1-项目定位)、[总体架构](distributed-architecture-conclusions.md#2-总体架构) |
| 对象与节点内部职责 | [核心对象模型](distributed-architecture-conclusions.md#3-核心对象模型)、[Node 内部架构](distributed-architecture-conclusions.md#4-node-内部架构) |
| 能力发现与调度 | [能力发现与调度](distributed-architecture-conclusions.md#5-能力发现与调度)、[Scheduler](distributed-architecture-conclusions.md#10-scheduler分布式能力调度) |
| 跨网络通信与协议 | [Communication Fabric](distributed-architecture-conclusions.md#6-communication-fabric分布式通信基础设施)、[协议体系与兼容层](distributed-architecture-conclusions.md#7-协议体系与兼容层) |
| 身份、授权与凭证边界 | [身份、安全与权限体系](distributed-architecture-conclusions.md#8-身份安全与权限体系) |
| Magpie 接入与分工 | [Magpie 接入架构](distributed-architecture-conclusions.md#11-magpie-接入架构) |
| 使用场景与故障恢复 | [典型分布式场景](distributed-architecture-conclusions.md#12-典型分布式场景)、[可靠性与故障恢复](distributed-architecture-conclusions.md#14-可靠性与故障恢复) |
| 建设顺序与验证 | [实施路线](distributed-architecture-conclusions.md#17-实施路线)、[关键风险与技术验证](distributed-architecture-conclusions.md#20-关键风险与技术验证) |
| 核心决策与首版范围 | [架构决策](distributed-architecture-conclusions.md#附录-c核心架构决策architecture-decision-records)、[v0.1 最小可行架构](distributed-architecture-conclusions.md#附录-ev01-最小可行架构) |

## 架构图

- [总体架构](assets/distributed-architecture/overall-architecture.png)
- [核心对象模型](assets/distributed-architecture/core-object-model.png)
- [能力发现与分布式执行](assets/distributed-architecture/discovery-and-execution.png)
- [Magpie 接入架构](assets/distributed-architecture/magpie-integration.png)

所有后续设计文档以这套分布式能力架构为准；模型或职责变化应同步更新架构正文、索引与相关图示。
