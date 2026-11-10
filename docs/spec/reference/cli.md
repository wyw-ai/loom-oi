# CLI 与进程生命周期

本文覆盖当前 `ois`、`oic`、`oiapp-terminal` 三个可执行文件的公开命令和参数。配置字段见[配置参考](configuration.md)，App 定义与 worker 协议见 [App runtime](app-runtime.md)，命令执行参数见[终端 Actions](terminal-actions.md)。

## 命令与参数

```text
ois [--config <path>] start [--foreground]
ois [--config <path>] status
ois [--config <path>] stop
ois help

oic [--config <path>] start [--foreground]
oic [--config <path>] load <app.yaml>
oic [--config <path>] unload <app-code>
oic [--config <path>] apps
oic [--config <path>] status
oic [--config <path>] stop
oic help
```

Windows 可执行文件带 `.exe`。没有子命令时显示帮助。`help`、`--help`、`-h` 不需要读取配置文件。

| 参数 | 默认 | 作用 |
|---|---|---|
| `--config <path>` | 当前目录的 `ois.yaml` 或 `oic.yaml` | 指定服务配置；相对路径先按执行 CLI 时的当前目录转为绝对路径。可放在子命令前后。 |
| `--foreground` | 不启用 | 仅用于 `start`，让服务留在前台。 |
| `--help`、`-h` | 不启用 | 显示帮助。 |

以上是全部公开 flag；没有 `--version`、`restart`、系统服务安装或开机启动命令。`--config` 必须使用独立的下一个参数，当前不支持 `--config=path`。`load` 和 `unload` 各要求一个位置参数，其他业务子命令不接受位置参数。成功退出码为 `0`，参数、配置或操作失败为 `1`，错误写入 stderr。

## start、status 与 stop

`start` 默认派生后台进程，将 stdout、stderr 追加到 `state_dir/ois.log` 或 `state_dir/oic.log`。Linux 使用独立 session，Windows 使用隐藏的独立进程。当前实现不安装 systemd 或 Windows Service；需要外部服务管理器时使用 `start --foreground`。

后台启动使用 15 秒的就绪轮询窗口，收到本地控制端的状态响应后打印 JSON 并结束 CLI。这个成功结果表示服务进程和本地控制接口已就绪；OIC 此时可能还在恢复 App、连接 OIS 或等待登记确认。应继续检查 `status` 和 `apps`，不能把后台启动成功等同于四个业务工具都可用。启动未就绪会提示检查日志。单次本地控制请求的客户端超时为 20 秒，因此轮询窗口不是整个 CLI 操作的严格耗时上限；请求超时也不能作为后台操作已回滚的证明，应重新查询状态。

同一配置已有可访问实例时，再次 `start` 返回该实例的状态。每个 `state_dir` 使用操作系统文件锁避免重复启动，不应让两个服务实例共用同一个状态目录。

`status` 的在线响应结构如下，`status` 内部内容由服务类型决定：

```json
{"running":true,"pid":12345,"service":"oic","status":{"running":true,"connected":true,"host_id":"linux-151","channel_id":"linux-151","app_count":1}}
```

| 服务 | `status` 中的内容 |
|---|---|
| OIS | `running`、主 `listen`、`channels.configured/online`、`apps.registered/available`、`tools.configured/available`、`vmcps` 数量。登记计数可包含已离线通道的最后快照；可用计数要求在线且完成登记。 |
| OIC | `running`、`connected`、`host_id`、`channel_id`、`app_count`，以及存在错误时的 `last_error`。`connected` 表示通道已绑定；App 登记状态另看 `apps`。 |

找不到有效本地服务记录时，`status` 返回 `{"running":false}`。已有记录但控制请求失败时，只有确认 PID 已退出才返回该结果，否则报错。除帮助外，CLI 都需要能读取指定的配置文件来定位状态目录。

`stop` 请求资源清理，然后等待服务进程退出，正常输出 `{"stopped":true}`。停止未运行的服务会报错。前台服务也可以通过 Ctrl+C 停止；Linux 还处理 SIGTERM。日常停止使用 CLI，不需要手工处理 PID 或本地控制凭据。

- 停止 OIS 会关闭 HTTP/HTTPS 监听和 OIC 通道，已接受而未返回的调用可能以 `CALL_RESULT_UNKNOWN` 结束。它不等同于停止远端 OIC 或终端 App 中的执行。
- 停止 OIC 会取消宿主调用、关闭 worker 并回收其托管进程；保留 App 加载记录。再次启动重载 App，但旧命令 `session_id` 不恢复。
- OIC 通道断开后会重连并重新登记；worker 崩溃后不会自动重启。

## OIC 的 load、apps、unload

`oic load <app.yaml>` 将 App YAML 的路径按调用 CLI 时的当前目录解析为绝对路径，发送给已运行的本机 OIC。文件由 OIC 本机读取；此命令不上传 App 包，不创建 OIS Tool，也不修改 vMCP。

加载过程解析 Action Schema 和有效 runtime，依次启动所需 worker 并检查 `ready`/handler；每个 worker 的 ready 等待期限为 10 秒。所有入口就绪后才保存加载记录并发布登记；中途失败会回滚本次启动的 worker。

同一 `app.code` 的已加载定义、App 目录和本机 App 配置相同时，重复 `load` 幂等，不重新启动 worker；仅 YAML 空白变化不算定义变化。已加载 App 的定义发生变化会返回 `APP_ALREADY_LOADED`，需要先 `unload` 再 `load`。已经退出的 worker 不会因为同定义重复 `load` 自动恢复，也应先卸载再加载。

`load` 返回该 App 状态，`apps` 返回按 App code 排序的状态数组。字段包括：

| 字段 | 含义 |
|---|---|
| `app`、`path` | App code 与本机 App YAML 绝对路径。 |
| `ready` | 本机 App 的全部所需 worker 是否可用。 |
| `registered`、`registration` | OIS 是否确认相应登记；文本为 `pending` 或 `registered`。登记完成仍可能报告某个 Action 不可用，必须同时看 `ready`。 |
| `actions` | 各 Action 的 `name`、`description`、`input_schema`、`ready`。 |
| `runtime_count` | 该 App 管理的 runtime 实例数。 |
| `error` | 有错误时返回本机加载或 worker 不可用原因。 |

OIS 暂不可达时也可以在本机加载 App，登记保持待完成状态。加载成功不表示中心 Tool 已配置，也不表示 Tool 可立即调用。

`oic unload <app-code>` 先删除该 App 的持久加载记录并撤销当前登记，再关闭其 worker。已有执行句柄失效；关闭阶段若报错，不会恢复已删除的记录或登记。对未加载且没有记录的 App 卸载会报 `NOT_FOUND`。

重启 OIC 时重新读取保存路径上的 App YAML 和启动配置中的 `app_config`，不保存 App 包副本。恢复失败的 App 保留记录并显示为不可用，不阻止其他 App 恢复；保存记录中的 App code 不能悄悄变更。修改 OIC YAML 中的本机配置需要重启 OIC，单独 `load` 不会重新读取 OIC 配置。

## 状态文件与本地控制

`state_dir` 相对配置文件目录解析，默认分别为 `.ois` 和 `.oic`。

| 文件 | 用途与保留规则 |
|---|---|
| `control.json` | 本机随机 `127.0.0.1` 控制地址、自动生成的 Bearer token、PID 和配置路径；正常退出时移除。CLI 自动使用，不是 Agent 的 MCP Token。 |
| `service.pid` | 当前服务 PID；正常退出时移除。 |
| `service.lock` | 实例锁的稳定文件；文件可在退出后保留，锁由操作系统释放。 |
| `ois.log` / `oic.log` | 后台运行日志，追加保留。前台模式直接使用其 stdout/stderr。 |
| OIC `loaded.json` | App code 与 App YAML 绝对路径列表；`stop` 保留，`unload` 删除对应条目。 |
| OIC `logs/worker-*.log` | worker stderr 诊断，单个日志保留上限约 1 MiB。 |

OIS 的业务持久配置另由 `server.data_file` 指定，不必位于 `state_dir`。这些运行文件不是供用户编辑的公开管理 API；资源变更使用[OIS API](ois-api.md)，本机生命周期使用这里的 CLI。

## oiapp-terminal

`oiapp-terminal` 是由 OIC 托管的 worker，不是供用户直接输入命令的 shell CLI。当前没有公开子命令或 flag，也不读取独立 `--config`；它在 stdin/stdout 使用 JSONL 协议，由 OIC 通过 `start.config` 提供本机配置，诊断写入 stderr。协议结束或 stdin EOF 后退出；协议服务失败时退出码为 `1`。

Bash 和 PowerShell 包通过 App YAML 的 `runtime.command` 引用相应平台二进制，同一终端实现的 shell 选择由本机配置完成。具体字段见 [App runtime](app-runtime.md) 和[终端 Actions](terminal-actions.md)。

## 实现依据

命令解析与生命周期：[servicecmd](../../../internal/servicecmd/service.go)；服务入口：[OIS](../../../cmd/ois/main.go)、[OIC](../../../cmd/oic/main.go)、[终端 worker](../../../cmd/oiapp-terminal/main.go)；App 加载与状态：[OIC Host](../../../internal/oic/host.go)。
