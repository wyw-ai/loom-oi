# Loom 集成与迁移方案

**状态：** 方案建议稿  
**范围：** Loom 对 OI 的集成方式，以及现有 Machine / daemon 机制向 OIC 演进的路径。

## 1. 目标与职责边界

Loom 是 OI 的首个集成平台，但 OI 不依赖 Loom 的内部领域模型。

- **loom-server**：继续负责 Agent / Actor 身份、Channel、Thread、Message、Task、Run、协作和执行路由。
- **OIS**：管理受控端、Capability、Binding、策略、配置和实际状态。
- **OIC**：提供受控端身份、连接、能力暴露、本地分发和扩展承载。
- **Loom Integration**：将 Loom 的主体身份和现有传输机制映射到 OI 协议。

OI 的实现和协议归属独立的 `wyw-ai/loom-oi` 仓库。Loom 应依赖稳定的 OI 协议或 SDK，不应复制 OI 的领域实现。

## 2. Loom 当前代码与演进方向

| 当前路径 | 当前职责 | 演进方向 |
|---|---|---|
| `crates/server` | WebSocket Server、Journal、Handlers、Machine Commands 和状态管理 | 按领域模块化，增加 Loom Integration |
| `crates/cli` | Loom CLI 和 daemon 实现 | 拆分 CLI 入口、OIC 接入和 Agent Runtime Host |
| `crates/cli/src/cmd/daemon.rs` | Machine 身份、Inventory、Machine Command、Service Host 和 Agent Host 启动 | 抽取 OIC 相关职责，保留迁移兼容 |
| `crates/cli/src/cmd/agent_serve.rs` | Agent Worker、Agent 连接和 Run 交互 | 迁入 Agent Runtime Host / 官方扩展边界 |
| `crates/cli/src/daemon_ipc.rs` | daemon 本地 IPC 和 Proxy | 保留或调整为 OIC 本地管理接口 |
| `crates/agent-runtime` | ACP、Command、Interactive、Provider 和 Discovery | 保留为可复用 Agent 执行引擎 |
| `crates/proto` | JSON-RPC 方法和共享类型 | 保留 Loom 协议，并通过适配层接入 OI 协议 |
| `crates/loom-platform` | OS、进程、文件、IPC 和路径能力 | 保留为平台基础库 |
| `crates/gui`、`apps/gui-web` | Tauri 和 React GUI | 保留，增加受控资源管理界面 |

以上是目标迁移方向，不代表代码已经完成迁移，也不意味着每个模块都必须新建独立 Crate。

## 3. 集成结构

~~~text
                 loom-gui / loom-cli
                         |
                         v
                    loom-server
             +-----------+------------+
             |                        |
      Agent / Collaboration     Loom Integration
      Task / Run / Routing             |
                                       v
                               OI Protocol / SDK
                                       |
                                +------+------+
                                |             |
                                v             v
                               OIS           OIC
                         受控端管理面    受控端运行时
                                             |
                                         Extensions
~~~

Loom Integration 负责主体身份映射、管理 API 调用和现有连接机制适配。它不应在 Loom 内复制 OIS 的 Node、Capability、Binding 等领域逻辑。

## 4. OIS 的部署选择

仓库独立与进程独立是两个不同决策。OIS 的实现归属 loom-oi 仓库，但可以有不同部署方式。

### 方案 A：嵌入 loom-server

loom-server 通过稳定接口链接或承载 OIS 核心实现。

优点：
- 部署和运维组件较少。
- 容易复用 Loom 的身份引用。
- 适合早期单一集成方场景。

约束：
- 必须保持 OIS 独立的领域边界和存储接口。
- OIS 不得依赖 Loom 内部 Agent、Channel 等领域类型。
- 未来必须能够抽取为独立服务。

### 方案 B：独立 OIS 服务

OIS 独立运行，loom-server 通过 API 调用。

优点：
- 独立部署和扩缩容。
- 多个 Agent 平台可以共享 OIS。
- 进程和安全边界更清晰。

代价：
- 需要服务间认证和可用性治理。
- 需要处理跨服务失败和状态一致性。
- 增加部署与运维成本。

初期可以先确定接口和模块边界，再根据部署需求选择。仓库独立不要求第一天就独立部署。

## 5. daemon 向 OIC 演进

现有 daemon 是 OIC 的演进基础，不是需要直接废弃的旧组件。

| 现有 daemon 职责 | 目标归属 |
|---|---|
| Machine 身份与注册 | OIC 身份 / 受控端注册 |
| 现有 Loom 连接 | Loom 连接适配器；独立 OIC 可使用 OI 原生连接 |
| Machine Inventory | OIC 能力发布和受控端状态 |
| Machine Command | OIC 调用与控制适配 |
| AgentSpec 管理 | Agent Runtime Host |
| Provider Discovery | Agent Runtime Host，复用 agent-runtime |
| Agent Worker Supervisor | Agent Runtime Host |
| ACP / Command Adapter | 可复用的 agent-runtime |
| Service Host | 按实际资源职责决定是否作为扩展 |
| 本地 daemon IPC | OIC 本地管理接口或兼容层 |

### 5.1 Agent Runtime 的边界

Agent Runtime 是可选的运行能力，不属于 OIC Core。

- OIC Core 不依赖 Agent Runtime。
- Agent Runtime Host / Extension 负责 AgentSpec、Worker 生命周期和相关配置。
- `crates/agent-runtime` 继续作为可复用执行引擎。
- 受控端可以只运行 OIC，不启动任何 Agent。

### 5.2 兼容策略

迁移期间保留现有 `loom-daemon` 命令和配置作为兼容入口。新接口和新文档统一采用 OIC 命名。只有在脚本、配置和部署方式完成迁移后，才移除旧入口。

## 6. Loom Server 的模块化方向

本次 OI 集成不要求重设计 Agent 和 Collaboration 的业务语义。Server 可以逐步按领域组织：

- `identity`：认证和主体解析。
- `agent`：Agent 定义和配置。
- `collaboration`：Actor、Channel、Thread、Message 和成员关系。
- `execution`：Task、Run、Delivery 和 Artifact 生命周期。
- `oi_integration`：OI API 调用及 Loom 主体 / 传输映射。

复杂领域可以按需要采用 Domain、Application、Infrastructure 和 API 分层，不要求每个模块机械创建空目录。

依赖规则：

1. Domain 不依赖 Infrastructure。
2. 一个领域不直接访问另一个领域的内部 Repository 或数据表。
3. 跨领域通过明确的 Application API、Port 或事件协作。
4. RPC Handler 负责协议转换，不承载核心业务规则。
5. 模块内部默认私有，只暴露稳定接口。
6. 避免将共享模块变成新的 common / utils 大杂烩。

Agent、Channel、Thread、Message、Task 和 Run 可以进行结构性重组，但本次 OI 集成不顺带改变其业务模型。

## 7. 迁移范围

| 领域 | 变更类型 | 目标 |
|---|---|---|
| Server 目录和模块边界 | 重构 | 按领域拆分大型 Handler 和 Store |
| OI 集成层 | 新增 | 接入 OIS API 和 OI Protocol |
| OIC Core | 外部依赖 / 集成 | 使用 loom-oi 提供的 SDK 或 Runtime |
| Agent Runtime Host | 重构 | 从 daemon 职责中拆分 |
| Machine Inventory | 迁移 | 映射到受控端和 Capability 视图 |
| Machine Command | 适配 | 兼容现有协议，逐步映射到 OIC 调用和配置机制 |
| GUI / CLI | 扩展 | 增加受控端、能力和 Binding 管理 |
| Agent / Channel / Thread / Message | 结构性重构 | 保持现有行为和业务语义 |

## 8. 实施顺序

### P0：集成契约
- 确定 OI Protocol 和 SDK 边界。
- 定义 Loom Subject Reference。
- 明确传输适配器职责。
- 确定 Machine ID、配置和命令兼容要求。

### P1：接入 OIS
- 增加 Loom Integration Adapter。
- 将现有 Machine 管理信息映射到 OI 受控端模型。
- 接入 Capability 和 Binding 管理。
- 条件允许时复用现有传输与命令路径。

### P2：daemon 演进
- 将通用受控端能力迁入 OIC。
- 将 Agent Worker 和 AgentSpec 管理迁入 Agent Runtime Host / Extension。
- 保留旧 daemon 命令作为兼容入口。
- 验证不运行 Agent 的独立 OIC 模式。

### P3：管理入口
- GUI / CLI 增加受控端、Capability 和 Binding 管理。
- Agent、Channel 和协作操作继续使用现有 Loom API。
- 客户端不重复实现 OIS 策略逻辑。

### P4：渐进式模块重构
- 按领域拆分大型 Handler 和 Service。
- 通过测试和兼容适配保持现有行为。
- 所有消费者迁移后再移除旧实现。

## 9. 验收标准

1. Loom 能查询 OIS 管理的受控端和 Capability。
2. Loom Agent 身份通过带签发方的主体引用映射，不在 OIS 中复制 Agent 记录。
3. Agent 可以绑定到受控端上的指定 Capability。
4. OIC 能接收并执行相应策略。
5. OIS 能提供期望状态和实际状态。
6. Agent 操作仍通过 Loom 现有执行与协作体系完成。
7. 迁移期间现有 Agent Runtime Host 保持可用。
8. 独立 OIC 无需启动 Loom Agent Runtime 即可接入。

## 10. 总结

Loom 将 OI 作为独立的受控端基础设施集成。loom-server 保留 Agent 和协作管理；OIS 管理受控端、能力、授权和状态；OIC 提供受控端连接与调用分发；Extensions 实现具体操作。

迁移应保留现有 Loom 行为，复用适合的 Machine / daemon 机制，并先建立清晰的集成边界，再逐步抽取或替换现有组件。
