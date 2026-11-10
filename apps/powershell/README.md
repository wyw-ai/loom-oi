# PowerShell App

独立的 Windows PowerShell 7 App，通过 protocol 1 的 stdio JSON Lines 与 OIC 对接。提供 `exec_command` 和 `write_stdin`，两者共享本次 worker 的执行后端。此目录可以完整复制到仓库之外构建、测试和加载；不依赖 OIS/OIC 源码、Go workspace、相对 replace 或未发布的公共 SDK。

## 构建与测试

需要 Go 1.25+。运行及真实终端测试需要 Windows、支持 ConPTY 的系统和可由 PATH 找到的 PowerShell 7 `pwsh`。这是 Windows App，不包含 Bash 或 Linux 执行适配。

在本目录执行：

```powershell
./build.ps1
go test -count=1 -timeout=180s ./...
go vet ./...
```

构建产物为 `runtime/oiapp-powershell.exe`，与 `app.yaml` 的入口一致。`build.ps1 -OutputDirectory <path>` 可以改变产物目录；相对路径按本 App 目录解析，自定义位置需相应部署或调整 YAML。直接构建等价于在 Windows 执行 `go build -trimpath -o runtime/oiapp-powershell.exe ./cmd/worker`。唯一第三方模块依赖为公开的 `golang.org/x/sys`。

## 加载与本机配置

保持 `app.yaml` 和 `runtime/` 的相对位置。启动既有 OIC 后，加载本目录的定义文件：

```text
oic --config C:\deployment\oic.yaml load C:\apps\powershell\app.yaml
oic --config C:\deployment\oic.yaml apps
```

本包 YAML 使用 `app.code: powershell`，OIC 配置的 `app_config.powershell` 在 worker 启动时通过 `start.config` 交付。例如：

```yaml
app_config:
  powershell:
    shell_kind: powershell
    shell_path: 'C:\Program Files\PowerShell\7\pwsh.exe'
    default_cwd: 'C:\workspace'
    env: {LANG: C.UTF-8}
```

`shell_kind` 省略时也是 `powershell`，其他值会报错。`shell_path` 默认查找 `pwsh`，不回退到 Windows PowerShell 5.1。`default_cwd` 必须存在，省略时使用 worker 初始目录，即 App YAML 所在目录。OIC 本机配置变化需要重启 OIC；旧 session 不恢复。两个 Action 都继承根 runtime，不要将它们配置成两个独立实例。

## Actions

| Action | 参数 | 行为 |
|---|---|---|
| `exec_command` | 必填 `cmd`；可选 `workdir`、`tty`、`yield_time_ms`、`max_output_tokens` | 新建独立 PowerShell 执行。默认管道模式，`tty=true` 使用真实 ConPTY。 |
| `write_stdin` | 必填 `session_id`；可选 `chars`、`yield_time_ms`、`max_output_tokens` | 空 `chars` 等待并领取新输出；非空输入写入原终端，再领取输出。 |

两者返回本批 `output`，运行中返回 `session_id`，结束返回 `exit_code`，以及 `output_truncated`、`omitted_bytes`、`output_incomplete`。终态还包括 `termination_reason`。非零退出是业务结果；输入适配、启动或 session 错误使用结构化 `error`。

`exec_command` 默认等待 10000 毫秒；`write_stdin` 空输入默认 5000 毫秒，非空默认 250 毫秒。等待范围为 0–30000 毫秒，输出预算默认 10000，按每单位约 4 个 UTF-8 字节估算。每次新执行不继承前次 `cd` 或环境修改。非空 stdin 只允许原执行为 `tty=true`，`Read-Host` 使用 `\r` 提交输入，不自动转换换行。

默认资源限制可在 `app_config.powershell.limits` 中调整：

| 字段 | 默认 |
|---|---|
| `max_concurrent_sessions` | 8 |
| `session_buffer_bytes` | 1048576 |
| `max_cmd_bytes`、`max_input_bytes` | 各 65536 |
| `max_output_tokens` | 20000 |
| `max_runtime_ms` | 1800000 |
| `idle_timeout_ms`、`finished_session_ttl_ms` | 各 600000 |
| `max_finished_sessions` | 32 |

限额必须为正；输出缓冲允许 4–67108864 字节，`max_output_tokens` 最多 40000。初始终端尺寸为 `terminal: {columns: 120, rows: 40}`，各维度范围 1–32767。缓冲超限丢弃最旧内容；每批因显示预算省略的内容不会补读。输出不延长闲置期限，合法的持续轮询会延长闲置期限。

## 协议与生命周期

首帧为 `start`（`protocol: 1`、可选 `config`），worker 返回 `ready` 和两个 handler。调用帧为 `invoke`，携带 `call_id`、`handler`、`input`、可信 `context.access_identity` 和 `context.vmcp_id`；响应为同 ID 的 `result` 或 `error`。`cancel` 按 `call_id` 尽力取消一次调用，stdin EOF 结束 worker。stdout 专用于 JSONL，诊断写 stderr。

session 绑定当前 worker 和可信访问范围，同一 session 一次只允许一个读写操作。取消续读不会结束已有执行，已写入字符不能回滚；取消尚未提交结果的新执行会请求回收它。没有公开的 session 列表、强制停止或 resize Action。未知结果不得自动重跑。

Windows 进程在恢复执行前加入 App 自有的 Job Object，支持普通管道和 ConPTY 后代回收。EOF、卸载或宿主关闭会回收执行；worker 崩溃由其 OIC 宿主的进程托管兜底。这是进程生命周期管理，不是文件系统或网络沙箱。
