# 概念与标识

oi（loom-oi）让 Agent 调用不同机器上的受控能力。当前把受控端的能力包装成 App 和 Action，经 OIC 接入 OIS，以 vMCP 中的 Tool 组织消费范围，并提供 HTTP API 和 MCP 两种接入方式。本版通过 MCP 验证完整使用流程，未来扩展更多消费方式。下列术语适用于当前 beta；整体关系见[架构](../architecture/system.md)。

## 组成

| 概念 | 定义与职责 | 当前标识或组织方式 |
| --- | --- | --- |
| oi / loom-oi | 项目整体，连接消费者与不同机器上的受控能力 | 当前消费接入为 HTTP API 与 MCP；组件包括 OIS、OIC 与 App |
| OIS | 中心服务，维护工具定义、固定绑定和 vMCP，处理管理、MCP 与 REST 调用并转交执行 | `ois` CLI 启动的服务进程 |
| OIC | 受控侧通用宿主，加载本地 App 描述，托管 worker，连接 OIS，适配并交付参数 | `oic` CLI 启动的服务进程 |
| Host | 实际承载 OIC 和 App 的宿主机 | 本地显式配置 `host.id`；不是 Agent 的路由参数 |
| 通道 | OIS 配置的固定 OIC 连接入口 | `channel.id`；OIS 配置 `expected_host_id`，一条通道只接受一个活动连接 |
| App / oiapp | 一组相关 Action 的接入单位，包含 YAML 描述和配套业务实现 | 本地 `app.code`，例如 `bash`、`powershell` |
| Action | App 内可调用的操作，声明本地输入及执行入口 | `actions` 的键，例如 `exec_command`；在通道和 App 范围内定位 |
| runtime / worker | runtime 声明启动方式；worker 是 OIC 按该配置托管的实际进程 | 通过 stdin/stdout 上的 JSON Lines 接受调用 |
| Tool | OIS 面向消费者配置的操作，具有名称、描述、输入 Schema 和固定来源 | `source.channel/app/action`；Tool 名称可以与 Action 名不同 |
| vMCP | 将选定 Tool 组成工具集的 MCP 入口，也是消费访问范围 | `/mcp/{vmcp_id}`，例如 `workbench` |

Host 承载 App，App 组织 Action；Tool 引用确定的 Action 和通道，vMCP 组合 Tool。Host、Action 登记视图来自 OIC，并非可通过管理 API 创建的在线执行服务。一个 vMCP 可以包含不同通道的 Tool；这不意味着一个 Tool 会在多个目标之间选路。

“Node”不作为 Host 的别名。未来若引入逻辑节点或负载池，需要单独定义；当前没有该实体。

## 两侧 Schema

OIS 的 Tool 输入 Schema 用于消费侧说明，本地 App 的 Action Schema 用于执行参数适配。二者由各自负责的一侧维护，登记不会自动覆盖中心定义，加载也不要求两份 Schema 相等。

`handler` 表示 worker 中处理操作的入口名，省略时取 Action 名；它不是可由消费者传入的任意命令。调用的 `arguments` / `input` 是业务参数，不能覆盖可信身份、固定通道或 runtime。具体规则见 [App 描述与运行协议](../reference/app-runtime.md)。

## 调用、执行和协议会话

| 标识 | 含义与范围 |
| --- | --- |
| `call_id` | 一次调用的关联标识；每次 `exec_command` 或 `write_stdin` 请求各自获得调用 ID。有限周期去重不等于跨重启恰好执行一次。 |
| `session_id` | 终端 App 管理的一次持续命令执行，绑定原 App worker 和可信访问范围；用于后续输入或读取。 |
| `Mcp-Session-Id` | MCP 初始化及后续 HTTP 请求使用的协议会话标识；不是终端执行句柄。 |
| `context.access_identity` / `context.vmcp_id` | OIS 建立并传递的消费身份及 vMCP 范围，不能从业务参数中声明。 |

匿名消费者在同一 vMCP 内共享 `anonymous:<vmcp_id>` 身份，随意附加 Bearer 内容不会产生独立用户。共享凭据也不自动把多个 Agent 分成不同用户。`session_id` 本身不是凭据。

新 `exec_command` 创建独立执行，不自动继承此前命令的工作目录和环境变量；持续交互通过原句柄接续同一执行。执行结果和期限见[终端 Action](../reference/terminal-actions.md)。

## 就绪与可用

- 本地 ready 表示相关 worker 已完成初始化并支持对应 handler。
- registered 表示当前能力快照已获得 OIS 的登记确认；本地加载成功时，中心登记可以仍为 pending。
- available 需要固定通道在线、完成当前登记且相应 App/Action 当前就绪。断线后保留的登记是旧快照，不能证明当前可执行。

状态查询仅代表查询时刻；真正调用仍需检查对应绑定和依赖。完整查询字段见 [OIS API](../reference/ois-api.md)。
