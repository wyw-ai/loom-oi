# loom-oi beta：OIS 首版 API 草案

状态：候选设计，2026-11-09。本文按已确认的固定调用链展开 API，路径、字段、认证形式、配置持久化及变更规则均为建议，尚未实现或整体采纳。已确认边界见[当前共识](../../../current/beta/design.md)，Bash 行为见[执行机制详稿](bash-action-design.md)。

首版链路为 Agent → vMCP → Tool → Action → 通道 → OIC → oiapp → cmd。每个 Tool 固定绑定一个 App.Action 和通道；消费者不能通过输入改变执行位置。OIS 管理定义及调用交付，OIC 加载本地实现，Bash App 管理实际进程和持续执行状态。

## 1. 入口与共同约定

| 入口 | 使用者 | 用途 |
| --- | --- | --- |
| /api/v1/* 的配置与状态接口 | 管理员 | 配置通道、Tool、vMCP，查看 Host、App、Action 登记及运行状态 |
| POST /api/v1/exec | 脚本或程序消费者 | 在指定 vMCP 范围内调用已配置 Tool |
| /mcp/{vmcp_id} | MCP 客户端 | 发现并调用该 vMCP 暴露的工具 |
| OIC 接入入口，载体与路径待定 | OIC | 建立通道、登记本地能力、接收调用并返回结果 |

REST 请求与响应采用 JSON。建议使用 Authorization: Bearer <token>；管理凭据、vMCP 消费凭据和 OIC 通道接入凭据分别授权，不能因拥有其中一种就获得另一种权限。凭据映射为系统可信身份；普通调用不能自报身份或执行上下文。具体 MCP 认证与客户端兼容方式仍需随所选客户端验证。

管理接口的 PUT 按路径标识创建或完整替换资源，首次创建返回 201，替换返回 200；不支持隐式部分合并。正文不另外提交资源 ID，在线、就绪、endpoint 等只读状态不能由 PUT 设置。GET 详情返回资源，列表返回 {"items": [...]}；DELETE 成功返回 204 且无正文。PUT/GET 不回显凭据明文，credential_env 仅表示 OIS 进程环境中的凭据引用名称。首版不增加账号、令牌签发或密钥存储管理 API。

配置权威推荐采用一份 OIS 持久配置：首次初始化时从 YAML 导入通道、Tool、vMCP 定义，此后管理 API 更新该配置，正常重启不重新用旧 YAML 覆盖。监听地址、存储位置等进程启动参数仍由启动配置提供。YAML 导入导出及持久化介质不在本次冻结；若最终选择始终以文件为准，必须相应收缩写 API，不能维持两份独立可变定义。本段是为使 CRUD 行为可解释而提出的新候选。

## 2. 运行状态

| 方法 | 路径 | 行为与权限 |
| --- | --- | --- |
| GET | /healthz | 最小存活检查，只返回 OIS 服务可响应，不表示 OIC 或 App 可执行 |
| GET | /api/v1/status | 管理员查看 OIS 运行状态及配置/在线资源数量 |

status 响应示例：

```json
{
  "status": "running",
  "channels": {"configured": 1, "online": 1},
  "tools": {"configured": 2, "available": 2},
  "vmcps": {"configured": 1}
}
```

available 取决于固定通道在线及对应 App.Action 当前就绪，不仅取决于定义存在。运行状态是快照，实际调用仍需检查当前可用性。

## 3. 通道配置与 OIC 能力查看

这些接口只供管理员配置或检查固定接入关系，不是 Agent 的目标发现接口。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/channels | 列出已配置通道及连接状态 |
| GET | /api/v1/channels/{channel_id} | 查看一个通道 |
| PUT | /api/v1/channels/{channel_id} | 创建或替换通道配置 |
| DELETE | /api/v1/channels/{channel_id} | 删除未被 Tool 引用的通道 |
| GET | /api/v1/channels/{channel_id}/apps | 查看该通道的 App/Action 登记快照 |

例如 PUT /api/v1/channels/bash-local：

```json
{
  "description": "本机 Bash 接入",
  "credential_env": "OIC_BASH_TOKEN"
}
```

channel_id 取自路径。通道配置不指定远程机器地址，OIC 主动连接 OIS；接入凭据必须与该通道的授权关系匹配，不能仅凭自报 channel_id 占用通道。建议同一通道已有活动连接时拒绝重复接入，失效连接确认关闭后才能重新接入；旧连接的迟到消息不能覆盖新连接状态。具体存活检测仍待设计。

GET /api/v1/channels/bash-local/apps 示例：

```json
{
  "channel": "bash-local",
  "online": true,
  "items": [
    {
      "app": "bash",
      "ready": true,
      "actions": [
        {"name": "exec_command", "ready": true},
        {"name": "write_stdin", "ready": true}
      ]
    }
  ]
}
```

这些状态来自 OIC 登记，不允许管理员用 PUT 把 App 伪装成 ready，也不自动生成或覆盖 Tool 定义。断连后标记 online=false；若保留上次登记快照，不能据其报告当前可执行。首版不提供 OIS 远程上传 App、执行 oic load 或控制 oiapp 文件的接口。

通道被 Tool 引用时删除返回 409 RESOURCE_IN_USE，不级联删除工具。建议活动连接期间修改接入凭据或删除通道返回 409 CHANNEL_CONNECTED，先由管理员停止对应 OIC，再更新配置并按需重新连接，避免默默切换接入身份。删除配置本身不承担远程卸载 App 或终止 Bash 的职责。

### Host 管理查询

Host 是提供能力的实际宿主机，通道是到 OIC 的通信接入关系。首版不做多 Host 路由，不意味着管理侧不能查看已登记机器和其能力。以下查询只用于管理和配置检查，不向 Agent 增加目标发现 Tool，也不改变 Tool 的固定 source。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/hosts | 列出已登记 Host 及接入状态 |
| GET | /api/v1/hosts/{host_id} | 查看 Host 信息及关联通道 |
| GET | /api/v1/hosts/{host_id}/apps | 查看该 Host 登记的 App；每项保留 channel |
| GET | /api/v1/hosts/{host_id}/actions | 查看该 Host 登记的 Action；每项保留 channel、app 和 action |

GET /api/v1/hosts/host-a 示例：

```json
{
  "id": "host-a",
  "name": "开发机 A",
  "os": "linux",
  "arch": "amd64",
  "online": true,
  "channels": [
    {"id": "bash-local", "online": true}
  ]
}
```

建议 OIC 在认证接入时登记本机 Host 信息，OIS 保存管理视图；Host 名称、系统和架构是上报元数据，不视为硬件身份认证。host_id 应稳定且受接入授权绑定，不直接使用 hostname/IP 作为唯一身份，也不能让客户端仅凭自报同一 ID 覆盖其他 Host。具体生成、绑定、重新安装和重复身份处理仍待设计；这些查询路径不代表已经确定新的 Host 接入协议。

Host.online 仅表示存在当前可用的关联 OIC 通道，不表示机器电源状态或全部 Action 就绪。channels 数组用于展示关联，首版例子只有一个；它不承诺一台机器多 OIC 的自动归并、迁移或选路。Host 与通道如何持久关联属于登记契约的待定细节，调用仍直接使用 Tool 已配置的 channel。

首版建议 Host 由 OIC 接入登记，暂不提供用于伪造上线状态的 POST/PUT，也不以 DELETE Host 记录实现关机、停止 OIC 或清除 App。独立的 Host 预登记、备注编辑和历史记录清理尚未纳入本版 API 候选。

### Action 管理查询

Action 的名称、描述和本地参数 Schema 来自 App YAML，由 OIC 加载后登记；OIS 提供明确的查询入口。它与 OIS 自己维护的 Tool 消费定义分开，不增加第二份可独立修改的本地 Action 定义。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/actions | 列出 Action 登记项，支持可选 host_id、channel、app 过滤 |
| GET | /api/v1/channels/{channel_id}/apps/{app_code}/actions | 查看某个通道中一个 App 的 Action 列表 |
| GET | /api/v1/channels/{channel_id}/apps/{app_code}/actions/{action_name} | 查看具体 Action 的本地描述、参数 Schema 与可用状态 |

例如 GET /api/v1/actions?host_id=host-a&app=bash；过滤条件是管理员的查询范围，不进入 /exec 参数，也不用于自动选择执行目标。列表各项保留来源；同名 Action 不去重合并为一个可路由资源。

GET /api/v1/channels/bash-local/apps/bash/actions/exec_command 示例：

```json
{
  "host_id": "host-a",
  "channel": "bash-local",
  "app": "bash",
  "action": "exec_command",
  "description": "创建一次 Bash 执行",
  "input_schema": {
    "type": "object",
    "properties": {
      "cmd": {"type": "string"},
      "workdir": {"type": "string"},
      "tty": {"type": "boolean", "default": false},
      "yield_time_ms": {"type": "integer"},
      "max_output_tokens": {"type": "integer"}
    },
    "required": ["cmd"]
  },
  "ready": true,
  "available": true,
  "stale": false
}
```

本首版候选通过 (channel, app, action) 定位一项登记，不引入独立的全局 action_id，也不允许同一通道下同一 App code 的多个活动实例含混登记。这个配置定位不是运行实例身份；worker 重启后即使三者相同，旧 session 也不因此恢复。

ready 表示 OIC 上报的本地就绪状态；available 要求通道在线、登记属于当前连接且入口就绪。如果断连后保留上一轮登记，必须设置 stale=true、available=false，ready 只能解释为上次观测值。重新连接后的能力必须重新登记。正常卸载或移除 Action 后从当前清单撤下，详情返回 404；已有 Tool 定义保留但显示不可用。

OIS 管理 API 不通过 PUT/PATCH 修改这些登记项的本地 Schema、handler 或 runtime；修改本地 YAML 并通过 OIC 加载机制生效后更新登记。是否支持重复 load 或热更新仍按本地生命周期设计另定。停用同样由本地 OIC/App 管理，不用删除查询记录替代实际停用。消费描述修改则使用 PUT /api/v1/tools/{tool_name}，它不覆盖本地 Schema。

Host 与 Action 的上述具体端点及字段均为本轮补充候选，并未作为整体已确认。它们补全管理视图，执行链继续保持 Agent → vMCP → Tool → Action → 固定通道 → OIC → oiapp → cmd。

## 4. Tool 定义与固定绑定

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/tools | 列出工具定义及可用状态，管理员使用 |
| GET | /api/v1/tools/{tool_name} | 查看工具定义及固定绑定 |
| PUT | /api/v1/tools/{tool_name} | 创建或替换工具定义 |
| DELETE | /api/v1/tools/{tool_name} | 删除未被 vMCP 引用的工具 |

例如 PUT /api/v1/tools/terminal_exec：

```json
{
  "description": "启动一次 Bash 执行，返回输出和执行句柄或退出码",
  "input_schema": {
    "type": "object",
    "properties": {
      "cmd": {"type": "string"},
      "workdir": {"type": "string"},
      "tty": {"type": "boolean"},
      "yield_time_ms": {"type": "integer"},
      "max_output_tokens": {"type": "integer"}
    },
    "required": ["cmd"]
  },
  "source": {
    "channel": "bash-local",
    "app": "bash",
    "action": "exec_command"
  }
}
```

第二个工具 PUT /api/v1/tools/terminal_write_stdin：

```json
{
  "description": "向已有执行输入，或等待并读取后续输出",
  "input_schema": {
    "type": "object",
    "properties": {
      "session_id": {"type": "string"},
      "chars": {"type": "string"},
      "yield_time_ms": {"type": "integer"},
      "max_output_tokens": {"type": "integer"}
    },
    "required": ["session_id"]
  },
  "source": {
    "channel": "bash-local",
    "app": "bash",
    "action": "write_stdin"
  }
}
```

source.channel 必须引用已有通道配置；App.Action 暂未登记或通道离线不阻止保存工具定义，但工具状态显示不可用。OIS 可检查自身定义的基本结构及引用，不以 OIS Schema 与本地 Action Schema 相等作为保存或加载条件；可识别差异最多提示。

source 是单个固定对象，不接受目标数组、优先级或负载均衡字段。建议首版既有 Tool 的 source 不原位变更，变化时返回 409 BINDING_IMMUTABLE；需要新目标时创建新 Tool，再显式调整 vMCP。这样无需引入执行迁移机制。删除被 vMCP 引用的 Tool 返回 409 RESOURCE_IN_USE。这些变更规则仍是候选；不能把旧 session 自动解释成新实现中的执行。

## 5. vMCP 工具集

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/vmcps | 列出 vMCP 定义，管理员使用 |
| GET | /api/v1/vmcps/{vmcp_id} | 查看工具集合与 MCP 入口 |
| PUT | /api/v1/vmcps/{vmcp_id} | 创建或替换 vMCP 配置 |
| DELETE | /api/v1/vmcps/{vmcp_id} | 删除 vMCP，停止接受其新调用 |

例如 PUT /api/v1/vmcps/workbench：

```json
{
  "tools": ["terminal_exec", "terminal_write_stdin"],
  "access": {
    "credential_env": "WORKBENCH_AGENT_TOKEN"
  }
}
```

vmcp_id 取自路径；本候选将 MCP 地址固定生成为 /mcp/{vmcp_id}，无需再提交可任意变更的 path。它对应此前示例的 path: /mcp/workbench，是否保留自定义 path 尚未冻结。工具必须已存在；同一 vMCP 内不重复添加同名工具。

GET 结果可增加只读 endpoint 字段。管理接口返回的凭据引用不是凭据值。消费者经 MCP 只发现自己有权使用的工具，不取得管理接口中的 source 和 credential_env。

建议配置更新只影响随后进入的新请求；已接受的请求保留当次固定绑定与身份上下文，结果仍可返回，不迁移或重跑。删除 vMCP 或移除 Tool 后，相应新请求被拒绝；这不会自动终止 App 中已有 Bash 进程。管理者需要结束执行时使用 OIC/App 的生命周期机制。首版不承诺跨配置改动恢复旧执行。

变更后新的 tools/list 返回当前定义。支持工具列表变更通知的客户端按所选 MCP 版本获取通知；其他客户端需要重新发现工具或重新连接，不能假定修改 API 后所有 Agent 的缓存会立即刷新。即使客户端使用旧缓存发起调用，OIS 仍检查当前工具集合和访问权限。通知行为依所选协议与客户端能力实现。[MCP Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)

## 6. HTTP 执行入口

POST /api/v1/exec 使用相应 vMCP 的消费凭据。请求由 vmcp、tool 和 arguments 三部分组成；vmcp/tool 是非空字符串，arguments 必须是对象。不提供绕过 Tool 定义直接调用任意 App.Action 的旁路。

```json
{
  "vmcp": "workbench",
  "tool": "terminal_exec",
  "arguments": {
    "cmd": "ls -l",
    "workdir": "/srv/workspace",
    "yield_time_ms": 1000,
    "max_output_tokens": 2000
  }
}
```

vmcp/tool 是调用入口标识，不是目标机器选择；arguments 是工具业务参数，与 MCP tools/call 中的 arguments 对应。业务参数不能改变固定通道、runtime、handler 或可信身份。HTTP 请求信封必须结构有效，但不能在 OIC 轻量适配前因业务值与中心描述类型不同就一律拒绝，例如可转换的 "1000" 应允许交给本地绑定。

处理顺序：确认消费凭据及 vMCP 访问 → 确认 Tool 属于该 vMCP → 读取固定 source → 经在线通道转交 → OIC 按本地 YAML 适配 → App 执行 → 返回本次结果。HTTP 与 MCP 使用同一套调用处理逻辑；MCP 不必再通过额外 HTTP 请求绕到 /exec。

OIS 为每次接受的请求生成内部 call_id，贯穿一次转交和响应；它不是 Bash session，也不是可由客户端任意复用的重试承诺。成功响应返回 HTTP 200，示例只展示业务核心字段：

```json
{
  "call_id": "call-001",
  "result": {
    "output": "total 0\n",
    "exit_code": 0
  }
}
```

长命令本轮等待结束后可以返回：

```json
{
  "call_id": "call-002",
  "result": {
    "output": "Building...\n",
    "session_id": "s_123"
  }
}
```

这是本次 Action 调用成功完成，不使用 202 表示 OIS 排队了一个后台任务。Bash 是否结束由 App 结果表达。继续读取仍使用同一个 HTTP 接口：

```json
{
  "vmcp": "workbench",
  "tool": "terminal_write_stdin",
  "arguments": {
    "session_id": "s_123",
    "chars": "",
    "yield_time_ms": 30000,
    "max_output_tokens": 2000
  }
}
```

后续请求产生新的 call_id，引用同一个 session_id；两个 Tool 必须固定到同一 Bash 执行后端。执行句柄不是凭据，本地 App 需按可信访问身份和约定范围检查其归属。共享同一消费凭据的 Agent 在该简单认证候选下属于同一身份，不假装提供独立用户隔离。yield_time_ms 是本次等待窗口，非进程期限；HTTP 连接断开不能简单解释成 Bash 已停止。

首版通过两 Action 分批返回输出，不增加独立 /sessions、/jobs、stdout 订阅、结果队列或全局 kill API。Bash 输出截断等附加结果字段沿用详稿；OIS 不解释或改写其业务状态。命令非零退出仍返回 HTTP 200 和实际 exit_code。

## 7. 错误与副作用边界

REST 错误响应示例：

```json
{
  "call_id": "call-003",
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "yield_time_ms cannot be converted to an integer.",
    "details": {
      "field": "yield_time_ms",
      "expected": "integer",
      "actual_type": "object"
    }
  }
}
```

call_id 仅在已分配时返回；管理接口和尚未接受的错误可以没有它。result 与 error 二选一。

| HTTP 状态 | 示例代码 | 语义 |
| --- | --- | --- |
| 400 | INVALID_REQUEST | 请求信封或管理资源结构不合法 |
| 401 / 403 | UNAUTHENTICATED / FORBIDDEN | 缺少有效凭据或无访问权限 |
| 404 | NOT_FOUND | 指定配置资源不存在，或工具不属于该消费入口 |
| 409 | RESOURCE_IN_USE / BINDING_IMMUTABLE / SESSION_BUSY | 配置引用冲突、禁止原位换绑，或本次会话操作冲突 |
| 422 | INVALID_ARGUMENT / SESSION_UNAVAILABLE / STDIN_NOT_WRITABLE | 已明确的参数或业务调用错误，具体错误含义遵循 App 契约 |
| 503 | CHANNEL_UNAVAILABLE / APP_UNAVAILABLE | 已确认本次未交付执行的依赖不可用 |
| 504 | CALL_RESULT_UNKNOWN | 交付后超时或断连，无法确定执行或输入结果 |
| 500 / 502 | INTERNAL_ERROR / EXECUTION_ERROR | 服务内部或执行链异常，必须按已知事实标注是否可能产生副作用 |

以上映射是候选，不意味着所有同类 HTTP 错误都可安全重试。失联前可能已启动命令或写入部分字符时必须保留未知/部分完成的信息，不能伪装成未执行。OIS 不自动重投；短时内部去重不能承诺跨重启或跨缓存保留期恰好执行一次。

REST 错误不能原样当作 MCP HTTP 错误输出。MCP 适配层按选定协议将工具执行错误与 JSON-RPC/传输错误分别映射；两入口保留一致的业务错误含义。

## 8. MCP 入口

示例 endpoint 为 /mcp/workbench。客户端通过 MCP 的 tools/list 发现工具，通过 tools/call 提交 name 和 arguments；这与 OIS 管理 REST 资源不同。标准 Streamable HTTP 将消息 POST 到 MCP 入口，具体版本元数据、兼容及取消由 MCP 适配层处理，不在本文自创一套 MCP 协议。[MCP Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)、[MCP Transports](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)

消费侧 inputSchema 来自 OIS 的 input_schema。OIS 将 App 结果转为 MCP 工具结果，结构化内容可放 structuredContent，并提供文本表示方便客户端消费。本文不展示省略必需元数据的请求作为完整可发送的 MCP 帧；先选定客户端与协议兼容范围，再给出可运行示例。

OIS 对内只采用一条调用处理路径：Tool → 固定 App.Action/通道 → OIC → App。HTTP /exec 与 MCP 的差别主要在请求及结果封装，不增加选路、计划或作业服务。

## 9. OIC 接入协议的边界

OIC 主动连接 OIS 已确认，具体 HTTP、WebSocket 或其他载体未定，因此当前不把某个 GET Upgrade 路径写成已选接口。需要承载的逻辑交互如下；名称均为占位候选：

| 逻辑交互 | 方向 | 内容 |
| --- | --- | --- |
| 接入与认证 | OIC → OIS | 身份、固定通道及协议能力；候选登记契约另包含受授权约束的 Host 标识和机器元数据 |
| 能力登记/更新 | OIC → OIS | 已加载的 App、Action、本地描述/参数 Schema 及就绪状态，用于管理查询而不覆盖 Tool 定义 |
| 调用交付 | OIS → OIC | call_id、app、action、input 和系统生成的可信上下文 |
| 调用结果或错误 | OIC → OIS | 同一 call_id 对应的 result 或 error |
| 存活/断开 | 双方 | 更新通道可用状态，失联后不再发送新调用 |
| 当前请求取消，如选用 | OIS → OIC | 指定一次调用，不能直接等同于终止既有 Bash 执行 |

能力登记不把本地 Schema 升格为 OIS Tool 定义权威。OIC 接入不能修改消费权限或替调用者伪造身份。通道断开不保留待执行的离线队列，重新连接后重新登记能力；是否能继续已有 session 取决于原 App 实例是否存活和生命周期契约，不能仅凭通道名称推断已经恢复。

## 10. 首个 API 使用流程

1. 管理员准备管理、OIC 接入和 vMCP 消费凭据及 OIS 启动配置。
2. PUT /api/v1/channels/bash-local，创建固定通道定义。
3. PUT 两个 Tool 定义，固定到 bash-local 的两个 Bash Action。
4. PUT /api/v1/vmcps/workbench，暴露这两个 Tool。
5. 本机执行 oic start 和 oic load，等待通道在线且 Bash Action 就绪。
6. 使用消费凭据经 POST /api/v1/exec 或 /mcp/workbench 调用。
7. 长命令用 terminal_write_stdin 和同一 session_id 继续，最终取得退出结果。

文档与示例的可解析性检查不是功能验收。具体认证、配置权威、变更规则、运行协议和错误字段仍待采纳，讨论不自动转入实现或发布。
