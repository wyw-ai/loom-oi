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
