# OI 改造技术评审

**状态：** Proposed  
**评审范围：** 受控端管理基础设施、OIC / OIS 架构及 Loom 集成方案  
**关联文档：** [Architecture](architecture.md)、[Loom Integration and Migration](loom-architecture-v2.md)

## 1. 评审摘要

本方案将 OI 定义为独立的受控端管理基础设施，由 OIS 和 OIC 两部分组成。

- **OIS（Loom OI Server）** 管理 OIC 接入、受控端目录、Capability、授权 Binding、配置策略和实际状态。
- **OIC（Loom OI Client）** 是受控端 SDK / CLI / Runtime，负责身份、连接、能力暴露、本地分发和扩展承载。
- **Extensions** 实现具体资源操作。
- **Integrations** 将 OI 接入 Loom 或其他 Agent / 管理平台。

Loom 是 OI 的首个集成平台。loom-server 继续管理 Agent、Actor、Channel、Thread、Message、Task、Run 及协作执行流程；OIS 管理受控资源；OIC 负责受控端接入和本地控制。

本方案以现有 Machine / daemon 机制为迁移基础，不要求推倒重建。核心改造是将受控端管理从 Agent Host 概念中解耦，并建立独立、稳定的 OI 协议和 SDK 边界。

## 2. 背景与问题

### 2.1 现有基础

Loom 当前已有 Machine 身份和配置、daemon 进程、daemon 与 loom-server 的连接、Machine Inventory、Machine Command、命令结果返回、配置刷新、Agent Worker 管理、Provider 探测、Service Host 和本机 IPC。

这些机制已经形成中心管理端与机器侧常驻进程之间的管理和控制链路，可作为 OIC / OIS 演进基础。

### 2.2 现有抽象的局限

当前 daemon 主要作为 machine-scoped Agent Host 使用，机器身份、连接、Agent 配置、Provider 探测、Worker Supervisor、Service Host 和 Machine Command 集中在同一运行体系中。

该抽象在以下场景中不够通用：

1. 不运行 Agent 的机器或应用也可能需要作为受控端。
2. 受控资源不局限于物理机器，还可能是桌面环境、应用、服务、运行基座或远程 HTTP 接口。
3. 资源能力、授权范围和实际可用状态需要统一表达。
4. Agent Runtime 生命周期与受控端连接、身份和扩展分发应当解耦。
5. 大型 Handler、Service 和命令文件需要按领域职责逐步拆分，以支持持续演进。

## 3. 设计目标

1. 支持机器、应用、进程和服务通过 OIC 接入 OIS。
2. 统一描述受控端及其 Capability。
3. 管理外部主体与受控资源之间的 Binding。
4. 支持配置下发、策略落实和 Desired / Observed State。
5. OIC 支持独立 CLI 运行及 SDK 嵌入。
6. 支持公网、NAT 和企业网络环境下的安全连接，必要时使用 Relay。
7. 通过 Extension Contract 开放具体业务能力。
8. OI 与 Agent 平台解耦，避免依赖 Loom 内部领域模型。
9. Loom 优先复用现有 Machine Connection 和命令通道，通过 Integration Adapter 接入 OI。

## 4. 非目标

OI 不负责 Agent 创建与生命周期、Channel / Thread / Message 管理、Task / Run 编排、Agent-to-Agent 协作、通用工作流调度，也不替代 Agent Runtime。

OI 不要求内置所有业务连接器。具体业务能力由 Extensions 提供；Agent 如何规划和组织调用由集成的 Agent 平台负责。

## 5. 目标架构

~~~text
       Loom / Other Agent or Management Platforms
                         |
                  Integration API
                         |
                  +------+------+
                  |     OIS     |
                  | Endpoint    |
                  | Capability  |
                  | Binding     |
                  | Policy      |
                  | Reconcile   |
                  +------+------+
                         |
                   OI Protocol
                         |
                  +------+------+
                  |     OIC     |
                  | Identity    |
                  | Connectivity|
                  | Dispatcher  |
                  | Enforcement |
                  | Extension   |
                  | Host        |
                  +------+------+
                         |
                     Extensions
~~~

OIS 和 OIC 属于独立仓库和协议边界。它们是否在部署时合并为同一进程，是部署选择，不改变代码所有权和模块边界。

## 6. 核心组件职责

### 6.1 OIS

- OIC 注册和身份记录。
- Endpoint 元数据、归属和生命周期。
- Capability 声明、版本和可用状态。
- 外部 Subject 与 Capability 的 Binding。
- 配置、授权策略和期望状态。
- 观察状态、配置应用结果和审计记录。
- 面向集成平台的管理 API 与资源视图。

OIS 不成为外部 Agent 平台的身份权威。Subject 使用 issuer-qualified reference，例如 issuer、type、id。

### 6.2 OIC

- OIC 身份、注册和认证。
- 与 OIS 建立安全连接。
- 连接协商、直接连接尝试和 Relay 回退。
- Capability 发布与状态上报。
- 接收授权请求并分发到 Extension。
- 本地权限和策略约束执行。
- Extension 加载、生命周期和健康状态管理。

OIC Core 不依赖 Agent Runtime，可以在不运行 Agent 的环境中独立工作。

### 6.3 Extensions

Extension 实现具体资源操作。建议契约包含 Manifest、Capability Declaration、Input / Output Schema、Permission Scope、Invocation Interface 和 Lifecycle。

候选扩展包括 Agent Runtime、Shell、Computer Use、HTTP、桌面应用自动化、Magpie 和 OpenConnector。高风险扩展应默认关闭或要求显式授权。

## 7. 关键架构决策及理由

### 7.1 OI 独立仓库

OI 的核心对象和协议不应依赖 Loom 的 Actor、Channel、Message 或数据库内部结构。独立仓库使 OIC SDK 可嵌入其他应用，也使 OIS 可以被其他 Agent 平台集成。

独立仓库不意味着必须立即独立部署。代码所有权、进程部署和服务拓扑是三个不同维度，应分别决策。

### 7.2 OIS 与 loom-server 的关系

Loom 是首个集成方。loom-server 通过 Loom Integration Adapter 使用 OIS API；OIS 只保存 Loom Agent 的外部身份引用和授权关系，不复制 Agent Registry。

OIS 可以在初期与 loom-server 同一部署环境运行，也可以独立部署。若采用嵌入式方式，应通过稳定接口、独立存储边界和依赖约束保持未来抽取能力。

### 7.3 daemon 演进为 OIC

现有 daemon 已具备机器身份、连接、Inventory、Machine Command、本地 IPC 和运行时管理能力。

迁移时应拆分：
- 通用受控端能力进入 OIC Core。
- AgentSpec、Provider Discovery、Agent Worker Supervisor 和 Agent Adapter 管理进入 Agent Runtime Host / Extension。
- ACP、Command、Interactive 等执行引擎继续作为可复用 Agent Runtime 库。
- Service Host 按实际资源职责决定是否作为 Extension。

过渡期保留 loom-daemon 命令和配置兼容，待迁移完成后再调整产品入口。

### 7.4 连接体系

Loom 已有 Machine Connection、WebSocket、Machine Command、通知和结果返回机制。Loom 集成初期应优先复用这些能力，避免同时运行两套连接体系。

独立 OIC 部署需要 OI 自身可用的连接能力，因此 OIC Connectivity 应具有平台无关接口，并提供独立传输实现。Loom 连接只是其中一种 Adapter。

NAT 穿透不能保证所有网络均直连成功，Relay 是必要回退机制。网络可达与授权必须分开判断。

### 7.5 Extension 生态

OIC Core 只提供身份、连接、扩展契约、调用分发和本地约束。业务逻辑由 Extension 实现。

OpenConnector 可作为可选 Extension 接入，复用其 Provider / Action 生态；它不替代 OIC 的受控端接入、连接和管理能力。

## 8. 与 Loom 现有代码的映射

| Loom 当前路径 | 当前职责 | 目标方向 |
|---|---|---|
| crates/server | Server、Journal、Handlers、Machine Commands | 领域模块化；增加 Loom Integration Adapter |
| crates/cli/src/cmd/daemon.rs | Machine 身份、Inventory、Command、Service Host、Agent Host 启动 | 拆分 OIC 适配与 Agent Runtime Host |
| crates/cli/src/cmd/agent_serve.rs | Agent Worker、Agent 连接和 Run 交互 | Agent Runtime Host |
| crates/cli/src/daemon_ipc.rs | daemon 本地 IPC / Proxy | OIC 本地管理接口或兼容层 |
| crates/agent-runtime | ACP、Command、Interactive、Provider、Discovery | 保留为 Agent 执行引擎 |
| crates/proto | RPC 方法和共享类型 | 保留 Loom 协议；增加 OI Integration DTO / Adapter |
| crates/loom-platform | OS、进程、文件、IPC、路径 | 保留为平台基础库 |
| crates/gui / apps/gui-web | 桌面和 Web GUI | 保留，增加受控资源管理界面 |

该映射是目标方向，不代表代码已经迁移。Agent、Channel、Thread、Message、Task 和 Run 的业务语义不属于本次 OI 改造范围；可以做结构性拆分，但不应顺带重设计业务模型。

## 9. 收益

### 产品收益

- 从管理 Agent Host 扩展到管理任意受控资源。
- OIC 可以部署在用户机器、服务器或嵌入用户应用。
- 不运行 Agent 的设备也能作为受控端。
- 业务能力可以通过社区 Extension 扩展。

### 架构收益

- 受控端管理与 Agent 执行解耦。
- OI 核心不依赖 Loom 内部领域模型。
- OIS 和 OIC 有独立协议和演进边界。
- Loom 可以复用现有连接作为首个 Adapter。
- 后续可以支持其他 Agent 平台。

### 工程收益

- daemon 不再承担所有业务能力。
- 新能力通过 Extension 增加，减少对 Core 的修改。
- Server 按领域组织代码，降低大文件和跨域耦合。
- 兼容迁移降低一次性重写风险。

## 10. 代价与风险

| 风险 | 影响 | 应对 |
|---|---|---|
| daemon 拆分复杂 | 影响现有 Agent Host | 分阶段抽取并保留兼容入口 |
| Machine / Node 模型差异 | 数据和协议迁移成本 | 先建立映射，不立即删除 Machine |
| OIS 与 Loom 身份集成 | 身份映射和授权边界 | 使用 issuer-qualified Subject Reference |
| OIC SDK 嵌入复杂 | 宿主生命周期、线程和权限差异 | 先稳定 Rust API，再扩展语言绑定 |
| NAT 穿透不保证成功 | 某些网络无法直连 | Relay 回退和企业 Gateway |
| Extension 权限风险 | Shell、桌面控制可能造成高影响操作 | 最小权限、默认关闭、执行时校验和审计 |
| 协议版本迁移 | 新旧客户端共存 | 协议版本化和兼容适配 |
| 过度模块化 | 增加 Crate 和维护成本 | 以稳定边界为依据，不机械拆分 |

## 11. 实施路线与验收

### P0：协议和边界
- 冻结 OIS / OIC 职责。
- 定义 OI Protocol、Subject Reference 和 Extension Contract。
- 定义 Loom Integration Adapter 的职责。
- 明确 Machine ID、配置和命令兼容策略。

### P1：最小 OIS
- 实现 Endpoint、Capability、Binding。
- 实现配置下发与 Desired / Observed State。
- 提供集成 API。
- 使用 Loom Adapter 复用现有 Machine Connection。

### P2：OIC Core
- 抽取身份、连接、Capability 发布、Dispatch 和本地 Enforcement。
- 实现一个低风险 Extension。
- 验证独立 OIC 不依赖 Agent Runtime。

### P3：daemon 迁移
- 将通用受控端职责迁入 OIC。
- 将 Agent Worker 管理迁入 Agent Runtime Host / Extension。
- 保留旧命令和配置兼容。
- 验证完整 Loom Host 与纯 OIC 两种部署模式。

### P4：扩展生态与其他集成
- 增加 Shell、Computer Use、HTTP 等扩展。
- 评估 OpenConnector 等外部生态的适配。
- 支持其他 Agent 平台接入。

### 验收标准

1. OIC 可独立注册到 OIS。
2. Extension 能发布 Capability。
3. OIS 能绑定外部 Subject 到指定 Capability。
4. OIS 能下发策略，OIC 能在本地执行授权校验。
5. OIS 能区分期望状态与实际状态。
6. 集成平台能查询特定 Agent 的可控资源视图。
7. 撤销 Binding 后，受控端拒绝后续未授权操作。
8. 现有 Loom Agent Runtime 在迁移期保持兼容。

## 12. 待详细设计事项

1. OIC 独立模式与 Loom Adapter 模式的连接协议。
2. OIS 的嵌入式和独立部署接口。
3. OIC 身份签发、轮换和撤销。
4. Node / Endpoint / Capability / Binding 的最终 Schema。
5. 配置版本、确认机制和离线重连后的状态对账。
6. Extension 进程隔离、权限模型和跨语言 SDK。
7. 高风险操作审批、审计和撤销语义。

## 13. 评审结论

OI 改造将 Loom 现有 Machine / daemon 管理机制升级为通用受控端基础设施。

- **OIS** 管理受控端、能力、授权、配置和状态。
- **OIC** 让机器、应用和服务接入 OIS，并将请求分发给扩展。
- **Extensions** 提供具体业务能力。
- **Integrations** 连接 OI 与 Loom 或其他平台。

Loom 是首个集成方，但 OI 的核心协议和实现不应依赖 Loom 内部领域模型。实施应先建立独立协议和模块边界，再渐进迁移现有 daemon 与 Machine 管理能力。
