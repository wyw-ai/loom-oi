# Loom Architecture V2：目录结构与模块迁移方案

**状态：** Proposed  
**范围：** Loom Workspace 重构、OIS 纳入 loom-server、loom-daemon 演进为 OIC。

## 1. 改造目标

本次改造不只是增加 OI 功能，而是建立 Loom 后续新功能可持续遵循的代码组织方式。

核心边界：

- **loom-server 管理 Agent 及其交互、协作和执行流程。**
- **OIS 管理受控端、能力、授权关系、配置和实际状态。**
- **OIC 是受控端 SDK / CLI / Runtime，负责连接 OIS、暴露能力和本地分发。**
- **Extension 实现具体业务能力。**

采用模块化单体和领域模块化：先明确代码边界，不急于拆成大量独立服务或 Crate；新功能按领域组织，跨领域通过明确接口协作。

## 2. 当前代码基线

| 当前路径 | 当前职责 | 演进方向 |
|---|---|---|
| `crates/server` | Server、WebSocket、Journal、Handlers、Machine Commands | 按领域模块重组；增加 OIS |
| `crates/cli` | Loom CLI、daemon 命令和本机管理逻辑 | 拆分应用入口与可复用 Runtime |
| `crates/cli/src/cmd/daemon.rs` | Machine-scoped Agent Host、Machine Inventory、Machine Command、Service Host | 拆分为 OIC Core、资源管理和 Agent Runtime Host |
| `crates/cli/src/cmd/agent_serve.rs` | Agent Worker、Agent 连接、Run 生命周期和消息交互 | 迁入 Agent Runtime Host |
| `crates/cli/src/daemon_ipc.rs` | daemon 本地 IPC / Proxy | 评估保留为 OIC 本地管理接口或兼容层 |
| `crates/agent-runtime` | ACP、Command、Interactive Adapter、Provider、Discovery | 保留为可复用 Agent 执行引擎 |
| `crates/proto` | JSON-RPC 方法、共享类型和协议定义 | 整理为 protocol，增加 OI 协议 |
| `crates/loom-platform` | 文件、IPC、进程、路径、信号等跨平台能力 | 保留为 platform 基础库 |
| `crates/gui` | Tauri 桌面应用后端 | 保留并接入新管理接口 |
| `crates/loom-shell` | Shell UI 相关能力 | 保留 |
| `apps/gui-web` | React Web GUI | 保留现有位置 |

当前 daemon 已有 Machine Identity、Machine Inventory、Machine Command、Service Runtime、配置刷新和本机 IPC 等机制，是 OIC 演进的基础。

## 3. 目标 Workspace 结构

以下是目标职责结构，不要求一次性创建所有目录或 Crate。

~~~text
loom/
├── apps/                              # 可执行应用入口
│   ├── loom-server/
│   ├── loom-cli/
│   └── oic/
├── crates/                            # 可复用实现
│   ├── kernel/                        # [新增] 少量稳定基础抽象
│   ├── protocol/                      # [改造] 由 crates/proto 演进
│   │   ├── rpc/
│   │   ├── events/
│   │   ├── types/
│   │   └── oi/
│   ├── server/                        # [改造] Server 核心
│   │   └── src/modules/
│   │       ├── identity/
│   │       ├── agent/
│   │       ├── collaboration/
│   │       ├── execution/
│   │       ├── service/
│   │       └── oi/                    # [新增] OIS
│   │           ├── domain/
│   │           │   ├── node/
│   │           │   ├── capability/
│   │           │   └── binding/
│   │           ├── application/
│   │           ├── infrastructure/
│   │           └── api/
│   ├── oic/                           # [新增] OIC Core
│   │   └── src/
│   │       ├── identity/
│   │       ├── connectivity/
│   │       ├── capability/
│   │       ├── dispatch/
│   │       ├── policy/
│   │       ├── extension/
│   │       └── lifecycle/
│   ├── extensions/
│   │   └── agent-runtime/             # [新增] 官方 Agent Runtime Extension
│   ├── agent-runtime/                 # [保留/改造] 执行引擎
│   ├── client/                        # [新增或抽取] Loom Server Client
│   ├── platform/                      # [迁移] 原 loom-platform
│   ├── gui/
│   └── loom-shell/
├── extensions/                        # [可选] 独立发布的社区扩展
│   ├── computer-use/
│   ├── wechat/
│   ├── qq/
│   ├── magpie/
│   └── open-connector/
├── docs/
└── Cargo.toml
~~~

### apps 与 crates

- `apps` 放可执行程序入口，负责参数解析、配置装载、日志初始化、依赖装配和进程生命周期。
- `crates` 放可复用实现，包括领域逻辑、协议、客户端、运行时和平台抽象。
- 应用入口不承载大段业务逻辑；Library Crate 不反向依赖应用入口。
- 现有可执行文件不必第一阶段全部搬入 apps。先整理内部模块，边界稳定后再移动 Cargo Package。

### OIS 的位置

第一阶段 OIS 作为 `crates/server` 内部的独立领域模块实现，不单独部署为服务。它复用 loom-server 的身份引用、数据库基础设施及现有连接/命令通道。

OIS 核心对象保持精简：

- **Node：** 受控端或受控资源的管理对象。
- **Capability：** 受控端暴露的能力声明及状态。
- **Binding：** Agent 等控制主体与受控资源之间的授权关系。
- **Desired / Observed State：** 期望配置与受控端实际应用状态。

OIS 不建立第二套 Agent Registry、Channel、Task、Run、业务 Session 或通用任务调度系统。

### OIC 的位置

`crates/oic` 是可复用的受控端核心库，`apps/oic` 是 CLI / Runtime 启动入口。

OIC Core 负责身份注册、安全连接、连接协商、NAT 穿透与 Relay 回退、Capability 暴露、远程请求本地分发、本地策略约束和 Extension 生命周期。

OIC 不依赖 Agent Runtime，可以独立运行，也可以嵌入用户自己的应用。

## 4. daemon 演进方案

本次改造不取消 daemon 已有的运行机制，而是将其职责拆分并演进为 OIC。

| 现有 daemon 能力 | 目标归属 |
|---|---|
| Machine Identity | OIC Core |
| Machine Connection | OIC Core / 现有连接适配 |
| Machine Inventory | OIC Resource / Capability 管理 |
| Machine Command | OIC Invocation / 控制适配 |
| AgentSpec 管理 | Agent Runtime Extension |
| Provider Discovery | Agent Runtime Extension / agent-runtime |
| Agent Worker Supervisor | Agent Runtime Extension |
| ACP / Command Adapter | agent-runtime |
| Service Host | 按实际职责拆入 Extension |
| Unix Socket IPC | OIC 本地管理接口或兼容层 |

原 daemon 的 Agent 执行能力形成官方 Agent Runtime Extension。它可以依赖 `crates/agent-runtime`，但执行引擎本身不等于 Extension Host。用户可以只安装 OIC，不运行任何 Agent。

过渡期间保留 `loom-daemon` 作为兼容命令或包装入口，内部转发到 OIC Runtime。新文档、新模块和新 CLI 统一使用 OIC 命名，待配置和脚本迁移完成后再移除旧命令别名。

## 5. Server 内部模块化规则

`crates/server/src/modules` 是本次重构重点。领域模块按需要采用：

- `domain/`：实体、值对象、领域规则和 Repository Trait。
- `application/`：用例、命令处理、查询和事务边界。
- `infrastructure/`：数据库、外部系统和底层实现适配。
- `api/`：RPC Handler、DTO 和模块对外接口。

不要求每个小模块机械创建四层目录；只有复杂度真实存在时才拆分。

依赖规则：

1. Domain 不依赖 Infrastructure。
2. 一个领域不得直接读写另一个领域的内部表或 Repository。
3. 跨领域交互通过明确的 Application API、Port 或领域事件。
4. API Handler 负责协议转换，不承载核心业务规则。
5. 模块内部默认私有，只暴露稳定接口。
6. Kernel 只放真正稳定且跨领域复用的基础抽象，不成为新的 common/utils 大杂烩。

## 6. Extension 设计原则

OIC Extension 是业务能力的实现单位。第一阶段定义稳定的 Extension Contract：

- Manifest：扩展身份、版本和元信息。
- Capability Declaration：能力名称、版本、输入输出 Schema、权限需求。
- Invocation Interface：统一调用入口和结果格式。
- Lifecycle：初始化、启动、停止和健康状态。
- Policy Context：调用者、授权范围和本地限制。

第一阶段官方 Extension 可以通过 Rust Crate 静态集成，不急于引入复杂动态插件 ABI。后续根据社区需求，再支持独立进程、跨语言 SDK 和远程 Extension。

社区扩展示例：Shell / Bash、Computer Use、WeChat / QQ 自动化、Browser Automation、HTTP Connector、Magpie、OpenConnector、Docker / Kubernetes，以及用户自定义应用和设备控制。

OIC Core 只负责接入、连接、授权上下文和调用分发；具体业务逻辑交给 Extension。

## 7. 与 loom-server 的职责边界

| 能力 | loom-server | OIS / OIC |
|---|---|---|
| Agent / Actor 身份与生命周期 | 管理 | 只引用主体身份 |
| Channel / Thread / Message | 管理 | 不管理 |
| Task / Run / Agent 执行流程 | 管理 | 不建立平行执行系统 |
| 现有 Loom 连接与协作路由 | 管理 | 优先复用 |
| 受控端注册与纳管 | 通过 OIS 查询和管理 | OIS 管理，OIC 接入 |
| Capability 声明与状态 | 消费 | OIS 管理，OIC 上报 |
| Agent 与资源 Binding | 提供 Agent 身份引用和入口 | OIS 管理授权关系 |
| 资源配置与实际状态协调 | 展示和使用结果 | OIS 管理，OIC 落实 |
| 本地操作执行 | 通过现有执行链路发起 | OIC Extension 执行 |

OIS 与 loom-server 第一阶段同进程或同一服务部署，但保持模块边界。未来第三方产品需要独立使用 OIS，或规模和安全边界要求独立扩缩容时，再抽取为独立服务。

## 8. 迁移标记

**新增**
- `crates/oic`：OIC Core。
- `crates/server/src/modules/oi`：OIS 领域模块。
- OIC Extension Contract、OIC CLI 入口。
- Node / Capability / Binding 协议定义。
- Server 领域模块边界与依赖规则。

**改造**
- `crates/server`：从大 Handler / Store 结构逐步演进为领域模块。
- `crates/proto`：整理为 protocol，逐步划分 RPC、事件和 OI 类型。
- `crates/cli`：拆出 OIC Runtime、Agent Runtime Host 和 CLI 入口。
- `crates/agent-runtime`：保持执行引擎定位，明确与 Agent Runtime Extension 的关系。
- `crates/loom-platform`：整理为平台能力库，是否改名以迁移成本为准。
- Machine Inventory / Machine Command：向受控资源和能力管理演进。

**迁移**
- 原 `daemon.rs` 中身份、连接、资源清单和控制分发逻辑迁入 OIC 相关模块。
- 原 `agent_serve.rs` 中 Agent Worker 管理迁入 Agent Runtime Host。
- 原 Service Host 按实际职责拆分。
- Server Handlers 按领域迁移，避免一次性大爆炸式搬迁。

**逐步移除**
- `loom-daemon` 作为主要产品命名和新功能入口。
- 旧 Machine Inventory 中仅服务于旧模型的重复字段和逻辑。
- 旧 Handler / Service 大文件中的重复实现。
- 被新协议完全替代且已无兼容调用者的旧接口。

移除必须在兼容迁移完成后进行，不应仅为目录整洁而删除仍在使用的能力。

## 9. 推荐实施顺序

### P0 — 架构基线
- 确定 Workspace 组织原则和模块依赖规则。
- 建立 Server 领域模块模板和测试约定。
- 冻结 OIS / OIC 职责边界。
- 明确旧 Machine 协议兼容策略。

### P1 — OIS 首个领域模块
- 在 Server 内增加 OI 模块。
- 从现有 Machine 管理能力中抽取 Node 管理基础。
- 建立 Capability、Binding 和 Desired / Observed State。
- 复用现有连接和 Machine Command，不先建设第二套连接。

### P2 — daemon 演进为 OIC
- 抽取 OIC Core。
- 将 Agent Worker 和 AgentSpec 管理迁入 Agent Runtime Extension。
- 保留现有 loom-daemon 兼容入口。
- 验证纯 OIC 模式不依赖 Agent Runtime。

### P3 — 管理入口融合
- GUI / CLI 接入 OIS 管理 API。
- 通过 loom-server 统一身份引用和管理入口。
- 展示受控端、Capability、Binding 和实际状态。

### P4 — 渐进迁移历史代码
- 按领域拆分大 Handler、Service 和超大文件。
- 每次迁移保持行为兼容并增加测试。
- 只有在新旧路径均验证后才移除旧实现。

## 10. 架构决策摘要

| 决策 | 结论 |
|---|---|
| OIS 是否独立部署 | 第一阶段不独立部署，作为 loom-server 内部模块 |
| OIC 是否独立 | 是，作为可复用 Crate 和可运行 CLI |
| daemon 是否取消 | 不取消运行机制；演进为 OIC，旧命令过渡兼容 |
| 原 Agent Runtime 是否属于 OIC Core | 否，作为官方 Agent Runtime Extension |
| OIC 是否必须运行 Agent | 不需要 |
| 是否重建连接体系 | 不重建，优先复用 Loom 现有连接 |
| Extension 是否开放社区 | 是，先稳定契约，再演进动态插件能力 |
| 是否一次性迁移所有旧代码 | 否，按领域渐进迁移 |

**最终原则：loom-server 管 Agent，OIS 管受控方，OIC 让受控方接入并可被管理，Extensions 提供具体业务能力。**
