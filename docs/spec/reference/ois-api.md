# OIS 接口参考

本文描述 beta 当前实现的 OIS HTTP、MCP 和 OIC 接入接口。新部署使用 Service 管理固定来源、访问规则和 MCP 发布。旧 Tool、vMCP 管理路由继续服务于兼容配置。[Service 契约与迁移](service.md) · [配置参考](configuration.md) · [App 运行协议](app-runtime.md)

第 7 节规定 `content.v1` 在 REST 和 MCP 中的成功回执，支持 Filesystem 等 App 返回原生图像。

OIS 管理通道和 Service，将一次成员调用交付到其固定的 `channel + app + action`。Host、App、Action 是 OIC 登记的只读视图，不提供通过 Host 选择执行位置、自动换目标、离线排队或失败重跑的接口。

## 1. 入口、访问控制与通用格式

| 入口 | 用途 | 访问控制来源 |
| --- | --- | --- |
| `GET /healthz` | OIS 存活检查 | 不要求 Token |
| `/api/v1/` 下的管理接口 | 配置与登记查询 | `admin.token_env` |
| `POST /api/v1/exec` | 调用指定 Service 内的成员 | 该 Service 的 `access` |
| `/mcp/{service_id}` | 已发布的 MCP 发现、调用和协议会话 | 该 Service 的 `access` |
| `GET /api/v1/oic/connect` | WebSocket 升级及 OIC 接入 | 目标通道的 `credential_env` 和 `expected_host_id` |

默认监听提供 HTTP/WS，不要求 Token。管理 `token_env`、通道 `credential_env`、Service `access.credential_env` 相互独立：省略或空字符串关闭对应鉴权；非空时要求 `Authorization: Bearer <token>` 与 OIS 进程中所引用环境变量的非空值一致。显式引用缺值不会降级匿名；管理员凭据缺值还会阻止启动。响应只包含环境变量名称，不返回凭据值。

Service 的 `access.require_https` 默认 `false`，与 Token 开关独立。设为 `true` 时必须存在已配置的 HTTPS 入口；MCP 和 REST 执行只接受 OIS 收到的真实 TLS 连接，HTTP 返回 `403 HTTPS_REQUIRED`，`Forwarded` 和 `X-Forwarded-Proto` 不能代替 TLS。这个开关不改变管理 API 或 OIC 通道的访问方式。

所有入口都校验存在的 `Origin`：允许配置的公共 HTTP/HTTPS origin 以及 `server.allowed_origins`，其他来源返回 `403 FORBIDDEN`。没有 `Origin` 的客户端可继续按对应入口鉴权。响应包含 `Cache-Control: no-store` 和 `X-Content-Type-Options: nosniff`。

### 1.1 JSON 与标识符

- HTTP JSON 请求正文和 OIC WebSocket 入站消息的上限分别为 1 MiB。一次正文或消息只接受一个 JSON 值；WebSocket 使用文本消息。
- 通道、Host、App、Action、Service 和成员发布名（包括旧 Tool、vMCP ID）的配置标识匹配 `[A-Za-z0-9][A-Za-z0-9_.-]{0,127}`。
- 管理 PUT 与 REST 执行外层拒绝未知字段。`arguments`、`input_schema` 等对象内部不按这个规则限制键名；OIS 不用 Tool Schema 对业务输入做严格类型门禁。
- 管理列表通常返回 `{"items": [...]}`，详情直接返回资源对象。无分页接口。
- JSON 错误通常为下列结构；没有分配调用标识时省略 `call_id`。不支持的方法可能仅返回空正文 `405`，未知路由也可能使用 HTTP 默认 `404`。

```json
{
  "call_id": "call_example",
  "error": {
    "code": "CALL_RESULT_UNKNOWN",
    "message": "OIC connection was lost after the call was accepted"
  }
}
```

错误对象支持可选 `details` 对象。客户端应依据 `code` 处理，不依赖英文 `message` 的固定文案。

## 2. 管理路由

以下路由使用管理访问控制；其中 Host、App、Action 只有查询。

| 方法 | 路径 | 返回或操作 |
| --- | --- | --- |
| GET | `/api/v1/status` | 服务与资源计数 |
| GET | `/api/v1/channels` | 通道列表 |
| GET、PUT、DELETE | `/api/v1/channels/{channel_id}` | 通道详情、创建/替换、删除 |
| GET | `/api/v1/channels/{channel_id}/apps` | 通道的 App 登记列表 |
| GET | `/api/v1/channels/{channel_id}/apps/{app}/actions` | 本地 App 的 Action 列表 |
| GET | `/api/v1/channels/{channel_id}/apps/{app}/actions/{action}` | 一个 Action 的登记详情 |
| GET | `/api/v1/hosts` | 已登记 Host 列表 |
| GET | `/api/v1/hosts/{host_id}` | Host 详情 |
| GET | `/api/v1/hosts/{host_id}/apps` | Host 关联通道中的 App 列表 |
| GET | `/api/v1/hosts/{host_id}/actions` | Host 关联通道中的 Action 列表 |
| GET | `/api/v1/actions` | Action 列表；可用 `host_id`、`channel`、`app` 查询参数组合做精确筛选 |
| GET | `/api/v1/services` | Service 列表及其有序成员、访问和发布配置 |
| GET、PUT、DELETE | `/api/v1/services/{id}` | 读取、创建、完整替换或删除 Service。见 [Service 请求示例](service.md) |
| GET | `/api/v1/tools` | 兼容管理的全局 Tool 列表 |
| GET、PUT、DELETE | `/api/v1/tools/{tool_name}` | Tool 详情、创建/替换、删除 |
| GET | `/api/v1/vmcps` | 兼容管理的 vMCP 列表 |
| GET、PUT、DELETE | `/api/v1/vmcps/{vmcp_id}` | vMCP 详情、创建/替换、删除 |

没有 App 详情独立路由、远程 `load/unload`、Host/Action 写接口或任意 Action 直调接口。App 的加载和卸载由对应机器的 OIC CLI 完成。

### 2.1 创建、替换和持久化

PUT 的资源 ID 只在路径中提供，正文是完整配置，不做部分合并。创建返回 `201`，替换返回 `200`；省略可选字段会将其恢复为空值。DELETE 成功返回 `204` 且无正文。GET 中的 `online`、`available`、`endpoint` 等派生字段不能提交到 PUT。

首次启动且持久数据文件不存在时，OIS 从 YAML 导入 `channels`、`services` 及兼容配置 `tools/vmcps`。初始导入或打开 v1 持久文件时展开旧 vMCP，保存 v2 状态。之后使用已保存的资源配置，管理 API 写入成功后才替换内存配置。监听、TLS、管理凭据引用等启动设置仍来自 YAML。[Service 迁移规则](service.md)

已接受的调用保留当次来源与可信访问身份。修改 Service 的成员或访问规则、删除 Service，不撤销已接受调用；新请求重新检查当前配置。OIS 不维护可恢复的调用结果历史。

### 2.2 通道

`PUT /api/v1/channels/linux-151`：

```json
{
  "description": "Linux Bash",
  "expected_host_id": "linux-151",
  "credential_env": ""
}
```

| 字段 | 规则 |
| --- | --- |
| `description` | 可选字符串 |
| `expected_host_id` | 必填；OIC 首帧的 Host ID 必须匹配 |
| `credential_env` | 可选；空值允许匿名接入，非空启用该通道 Token |

PUT 响应增加 `id`；GET 另增加 `online`。`online=true` 表示已有活动连接，不代表已完成 App 登记。

同通道只允许一个活动 OIC。在线时不能修改 `expected_host_id` 或 `credential_env`，否则返回 `409 CHANNEL_CONNECTED`；可以修改描述。离线修改这些绑定字段会清除该通道旧登记快照。

被 Service 成员或旧 Tool 引用的通道不能删除，返回 `409 RESOURCE_IN_USE`；未被引用但仍在线时返回 `409 CHANNEL_CONNECTED`。没有级联删除或替换活动连接的选项。匿名模式下 ID 匹配只是配置关系检查，不等于机器身份认证。

### 2.3 兼容管理的 Tool

`PUT /api/v1/tools/linux_exec_command`：

```json
{
  "description": "在 Linux Bash 中启动命令",
  "input_schema": {
    "type": "object",
    "properties": {"cmd": {"type": "string"}},
    "required": ["cmd"]
  },
  "source": {
    "channel": "linux-151",
    "app": "bash",
    "action": "exec_command"
  }
}
```

| 字段 | 规则 |
| --- | --- |
| `description` | 面向消费者的工具说明 |
| `input_schema` | 必须是非 null 对象；作为 MCP `inputSchema` 发布 |
| `source.channel` | 必须引用已有通道 |
| `source.app`、`source.action` | 固定 App/Action 标识；允许对应实现尚未在线登记 |

PUT 响应增加 `name`；GET 另增加 `available`。可用性要求固定通道在线、完成本次连接登记、对应 App 与 Action 都就绪。

Tool 创建后不允许原位修改任一 `source` 字段，返回 `409 BINDING_IMMUTABLE`；更换实现位置需创建另一个 Tool，再更新 vMCP 引用。描述和消费者 Schema 可以完整替换。被任一 vMCP 引用的 Tool 不能删除，返回 `409 RESOURCE_IN_USE`。

OIS 不自动用 OIC 登记改写 Tool，也不要求两侧 Schema 相等。当前只要求配置中的 `input_schema` 是对象，不进行完整 JSON Schema 元规范检查；实际输入适配和参数错误由本地 OIC/Action 处理。

### 2.4 兼容管理的 vMCP

`PUT /api/v1/vmcps/workbench`：

```json
{
  "tools": [
    "linux_exec_command",
    "linux_write_stdin",
    "windows_exec_command",
    "windows_write_stdin"
  ],
  "access": {
    "credential_env": "",
    "require_https": false
  }
}
```

`tools` 是已有 Tool 名称的有序列表，不能重复；允许空列表。`access` 可省略，等价于无 Token、无 HTTPS 强制要求。PUT 与 GET 响应增加 `id` 和 `endpoint`。

`endpoint` 为根 `public_url + /mcp/{id}`；`require_https=true` 时选择配置的 HTTPS 公共地址。未配置 HTTPS 入口时创建或更新这种 vMCP 返回 `400 INVALID_REQUEST`，已有持久配置也不能在失去 HTTPS 入口后静默降级启动。`require_https=false` 并不禁止客户端使用已有 HTTPS 入口。

vMCP 仅暴露其列表中的 Tool。没有单独的消费者账号、Token 签发或访问角色 CRUD。无 Token 时该 vMCP 的所有调用共享 `anonymous:<vmcp_id>` 身份，随意提交不同 Bearer 不会产生不同用户。启用 Token 后可信身份由 vMCP 与凭据引用确定，不由业务参数提供。

### 2.5 Host、App、Action 登记视图

Host 由通道接入和 OIC 登记产生；相同 `host_id` 的多条通道可在管理视图中归组，不参与执行路由。不同通道或 App 下的同名 Action 不合并，完整定位为 `(channel, app, action)`。

| 视图 | 主要字段 |
| --- | --- |
| Host | `id, name, os, arch, online, channels`；`channels` 项为 `{id, online}` |
| App | `host_id, channel, app, ready, available, stale, actions` |
| Action | `host_id, channel, app, action, description, input_schema, ready, available, stale` |

App 内嵌的 `actions` 使用登记格式 `{name, description?, input_schema, ready}`，不包含外层管理 Action 的 `available/stale`。查询 `/api/v1/channels/{id}/apps` 还返回外层 `channel` 与 `online`。

- App `ready` 是最后登记的本地状态；独立 Action 视图的 `ready` 同时要求 App 和 Action 的登记状态为真。
- `available` 还要求当前活动连接已经重新登记；不能只靠历史 `ready` 判断可调用。
- `stale=true` 表示没有已登记的当前连接。断连保留最后快照供查询；重连但尚未登记时仍为 stale。
- 每次 `register` 全量替换本通道 App/Action 快照，移除的能力不继续可用。
- 快照只存在 OIS 内存中，重启后等待 OIC 重新接入与登记，不由持久配置恢复。尚未登记的 Host 名称、OS、架构可为空。

### 2.6 存活与状态

`GET /healthz` 返回 `200 {"status":"ok"}`，只说明 OIS 可以响应，不保证任何执行依赖可用。

`GET /api/v1/status` 示例：

```json
{
  "status": "running",
  "hosts": {"registered": 2, "online": 2},
  "channels": {"configured": 2, "online": 2},
  "apps": {"registered": 2, "available": 2},
  "actions": {"registered": 4, "available": 4},
  "tools": {"configured": 4, "available": 4},
  "vmcps": {"configured": 1},
  "services": {"configured": 1}
}
```

`registered` 统计当前内存快照，包含离线快照；Host 按 ID 去重，App/Action 按通道登记计数。这些不是执行次数，查询结果也不能保证下一次调用时仍在线。OIS 关闭期间请求返回 `503 UNAVAILABLE`。

## 3. REST 执行

`POST /api/v1/exec` 使用目标 Service 的访问规则。正文包含非空 `service`、成员发布名 `action` 和对象 `arguments`。旧客户端可以继续发送 `vmcp/tool` 字段对。混用两组非空字段时返回 `400 INVALID_REQUEST`。

```json
{
  "service": "workbench",
  "action": "linux_exec_command",
  "arguments": {"cmd": "printf 'hello\\n'", "yield_time_ms": 1000}
}
```

OIS 检查 Service 可访问、发布名属于该 Service，且固定来源的 Action 当前可用，然后分配 `call_id` 并交付。消费者不提交路由 ID、runtime 或可信身份；业务参数中即使出现同名字段，也不能覆盖 Tool 来源或内部 `context`。

成功返回 `200`，App 结果保留在 `result` 中：

```json
{
  "call_id": "call_example",
  "result": {"output": "hello\n", "exit_code": 0}
}
```

仍在运行的命令也使用 `200`，其 App 结果可以是 `{"output":"","session_id":"..."}`。命令非零 `exit_code` 仍是正常执行结果。`call_id` 标识一次交付，`session_id` 是 App 持续执行句柄，两者不同；OIS 不解释或拥有 App 会话。

REST 不提供公开取消端点。请求使用独立的服务期限，默认 60 秒；HTTP 连接意外断开不视为用户取消。服务期限到达时 OIS 尝试发送内部 `cancel`，发送成功后等待执行方最多 5 秒确认结果；确认可能是执行结果或取消错误，不能仅凭发出取消就宣称进程已停止。没有确认时返回 `CALL_RESULT_UNKNOWN`。此期限与 App 的最长执行期限、每次读取等待窗口分别管理。

## 4. MCP 入口

协议版本为 `2025-11-25`，端点 `/mcp/{service_id}`，由 `publish.mcp: true` 启用。当前实现采用 Streamable HTTP 的 POST JSON 响应方式，不开放独立 GET 事件流。MCP 协议会话与命令 `session_id` 分属不同层。

### 4.1 HTTP 与初始化

POST 请求头：

```http
Content-Type: application/json
Accept: application/json, text/event-stream
```

启用 Token 时另带 Bearer。`Content-Type` 错误返回 `415`；`Accept` 未同时接受这两个媒体类型返回 `406`。请求正文是单个 JSON-RPC 2.0 对象，不支持批量数组；请求 `id` 为字符串或 64 位整数，通知省略 `id`。

首次请求：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": {},
    "clientInfo": {"name": "example-client", "version": "1"}
  }
}
```

成功响应头包含 `MCP-Session-Id`，结果为：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-11-25",
    "capabilities": {"tools": {"listChanged": false}},
    "serverInfo": {"name": "loom-oi", "version": "beta"}
  }
}
```

客户端检查协商版本后，后续 POST 带 `MCP-Session-Id` 和 `MCP-Protocol-Version: 2025-11-25`，先发送 `notifications/initialized`，再发现和调用 Tool。通知返回 `202` 无正文。提供其他协议版本头返回 `400 INVALID_REQUEST`。

### 4.2 支持的方法

| 方法 | 当前行为 |
| --- | --- |
| `initialize` | 建立协议会话并返回服务端支持版本 |
| `notifications/initialized` | 标记该会话已初始化 |
| `ping` | 已建立会话中返回空对象；无需先完成 initialized 通知 |
| `tools/list` | 返回该 Service 当前配置的全部成员，无分页 |
| `tools/call` | 经与 REST 相同的固定绑定执行入口交付 |
| `notifications/cancelled` | 使用 `params.requestId` 取消同一协议会话内仍活动的请求 |

`tools/list` 中每项为 `{name, description, inputSchema}`，顺序与 Service 成员配置一致。离线工具仍在列表中，调用时检查可用性；不会向消费者泄露通道、App、凭据等管理配置。服务端不发送 `tools/list_changed`，客户端需再次查询才能看到配置更新。

`tools/call` 示例：

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "linux_exec_command",
    "arguments": {"cmd": "printf 'hello\\n'"}
  }
}
```

`name` 必填，省略或 null 的 `arguments` 作为空对象处理；非对象参数不接受。调用仍由本地 Action 做业务参数适配。成功响应示例：

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "isError": false,
    "_meta": {"call_id": "call_example"},
    "content": [{"type": "text", "text": "{\"output\":\"hello\\n\",\"exit_code\":0}"}],
    "structuredContent": {"output": "hello\n", "exit_code": 0}
  }
}
```

`structuredContent` 直接承载 App 结果对象，没有额外 `result` 包装；`call_id` 在 `_meta` 中。非对象 App 结果只通过 `content` 中的 JSON 文本返回。执行错误使用 `isError:true`，其文本与 `structuredContent` 内容为 `{"error":{"code":"...","message":"..."}}`；已分配调用标识时保留 `_meta.call_id`。命令非零退出不自动变成 MCP 工具错误。

### 4.3 协议会话、取消与限制

协议会话绑定 Service 和 OIS 计算的访问身份。缺少会话头返回 `400`，未知会话返回 `404`，跨 Service/身份复用返回 `403`。同一会话的活动 `tools/call` 不能重复使用同一请求 ID；不同会话可独立使用同一 ID。

`DELETE /mcp/{service_id}` 携带有效会话头和访问凭据，可关闭协议会话并取消其中活动的 MCP 请求，成功返回 `204`。这不等于杀死此前已返回 `session_id` 的全部 App 执行。协议会话仅存于内存，当前上限为 4096，没有自动过期回收；客户端使用 DELETE 释放，OIS 重启后需重新初始化。

显式取消通知定位同会话、同身份的活动请求，未知或已完成的请求被忽略。取消映射为内部 `cancel`，具体执行阶段行为由 App 负责；取消通知返回 `202`，被取消的 `tools/call` HTTP 请求以 `204` 结束，不再返回正常 JSON-RPC 工具结果。HTTP 流意外断开不等同取消，也不自动重试。

当前不提供 GET/SSE、断点续传、事件回放、Tasks、资源或提示词接口。GET 和其他不支持的 HTTP 方法返回 `405`，`Allow: POST, DELETE`；未知通知忽略并返回 `202`，未知请求方法返回 JSON-RPC `-32601`。

JSON-RPC 错误使用 HTTP `200`：解析错误 `-32700`，请求格式/初始化顺序/活动 ID 冲突 `-32600`，方法不存在 `-32601`，工具调用参数或暴露范围错误 `-32602`。HTTP 入口鉴权、TLS、媒体类型和会话错误使用相应 HTTP 状态；执行失败则使用上述 `isError` 工具结果。

## 5. OIC 接入与通道消息

OIC 主动使用 WS，或在已配置 HTTPS 监听上使用 WSS，连接 `/api/v1/oic/connect`。升级握手沿用 Origin 检查与通道凭据检查；随后用首帧确定并重新核对具体通道绑定。

### 5.1 绑定

OIC 在连接后 10 秒内发送：

```json
{"type":"bind","protocol":1,"host_id":"linux-151","channel_id":"linux-151"}
```

OIS 检查通道存在、Host ID 匹配、该通道启用时的 Token 有效，以及没有另一条活动连接。成功返回：

```json
{"type":"bound","protocol":1,"host_id":"linux-151","channel_id":"linux-151"}
```

当前还接受首帧 `type:"hello"` 或省略 `type`，以及省略 `protocol`；新客户端使用明确的 `bind` 和协议 `1`。不支持的协议返回 `UNSUPPORTED_PROTOCOL`，绑定不符返回 `FORBIDDEN`，重复活动连接返回 `CHANNEL_CONNECTED`，随后关闭连接。未通过握手凭据检查的连接在升级前返回 `401`。

### 5.2 全量能力登记

绑定后，OIC 发送本机当前 App 列表：

```json
{
  "type": "register",
  "host": {"id":"linux-151","name":"linux","os":"linux","arch":"amd64"},
  "apps": [
    {
      "app": "bash",
      "ready": true,
      "actions": [
        {
          "name": "exec_command",
          "description": "启动 Bash 命令",
          "input_schema": {"type":"object"},
          "ready": true
        },
        {
          "name": "write_stdin",
          "input_schema": {"type":"object"},
          "ready": true
        }
      ]
    }
  ]
}
```

成功返回 `{"type":"registered"}`。`host.id` 必须与绑定一致；省略 `host` 保留已有元信息。App 名在通道内唯一，Action 名在 App 内唯一，每个 Action 必须提供非 null 的 `input_schema` 对象；`description` 可省略。省略或清空 `apps` 会清空本通道能力快照。登记不创建 Tool，不强制比较消费者与本地 Schema。

App 加载、卸载或就绪状态变化时使用新的全量登记；OIS 不通过此协议远程要求 OIC 安装或加载 App。

### 5.3 调用与结果

OIS → OIC：

```json
{
  "type": "invoke",
  "call_id": "call_example",
  "app": "bash",
  "action": "exec_command",
  "input": {"cmd":"printf 'hello\\n'"},
  "context": {"access_identity":"anonymous:workbench","vmcp_id":"workbench"}
}
```

`app/action` 来自 Service 成员的固定来源，`input` 来自消费者参数。OIS 生成可信 `context`，在协议 1 的 `vmcp_id` 字段中传递 Service ID。OIC 按本地声明适配后交给实现；OIS 不下发 shell 路径、worker 命令或客户端自报身份。

OIC → OIS 的两种终结消息：

```json
{"type":"result","call_id":"call_example","result":{"output":"hello\n","exit_code":0}}
```

```json
{"type":"error","call_id":"call_example","error":{"code":"INVALID_ARGUMENT","message":"invalid workdir"}}
```

`result` 是有效 JSON；`error` 使用共同错误结构。每次调用只接受一次终结结果，必须来自当次调用绑定的活动连接。未知、已结束或不属于该连接的 `call_id` 消息不结算其他调用；旧连接不能覆盖新登记。结果/错误缺少 `call_id` 或发送不支持的消息类型会关闭连接。

取消使用 OIS → OIC `{"type":"cancel","call_id":"call_example"}`；终结仍由原调用的 result/error 消息完成，没有“已发送即已停止”的确认语义。对 `exec_command` 与 `write_stdin` 的不同执行阶段处理见 [App 运行时参考](app-runtime.md)。

### 5.4 心跳与断连

双方可发送 `{"type":"ping"}`，对方以 `{"type":"pong"}` 响应。OIS 默认每 10 秒发送应用层 ping，接收等待期限默认 30 秒；这些值由启动配置调整，与命令是否有输出无关。

断连后固定通道立即不可用，旧快照保留但标记 stale。已经交付、尚未结算的调用报告 `CALL_RESULT_UNKNOWN`，不会转发到另一连接、自动重发或排入队列。若原 App 实例仍存活、访问身份仍有效，重连并登记后可以使用此前已知的 App `session_id` 继续调用；失去首次响应而未知句柄时，OIS 没有结果找回 API。

## 6. HTTP 错误与执行边界

以下为管理/REST 的主要映射；OIC 通过消息中的 `error.code` 表达错误，MCP 工具执行错误按第 4 节包装。

| HTTP | 错误码 | 含义 |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | JSON/配置/引用或 HTTPS 配置前提无效 |
| 401 | `UNAUTHENTICATED` | 已启用的凭据缺失、错误或引用缺值 |
| 403 | `FORBIDDEN`、`HTTPS_REQUIRED` | 来源/身份范围不符，或该 Service 要求真实 TLS |
| 404 | `NOT_FOUND` | 资源不存在，或发布名不在指定 Service 中 |
| 409 | `RESOURCE_IN_USE` | 删除仍被引用的通道或 Tool |
| 409 | `BINDING_IMMUTABLE` | 试图原位更换 Tool 来源 |
| 409 | `CHANNEL_CONNECTED` | 活动通道的身份变更、删除或重复接入冲突 |
| 409 | `SESSION_BUSY`、`CALL_ID_CONFLICT` | 执行方报告的并发/调用冲突 |
| 422 | 其他执行方业务错误码，例如 `CANCELLED` | 参数、会话或执行状态不满足要求 |
| 500 | `INTERNAL_ERROR` | 例如持久配置写入失败 |
| 502 | `EXECUTION_ERROR` | OIC 返回无法使用的结果/错误等执行交付异常 |
| 503 | `CHANNEL_UNAVAILABLE`、`APP_UNAVAILABLE` | 固定通道或 Action 不可用，未交付此次调用 |
| 503 | `CAPACITY_EXCEEDED`、`UNAVAILABLE` | 容量受限或 OIS 正在关闭 |
| 504 | `CALL_RESULT_UNKNOWN` | 交付或取消结果无法确认 |

`CALL_RESULT_UNKNOWN` 不证明命令未执行或已停止；调用方不能将其当成安全重试许可。执行错误码来自 OIC/App 时可透传，未单独映射的错误码使用 `422`。没有幂等键、自动去重重试、会话枚举、历史输出回放或公开强制 kill API。

## 7. 扩展版 `content.v1` 对外回执

`POST /api/v1/exec` 和 MCP `tools/call` 继续使用现有请求体、固定来源、Service 鉴权和调用期限。OIS 仅在连接已协商 `content.v1` 且本次绑定的 Action 已声明该格式时接受带此标记的成功结果。`tools/list` 的输入 Schema 不因此增加调用参数。能力在重连后重新协商，OIS 仍须检查当次来源和 `call_id`。两跳协商、混合版本与字节预算见 [App 运行时第 7 节](app-runtime.md)。

| 执行结果 | REST | MCP `tools/call` 的 JSON-RPC `result` |
| --- | --- | --- |
| 普通 `json` | `200`，`{call_id, result}` | `content` 含序列化 JSON 的文本块，对象另放入 `structuredContent`，`isError:false`。业务 JSON 恰好有 `content` 或 `type:"image"` 也走此路。 |
| 已协商 `content.v1` | `200`，`{call_id, result_format:"content.v1", result}`。`result` 含内容块与本次预览的元信息。 | 直接使用 worker 结果的 `content` 数组和 `structuredContent` 元信息对象，附 `isError:false` 和 `_meta.call_id`。Base64 只在原生 image 块出现一次。 |
| 已确认执行失败 | 沿用第 6 节的状态码和 `{error, call_id?}`。例如选区超限为 `422 IMAGE_PREVIEW_TOO_LARGE`。 | 沿用第 4 节的 `isError:true` 错误工具结果，不附图像。分配了调用标识时保留 `_meta.call_id`。 |

例如 `view_image` 复用一张 1×1 灰度 PNG 的原编码时，以下是完整的 REST 成功回执。对同一结果，MCP 客户端可直接从 `result.content` 中选 `type:"image"`，按块的 `mimeType` 使用 `data`。两种入口都在本次响应中交付像素，OIS 无需保存图片或签发下载 URL。

```json
{
  "call_id": "call_demo",
  "result_format": "content.v1",
  "result": {
    "content": [
      {"type": "text", "text": "1×1 PNG"},
      {"type": "image", "mimeType": "image/png", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNgAAAAAgABSK+kcQAAAABJRU5ErkJggg=="}
    ],
    "structuredContent": {
      "path": "images/pixel.png", "source_mime": "image/png",
      "source_width": 1, "source_height": 1, "orientation_applied": false,
      "crop": null, "preview_width": 1, "preview_height": 1,
      "resized": false, "reencoded": false, "mime_type": "image/png", "encoded_bytes": 67
    }
  }
}
```

OIS 在登记表明图像 Action 不可用时沿用 `503 APP_UNAVAILABLE`，在派发前结束调用。若 OIC 在复核时发现格式不兼容，则以 `RESULT_FORMAT_UNSUPPORTED` 错误帧结束。已交付但在断连后无法确认的结果仍为 `504 CALL_RESULT_UNKNOWN`。业务错误和传输中断均不能触发换目标或自动重放。

## 8. beta 实现对应

- [路由、鉴权和通用响应](../../../internal/ois/server.go)
- [管理资源与登记视图](../../../internal/ois/admin.go)
- [Service 视图、成员契约与兼容管理](../../../internal/ois/service.go)
- [持久配置与引用校验](../../../internal/ois/config.go)
- [REST 与内部调用交付](../../../internal/ois/invoke.go)
- [MCP 处理](../../../internal/ois/mcp.go)
- [OIC 通道处理](../../../internal/ois/connection.go)
- [共享消息结构与版本](../../../internal/protocol/protocol.go)
