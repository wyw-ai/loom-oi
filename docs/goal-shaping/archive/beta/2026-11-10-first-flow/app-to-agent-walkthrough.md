# loom-oi beta：双端命令 App 从 YAML 到 Agent 调用

> 本轮讨论已于 2026-11-10 结束。本文作为归档快照保留；持续维护的当前契约见[项目规范](../../../../spec/README.md)，讨论结束与取舍见[笔记](notes.md)。

状态：首版流程说明，已同步 2026-11-10 新共识：默认 HTTP/WS、无 Token，按配置启用 HTTPS/Token。用户已授权现有 151/Windows 部署切换；原 HTTPS+Token 验收记录继续作为历史启用配置保留，新默认另行验证。权威范围见[当前共识](../../../current/beta/design.md)和[验收规格](../../../current/beta/acceptance.md)，实际配置见[访问配置](../../../../guides/access-configuration.md)。本文示例说明契约，不替代运行证据。

本版链路保持 **Agent → vMCP → Tool → Action → 通道 → OIC → oiapp → 目标 cmd**。在 `admin@121.199.175.151` 对应的 Linux 服务器部署 OIS 和 Linux OIC，在本机运行 Windows OIC；Windows 上的 PowerShell 7 MCP 协议验收脚本通过一个 `workbench` vMCP 使用两组固定工具。

Linux App 执行 Bash，Windows App 执行 PowerShell 7（`pwsh`）。两者共享协议、参数形状和两个 Action，使用各自的命令语法与平台适配；Windows 工具不承诺 Bash 兼容。

## 1. 两组固定绑定

| OIS Tool | App.Action | 通道 | 固定 Host |
| --- | --- | --- | --- |
| `linux_exec_command` | `bash.exec_command` | `linux-151` | `linux-151` |
| `linux_write_stdin` | `bash.write_stdin` | `linux-151` | `linux-151` |
| `windows_exec_command` | `powershell.exec_command` | `windows-local` | `windows-local` |
| `windows_write_stdin` | `powershell.write_stdin` | `windows-local` | `windows-local` |

两机各运行一个 OIC，各有一条固定通道。每组两个 Tool 访问该 App 的同一个执行后端；Linux 与 Windows 各自保存自己的执行状态。Agent 选择有明确平台说明的 Tool，只传 `cmd`、`session_id` 等业务参数，不另传 Host 或通道进行路由。

通道是 OIS 对接指定 OIC 的入口，不是独立业务服务或调度层。OIC 主动连接；始终匹配预期 `host_id` 并拒绝重复活动连接，配置了通道 Token 时再核对凭据。默认匿名 ID 匹配不表示机器身份认证。稳定标识为 `linux-151`、`windows-local`，不自动迁移身份。

151 服务器允许使用 `25000–27000` 端口范围，本版 OIS 公网入口计划采用 `25100`：

```text
http://121.199.175.151:25100/mcp/workbench
```

默认公网入口使用 HTTP/WS，无 Token，无需证书。可配置 `server.https` 共享 HTTPS/WSS 监听，并由管理员提供 CA 签发的证书；OIS 不自动申请、签发或续期。新建或修改 vMCP 时用 `access.require_https` 选择是否强制真实 TLS，不能用转发头代替；多个 vMCP 共用监听与证书。

## 2. 交付目录与本地职责

两端 App 使用普通目录交付。以下入口名用于说明包内关系，具体二进制名和编程语言由实现设计落实；示例不要求应用作者额外运行网络服务。

```text
linux-deployment/
  ois
  ois.yaml
  data/                            # OIS 持久配置与状态
  oic
  oic.yaml
  apps/bash/
    app.yaml
    runtime/worker                 # Bash 共享执行后端入口
    actions/                       # 两个 Action 的业务实现
  .oic/                            # 本机加载记录与日志

windows-deployment/
  oic.exe
  oic.yaml
  apps/powershell/
    app.yaml
    runtime/worker.exe             # PowerShell 共享执行后端入口
    actions/                       # 两个 Action 的业务实现
  .oic/

agent-workspace/
  mcp-client-config                # 按所用 Windows MCP 客户端填写
```

OIS 维护消费工具定义、参数说明、固定绑定及 vMCP。OIC 解析 App YAML、适配参数、托管实现并转交调用；命令程序、进程或终端、输出缓冲和执行句柄由 App 处理。OIS/OIC 不按 Bash 或 PowerShell 的 Action 名称加入业务分支。

## 3. 两份 App YAML

App YAML 声明 Action 描述、基本参数 Schema 和 runtime。Linux 的 `apps/bash/app.yaml`：

```yaml
version: 1
app:
  code: bash
runtime:
  command: ["./runtime/worker"]
actions:
  exec_command:
    description: 在 Linux 启动一次 Bash 执行，返回本轮输出及继续句柄或退出码。
    input_schema:
      type: object
      properties:
        cmd: {type: string}
        workdir: {type: string}
        tty: {type: boolean, default: false}
        yield_time_ms: {type: integer, default: 10000}
        max_output_tokens: {type: integer, default: 10000}
      required: [cmd]
  write_stdin:
    description: 继续本 App 已有 Bash 执行，输入或等待读取输出。
    input_schema:
      type: object
      properties:
        session_id: {type: string}
        chars: {type: string, default: ""}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer, default: 10000}
      required: [session_id]
```

Windows 的 `apps/powershell/app.yaml`：

```yaml
version: 1
app:
  code: powershell
runtime:
  command: ["./runtime/worker.exe"]
actions:
  exec_command:
    description: 在本机 Windows 启动一次 PowerShell 7 执行；cmd 使用 PowerShell 语法。
    input_schema:
      type: object
      properties:
        cmd: {type: string}
        workdir: {type: string}
        tty: {type: boolean, default: false}
        yield_time_ms: {type: integer, default: 10000}
        max_output_tokens: {type: integer, default: 10000}
      required: [cmd]
  write_stdin:
    description: 继续本 App 已有 PowerShell 执行，输入或等待读取输出。
    input_schema:
      type: object
      properties:
        session_id: {type: string}
        chars: {type: string, default: ""}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer, default: 10000}
      required: [session_id]
```

两份 YAML 都省略 `handler`，使用 Action 同名入口。根 runtime 提供默认，Action 的同名顶层字段覆盖根字段，数组和对象整体替换；`runtime: {}` 与未配置一样继承根实例，非空 Action runtime 使用其独立实例。两份命令 App 各自的两个 Action 都继承根 runtime，访问同一后端；不能以 command 文本相同推断共享实例。

OIC 先做可选 `input_mapping` 重命名，再按本地 Schema 应用固定默认、检查必填并轻量转换。例如 `input_mapping: {cmd: command}` 将调用的 `command` 交给实现的 `cmd`；本例同名，无需映射。可转换字符串与整数尽量交付，对象或数组无法转为整数则在执行前报错。两侧 Schema 不必相等，加载不做严格一致性门禁。

参数的业务意义、动态默认及资源限额仍由 App 处理。`workdir` 省略时使用本机配置；`write_stdin.yield_time_ms` 的默认依赖 `chars`，不在 YAML 中写成固定值。未列出的字段交给 App，不据此允许覆盖本机配置或可信调用身份。

## 4. 两端 OIC 配置与加载

Linux `oic.yaml`：

```yaml
version: 1
host:
  id: linux-151
channel:
  id: linux-151
ois:
  url: http://121.199.175.151:25100
  credential_env: ""
state_dir: ./.oic
app_config:
  bash:
    shell_path: /bin/bash
    default_cwd: /home/admin
```

Windows `oic.yaml`：

```yaml
version: 1
host:
  id: windows-local
channel:
  id: windows-local
ois:
  url: http://121.199.175.151:25100
  credential_env: ""
state_dir: ./.oic
app_config:
  powershell:
    shell_path: 'C:\Program Files\PowerShell\7\pwsh.exe'
    default_cwd: 'C:\Users\c6321'
```

`shell_path` 按实际安装位置配置，Windows 使用 PowerShell 7。`ois.url` 是服务基址，默认对应 `ws://121.199.175.151:25100/api/v1/oic/connect`。`credential_env` 空/省略时不发送 Token；填写环境变量名时启用，引用缺值必须报错，接入凭据不继承给命令进程。选择 HTTPS/WSS 时验证服务端证书，按需要配置 CA；不跳过验证。

实现交付后，先启动配置完成的 OIS，再分别执行本机 OIC 命令：

```sh
./ois --config ./ois.yaml start
./oic --config ./oic.yaml start
./oic --config ./oic.yaml load ./apps/bash/app.yaml
./oic --config ./oic.yaml apps
```

```powershell
.\oic.exe --config .\oic.yaml start
.\oic.exe --config .\oic.yaml load .\apps\powershell\app.yaml
.\oic.exe --config .\oic.yaml apps
```

`start` 默认后台启动，`--foreground` 用于前台观察。管理命令定位同一份本机配置对应的 OIC。`load` 解析 Action、Schema 与有效 runtime，初始化全部所需实例，等待 `ready` 并确认 handler；全部本地入口 ready 即本地加载成功，远端登记另显示 `pending/registered`，不把中心离线误报成本地未加载。

加载不执行用户命令。相同定义重复 load 为无操作，变更先卸载再加载；初始化部分失败时回收本次新建资源，不发布半个 App。加载记录持久保存，OIC 重启后重新校验并加载；旧 `session_id` 不恢复。`unload` 回收 App 执行并移除记录，`stop` 回收托管执行但保留记录；不做执行热迁移或 worker 崩溃后自动恢复。

## 5. OIS 的首次配置与四个工具

以下 `ois.yaml` 包含启动信息及首次导入定义。初始化后由 API 修改 OIS 的持久配置，重启不能拿旧 YAML 覆盖 API 改动。本地 Action 定义仍由两端 YAML/OIC 管理。

```yaml
version: 1
server:
  listen: ':25100'
  public_url: http://121.199.175.151:25100
admin:
  token_env: ""
channels:
  linux-151:
    expected_host_id: linux-151
    credential_env: ""
  windows-local:
    expected_host_id: windows-local
    credential_env: ""

tools:
  linux_exec_command:
    description: 在 151 Linux 上运行 Bash 命令；cmd 使用 Bash 语法。
    source: {app: bash, action: exec_command, channel: linux-151}
    input_schema: &exec_input
      type: object
      properties:
        cmd: {type: string}
        workdir: {type: string}
        tty: {type: boolean}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer}
      required: [cmd]
  linux_write_stdin:
    description: 继续 linux_exec_command 返回的执行句柄，输入或等待读取输出。
    source: {app: bash, action: write_stdin, channel: linux-151}
    input_schema: &write_input
      type: object
      properties:
        session_id: {type: string}
        chars: {type: string}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer}
      required: [session_id]
  windows_exec_command:
    description: 在本机 Windows 运行 PowerShell 7 命令；不使用 Bash 语法。
    source: {app: powershell, action: exec_command, channel: windows-local}
    input_schema: *exec_input
  windows_write_stdin:
    description: 继续 windows_exec_command 返回的执行句柄，输入或等待读取输出。
    source: {app: powershell, action: write_stdin, channel: windows-local}
    input_schema: *write_input

vmcps:
  workbench:
    tools:
      - linux_exec_command
      - linux_write_stdin
      - windows_exec_command
      - windows_write_stdin
    access:
      credential_env: ""
      require_https: false
```

YAML 锚点只复用同一文件中的消费参数结构，不把四个 Tool 合并。每个 `source` 都是单个固定对象。管理 API 可以查询 Host、Action 和通道状态，但这些信息不变成 Agent 的目标查询工具，也不参与自动选路。

默认关闭各处 Token。需要时分别填写 `admin.token_env`、通道 `credential_env`、vMCP `access.credential_env`；每项独立启用，变量未设置不能降级匿名。匿名同 vMCP 共享 `anonymous:<vmcp_id>` 身份，任意 Bearer 不会制造新 owner。Host/通道 ID 匹配与重复连接限制始终保留。Tool 不原位换绑，被引用资源不直接删除；新配置只影响新调用，删除 vMCP 不终止已有命令。

需要 HTTPS 时增加 `server.https`（`listen`、`public_url`、`cert_file`、`key_file`），证书由管理员提供；把目标 vMCP 的 `access.require_https` 设为 true 前必须已有 HTTPS 入口。OIS 返回该入口的 endpoint，HTTP 调用被拒绝为 403 HTTPS_REQUIRED；这不自动启用 Token。旧 `tls_cert/tls_key` 仅保留 HTTPS-only 兼容，不能与新块混用。

配置完成后的可用标准是：两个通道在线，两个 App 本地 ready 且已登记，四个 Tool 引用各自入口，`workbench` 返回四个 Tool。端点与错误约定见 [OIS API 设计](ois-api-design.md)，与权威验收规格一起使用。

## 6. Windows MCP 客户端对接

本机 PowerShell 7 客户端使用 MCP `2025-11-25` Streamable HTTP。默认连接 `http://121.199.175.151:25100/mcp/workbench`，不发送 Token；仅在 vMCP 配置启用时提供消费凭据。启用 HTTPS/require_https 时使用相应 endpoint 并验证证书，不能通过关闭证书检查判定成功。

验收脚本依次完成以下协议交互，而不是绕过 MCP 调用 REST `/exec`：

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"loom-oi-acceptance","version":"1"}}}
```

```json
{"jsonrpc":"2.0","method":"notifications/initialized"}
```

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

收到四个 Tool 的定义后，再使用 `tools/call`，例如：

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"linux_exec_command","arguments":{"cmd":"uname -s; pwd","workdir":"/home/admin","yield_time_ms":1000,"max_output_tokens":1000}}}
```

客户端遵循协议版本、服务端会话信息和结果格式，逐项核对工具返回。旧 `acceptance-management.ps1`/`channelprobe` 保留为显式 HTTPS+Token 配置的回归探针，不能直接当作匿名默认部署脚本；匿名管理以集成测试和实际 API 检查覆盖，脚本配置见访问配置说明。

IDE 接入只作辅助。如果使用 VS Code 格式的本地 MCP 配置，可以参考下面的 JSON，它不是本版唯一验收客户端的配置：

```json
{
  "servers": {
    "loomWorkbench": {
      "type": "http",
      "url": "http://121.199.175.151:25100/mcp/workbench"
    }
  }
}
```

客户端通过 `tools/list` 取得四个工具的定义，通过 `tools/call` 提交工具名与扁平业务参数。Agent 看见平台明确的工具名称，选择 Linux 或 Windows 工具即确定调用位置。[VS Code MCP 配置文档](https://code.visualstudio.com/docs/agents/reference/mcp-configuration)、[MCP Tools 规范](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)

每次调用返回一次结果；持续输出使用后续 `write_stdin` 调用获取，不依赖 Tasks 扩展或自定义 stdout 通知。HTTP 传输流不等于命令输出流。

## 7. 从同一个 vMCP 分别运行两端命令

以下都是验收时计划发出的请求，不是本轮执行结果；返回片段只展示核心字段。命令输出可能因目录内容、终端回显而不同。

先调用 `linux_exec_command` 验证 Linux 环境：

```json
{"cmd":"uname -s; pwd","workdir":"/home/admin","yield_time_ms":1000,"max_output_tokens":1000}
```

再调用 `windows_exec_command` 验证 PowerShell 7 与 Windows 目录：

```json
{"cmd":"$PSVersionTable.PSVersion.Major; (Get-Location).Path","workdir":"C:\\Users\\c6321","yield_time_ms":1000,"max_output_tokens":1000}
```

应分别取得 Linux 系统名和路径、PowerShell 主版本 `7` 和 Windows 路径，并返回 `exit_code: 0`。结果放在统一 `output` 字段，不保证分离 stdout/stderr。非零命令退出码也属于业务结果，不等同于协议失败。

分别启动会持续输出的命令，演示两条独立执行链：

```json
{"cmd":"for i in 1 2 3; do printf 'linux-%s\\n' \"$i\"; sleep 1; done","yield_time_ms":100,"max_output_tokens":1000}
```

上面调用 `linux_exec_command`；下面调用 `windows_exec_command`：

```json
{"cmd":"1..3 | ForEach-Object { Write-Output \"windows-$_\"; Start-Sleep -Seconds 1 }","yield_time_ms":100,"max_output_tokens":1000}
```

仍在运行时分别取得不透明字符串句柄，例如 `s_linux_001`、`s_windows_001`；输出可能已经包含第一行。随后只把各自句柄传给配对工具：

```json
{"session_id":"s_linux_001","chars":"","yield_time_ms":1000,"max_output_tokens":1000}
```

```json
{"session_id":"s_windows_001","chars":"","yield_time_ms":1000,"max_output_tokens":1000}
```

前一个请求调用 `linux_write_stdin`，后一个调用 `windows_write_stdin`。每次读取新增输出，仍未结束继续返回 `session_id`，结束返回 `exit_code`；两端输出不得串线。Windows 工具收到 Linux 句柄时应明确失败，不会转发到 Linux 或新建执行。

接着分别验证 TTY 输入。Linux 调用 `linux_exec_command`：

```json
{"cmd":"printf 'name: '; read -r name; printf 'hello %s\\n' \"$name\"","tty":true,"yield_time_ms":1000,"max_output_tokens":1000}
```

Windows 调用 `windows_exec_command`：

```json
{"cmd":"[Console]::Write('name: '); $name = [Console]::ReadLine(); [Console]::WriteLine(\"hello $name\")","tty":true,"yield_time_ms":1000,"max_output_tokens":1000}
```

使用每次实际返回的句柄，分别向对应 `write_stdin` 传入 `chars: "loom\n"`；核对收到 `hello loom` 与最终退出码。终端可能回显输入，换行形式可因平台不同而不同。空 `chars` 只等待并读取，不隐式发送 EOF；非空输入不自动补换行。

新的 `exec_command` 创建独立执行，不继承上次执行的目录或变量。需要后续输入时显式设置 `tty: true`；普通管道模式的非空输入返回 `STDIN_NOT_WRITABLE`。发送 Ctrl-C 只请求中断，必须根据后续结果确认停止。

## 8. 内部协议与最终验收

两个 App 使用同一 JSON Lines worker 协议：启动交付 `start/config`，初始化完成回应 `ready`，OIC 用 `invoke` 转交调用，worker 回应 `result/error`；诊断走 stderr，原始命令输出保存在业务缓冲中。内部 `invoke.input` 包装不改变 Agent 的扁平参数，`handler` 由已加载 YAML 查表产生，身份上下文由可信调用链提供。

每次 Action 返回不代表目标进程已经结束。`session_id` 归原 App 实例与访问身份，OIC 重连只有在原 App 实例仍存活时才能接续；App 或 OIC 重启后旧句柄失效。调用结果未知时不自动重投。取消一次等待读取不等于终止执行；关闭 worker 时按本地生命周期回收进程及终端。完整参数、输出限额、去重、取消和资源回收见[执行机制详稿](bash-action-design.md)，不在本文再复制状态机。

本版验收必须经过本机 PowerShell 7 MCP 协议脚本、同一个 `workbench` 入口完成两端四工具的完整流程，并核对：

- 默认 HTTP/WS 无 Token 能使用两平台工具；Host 绑定和重复连接规则有效。显式启用 Token/HTTPS 时再验证错误凭据拒绝、证书信任及 require_https。
- 两端短命令、持续输出、TTY 输入、非零退出及配对句柄都符合协议，Windows 使用 PowerShell 语法。
- 本地 Schema 的固定默认、宽松转换、无法转换报错及 runtime 优先级可验证。
- 大输出有界、截断可见；断连、取消、App/OIC 停止和重启按契约处理，旧句柄不恢复且资源回收可确认。
- OIS API 修改持久配置后重启保留；OIC 重启重载已保存 App，状态区分本地 ready 与远端 registered。

实际通过与迁移结果单独记录；原 HTTPS+Token 的成功记录不改写为匿名默认结果。CU、同 Tool 多目标路由、负载均衡及执行迁移不进入该闭环。完整 Done 以[验收规格](../../../current/beta/acceptance.md)为准。
