# loom-oi

**loom-oi 是一套用于管理和连接受控端的基础设施。**

它提供统一的机制，用于接入 OIC 客户端、暴露能力、管理授权关系、下发配置，并观察跨网络受控端的实际状态。

## 核心组成

- **OIS（Loom OI Server）：** OIC 客户端及其所暴露资源的管理控制面。
- **OIC（Loom OI Client）：** 部署在受控侧的 SDK / CLI / Runtime，负责连接 OIS、发布能力、执行本地策略，并将请求分发给扩展。
- **Extensions（扩展）：** 面向具体资源的能力实现，例如 Shell 执行、桌面自动化、应用自动化、HTTP 连接器和运行时托管。
- **Integrations（集成适配器）：** 将 OI 接入外部平台、身份体系和传输机制。

## 职责边界

| 组件 | 职责 |
|---|---|
| OIS | 受控端接入、能力目录、授权关系、期望配置、策略和实际状态 |
| OIC | 客户端身份、安全连接、能力暴露、本地分发、扩展生命周期和本地权限执行 |
| Extensions | 对机器、应用、运行时和服务执行具体操作 |
| Integrations | 适配外部身份、API 和传输体系，包括 Loom |

Loom 是 OI 的首个集成平台：

- **loom-server** 继续负责 Agent、Actor、Channel、Thread、Message、Task、Run 以及协作和执行流程。
- Loom 集成层负责将 Loom 主体身份和现有传输机制映射到 OIS / OIC 的模型。
- 现有 Loom daemon 的部分能力可以逐步迁移到 OIC Core 和官方 Agent Runtime Extension。

## 设计原则

1. 保持 OIC Core 精简，将资源相关的具体业务逻辑放入扩展。
2. OIS 不依赖某一个 Agent 平台或协作模型。
3. 使用明确的能力契约和授权绑定关系。
4. 区分期望配置与实际应用状态。
5. 优先由 OIC 主动建立连接；条件允许时尝试直连，无法直连时使用中继。
6. 网络可达不等于拥有操作权限。
7. OIC 既可以独立运行，也可以嵌入宿主应用。

## 文档

- [总体架构](docs/architecture.md)
- [OI 改造技术评审](docs/oi-technical-review.md)
- [Loom 集成与迁移方案](docs/loom-architecture-v2.md)
