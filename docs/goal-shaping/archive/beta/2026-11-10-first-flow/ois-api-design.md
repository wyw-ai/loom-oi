# loom-oi beta：OIS 首版 API 冻结规格

> 本轮讨论已于 2026-11-10 结束。本文作为归档快照保留；持续维护的当前契约见[项目规范](../../../../spec/README.md)，讨论结束与取舍见[笔记](notes.md)。

状态：首版 API 规格，包含 2026-11-10 新确认的默认 HTTP/WS、无 Token 与可选访问配置。本文全部管理、查询、执行和接入端点均纳入首版。[当前共识](../../../current/beta/design.md)和[验收规格](../../../current/beta/acceptance.md)为权威，执行细节见[Action 详稿](bash-action-design.md)，部署选项见[访问配置](../../../../guides/access-configuration.md)。此前 HTTPS/独立 Token 下的验收是历史启用配置记录，新默认与迁移另记实际验证结果。

首版链路为 Agent → vMCP → Tool → Action → 通道 → OIC → oiapp → cmd。每个 Tool 固定绑定一个 App.Action 和通道；消费者不能通过输入改变执行位置。OIS 管理定义及调用交付，OIC 加载本地实现，bash/powershell App 管理各自实际进程和持续执行状态。两个 Host 不增加执行选路层，Windows 验收端通过一个 workbench vMCP 调用两组固定工具。

## 1. 入口与共同约定

| 入口 | 使用者 | 用途 |
| --- | --- | --- |
| /api/v1/* 的配置与状态接口 | 管理员 | 配置通道、Tool、vMCP，查看 Host、App、Action 登记及运行状态 |
| POST /api/v1/exec | 脚本或程序消费者 | 在指定 vMCP 范围内调用已配置 Tool |
| /mcp/{vmcp_id} | MCP 客户端 | 发现并调用该 vMCP 暴露的工具 |
| WS /api/v1/oic/connect；可选 WSS | OIC | 主动建立通道、登记本地能力、接收调用并返回结果 |

默认公网入口为 http://121.199.175.151:25100；OIS 与 Linux OIC 位于 151，Windows OIC 位于本机。默认 REST/MCP 使用 HTTP，OIC 使用 ws://121.199.175.151:25100/api/v1/oic/connect，不需要 Token 或证书。server.https 可额外提供共享 HTTPS/WSS 监听，默认附加端口为 25101；证书由管理员提供并由 CA 签发，客户端验证证书及目标名称，OIS 不自动申请、签发或续期。只使用显式配置的端口，不占用整个 25000–27000 范围。

REST 使用 JSON。默认不要求 Authorization；admin.token_env、通道 credential_env、vMCP access.credential_env 分别为空时关闭对应 Token 鉴权，非空时启用并要求 Authorization: Bearer <token>。显式引用的环境变量缺失或为空必须拒绝，不能转为匿名；已启用的管理、消费和通道凭据不互相替代。匿名同 vMCP 共享 anonymous:<vmcp_id> 身份，任意 Bearer 不生成另一身份；业务输入不能自报可信身份。Windows PowerShell 7 脚本按所选配置完成 MCP，不增加 OAuth 或账号系统。

管理接口的 PUT 按路径标识创建或完整替换资源，首次创建返回 201，替换返回 200；不支持隐式部分合并。正文不另外提交资源 ID，在线、就绪、endpoint 等只读状态不能由 PUT 设置。GET 详情返回资源，列表返回 {"items": [...]}；DELETE 成功返回 204 且无正文。PUT/GET 不回显凭据明文，credential_env 仅表示 OIS 进程环境中的凭据引用名称。首版不增加账号、令牌签发或密钥存储管理 API。

配置权威是一份 OIS 持久配置：首次初始化时从 YAML 导入通道、Tool、vMCP 定义，此后管理 API 更新该配置，正常重启不重新用旧 YAML 覆盖。监听地址、TLS 和存储位置等进程启动参数仍由启动配置提供。首版不提供第二套持续覆盖 API 数据的文件编辑入口；初始化导入与持久化实现必须遵守这一唯一权威。

根 server.listen/public_url 默认提供 HTTP。server.https 的 listen/public_url/cert_file/key_file 定义额外共享 HTTPS 入口；旧 tls_cert/tls_key 继续表示根监听为 HTTPS-only，不能与新块混用。各 vMCP 通过 access.require_https 选择是否强制 HTTPS，默认 false。设为 true 必须有 HTTPS 入口，初始化、API 更新和持久配置重载都检查该条件；不能因移除 HTTPS 配置而静默降级。

## 2. 运行状态

| 方法 | 路径 | 行为与权限 |
| --- | --- | --- |
| GET | /healthz | 最小存活检查，只返回 OIS 服务可响应，不表示 OIC 或 App 可执行 |
| GET | /api/v1/status | 管理员查看 OIS 运行状态及配置/在线资源数量 |

status 响应示例：

```json
{
  "status": "running",
  "hosts": {"registered": 2, "online": 2},
  "channels": {"configured": 2, "online": 2},
  "apps": {"registered": 2, "available": 2},
  "actions": {"registered": 4, "available": 4},
  "tools": {"configured": 4, "available": 4},
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

Linux 通道使用 PUT /api/v1/channels/linux-151：

```json
{
  "description": "151 Linux Bash 接入",
  "expected_host_id": "linux-151",
  "credential_env": ""
}
```

Windows 通道使用 PUT /api/v1/channels/windows-local：

```json
{
  "description": "本机 Windows PowerShell 7 接入",
  "expected_host_id": "windows-local",
  "credential_env": ""
}
```

channel_id 取自路径。通道不指定待拨入机器地址，两个 OIC 均主动连接；首帧 channel_id、host_id 必须匹配已配置通道及 expected_host_id。credential_env 为空表示匿名接入；显式填写变量名才要求握手凭据，并在绑定时再次核对该通道，避免匿名通道绕过其他通道的 Token。匿名 ID 匹配不提供机器身份认证。同一通道拒绝重复活动连接，失效连接关闭后才允许重连；旧连接消息不能覆盖新登记。

GET /api/v1/channels/linux-151/apps 示例：

```json
{
  "channel": "linux-151",
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

Windows 通道的同一接口返回 app 为 powershell，并包含 exec_command、write_stdin 两个 Action。两台 OIC 的登记分别保留，不按动作同名合并。

通道被 Tool 引用时删除返回 409 RESOURCE_IN_USE，不级联删除工具。活动连接期间修改 expected_host_id、接入凭据或删除通道返回 409 CHANNEL_CONNECTED，先由管理员停止对应 OIC，再更新配置并按需重新连接；若删除同时受引用限制，先报告 RESOURCE_IN_USE。删除配置本身不承担远程卸载 App 或终止命令进程的职责。

### Host 管理查询

Host 是提供能力的实际宿主机，通道是到 OIC 的通信接入关系。首版不做多 Host 路由，不意味着管理侧不能查看已登记机器和其能力。以下查询只用于管理和配置检查，不向 Agent 增加目标发现 Tool，也不改变 Tool 的固定 source。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/hosts | 列出已登记 Host 及接入状态 |
| GET | /api/v1/hosts/{host_id} | 查看 Host 信息及关联通道 |
| GET | /api/v1/hosts/{host_id}/apps | 查看该 Host 登记的 App；每项保留 channel |
| GET | /api/v1/hosts/{host_id}/actions | 查看该 Host 登记的 Action；每项保留 channel、app 和 action |

GET /api/v1/hosts/linux-151 示例：

```json
{
  "id": "linux-151",
  "name": "151 Linux",
  "os": "linux",
  "arch": "x86_64",
  "online": true,
  "channels": [
    {"id": "linux-151", "online": true}
  ]
}
```

Host ID 在 OIC 配置中显式保存：Linux 为 host.id=linux-151、channel.id=linux-151，Windows 对应 windows-local。expected_host_id 固定接入关系，接入通过当前通道的可选 Token 策略后登记元数据；名称、系统和架构不是硬件身份认证。重新安装需保留或显式重配标识，不自动归并、替换身份或迁移 session。

Host.online 仅表示存在当前可用的关联 OIC 通道，不表示机器电源状态或全部 Action 就绪。channels 数组展示已授权接入关系；本次验收每个 Host 对应一条通道，不实现硬件身份识别、自动归并、迁移或选路。调用仍直接使用 Tool 已配置的 channel，Host ID 不进入消费者的业务参数。

Host 由 OIC 接入登记，首版仅提供上表 GET 查询，不提供用于伪造上线状态的 POST/PUT，也不以 DELETE Host 记录实现关机、停止 OIC 或清除 App。独立的 Host 预登记、备注编辑和历史记录清理不纳入本版 API。

### Action 管理查询

Action 的名称、描述和本地参数 Schema 来自 App YAML，由 OIC 加载后登记；OIS 提供明确的查询入口。它与 OIS 自己维护的 Tool 消费定义分开，不增加第二份可独立修改的本地 Action 定义。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/actions | 列出 Action 登记项，支持可选 host_id、channel、app 过滤 |
| GET | /api/v1/channels/{channel_id}/apps/{app_code}/actions | 查看某个通道中一个 App 的 Action 列表 |
| GET | /api/v1/channels/{channel_id}/apps/{app_code}/actions/{action_name} | 查看具体 Action 的本地描述、参数 Schema 与可用状态 |

例如 GET /api/v1/actions?host_id=linux-151&app=bash，或 GET /api/v1/actions?host_id=windows-local&app=powershell；过滤条件是管理员的查询范围，不进入 /exec 参数，也不用于自动选择执行目标。列表各项保留来源；同名 Action 不去重合并为一个可路由资源。

GET /api/v1/channels/linux-151/apps/bash/actions/exec_command 示例：

```json
{
  "host_id": "linux-151",
  "channel": "linux-151",
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

首版通过 (channel, app, action) 定位一项登记，不引入独立的全局 action_id，也不允许同一通道下同一 App code 的多个活动实例含混登记。这个配置定位不是运行实例身份；worker 重启后即使三者相同，旧 session 也不因此恢复。

ready 表示 OIC 上报的本地就绪状态；available 要求通道在线、登记属于当前连接且入口就绪。如果断连后保留上一轮登记，必须设置 stale=true、available=false，ready 只能解释为上次观测值。重新连接后的能力必须重新登记。正常卸载或移除 Action 后从当前清单撤下，详情返回 404；已有 Tool 定义保留但显示不可用。

OIS 管理 API 不通过 PUT/PATCH 修改这些登记项的本地 Schema、handler 或 runtime。相同 App 的相同有效定义重复 load 返回已有状态；定义改变须先在 OIC 卸载再加载，不隐式热替换。停用由本地 OIC/App 管理，不用删除查询记录替代实际停用。消费描述修改使用 PUT /api/v1/tools/{tool_name}，不覆盖本地 Schema。

Host 与 Action 的上述只读端点全部纳入首版管理验收。它们补全管理视图，执行链继续保持 Agent → vMCP → Tool → Action → 固定通道 → OIC → oiapp → cmd。

## 4. Tool 定义与固定绑定

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /api/v1/tools | 列出工具定义及可用状态，管理员使用 |
| GET | /api/v1/tools/{tool_name} | 查看工具定义及固定绑定 |
| PUT | /api/v1/tools/{tool_name} | 创建或替换工具定义 |
| DELETE | /api/v1/tools/{tool_name} | 删除未被 vMCP 引用的工具 |

workbench 固定包含以下四个 Tool。消费者选择 Tool 就确定执行环境，不另外提交 Host、通道或 shell 参数：

| Tool | source.channel | source.app | source.action | cmd 语法 |
| --- | --- | --- | --- | --- |
| linux_exec_command | linux-151 | bash | exec_command | Linux Bash |
| linux_write_stdin | linux-151 | bash | write_stdin | 接续原 Bash 执行 |
| windows_exec_command | windows-local | powershell | exec_command | Windows PowerShell 7 |
| windows_write_stdin | windows-local | powershell | write_stdin | 接续原 PowerShell 执行 |

Linux 启动工具 PUT /api/v1/tools/linux_exec_command：

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
    "channel": "linux-151",
    "app": "bash",
    "action": "exec_command"
  }
}
```

Linux 接续工具 PUT /api/v1/tools/linux_write_stdin：

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
    "channel": "linux-151",
    "app": "bash",
    "action": "write_stdin"
  }
}
```

Windows 启动工具 PUT /api/v1/tools/windows_exec_command：

```json
{
  "description": "启动一次 Windows PowerShell 7 执行，返回输出和执行句柄或退出码",
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
    "channel": "windows-local",
    "app": "powershell",
    "action": "exec_command"
  }
}
```

Windows 接续工具 PUT /api/v1/tools/windows_write_stdin：

```json
{
  "description": "向已有 Windows PowerShell 7 执行输入，或等待并读取后续输出",
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
    "channel": "windows-local",
    "app": "powershell",
    "action": "write_stdin"
  }
}
```

source.channel 必须引用已有通道配置；App.Action 暂未登记或通道离线不阻止保存工具定义，但工具状态显示不可用。OIS 可检查自身定义的基本结构及引用，不以 OIS Schema 与本地 Action Schema 相等作为保存或加载条件；可识别差异最多提示。

source 是单个固定对象，不接受目标数组、优先级或负载均衡字段。首版既有 Tool 的 source 禁止原位变更，变化时返回 409 BINDING_IMMUTABLE；需要新目标时创建新 Tool，再显式调整 vMCP。删除被 vMCP 引用的 Tool 返回 409 RESOURCE_IN_USE。不进行执行迁移，也不能把旧 session 自动解释成新实现中的执行。

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

vmcp_id 取自路径；MCP 地址固定生成为 /mcp/{vmcp_id}，不接受自定义 path。默认地址为 http://121.199.175.151:25100/mcp/workbench。工具必须存在且不重复。access.credential_env 省略/空字符串表示匿名，非空启用消费 Token；access.require_https 省略为 false，可在创建或完整替换 vMCP 时设置。

GET 和 PUT 响应中的只读 endpoint：require_https=true 时选择共享 HTTPS public_url，否则选择根 public_url；legacy HTTPS-only 配置的根地址仍为 HTTPS。require_https 不创建监听、不申请证书，也不自动启用 Token。HTTP 调用要求 HTTPS 的 vMCP 时，MCP 与 REST /exec 都返回 403 HTTPS_REQUIRED，只信任 r.TLS 对应的真实 TLS，不信任 Forwarded/X-Forwarded-Proto。管理接口不回显凭据值；MCP 只公开该 vMCP 的工具定义，不公开 source/credential_env。

配置更新只影响随后进入的新请求；已接受的请求保留当次固定绑定与身份上下文，结果仍可返回，不迁移或重跑。删除 vMCP 或移除 Tool 后，相应新请求被拒绝；这不会自动终止 App 中已有命令进程。管理者需要结束执行时使用 OIC/App 的生命周期机制。首版不承诺跨配置改动恢复旧执行。

变更后新的 tools/list 返回当前定义。验收客户端在配置变更后重新获取工具列表，不假定管理 API 修改会立即刷新全部客户端缓存；若声明工具列表变更通知能力，通知必须符合冻结的 MCP 版本。即使客户端使用旧缓存发起调用，OIS 仍检查当前工具集合和访问权限。[MCP 2025-11-25 Tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)

## 6. HTTP 执行入口

POST /api/v1/exec 遵守相应 vMCP 的可选 Token 与 require_https 设置。请求由 vmcp、tool 和 arguments 组成；vmcp/tool 是非空字符串，arguments 必须是对象。不提供绕过 Tool 直接调用任意 App.Action 的旁路。

```json
{
  "vmcp": "workbench",
  "tool": "linux_exec_command",
  "arguments": {
    "cmd": "ls -l",
    "workdir": "/srv/workspace",
    "yield_time_ms": 1000,
    "max_output_tokens": 2000
  }
}
```

vmcp/tool 是调用入口标识，不是目标机器选择；arguments 是工具业务参数，与 MCP tools/call 中的 arguments 对应。业务参数不能改变固定通道、runtime、handler 或可信身份。HTTP 请求信封必须结构有效，但不能在 OIC 轻量适配前因业务值与中心描述类型不同就一律拒绝，例如可转换的 "1000" 应允许交给本地绑定。

处理顺序：按 vMCP 检查 HTTPS 要求及可选 Token → 确认 Tool 属于该 vMCP → 读取固定 source → 经在线通道转交 → OIC 按本地 YAML 适配 → App 执行 → 返回本次结果。HTTP 与 MCP 使用同一处理逻辑，MCP 不再额外绕到 /exec。

OIS 为每次接受的请求生成内部 call_id，贯穿一次转交和响应；它不是命令 session，也不是可由客户端任意复用的重试承诺。上层执行请求期限基线为 60 秒，与 Action 的 0–30000 毫秒等待窗口及本地最长执行期限分开。成功响应返回 HTTP 200，示例只展示业务核心字段：

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

这是本次 Action 调用成功完成，不使用 202 表示 OIS 排队了一个后台任务。命令是否结束由 App 结果表达。继续读取仍使用同一个 HTTP 接口：

```json
{
  "vmcp": "workbench",
  "tool": "linux_write_stdin",
  "arguments": {
    "session_id": "s_123",
    "chars": "",
    "yield_time_ms": 30000,
    "max_output_tokens": 2000
  }
}
```

Windows 执行使用同一个 vMCP 与 HTTP 入口，但选择已经固定到 Windows 的 Tool，并提交 PowerShell 语法：

```json
{
  "vmcp": "workbench",
  "tool": "windows_exec_command",
  "arguments": {
    "cmd": "$PSVersionTable.PSVersion.ToString(); Get-Location",
    "yield_time_ms": 1000,
    "max_output_tokens": 2000
  }
}
```

若该执行仍在运行，后续使用 windows_write_stdin 及它返回的 session_id。OIS/OIC 不把 Bash 命令翻译成 PowerShell，也不根据 cmd 内容自动选择执行环境。

后续请求产生新 call_id 并引用原 session_id；每组两 Tool 固定到同一 App 后端。句柄不是凭据，App 按可信身份及范围检查归属，Linux/Windows 不可串用。匿名同 vMCP 的 Agent 共享身份，随意改变 Bearer 不改变归属；启用 Token 后共享同一凭据仍不隔离为不同用户。yield_time_ms 是等待窗口，HTTP 意外断开不等于命令已停止或用户明确取消。

首版通过两 Action 分批返回输出，不增加独立 /sessions、/jobs、stdout 订阅、结果队列或全局 kill API。两个命令 App 的输出截断等附加结果字段遵循执行详稿；OIS 不解释或改写其业务状态。命令非零退出仍返回 HTTP 200 和实际 exit_code。

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
| 403 | HTTPS_REQUIRED | 该 vMCP 要求 HTTPS，但请求没有使用真实 TLS；转发头不能绕过 |
| 404 | NOT_FOUND | 指定配置资源不存在，或工具不属于该消费入口 |
| 409 | RESOURCE_IN_USE / BINDING_IMMUTABLE / CHANNEL_CONNECTED / SESSION_BUSY | 配置引用冲突、禁止原位换绑、通道仍连接，或本次会话操作冲突 |
| 422 | INVALID_ARGUMENT / SESSION_UNAVAILABLE / STDIN_NOT_WRITABLE | 已明确的参数或业务调用错误，具体错误含义遵循 App 契约 |
| 503 | CHANNEL_UNAVAILABLE / APP_UNAVAILABLE | 已确认本次未交付执行的依赖不可用 |
| 504 | CALL_RESULT_UNKNOWN | 交付后超时或断连，无法确定执行或输入结果 |
| 500 / 502 | INTERNAL_ERROR / EXECUTION_ERROR | 服务内部或执行链异常，必须按已知事实标注是否可能产生副作用 |

以上错误映射纳入首版契约，不意味着所有同类 HTTP 错误都可安全重试。失联前可能已启动命令或写入部分字符时必须保留未知/部分完成的信息，不能伪装成未执行。OIS 不自动重投；短时内部去重不承诺跨重启或跨缓存保留期恰好执行一次。

REST 错误不能原样当作 MCP HTTP 错误输出。MCP 适配层按选定协议将工具执行错误与 JSON-RPC/传输错误分别映射；两入口保留一致的业务错误含义。

## 8. MCP 入口

默认 endpoint 为 http://121.199.175.151:25100/mcp/workbench；require_https=true 时使用其返回的 HTTPS endpoint。协议固定 MCP 2025-11-25 Streamable HTTP。Windows PowerShell 7 客户端通过 initialize、notifications/initialized、tools/list、tools/call 验证四工具；REST /exec 不能替代 MCP 验收。[MCP Tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)、[MCP Transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)

客户端仅在该 vMCP 启用 Token 时提供消费凭据。POST 的 Accept 同时包含 application/json 和 text/event-stream；初始化后发送 MCP-Protocol-Version: 2025-11-25，并按响应携带 MCP-Session-Id。匿名并不取消这些协议要求。MCP 会话标识不等于命令 session_id，处理遵循该版本，不使用 2026-07-28 的元数据或取消规则。[MCP Transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)

以下是初始化正文；按所选访问配置设置可选认证和必需协议头：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": {},
    "clientInfo": {"name": "loom-oi-acceptance", "version": "0.1.0"}
  }
}
```

收到初始化结果后发送：

```json
{"jsonrpc":"2.0","method":"notifications/initialized"}
```

工具发现与 Linux 调用正文示例：

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "linux_exec_command",
    "arguments": {"cmd": "uname -s; pwd", "yield_time_ms": 1000}
  }
}
```

Windows 调用只换成对应 Tool 和 PowerShell 业务文本，不增加执行目标参数：

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "windows_exec_command",
    "arguments": {"cmd": "$PSVersionTable.PSVersion.ToString(); Get-Location", "yield_time_ms": 1000}
  }
}
```

消费侧 inputSchema 来自 OIS 的 input_schema。OIS 将 App 结果转为 MCP 工具结果，结构化内容放入 structuredContent，并提供文本表示方便客户端消费。两侧 Action、等待窗口和执行句柄行为与 REST 入口一致；内部 call_id 由 OIS 生成，MCP 请求 id 仅用于对应外部请求。

OIS 对内只采用一条调用处理路径：Tool → 固定 App.Action/通道 → OIC → App。HTTP /exec 与 MCP 的差别主要在请求及结果封装，不增加选路、计划或作业服务。

### 请求取消与意外断开

MCP 客户端明确取消正在处理的工具请求时发送 notifications/cancelled；OIS 按当前身份、MCP 会话和请求 id 查到内部 call_id，再向 OIC 传递内部取消。初始化请求不能取消，已完成或未知请求的迟到取消按规范忽略。MCP 2025-11-25 明确区分取消通知和网络断开：HTTP/SSE 流关闭本身不能当作客户端取消。[MCP 2025-11-25 Cancellation](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation)、[Streamable HTTP 断开规则](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)

REST 不增加公开的会话或全局取消 API；处理器明确中止本次调用或服务端调用期限触发时进入同一个内部取消过程。意外 HTTP(S)/WS(S) 断开只保留已知状态；交付后未取得结果时记录未知，不推断命令未运行、输入未送达或进程已停，也不自动重试。

内部取消按阶段处理：尚未投递或尚未产生副作用时阻止执行；正在等待已有执行输出时结束本次等待，不无条件杀掉已有命令；输入已经写入的部分不能回滚或重复发送；启动调用尚未提交执行句柄时，按执行契约回收新建执行并确认实际状态。取消与结果提交按同一次调用的状态裁决，不能同时提交两个结果；只有确认停止后才能报告执行停止。MCP 通知响应行为遵循协议，不能把 REST 错误对象当成取消通知的 JSON-RPC 响应。

## 9. OIC WebSocket 接入

OIC 默认连接 ws://121.199.175.151:25100/api/v1/oic/connect，无 Token。选择共享 HTTPS 入口时使用 WSS 并验证证书；通道 credential_env 非空时在握手提供 Authorization: Bearer <通道凭据>，OIC 的对应引用也必须有值。匿名与启用 Token 的通道可并存，每条连接仍按首帧选择的通道核对。首个 UTF-8 JSON 文本消息声明 host_id/channel_id，例如 Linux：

```json
{
  "host_id": "linux-151",
  "channel_id": "linux-151"
}
```

Windows 首帧对应为：

```json
{
  "host_id": "windows-local",
  "channel_id": "windows-local"
}
```

OIS 始终验证 host_id 等于 expected_host_id 且该通道没有其他活动连接；该通道启用 Token 时还必须验证对应凭据。绑定完成前不接收登记/业务结果，也不投递调用。后续 Host 元数据与能力登记属于这条已绑定连接，不能重新申报其他身份。匿名通道的存在不放宽其他通道已启用的鉴权。

通道以每个 WebSocket 文本消息一个 JSON 对象承载下列交互；业务 cmd 和输出只是 JSON 字段，不能与消息边界混用：

| 逻辑交互 | 方向 | 内容 |
| --- | --- | --- |
| 首帧绑定 | OIC → OIS | host_id、channel_id，以及该通道配置要求的可选握手凭据校验 |
| Host 登记 | OIC → OIS | 当前已绑定 Host 的名称、系统和架构元数据 |
| 能力登记/更新 | OIC → OIS | 已加载的 App、Action、本地描述/参数 Schema 及就绪状态，用于管理查询而不覆盖 Tool 定义 |
| 调用交付 | OIS → OIC | call_id、app、action、input 和系统生成的可信上下文 |
| 调用结果或错误 | OIC → OIS | 同一 call_id 对应的 result 或 error |
| 存活/断开 | 双方 | 更新通道可用状态，失联后不再发送新调用 |
| 当前请求取消 | OIS → OIC | 指定 call_id 的内部取消，按执行阶段处理，不直接等同于终止既有命令 |

心跳间隔 10 秒，连续 30 秒无法确认存活则不可用并关闭连接。旧消息不能更新新登记；重连须重新按当前配置校验绑定、可选 Token 并登记，未就绪 App 不因 OIC 在线而可调用。

能力登记不把本地 Schema 升格为 OIS Tool 定义权威。OIC 接入不能修改消费权限或替调用者伪造身份。通道断开不保留待执行的离线队列。同一通道恢复且原 App 实例存活时，已知 session 按原身份范围继续；App 或 worker 重启后的旧 session 失效，不迁移到另一通道或执行后端。

## 10. 首个 API 使用流程

本节规定当前配置下的实施与验收顺序，实际结果另存报告：

1. 通过 ssh admin@121.199.175.151 准备 OIS/Linux OIC；默认入口为 25100/HTTP。Windows 准备 OIC、PowerShell App 和协议验收客户端。
2. 默认不配置 Token/证书；需要时显式填写各凭据引用、准备 CA 签发的证书并配置 server.https。首次导入后由 API 维护持久配置，验证重启不覆盖。
3. PUT 两条主通道并绑定同名 expected_host_id；默认 credential_env 为空，启用 Token 时再分别配置。
4. PUT 四个工具定义，按第 4 节的绑定表固定到 bash 或 powershell 的两个 Action；PUT /api/v1/vmcps/workbench，暴露这四个 Tool。
5. 两端分别执行 oic start 和 oic load，等待对应通道在线且 App/Action 就绪。用 Host、通道及 Action GET 接口核对登记身份、来源、Schema 与可用状态。
6. Windows PowerShell 7 验收客户端连接同一个 /mcp/workbench，完成 MCP 2025-11-25 初始化和 tools/list，再分别调用 Linux 与 Windows 工具。REST /exec 另作同链路入口验收，不替代 MCP 验收。
7. 两端分别验证短命令、长命令、空输入轮询和可交互执行；使用各自 write_stdin 和原 session_id 接续，最终取得实际输出与退出状态，不能串用句柄。
8. 覆盖默认匿名、启用 Token、require_https 与证书验证，保留引用 409、source 不可换绑、配置持久化、Host 错配、重复连接和断连取消测试。原 acceptance-management.ps1/channelprobe 是显式 HTTPS+Token 配置的回归探针，不作为匿名默认下可直接运行的脚本；匿名管理按集成测试和实际 API 检查验证。完整标准见[验收规格](../../../current/beta/acceptance.md)。

文档与示例可解析只代表文档校验，不是功能验收。用户已授权现有部署切换；启用 HTTPS/Token 的历史记录与新默认的实测结果分别保留，不相互替代。
