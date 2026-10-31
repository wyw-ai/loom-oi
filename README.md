# loom-oi

**loom-oi 是面向异构 AI 能力的分布式基础设施。**

它通过统一的 Node、Workload、Capability、Lease 和 Session 对象模型，以及跨网络的 Communication Fabric，让分散在不同机器、网络和运行环境中的模型、Agent Runtime、工具及服务能够被发现、连接、授权和调用。

当前仓库处于架构设计阶段，以下内容描述目标能力，不代表已经实现。

## 架构组成

| 组成 | 职责 |
| --- | --- |
| Control Plane | 节点、工作负载和能力目录；身份、策略、授权、调度及全局状态视图 |
| Communication Fabric | 控制、执行、流、数据、事件及 Peer 通信；连接协商、直连和 Relay 回退 |
| Distributed Nodes | 承载或接入 Workload，暴露 Capability，执行本地策略并提供实际能力 |

Node Agent 负责节点侧管理和通信；Agent Runtime 负责自身的推理循环、上下文管理与工具选择。已有远程服务可以通过 Connector 接入，不要求所有 Node 都运行 Node Agent。

## 核心对象

| 对象 | 定义 |
| --- | --- |
| Node | 网络和资源归属单位，可代表机器、运行环境或独立服务实例 |
| Workload | 部署和运行单位；可由 loom-oi 管理，也可由外部系统管理 |
| Capability Definition / Instance | 能力契约及其实际可用实例，是发现和调用的对象 |
| Capability Lease | 能力使用权的临时授权及资源占用约束 |
| Execution Session | 一次执行过程及其状态、事件、产物和恢复信息 |
| Federation | 独立能力网络之间的跨域信任与选择性能力共享 |

## Magpie 接入

Magpie 作为 Gateway Workload 接入，可以由 loom-oi 管理生命周期，也可以通过 Connector 接入已有实例。

- Magpie 负责 Provider 管理、模型路由和模型调用。
- loom-oi 负责发现与选择 Magpie 实例、调用授权以及跨节点连接。
- 模型目录和功能以实际接入接口为准，Provider 凭证及配额保留在能力提供方。

## 设计原则

1. Node 与 Workload 分离，统一能力契约，通过 Adapter 兼容异构实现。
2. 控制面与业务数据面分离，避免所有流量经过中心转发。
3. P2P 优先，无法直连时使用 Relay 或 Gateway。
4. 网络可达与能力授权分别校验，中心授权必须同时满足本地策略。
5. 委托能力，不自动转交底层凭证。
6. 通过事件日志、幂等与检查点支持可追踪的执行；恢复能力以具体 Runtime 为准。
7. 分阶段验证基础链路，再扩展调度、能力组合和跨组织联邦。

## 最小版本目标

打通云端 Agent 发现并调用 Mac mini 上 Magpie、Claude Code、Computer Use 的完整链路：注册 Node / Workload / Capability，申请 Lease，建立连接，创建 Session，返回事件和 Artifact，最后释放 Lease。

完整多组织联邦、自动能力组合、全局优化调度和通用分布式文件系统不属于首个版本的必需范围。

## 文档

- [分布式能力架构](docs/distributed-architecture-conclusions.md)：当前架构设计基线，包含 21 章正文、6 个附录和 4 张架构图。
- [文档索引](docs/README.md)：按主题查阅对象模型、通信、Magpie 接入、实施路线和验收标准。
