# loom-oi beta：从 Bash App YAML 到 Agent 调用

首版完成一条固定绑定的 Bash 调用链：**Agent → vMCP → Tool → Action → 通道 → OIC → oiapp → 目标 cmd。** 不做多 Host 路由，Agent 不需要发现或选择机器。本文围绕启动命令、取得输出、继续输入或读取结果，说明从配置到使用的完整闭环。

不做多 Host 路由已经确认；**下文字段、目录、CLI、认证方式和 worker 协议仍是候选设计，尚未实现或整体采纳，示例不是当前可执行的安装教程。** 例子只配置一条通道，既不要求首版部署多个 OIC，也不限制平台只能存在一个 OIC。

OIS 维护消费工具定义、描述和参数；App YAML 必须包含本地 Action 及其参数 Schema。OIC 接收调用后按已加载 YAML 识别入口、轻量适配，再交给 oiapp。runtime 可以在根节点提供默认，也可以由 Action 覆盖；同名 handler 可以省略。Bash 共享 worker 是候选实现，不是全部 App 强制采用的形态。

详细机制见 [Bash Action、参数与执行机制](bash-action-design.md)，链路图见 [首版固定通道图](diagrams/2026-11-09-v2/README.md)。

## 1. 固定绑定关系

| OIS Tool | 固定 Action | 固定通道 |
| --- | --- | --- |
| `terminal_exec` | `bash.exec_command` | `bash-local` |
| `terminal_write_stdin` | `bash.write_stdin` | `bash-local` |

通道暂按“OIS 到某个 OIC 的通信入口”理解，标识用 `bash-local` 举例。它不是新增的独立服务、进程或调度层。OIC 主动连接 OIS，OIS 通过已经配置的通道向在线 OIC 即时交付调用，结果沿原链返回。

两个 Tool 必须落到同一个 Bash 执行后端，才能接续同一个 `session_id`。通道选择在 OIS 配置中完成，Agent 只提交 `cmd` 或 `session_id` 等业务参数，中间不增加目标选择、合并或调度机制。

| 配置 | 回答的问题 |
| --- | --- |
| App YAML 和 actions 代码 | 提供哪些 Action，参数是什么，如何运行和实现？ |
| OIC 本机配置 | 连接哪个 OIS、使用哪条通道、App 使用什么本机环境？ |
| OIS Tool / vMCP 配置 | 对外工具是什么、接收什么参数、固定交给哪条通道中的哪个 Action？ |
| Agent MCP 配置 | 连接哪个 vMCP，以什么凭据访问？ |

两份 Schema 分别描述消费接口与本地实现入口，不要求相等。OIS 管消费者访问，OIC 管本机执行范围，这不需要新增路由层。

## 2. 目录与运行环境

App 开发者交付普通目录。描述文件暂命名 `app.yaml`，OIC 根据文件内容识别 App，不根据文件名推断能力。

```text
bash/
  app.yaml                         # Action 描述、参数 Schema 与 runtime
  runtime/
    worker.py                      # OIC 托管共享 worker，分发调用帧
    executor.py                    # 执行句柄、进程/PTY、输出与回收
  actions/
    exec_command.py                # 创建执行
    write_stdin.py                 # 继续输入或读取
  README.md                        # 环境要求与行为说明

oic-deployment/
  oic.yaml                         # OIS 连接、通道、本机 App 配置
  apps/bash/                       # 上述 App 目录
  .oic/
    loaded.json                    # 已加载 App 的候选持久记录
    logs/

ois-deployment/
  ois.yaml                         # 通道接入、Tool 与 vMCP 配置
  data/

agent-workspace/
  .vscode/mcp.json                  # 本文以 VS Code 本地 Agent 举例
```

这些是部署目录，不是源码工程目录，也不要求位于同一个仓库。本例执行环境安装 Bash 和 Python，准备 `/srv/workspace`；OIC 使用执行账户运行 App。Python 只是语言示例，其他语言可提供对应 runtime 入口；具体平台和安装方式待定。

## 3. Bash App：一份 YAML，两个核心 Action

Bash 的交互模型沿用当前可见工具的使用方式：`exec_command` 启动命令并等待一段时间；已经结束就返回输出和退出码，仍在运行就返回输出和 `session_id`。之后用 `write_stdin` 对同一次执行继续输入或读取输出。

这个选择依据当前工具声明和无副作用交互进程实测：启动返回会话、空输入继续等待读取、写入输入后取得结束结果；没有查看 Codex 内部实现源码。官方指南也说明了 `exec_command` 与 `write_stdin` 配合支持交互及轮询输出的模式。[Codex Prompting Guide](https://developers.openai.com/cookbook/examples/gpt-5/codex_prompting_guide)

建议的 `bash/app.yaml`：

```yaml
version: 1
app:
  code: bash
runtime:
  command: ["python", "./runtime/worker.py"]
actions:
  exec_command:
    description: 创建一次 Bash 执行，返回本轮输出及继续执行句柄或退出码。
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
    description: 向已有 Bash 执行输入，或仅等待并读取后续输出。
    input_schema:
      type: object
      properties:
        session_id: {type: string}
        chars: {type: string, default: ""}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer, default: 10000}
      required: [session_id]
```

这些字段有明确的边界：

- `version` 是 YAML 格式版本，不是 App 发布版本。
- `app.code` 是可复用 App 的身份。它不包含通道地址或 vMCP 名称。
- `runtime` 位于根节点时提供默认运行信息。此例的 `command` 是 OIC 启动共享 Bash worker 的程序和参数数组，相对路径以 App YAML 所在目录解析；本例要求已经安装 `python`。
- `actions.<name>.description` 描述本地动作；`input_schema` 用基本 JSON Schema 的 `type`、`properties`、`required`、`default` 描述实现入口接收的参数。
- 省略 `handler` 时默认使用 Action 同名入口，需要不同名时才显式填写。省略 handler 不省略 Action 描述和参数 Schema，因此完整 Action 不再只写 `{}`。

OIC 根据本地 `input_schema` 完成基本绑定、固定默认和宽松类型适配，App 保留命令、路径、句柄、动态默认和资源限额等业务检查。例如 `write_stdin.yield_time_ms` 的默认值依赖 `chars` 是否为空，示例中不写一个固定 default，而由 App 决定。Schema 的 default 在这里由 OIC 的绑定机制应用，不是 YAML 解析器自动填值；具体默认值仍是候选。

本机的 `shell_path`、`default_cwd` 放在 `oic.yaml`，由 OIC 在 worker 启动时交付，调用输入不能覆盖它。Agent 的调用参数只描述本次业务操作。通用工具可提供 `shell`、`login` 等设置，本文的 beta Bash 接口由本地配置确定 Bash 路径，不把任意 shell 选择加入消费参数。字段重命名可另用候选 `input_mapping`；类型、必填和固定默认统一由 `input_schema` 表达，具体见 [Bash 详稿第 11 节](bash-action-design.md#11-app-目录和-yaml)。

runtime 的优先级按“根默认、Action 覆盖”处理。本文的候选合并规则是：先继承根 runtime 的顶层字段，再用 `actions.<name>.runtime` 的同名字段替换；`command` 数组整体替换，不与根数组拼接。`runtime: {}` 与未填写一样，继承根配置；没有根 runtime 时，Action 自己提供所需运行信息。具体合并粒度仍待定，不从这个例子推定任意嵌套结构都做递归合并。

本例两个 Bash Action 没有 runtime 覆盖，绑定本次 App 加载的同一个根 runtime worker，才能访问同一套执行状态。在该 worker 模式的候选规则中，非空 Action runtime 覆盖使用独立 worker，不按 command 内容相同自动共享。如果改用这种覆盖，两个 Bash 入口仍必须访问同一共享执行后端，否则不能接续 `session_id`。本文不对 Bash 做这种覆盖；具体实例规则见详稿。

| Action | 消费者传入什么 | 返回什么 |
| --- | --- | --- |
| `exec_command` | 必填 `cmd`；可选 `workdir`、`tty`、`yield_time_ms`、`max_output_tokens`。 | 本轮 `output`；仍在运行时有 `session_id`，执行结束时有 `exit_code`。 |
| `write_stdin` | 必填 `session_id`；可选 `chars`、`yield_time_ms`、`max_output_tokens`。 | 本轮新增 `output`；仍在运行时继续返回 `session_id`，结束时返回 `exit_code`。 |

`chars` 省略或为空字符串时，不写入内容，只等待并读取输出；可写的 TTY 执行收到非空 `chars` 时先写入，再收集输出，不自动补换行。`yield_time_ms` 控制本次等待窗口，不是命令运行到这个时刻就被终止。`max_output_tokens` 控制本次返回的输出预算，不是进程运行时间限制。默认值、范围和截断处理已有详稿中的候选规则，本文不重复完整限额表；下文数值只用于说明调用。

每次新的 `exec_command` 都创建独立执行，使用该次 `workdir`，省略时采用本机 `default_cwd`；所选目录仍受本地执行范围约束。`session_id` 表示某一次仍可继续读取或操作的执行，后续 `write_stdin` 通过同一对固定绑定工具回到原 OIC 的 Bash App，并通过身份归属检查。它不代表多次 `exec_command` 共用一个 shell；前一次命令里的 `cd` 或变量不会自动继承到下一次新执行。本候选方案采用 `s_b1_7ef4` 这样的不透明字符串句柄，调用者原样传回，不把它当作 PID；worker 重启后旧句柄失效。

本候选方案中，`tty=false` 使用普通管道，命令 stdin 从启动时即为 EOF；空 `chars` 仍可继续读取，非空输入返回 `STDIN_NOT_WRITABLE`。需要后续输入时显式使用 `tty=true`，保留 PTY 输入端。对适用的 TTY 执行，可以用 `write_stdin` 发送 Ctrl-C 字符 `\u0003` 请求中断；是否退出要看后续结果，不能把发送字符等同于强制终止进程。公开模型保持这两个 Action，轮询也通过 `write_stdin` 完成。

本项目建议由 OIC 直接托管一个共享 Bash worker，两个 Action 是它内部的业务模块。进程、输入通道和输出状态留在这个 worker 内：

| 文件 | 要实现什么 |
| --- | --- |
| `runtime/worker.py` | 接收本地调用帧、按已加载映射分发 handler、关联调用与并发响应。 |
| `runtime/executor.py` | 保持执行句柄、实际进程、可选 PTY、输入通道与输出缓冲，并完成收尾回收。 |
| `actions/exec_command.py` | 处理交付的参数，完成业务检查、动态默认与限额处理，要求 executor 创建执行并返回本轮结果。 |
| `actions/write_stdin.py` | 处理交付的参数和动态默认，检查句柄归属及可写状态，按需输入并等待读取本轮输出。 |

共享 worker 是 loom-oi 的候选实现方式，不是对 Codex 内部架构的描述。OIC 负责识别 Action Schema、计算有效 runtime、通用托管、参数适配和调用帧，不需要理解 Bash 的进程或输出缓冲；两个 runtime 模块不登记为额外 Action。具体启动、关闭和回收建议见详稿，首版无需同时实现所有 App 运行模式。

`default_cwd` 只定义初始工作目录，不等于文件访问隔离。Bash 使用 OIC 的执行账户所具备的系统权限。

## 4. OIC 与 Bash oiapp 的候选调用协议

Bash 的候选本地协议采用 UTF-8 JSON Lines：**OIC 启动一次 worker，通过保持打开的 stdin/stdout 交换调用帧；每行一个完整 JSON 对象，诊断写 stderr。** 字符串内部的换行按 JSON 转义，worker 负责避免并发响应交错。每个 Action 调用返回一个 `result` 或 `error`，worker 持续运行，Bash 原始输出留在 App 的缓冲中。

OIC 启动 `runtime.command` 后发送本机配置：

```json
{
  "type": "start",
  "protocol": 1,
  "config": {
    "shell_path": "/bin/bash",
    "default_cwd": "/srv/workspace"
  }
}
```

worker 初始化完成后回应：

```json
{"type": "ready", "protocol": 1, "handlers": ["exec_command", "write_stdin"]}
```

收到 OIS 转发的调用后，OIC 根据已加载 YAML 找到 Action、参数 Schema 和有效 runtime，完成必要适配，再向已绑定的 Bash worker 发送：

```json
{
  "type": "invoke",
  "call_id": "call-001",
  "handler": "exec_command",
  "input": {"cmd": "ls -l", "tty": false, "yield_time_ms": 1000, "max_output_tokens": 1000},
  "context": {
    "access_identity": "identity-dev",
    "vmcp_id": "workbench"
  }
}
```

`config` 只在启动时交付，`input` 不能覆盖它。`context` 来自可信调用链，供 App 检查执行句柄归属，不由 Agent 在业务参数中自报。worker 不需要处理 Tool 展示名称、OIS 网络协议或接入凭据；`handler` 由 OIC 查表产生，YAML 省略时取 Action 名，不接受调用者提供的任意入口。协议帧中仍明确携带 `handler`，OIS 引用的仍是原 App Action。

本例已经声明本地 `input_schema`。OIC 在有 `input_mapping` 时先完成字段重命名，然后按 Schema 的基本信息应用固定默认、处理必填和类型适配：例如字符串 `"1000"` 可以转成整数，对象或数组不能转成整数。没有固定默认的可选值可继续省略，由 App 按业务条件决定；未被重命名消费的其他字段保留给 App 处理。上面 `invoke` 中的 `tty: false` 就来自本地固定默认。

OIC 通过 YAML 识别和适配，不推断任意 Python 签名，也不要求中心与本地的两份 Schema 相等。App 继续检查命令、路径、句柄等实际业务语义、动态默认和执行限额；绑定或业务检查失败时，应在启动命令或写入输入之前返回 `INVALID_ARGUMENT`。基础 Schema 有效性检查与执行时的宽松适配是两个步骤，不能因为 OIS 描述整数、本地收到可转换字符串，就在适配前直接拒绝。

下面的业务结果示例只显示核心字段。详稿另有截断、遗漏字节数、输出完整性和结束原因等候选字段；这些元数据不应因显示预算较小而被省略。

正常执行完毕时：

```json
{
  "type": "result",
  "call_id": "call-001",
  "result": {
    "output": "total 0\n",
    "exit_code": 0
  }
}
```

仍可继续读取或操作时：

```json
{
  "type": "result",
  "call_id": "call-002",
  "result": {
    "output": "name: ",
    "session_id": "s_b1_7ef4"
  }
}
```

`result` 只表示本次 Action 已返回，Bash 是否结束仍由 `session_id` 或 `exit_code` 表达。两个 Action 共用这一业务形状；App 持续采集并合成 `output`，不承诺从中重新分离 stdout 与 stderr。Bash 返回非零退出码也是已结束的业务结果，不等同于 worker 崩溃或 MCP 协议错误。

调用错误按同一 `call_id` 返回：

```json
{
  "type": "error",
  "call_id": "call-003",
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "yield_time_ms cannot be converted to an integer.",
    "details": {"field": "yield_time_ms", "expected": "integer", "actual_type": "object"}
  }
}
```

YAML 无法解析、Action 基本信息或 Schema 缺失、有效 runtime 无法形成、入口不可用属于加载或托管错误；本次参数无法绑定属于调用错误。worker 报告一次错误后可以继续处理其他调用，不能要求它每次返回后退出。worker 崩溃或关闭则使对应 App 不可用、旧执行句柄失效；重新启动 worker 不恢复旧 session。

通用机制由 OIC 实现：解析本地 Action 与参数 Schema、计算有效 runtime、执行时轻量适配、托管及调用转交、可信上下文与结果关联。Bash App 负责业务参数语义、动态默认、限额、执行句柄、命令输入输出及可选 PTY。OIC 不按 Bash Action 名称加入业务分支，具体 Schema 支持范围和运行适配仍按候选方案收束。后续 CU 的运行适配另行设计。协议适配可抽取薄辅助模块，不要求每个 App 自建网络服务或大型 SDK。

详稿还建议宿主使用 `cancel` 帧处理某一次调用的取消：

```json
{"type": "cancel", "call_id": "call-001"}
```

它只转交给原 worker 和原调用，不是第三个公开 Bash Action；具体取消范围取决于调用阶段，停止等待一次 `write_stdin` 不等于结束原执行，已写入内容也无法撤销。关闭整个 App 则由 OIC 关闭 worker stdin，以 EOF 通知其回收执行并退出。收到取消请求、关闭通道和确认实际进程已停止必须区分；断连后结果未知时不能盲目重跑。完整调用状态、自动重试、取消与恢复规则仍是详稿中的候选设计，不能从执行句柄推定这些规则已经确认。

## 5. 配置 OIC 并加载 Bash App

候选 `oic.yaml`：

```yaml
version: 1
channel:
  id: bash-local
ois:
  url: https://ois.example.com
  credential_env: OIC_BASH_TOKEN
state_dir: ./.oic
app_config:
  bash:
    shell_path: /bin/bash
    default_cwd: /srv/workspace
```

`channel.id` 与 OIS 对应；`app_config.bash` 是部署的本机配置，不写进可复用 App 的参数 Schema。`credential_env` 指环境变量名称，具体认证配置仍是候选。

先按下一节准备并启动 OIS，再启动 OIC、加载 App。候选命令：

```sh
oic --config ./oic.yaml start
oic --config ./oic.yaml load ./apps/bash/app.yaml
oic --config ./oic.yaml apps
```

`load` 将 App 注册到已运行的本机 OIC，CLI 到进程的控制方式待定。加载建议依次完成：

1. 解析 App、Action 描述、参数 Schema 和可选映射，检查基本 YAML 与入口信息。
2. 按根默认、Action 覆盖计算有效 runtime，再按运行适配与实例规则分组，不能按 command 内容相同推断共享实例。
3. 本例启动一个根 runtime worker，交付本机配置，等待 `ready` 并核对 handler，绑定 `App.Action → 本地 Schema / runtime 实例 / handler`。
4. 记录加载信息，通过 `bash-local` 告知 OIS 这两个 Action 当前可用。

加载不执行用户 `cmd`。本地 Schema 缺失、格式错误或入口不可用应明确报告；本地 Schema 与 OIS 消费定义不同至多提示，不能以相等作为加载门槛。重启可按记录重新加载 App，但不恢复旧 worker 的执行或旧 `session_id`。

## 6. 在 OIS 定义两个固定绑定 Tool

OIS 维护消费参数，`source` 固定指定 App、Action 和通道。候选 `ois.yaml`：

```yaml
version: 1
server:
  public_url: https://ois.example.com
channels:
  bash-local:
    credential_env: OIC_BASH_TOKEN

tools:
  terminal_exec:
    description: 启动一次独立 Bash 执行；返回 output，仍可继续时返回 session_id，结束时返回 exit_code。
    source: {app: bash, action: exec_command, channel: bash-local}
    input_schema:
      type: object
      properties:
        cmd: {type: string}
        workdir: {type: string}
        tty: {type: boolean}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer}
      required: [cmd]
  terminal_write_stdin:
    description: 继续已有 Bash 执行；空 chars 等待读取，非空 chars 向可写终端输入后读取。
    source: {app: bash, action: write_stdin, channel: bash-local}
    input_schema:
      type: object
      properties:
        session_id: {type: string}
        chars: {type: string}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer}
      required: [session_id]

vmcps:
  workbench:
    path: /mcp/workbench
    tools: [terminal_exec, terminal_write_stdin]
    access:
      credential_env: WORKBENCH_AGENT_TOKEN
```

Tool 的名称、描述和参数对消费者可见，`source` 是配置，不是 Agent 传入的参数。本例同名字段直接交给 OIC，按本地 Schema 绑定和适配，不先做两份 Schema 的严格比较；字符串与整数存在可用转换时尽量交付。

若消费参数叫 `command` 而本地实现接收 `cmd`，可在本地 Action 加候选 `input_mapping: {cmd: command}`，按“本地目标名: 调用来源名”重命名，不重复类型、必填或默认值。本例两侧同名，无须映射。输出沿用公共 JSON 结果，不增加本地强制输出 Schema。

候选启动命令：

```sh
ois --config ./ois.yaml start
```

启动 OIS 后按上一节启动 OIC、加载 Bash；确认 `bash-local` 已连接、worker ready、两个 Action 引用可用，再让 Agent 接入 vMCP。通道或 Action 不可用时明确报错，不改变绑定；结果未知时不能盲目重跑命令。

## 7. Agent 对接 vMCP

本例使用 VS Code 的 `.vscode/mcp.json`，其他客户端按其 MCP 配置连接同一个入口：

```json
{
  "inputs": [
    {"type":"promptString","id":"loom-token","description":"loom-oi workbench access token","password":true}
  ],
  "servers": {
    "loomWorkbench": {
      "type": "http",
      "url": "https://ois.example.com/mcp/workbench",
      "headers": {"Authorization":"Bearer ${input:loom-token}"}
    }
  }
}
```

凭据对应 `WORKBENCH_AGENT_TOKEN`。这份 JSON 是客户端配置格式，不是所有 Agent 共用的标准。[VS Code MCP 配置文档](https://code.visualstudio.com/docs/agents/reference/mcp-configuration)

Agent 通过 MCP 工具发现取得两个 Tool 的消费定义，随后直接调用，不需要发现设备或选择目标。vMCP 是 OIS 的逻辑 MCP 入口，不要求为它启动独立服务进程。[MCP Tools 规范](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)

每次调用取得一次结果，需要后续输出时再调用 `write_stdin`。本流程不要求 MCP 客户端支持 Tasks 扩展，也不把 HTTP 传输流等同于命令 stdout。

## 8. 完整走一次 Bash 使用流程

先调用 `terminal_exec`，业务参数是扁平对象：

```json
{"cmd":"pwd; ls -l","workdir":"/srv/workspace","yield_time_ms":1000,"max_output_tokens":1000}
```

OIS 通过固定 `source` 将 `bash.exec_command` 交给 `bash-local`。OIC 按本地 YAML 补默认、适配参数并转给绑定 worker，handler 完成业务检查，由 executor 执行命令；`cmd` 文本保持原样。完成后返回核心结果：

```json
{"output":"/srv/workspace\ntotal 0\n","exit_code":0}
```

需要后续输入时，创建一次新的 TTY 执行：

```json
{"cmd":"printf 'name: '; read -r name; printf 'hello %s\\n' \"$name\"","workdir":"/srv/workspace","tty":true,"yield_time_ms":1000,"max_output_tokens":1000}
```

```json
{"output":"name: ","session_id":"s_b1_7ef4"}
```

调用 `terminal_write_stdin`，空 `chars` 只等待并读取：

```json
{"session_id":"s_b1_7ef4","chars":"","yield_time_ms":1000,"max_output_tokens":1000}
```

```json
{"output":"","session_id":"s_b1_7ef4"}
```

再通过同一工具写入一行，TTY 可能回显输入：

```json
{"session_id":"s_b1_7ef4","chars":"loom\n","yield_time_ms":1000,"max_output_tokens":1000}
```

```json
{"output":"loom\r\nhello loom\r\n","exit_code":0}
```

后续调用按同一固定通道回到原 Bash worker，用不透明字符串 `session_id` 找到同一次执行。Agent 参数没有额外目标字段或 `input` 嵌套层，内部 worker 的 `invoke.input` 保持不变。结果只展示核心字段；截断信息等按详稿补齐，数值和文本只用于示例。

## 9. 首个流程的验证边界

- 本地 Action 都有描述、参数 Schema 和有效 runtime；两个 Bash 入口访问同一执行后端。
- OIS 只暴露两个固定绑定 Tool，均通过 `bash-local` 到指定 OIC；Agent 提交扁平业务参数。
- `load` 等到 worker ready 后可用，加载本身不执行命令；两侧 Schema 差异不构成加载或发布门槛。
- 短命令返回输出和退出码，未结束执行返回字符串句柄；空输入读取、TTY 写入和结束结果可接续。
- 可用的字符串与整数转换能完成，不能转换的参数在执行前报错。
- 非零 Bash 退出码、参数错误、worker 退出和通道不可用可区分；重启不恢复旧句柄。
- 根 runtime 与 Action 覆盖按约定解析，不以相同命令推断共享实例；输出预算、回收等按详稿验证。

Computer-use 和多个来源的能力组合保留为后续背景，不作为首版安装与验收主线；扩展时再单独讨论。

继续讨论入口见 [当前共识](../../../current/beta/design.md)、[首版总稿](design.md) 和 [沟通笔记](notes.md)。
