# Bash oiapp

Linux Bash App，提供 `exec_command` 和 `write_stdin` 两个 Action。App 自己持有进程、PTY、会话、输出缓冲及回收状态；OIC 加载 `app.yaml`，通过 stdin/stdout 的 JSONL 协议托管 worker。

本目录是完整、独立的 Go 模块，可以直接复制到另一仓库构建。构建不依赖 loom-oi 主模块、`go.work`、本地 `replace` 或未发布共享库。OIC 与 App 之间共享的是协议，不是 Go 包。

## 目录

```text
app.yaml                 # App/Action 声明，指向本地构建产物
go.mod / go.sum          # 独立依赖与校验值
build.sh                 # Linux 构建入口
cmd/worker/              # JSONL worker 入口
internal/protocol/       # 本 App 自有的 worker 线协议类型
internal/terminal/       # Bash、PTY、会话、输出与 Action 实现及测试
internal/procgroup/      # Linux 进程监督、后代回收及测试
runtime/oiapp-bash       # 构建产物；不提交到源码仓库
```

## 构建与测试

需要 Go 1.25 或更高版本，以及依赖下载权限或预填充的 Go module cache。worker 运行环境为 Linux，需要可执行的 Bash；不支持 Windows 或 macOS worker。当前依赖仅为 `github.com/creack/pty` 和 `golang.org/x/sys`。

在 Linux 上，从本目录执行：

```sh
sh ./build.sh
GOWORK=off go test ./...
```

可从 Windows PowerShell 交叉编译 Linux amd64 产物：

```powershell
$env:GOWORK = 'off'
$env:CGO_ENABLED = '0'
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
New-Item -ItemType Directory -Force runtime | Out-Null
go build -trimpath -o runtime/oiapp-bash ./cmd/worker
```

交叉编译不等于运行测试；PTY、信号和后代回收测试必须在 Linux 上执行。测试使用本机 Bash、真实子进程和 PTY。部署时将整个 App 目录复制到 Linux，或仅交付 `app.yaml` 与 `runtime/oiapp-bash`，并确保 worker 可执行：

```sh
chmod +x /opt/oiapps/bash/runtime/oiapp-bash
oic --config /path/to/oic.yaml load /opt/oiapps/bash/app.yaml
oic --config /path/to/oic.yaml apps
```

OIC 必须已启动。`load` 不编译源码，也不安装依赖；相对程序路径按 `app.yaml` 所在目录解析。两个 Action 继承根 `runtime`，因此使用同一次加载的同一个 worker。不要为两个 Action 分别覆盖 runtime，否则它们不再共享本地会话。

同 App、同有效配置重复 `load` 是无操作；替换源码或二进制后应明确 `unload bash` 再 `load`。修改 OIC 的 `app_config` 需要重启 OIC 后重新加载；旧会话不能迁移。Tool 名称、通道与 vMCP 在 OIS 配置，App 不包含 OIS 地址或访问 Token。

## 本机配置

OIC 通过首帧 `start.config` 传入配置。以下是 `oic.yaml` 中的配置片段，不是完整 OIC 配置：

```yaml
app_config:
  bash:
    shell_kind: bash
    shell_path: /usr/bin/bash
    default_cwd: /opt/workspace
    env: {LANG: C.UTF-8}
    limits:
      max_concurrent_sessions: 8
      session_buffer_bytes: 1048576
      max_cmd_bytes: 65536
      max_input_bytes: 65536
      max_output_tokens: 20000
      max_runtime_ms: 1800000
      idle_timeout_ms: 600000
      finished_session_ttl_ms: 600000
      max_finished_sessions: 32
    terminal: {columns: 120, rows: 40}
```

`shell_kind` 省略时为 `bash`，其他值报错。`shell_path` 默认从 PATH 找 `bash`；工作目录默认是 worker 的当前目录（OIC 使用 App YAML 所在目录）。指定工作目录必须存在；相对路径按 worker 当前目录解析。`env` 覆盖受控继承环境，不复制宿主全部环境。

上例 limits 和终端尺寸就是默认值。所有限额必须为正数；缓冲区为 4–67108864 字节，`max_output_tokens` 配置上限为 40000，终端行列各为 1–32767。每个 worker 最多 64 个在途调用；完成调用的有限去重缓存最多 128 项、最长 10 分钟。进程管理用于生命周期回收，不提供命令沙箱。

## Action 契约

每次 `exec_command` 启动新的 `bash --noprofile --norc -c <cmd>`；不同执行之间不保留 `cd` 或环境变量修改。

| Action | 参数 | 语义 |
|---|---|---|
| `exec_command` | `cmd` | 必填，非空 Bash 脚本 |
| | `workdir` | 可选工作目录，默认 `default_cwd`；相对路径基于该目录 |
| | `tty` | 默认 `false`；`true` 使用真实 Linux PTY |
| | `yield_time_ms` | 默认 10000，范围 0–30000；只控制本次等待 |
| | `max_output_tokens` | 默认 10000，正整数，受本地配置上限约束 |
| `write_stdin` | `session_id` | 必填，必须来自同一 worker、身份和 vMCP |
| | `chars` | 默认空字符串；空串读取输出，非空写入 PTY，原样发送不补换行 |
| | `yield_time_ms` | 空输入默认 5000，非空默认 250，范围 0–30000 |
| | `max_output_tokens` | 同上 |

非 TTY 执行的 stdin 从启动起就是 EOF，只能读取后续输出。TTY 输入可使用 `\n` 表示 Enter、`\u0003` 表示 Ctrl+C；效果由目标程序和终端决定。每个会话只允许一个在途读写，冲突返回 `SESSION_BUSY`。输入写入最多等待 1000 毫秒；错误可能发生在部分字节已写入之后，不能盲目重发。

结果含 `output`，以及 `session_id` 或 `exit_code` 之一。执行结束结果可含 `termination_reason`、`signal`；输出丢弃可含 `output_truncated`、`omitted_bytes`，未正常排空可含 `output_incomplete`。每次交付消费全部待输出，预算不足时按头尾展示并明确截断；没有历史补读或游标。输出预算近似按每 token 4 个 UTF-8 字节计算，不是模型分词结果。后台持续排空输出，避免等待调用时阻塞命令。

长命令通过 `session_id` 继续调用 `write_stdin`，不会因为本次等待结束而终止。默认最长执行 30 分钟，闲置 10 分钟；获准的 session 操作续期，后台输出不续期。显式取消针对 `call_id`：尚未提交结果的 `exec_command` 回收其新执行，`write_stdin` 只取消本次输入/等待，不杀原会话、不能撤销已写字符；结果提交后的取消无效。没有公开的 session kill/list/resize Action。

## Worker 协议

stdin/stdout 为 UTF-8 JSON Lines，一行一个对象；stdout 只输出协议，诊断走 stderr。首帧必须为 `start`，成功后返回 `ready`，随后接受并发 `invoke` 和 `cancel`。单帧上限为 1 MiB。身份由 OIC 的可信上下文传入，`access_identity`、`vmcp_id` 均不能为空。

```jsonl
{"type":"start","protocol":1,"config":{"shell_kind":"bash"}}
{"type":"ready","protocol":1,"handlers":["exec_command","write_stdin"]}
{"type":"invoke","call_id":"example-1","handler":"exec_command","input":{"cmd":"printf hello","yield_time_ms":1000},"context":{"access_identity":"example-user","vmcp_id":"example-vmcp"}}
{"type":"result","call_id":"example-1","result":{"output":"hello","exit_code":0,"termination_reason":"exited"}}
{"type":"cancel","call_id":"example-1"}
```

上述示例交错展示两个方向：`start/invoke/cancel` 发给 worker，`ready/result` 由 worker 返回。失败响应为 `{"type":"error","call_id":"...","error":{"code":"...","message":"...","details":{}}}`；`details` 可省略。

stdin EOF 是关闭请求：回收所有执行及普通后代，收尾并退出。Linux supervisor 使用子进程收养与信号清理，保持 PTY、目标退出码及信号行为；OIC 仍需为托管 worker 提供自身的进程回收兜底。连接丢失或 worker 重启不提供跨实例会话恢复或恰好一次执行保证。
