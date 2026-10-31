# OI 改造技术评审：从 Machine 管理到 OIC / OIS

**状态：** Proposed  
**评审类型：** Architecture / Technical Review  
**关联文档：** [loom-oi Architecture](architecture.md)、[Loom Architecture V2](loom-architecture-v2.md)

## 1. 评审摘要

本次 OI 改造的目标，是把 Loom 现有围绕 Machine、daemon、Machine Inventory 和 Machine Command 建立的受控端管理能力，升级为一套边界清晰、可扩展、可独立复用的受控资源管理机制。

改造不是增加第二套 Agent 平台，也不是把所有外部工具、模型和服务统一重写。核心是将“受控方”从原有 Agent Host 概念中抽离，使任意位置的机器、应用、进程或服务，都可以通过 OIC 接入 OIS，声明能够提供的能力，并在授权范围内接受控制。

方案由两个部分组成：

- **OIS（Loom OI Server）：** 受控资源管理面，第一阶段作为 loom-server 内部的独立领域模块实现。
- **OIC（Loom OI Client）：** 受控端 SDK / CLI / Runtime，由现有 loom-daemon 演进而来，负责身份、连接、能力暴露、本地分发和扩展承载。

职责边界：

- loom-server 继续管理 Agent、Actor、Channel、Thread、Message、Task、Run、协作关系和现有执行流程。
- OIS 管理受控端注册、能力声明、Binding、配置、策略和实际状态。
- OIC 负责接入 OIS、暴露受控端能力，并将授权请求分发给本地 Extension。
- Extension 实现具体业务能力，允许官方和社区扩展。

**核心判断：现有 daemon 与 Machine 管理体系是 OIC / OIS 的演进基础，不需要推倒重建；需要做的是职责拆分、领域升级和协议收敛。**

## 2. 背景与现状

### 2.1 现有基础

当前 Loom 已有 Machine 身份和本机配置、daemon 进程、daemon 与 loom-server 的连接、Machine Inventory 上报、Machine Command 下发与结果返回、daemon 配置刷新、Agent Worker 管理、Provider 探测、Service Host 和本机 IPC。

这些能力已经形成初步的“中心管理端—机器侧常驻进程”模型，是 OI 改造的直接基础。

### 2.2 现有抽象的局限

现有 daemon 主要定位为 machine-scoped Agent Host，同时承载机器身份、连接、Agent 配置、Provider 探测、Worker Supervisor、Service Host 和 Machine Command 等职责。随着受控对象扩展，出现以下局限：

1. **受控对象被 Agent Host 概念限制。** 不运行 Agent 的机器仍可能需要提供 Bash、Computer Use、HTTP、桌面应用自动化等能力。
2. **资源能力缺少统一表达。** Machine Inventory 更偏向机器上已有 Agent、Provider 和 Service 的清单，尚不足以表达任意受控资源的能力契约、操作范围和可用状态。
3. **授权关系不够独立。** “某个 Agent 可以控制某个资源的哪些操作”应成为明确管理对象。
4. **daemon 职责过于集中。** Agent Worker 生命周期与受控端基础能力耦合，轻量受控端难以独立使用。
5. **代码边界不利于持续演进。** 部分文件同时承载协议处理、领域规则、运行时管理和基础设施逻辑，增加维护成本。

这些问题不意味着现有机制错误，而是说明抽象需要从“机器上的 Agent Host”演进为“通用受控端”。

## 3. 设计目标与非目标

### 3.1 设计目标

1. 任意位置的机器或应用可以通过 OIC 接入并成为受控端。
2. 受控端可以声明自己提供的 Capability。
3. 管理端可以配置受控端、启停能力并观察实际状态。
4. 管理端可以定义 Agent 与受控资源之间的 Binding 和操作范围。
5. 受控端执行本地授权校验，并将请求分发给对应 Extension。
6. OIC 可以作为 CLI 独立运行，也可以作为 SDK 嵌入用户自己的应用。
7. OIC 与 OIS 能在 NAT、企业网络和公网环境下建立安全通信；必要时使用 Relay。
8. 复用 Loom 现有连接、身份引用、命令路由和执行体系，避免重复建设。
9. 允许社区围绕稳定 Extension Contract 扩展业务能力。

### 3.2 非目标

本次不建设第二套 Agent Registry、Channel、Thread、Message、Task、Run、Agent Session 或通用工作流编排平台；不建设与 loom-server 并行的 Actor 连接和协作网络；不要求 OIS 内置所有 SaaS、桌面和业务系统能力。

业务操作的具体实现属于 Extension；Agent 如何规划和组织操作，仍属于 loom-server 及 Agent Runtime。

## 4. 目标架构与终态

~~~text
                    loom-gui / loom-cli
                            |
                            v
                       loom-server
              +-------------+--------------+
              |                            |
        Agent & Collaboration          OIS Module
        Actor / Channel / Thread       Node Manager
        Message / Task / Run           Capability Manager
        Agent Execution                Binding Manager
              |                         Config / Policy
              +-------------+--------------+
                            |
                Existing Loom Connection
                  / Machine Command Path
                            |
                            v
                           OIC
              +-------------+--------------+
              |                            |
           OIC Core                    Extensions
       Identity / Connectivity       Agent Runtime
       Capability Exposure           Shell / Bash
       Dispatch / Policy              Computer Use
       Lifecycle                      HTTP / Magpie
                                      Community Apps
~~~

该图表达逻辑职责，不要求所有组件必须独立进程部署。第一阶段 OIS 与 loom-server 同进程或同一服务部署；OIC 可以与现有 daemon 同进程演进，也可以作为独立 CLI / SDK 运行。

### 4.1 OIS：受控资源管理面

OIS 第一阶段作为 loom-server 内部独立领域模块，负责 OIC 注册、身份引用和受控端目录、Node / Endpoint 元数据与生命周期、Capability 声明和启用状态、Agent 与资源 Binding、资源配置和授权策略、Desired / Observed State 对账，以及向 loom-server、GUI 和 CLI 提供受控资源视图。

OIS 不拥有 Agent 身份。Agent 身份由 loom-server 管理，OIS 只保存必要的主体引用和 Binding。

### 4.2 OIC：受控端 SDK / CLI / Runtime

OIC 是 daemon 的演进方向，负责身份注册、安全连接、连接协商、NAT 穿透和 Relay 回退、Capability 暴露、本地请求分发、本地策略约束及 Extension 生命周期。

OIC Core 不依赖 Agent Runtime，必须能够在没有任何 Agent 的情况下独立运行。

### 4.3 Extension：具体业务能力

Extension 是能力实现单位。首批可考虑：

- Agent Runtime Extension：承接原 daemon 中 AgentSpec、Provider Discovery、Agent Worker Supervisor 和相关 Adapter 的管理能力。
- Shell Extension：Bash、PowerShell 等命令执行。
- Computer Use Extension：截图、鼠标、键盘和桌面操作。
- HTTP Extension：访问远程 HTTP 服务。
- Application Automation：微信、QQ 等本地应用控制。
- Magpie / OpenConnector Extension：接入现有模型网关或 SaaS Connector 生态。

第一阶段可以通过 Rust Crate 静态集成官方 Extension；不急于引入复杂动态插件 ABI。

## 5. 关键架构思考与方案演进

### 5.1 为什么不是继续扩展 Machine / daemon？

Machine / daemon 原本以一台机器上的 Agent Host 为主要管理对象。继续在这一模型中增加 Computer Use、Shell、微信、HTTP 等能力，会让 daemon 逐渐成为包含所有业务逻辑的巨大常驻进程。

更合适的抽象是：受控端负责接入和暴露能力，具体能力由扩展实现。Machine 可以是 Node 的一种部署载体，但 Node 不应被限制为物理机器，也不应要求必须运行 Agent。

### 5.2 为什么拆成 OIC 和 OIS？

OIC 位于受控侧，掌握本地资源、扩展和执行环境；OIS 位于管理侧，掌握受控端目录、能力、授权、期望配置和观察状态。

这种分离允许 OIC 部署在用户机器、服务器、边缘环境，甚至嵌入用户自己的应用；OIS 则统一管理这些受控端。

### 5.3 为什么 OIS 第一阶段不独立部署？

当前 OIS 的主要消费者是 loom-server，Agent 身份也由 loom-server 管理。若一开始拆成独立服务，会引入额外的身份同步、服务认证、状态一致性和部署维护成本。

因此第一阶段将 OIS 实现为 loom-server 内部独立领域模块：逻辑边界清晰，部署暂时合并。未来当第三方系统需要独立使用、规模要求独立扩缩容或安全边界要求隔离时，再抽取为独立服务。

### 5.4 为什么 OIC 不直接等于原 daemon？

原 daemon 同时承担受控端基础设施和 Agent Runtime Host 两类职责。

OIC Core 承载身份、连接、能力暴露、本地分发、策略执行和 Extension 生命周期；原 Agent Worker、AgentSpec、Provider Discovery 等属于 Agent Runtime Extension。

这样用户可以只运行 OIC，不运行 Agent；完整 Loom Host 则可以同时运行 OIC 和 Agent Runtime Extension。

### 5.5 为什么不重建连接体系？

loom-server 已有 Machine Connection、WebSocket、Machine Command、通知和结果返回机制。OIS 不应再建设一套平行的 Agent / Channel 连接系统。

第一阶段优先复用现有连接和命令路径。OIC 独立 SDK 部署模式需要的连接能力，应在 OIC Connectivity 中抽象，但与 Loom 现有连接适配保持边界清晰。P2P、NAT 穿透和 Relay 是 OIC 连接能力的演进方向，不意味着 OIS 接管 loom-server 的 Actor / Channel 路由。

### 5.6 为什么业务逻辑交给社区 Extension？

OIC Core 的长期价值在于稳定的受控端身份、连接和扩展契约，而不是内置每一种应用自动化。

Shell、Computer Use、微信、QQ、Magpie、OpenConnector 等能力实现差异很大，适合由独立 Extension 维护。OIC 只要求它们遵循统一的 Manifest、Capability Declaration、Invocation 和 Lifecycle Contract。

OpenConnector 可作为可选 Extension 接入，复用其 Provider / Action 生态；不应替代 OIC Core 的受控端连接和管理职责。


## 6. 现有代码迁移映射

| 当前代码 | 现有职责 | 目标归属 |
|---|---|---|
| `crates/server` | Server、Journal、Handlers、Machine Commands | 模块化 Server；增加 OIS |
| `crates/cli/src/cmd/daemon.rs` | Machine 身份、Inventory、Command、Service Host、Agent Host 启动 | 拆分 OIC Core 与 Agent Runtime Host |
| `crates/cli/src/cmd/agent_serve.rs` | Agent Worker、Agent 连接和 Run 交互 | Agent Runtime Extension |
| `crates/cli/src/daemon_ipc.rs` | daemon 本地 IPC / Proxy | OIC 本地管理接口或兼容层 |
| `crates/agent-runtime` | ACP、Command、Interactive、Provider、Discovery | 保留为 Agent 执行引擎 |
| `crates/proto` | RPC 方法和共享类型 | 整理为 protocol，增加 OI 类型 |
| `crates/loom-platform` | OS、进程、文件、IPC、路径 | 保留为 platform 基础能力 |
| `crates/gui` / `apps/gui-web` | 桌面和 Web GUI | 保留，增加受控资源管理界面 |

迁移原则：

- 不因目录改造一次性移动所有代码。
- 先建立新模块边界，再按功能迁移。
- 保留旧 Machine ID、配置和命令接口的兼容路径。
- 只有新旧行为经过测试验证后，才移除旧实现。
- 原 daemon 命令在过渡期保留为 OIC Runtime 的兼容入口，最终产品命名统一为 OIC。

## 7. 关键交互流程

### 7.1 OIC 注册与能力上报

1. OIC 启动并向 OIS 完成身份认证和注册。
2. OIC 加载已启用的 Extensions。
3. Extensions 声明 Capability、Schema 和权限需求。
4. OIC 上报能力清单和运行状态。
5. OIS 保存受控端及能力视图。
6. OIS 持续跟踪连接状态和能力实际状态。

### 7.2 Agent 与资源建立 Binding

1. 用户从 loom-gui / loom-cli 选择 loom-server 中已有的 Agent。
2. loom-server 将 Agent 身份引用提供给 OIS。
3. 用户选择目标 Node 和允许的 Capability。
4. OIS 保存 Binding 和期望策略。
5. OIS 通过现有连接机制向 OIC 下发配置。
6. OIC 在本地落实限制并回报 Observed State。
7. loom-server 获取该 Agent 的有效可控资源视图。

### 7.3 Agent 使用受控资源

1. Agent 从 loom-server 获取自身可控资源视图。
2. Agent 选择目标 Capability 并发起操作。
3. loom-server 通过现有执行和连接体系路由请求。
4. OIC 再次校验当前授权和本地限制。
5. OIC 将请求分发给对应 Extension。
6. Extension 执行实际业务操作。
7. 结果返回 loom-server，由其继续处理 Agent、Message、Run 等业务状态。

权限不能只在能力发现时过滤。受控端每次实际操作仍需校验当前授权，确保 Binding 撤销后，缓存的旧能力信息不能继续产生未授权操作。

## 8. 收益与价值

### 产品收益

- 从“管理运行 Agent 的机器”扩展为“管理任意可控资源”。
- 允许用户在任意位置部署 OIC，不要求部署完整 Loom Agent Runtime。
- 用户自己的程序可以通过 SDK 成为受控端。
- Shell、Computer Use、应用自动化、HTTP 服务等可统一纳管。
- 形成社区 Extension 生态，避免核心项目承担全部业务集成。

### 架构收益

- Agent Runtime 与受控端基础设施解耦。
- OIS 与 loom-server 领域边界清晰，但第一阶段不增加独立服务运维成本。
- 复用现有 Machine Connection 和命令路由，减少重复建设。
- 受控资源的身份、能力、授权和状态有统一管理模型。
- 未来可以独立复用 OIC SDK，也可以在需要时抽取 OIS。

### 工程收益

- 降低 daemon 单体文件和职责耦合。
- 新增业务能力优先通过 Extension 实现，不再修改 daemon 主循环。
- Server 新功能按领域模块开发，减少跨模块直接访问。
- 新代码遵循统一依赖方向、接口和测试约定。
- 历史代码可以渐进迁移，避免大爆炸式重写。

## 9. 代价、风险与应对

| 风险 / 代价 | 影响 | 应对 |
|---|---|---|
| OIC 与现有 daemon 迁移复杂 | 可能影响现有 Agent Host | 分阶段抽取，保留兼容入口和配置 |
| Machine 与 Node 模型不完全一致 | 数据和协议迁移 | 先做映射，不立即删除 Machine |
| OIS 与 loom-server 模块耦合 | 未来独立抽取困难 | 明确领域 API、Repository 和依赖边界 |
| OIC SDK 嵌入场景复杂 | 生命周期、线程、网络和权限差异 | 先稳定 Rust Core API，再扩展语言绑定 |
| NAT 穿透并非总能成功 | 部分网络无法直连 | Relay 回退；企业环境支持受控 Gateway |
| Extension 安全风险 | Shell、桌面和应用自动化权限较高 | 默认关闭高风险能力，按 Extension 和操作范围授权 |
| 协议迁移影响旧客户端 | 新旧版本并存 | 协议版本化、兼容适配和渐进下线 |
| 领域模块化流于目录调整 | 代码仍然互相耦合 | 通过依赖规则、接口和测试约束实际边界 |

## 10. 实施路线与验收

### P0：架构基线

- 确定 Workspace 组织原则和模块依赖规则。
- 建立 Server 领域模块模板和测试约定。
- 冻结 OIS / OIC 职责边界。
- 明确旧 Machine 协议兼容策略。

### P1：OIS 首个领域模块

- 在 Server 内增加 OI 模块。
- 从现有 Machine 管理能力中抽取 Node 管理基础。
- 建立 Capability、Binding 和 Desired / Observed State。
- 复用现有连接和 Machine Command，不先建设第二套连接。

### P2：daemon 演进为 OIC

- 抽取 OIC Core。
- 将 Agent Worker 和 AgentSpec 管理迁入 Agent Runtime Extension。
- 保留现有 loom-daemon 兼容入口。
- 验证纯 OIC 模式不依赖 Agent Runtime。

### P3：管理入口融合

- GUI / CLI 接入 OIS 管理 API。
- 通过 loom-server 统一身份引用和管理入口。
- 展示受控端、Capability、Binding 和实际状态。

### P4：渐进迁移历史代码

- 按领域拆分大 Handler、Service 和超大文件。
- 每次迁移保持行为兼容并增加测试。
- 只有在新旧路径均验证后才移除旧实现。

### 验收标准

1. 一个不运行 Agent 的 OIC 可以独立注册到 OIS。
2. OIC 可以发布至少一种 Extension 提供的 Capability。
3. OIS 可以将该 Capability 绑定给 loom-server 中已有的 Agent。
4. OIS 可以向 OIC 下发授权和配置，OIC 能报告实际应用状态。
5. Agent 通过 loom-server 现有执行链路调用该能力。
6. Binding 撤销后，受控端拒绝后续未授权操作。
7. 现有 Agent Runtime Host 能以官方 Extension 方式继续运行，旧 daemon 配置和入口在迁移期保持兼容。

## 11. 评审结论与待决事项

### 建议通过的架构决策

| 决策 | 建议结论 |
|---|---|
| OIS 是否独立部署 | 第一阶段不独立部署，作为 loom-server 内部独立领域模块 |
| OIC 是否独立 | 是，作为可复用 Crate、CLI 和可嵌入 SDK |
| daemon 是否取消 | 不取消运行机制；逐步演进为 OIC，旧命令过渡兼容 |
| 原 Agent Runtime 是否属于 OIC Core | 否，作为官方 Agent Runtime Extension |
| OIC 是否必须运行 Agent | 不需要 |
| 是否重建连接体系 | 不重建，优先复用 Loom 现有连接；独立 OIC 部署所需能力再抽象 |
| Extension 是否开放社区 | 是，先稳定契约，再演进动态插件能力 |
| 是否一次性迁移所有旧代码 | 否，按领域渐进迁移 |

### 仍需在详细设计阶段确定

1. OIC 在完整 Loom Host 中是与 daemon 同进程、独立进程，还是两种模式都支持。
2. OIC 独立部署时的身份签发、连接建立、NAT 穿透和 Relay 协议。
3. OIS 如何复用现有 Machine Connection / Machine Command，同时保持未来可独立抽取。
4. Extension 首版采用静态 Rust 集成还是独立进程；建议先静态集成官方扩展。
5. Node、Capability、Binding 的最终 Schema、版本兼容和数据库迁移策略。
6. 高风险 Extension 的本地权限隔离、人工审批和撤销语义。

以上事项不影响本次总体架构方向，但在进入对应模块实现前必须形成具体协议和安全设计。

## 12. 最终结论

本次 OI 改造不是给 Loom 增加一套新的 Agent 执行平台，而是把现有 Machine / daemon 管理能力升级为通用受控端基础设施。

- **loom-server 管 Agent 及其协作和执行。**
- **OIS 管受控方、能力、授权、配置和状态。**
- **OIC 让机器、应用和服务接入 OIS，成为可管理的受控端。**
- **Extensions 提供具体业务能力。**

第一阶段应优先复用现有连接和 Machine Command 机制，将 OIS 内聚为 loom-server 的独立领域模块，并将 daemon 渐进演进为 OIC。这样既能保留已有 Loom 能力，又能为未来独立 OIC、社区扩展和更广泛的受控资源管理建立稳定基础。
