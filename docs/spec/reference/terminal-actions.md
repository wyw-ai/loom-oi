# Bash / PowerShell 终端 Actions

当前命令 App 只提供 `exec_command` 和 `write_stdin` 两个 Action。Linux 使用 Bash 和 PTY，Windows 使用 PowerShell 7（`pwsh`）和 ConPTY；两端共享参数与结果形状，命令语法、路径和终端控制字符遵循目标平台。

本页覆盖已交付命令 worker 的业务接口、本地配置及生命周期。App YAML、Schema 适配、runtime 实例和 stdio 协议见 [App 运行时](app-runtime.md)；OIS 调用入口见 [OIS API](ois-api.md)；部署配置及命令分别见[配置参考](configuration.md)和 [CLI](cli.md)。

## 1. Action 与 Tool

Action 名属于本地 App；Tool 名由 OIS 配置，不硬编码在命令 worker 中。仓库示例为同一个 `workbench` vMCP 配置四个固定 Tool：

| Tool | 固定通道 | App.Action |
| --- | --- | --- |
| `linux_exec_command` | `linux-151` | `bash.exec_command` |
| `linux_write_stdin` | `linux-151` | `bash.write_stdin` |
| `windows_exec_command` | `windows-local` | `powershell.exec_command` |
| `windows_write_stdin` | `windows-local` | `powershell.write_stdin` |

每组两个 Action 继承本 App 的根 runtime，共用该 App worker。两平台、不同 App 或独立 runtime 不共享 session。Agent 选择 Tool 后直接传入业务参数，不传 `host_id`、`channel_id`、runtime 或任意程序入口；公开 Tool 参数没有 `input` 外包装。

一次 `exec_command` 创建一个新进程执行，不继承前一次执行中的 `cd`、环境变量修改或 shell 变量。只有显式启动并保持存活的同一交互 shell，才可通过后续输入继续使用其进程内状态。

## 2. `exec_command` 参数

| 字段 | 本地目标类型 | 必填 / 默认 | 行为 |
| --- | --- | --- | --- |
| `cmd` | string | 必填 | 对应 Bash 或 PowerShell 的完整脚本文本；不能全为空白；保持原文；受 `limits.max_cmd_bytes` 限制 |
| `workdir` | string | 省略使用 `default_cwd` | 绝对路径直接使用；相对路径基于 `default_cwd` 解析；必须是现有目录 |
| `tty` | boolean | `false` | `false` 为普通管道；`true` 为目标平台真实终端 |
| `yield_time_ms` | integer | `10000` | 单次输出等待窗口，范围 `0`–`30000` 毫秒 |
| `max_output_tokens` | integer | `10000` | 正整数；按每个单位约 4 个 UTF-8 字节估算正文预算；请求超过本机上限时按本机上限执行 |

`workdir` 不展开变量、`~` 或命令替换，不修改 OIC 默认配置，也不是文件访问沙箱。它由进程 API 设置，不能拼接进 `cmd`。

Bash 使用 `[shell_path, "--noprofile", "--norc", "-c", cmd]`，不因 `tty=true` 自动增加 `-i`。PowerShell 使用 `-NoLogo -NoProfile -Command <cmd>`；普通管道额外使用 `-NonInteractive`，终端模式保留 `Read-Host` 等交互能力。不得把 PowerShell 文本按 Bash 语法处理。

等待窗口从进程创建后的等待阶段计算，不是命令总运行期限，也不是网络端到端期限。`0` 表示启动后立即取得可返回结果；若执行已结束且输出已收尾，可直接返回终态。否则窗口到期返回当前输出和 `session_id`，不会只因一个字节输出到达就必须提前返回。

## 3. `write_stdin` 参数

| 字段 | 本地目标类型 | 必填 / 默认 | 行为 |
| --- | --- | --- | --- |
| `session_id` | string | 必填 | 来自本组同一 worker 实例的执行句柄 |
| `chars` | string | `""` | 空字符串仅等待读取；非空时按 UTF-8 原样写入终端，不自动补换行 |
| `yield_time_ms` | integer | 空输入 `5000`；非空输入 `250` | 输入写入之后的等待窗口，范围 `0`–`30000` 毫秒 |
| `max_output_tokens` | integer | `10000` | 与 `exec_command` 相同的输出预算和本机上限规则 |

空输入不是 EOF、关闭或取消。`write_stdin` 不接受 `cmd`、`workdir` 或 `tty`；这些属性在创建执行时固定。非空输入仅适用于仍可写入的终端执行，普通管道返回 `STDIN_NOT_WRITABLE`。

单次输入写入期限为 1000 毫秒，与输出等待窗口分开。写入失败或取消可能发生在部分字节已送达之后；错误中的 `details.input_bytes_written` 表示已知写入量，不能盲目重发整段内容。

两 Action 都拒绝未知业务字段。OIC 会先按本地 Schema 做固定默认值和基本类型适配；显式 `null` 不采用默认值。命令 App 再检查非空命令、范围、目录和自身支持的参数。`write_stdin` 的动态等待默认由 App 根据 `chars` 决定；若 YAML 显式声明了固定默认，OIC 交付的具体值优先。

## 4. 共同结果与错误

成功的 Action 结果始终包含 `output`，并且只返回 `session_id` 与 `exit_code` 中的一项：

```json
{
  "output": "building...\n",
  "session_id": "s_7cd0a2b1",
  "output_truncated": false,
  "omitted_bytes": 0,
  "output_incomplete": false
}
```

```json
{
  "output": "done\n",
  "exit_code": 0,
  "termination_reason": "exited",
  "output_truncated": false,
  "omitted_bytes": 0,
  "output_incomplete": false
}
```

| 字段 | 类型 / 出现条件 | 含义 |
| --- | --- | --- |
| `output` | string，始终出现 | 本次领取的可显示文本，可以为空；TTY 输出可能带回显和 ANSI 控制序列 |
| `session_id` | string，仍可继续时 | 不透明句柄，不是 PID、MCP 会话 ID 或权限凭证 |
| `exit_code` | integer，终态时 | 已确认的进程退出状态；出现后不再返回 session |
| `termination_reason` | string，终态时 | 通常为 `exited`、`signal`、`runtime_limit`、`idle_limit`；管理回收内部也记录 `shutdown`、`cancelled` |
| `signal` | integer，仅有非零 POSIX 信号信息时 | 信号编号；Windows 不伪造该字段 |
| `output_truncated` | boolean，始终出现 | 本批显示预算或缓冲容量导致输出被省略 |
| `omitted_bytes` | integer，始终出现 | 已知被省略的原始输出字节数，包含缓冲丢弃及本次显示省略；不计提示文字 |
| `output_incomplete` | boolean，始终出现 | 输出收尾被迫提前关闭，可能存在数量未知的未收集内容 |

非零退出是正常业务结果，例如 `exit 7` 返回 `exit_code: 7`，不自动变成调用错误。POSIX 已确认为信号终止时使用 `128 + signal` 的退出码并记录信号原因；Windows 使用实际进程退出状态。返回的是所启动进程的状态，不擅自用脚本中某条命令的状态替代整个进程结果。

取消、卸载或失联不保证调用方收到一个终态 `result`。无法确认退出状态时不能编造 `exit_code`；调用层可能报告结果未知，App 也可能保留未结束句柄和不完整输出状态。

### 错误码

错误使用公共 `error.code/message/details`，不消费未提交的业务输出。

| 错误码 | 当前含义与副作用边界 |
| --- | --- |
| `INVALID_ARGUMENT` | 缺参、无法适配、未知参数或非法范围；本次未创建命令或写入字符 |
| `WORKDIR_UNAVAILABLE` | 执行目录不可用；未启动目标 shell |
| `START_FAILED` | 目标进程启动失败；与进程启动后的非零退出区分 |
| `CAPACITY_EXCEEDED` | 活动执行或在途调用达到上限；未排队等待启动 |
| `SESSION_UNAVAILABLE` | 句柄不存在、已回收、不属于原实例，或可信身份/vMCP 不符 |
| `SESSION_BUSY` | 同一 session 已有读写操作；本次未输入且未消费输出 |
| `STDIN_NOT_WRITABLE` | 管道模式、输入端已关闭或执行正在结束；本次未写入 |
| `INPUT_WRITE_FAILED` | 输入失败或写入超时；可能已部分写入，尽可能附 `input_bytes_written` |
| `CALL_ID_CONFLICT` | 同一调用 ID 被用于不同输入、handler、身份或实现；不作为重复请求再次执行 |
| `CALL_CANCELLED` | 当前 Action 被取消；已经发生的命令和输入副作用不回滚 |
| `APP_UNAVAILABLE` | worker 关闭或本地 App 不可用；可能由宿主返回 |
| `CALL_RESULT_UNKNOWN` | 宿主无法确认已交付调用的结果，不是安全重试信号 |

当前实现统一以 `SESSION_UNAVAILABLE` 表达已不存在的句柄，不提供历史草稿中可选的 `SESSION_CLOSED` / `SESSION_EXPIRED` 细分记录。通道、访问配置和 OIS 路由错误另见 [OIS API](ois-api.md)。

## 5. 本机 App 配置与默认限额

配置位于 OIC YAML 的 `app_config.bash` 或 `app_config.powershell`，由 `start.config` 交付 worker。消费者不能用 Action 参数覆盖这些值。

| 字段 | 类型 | 省略时 / 约束 |
| --- | --- | --- |
| `shell_kind` | string | Linux 默认 `bash`，Windows 默认 `powershell`；两平台分别只接受对应值 |
| `shell_path` | string | 默认从 `PATH` 查找 `bash` / `pwsh`；加载时解析为绝对入口并检查可用性 |
| `default_cwd` | string | 默认 worker 初始目录，即 App YAML 所在目录；配置相对路径也相对于该目录；须为现有目录 |
| `env` | object<string,string> | 在 App 的受控基础环境上补充/覆盖同名环境变量；不是复制 OIC 全部环境 |
| `limits` | object | 下表中的命名限额；未配置项沿用默认值 |
| `terminal` | object | 初始终端尺寸，不提供运行中 resize Action |
| `terminal.columns` | integer | `120`，范围 `1`–`32767` |
| `terminal.rows` | integer | `40`，范围 `1`–`32767` |

`env` 键不能为空，不能含 `=` 或 NUL；值不能含 NUL。App 为命令设置 `TERM=xterm-256color`，本机显式 `env` 可覆盖它。OIC 首先限制 worker 继承的环境并排除配置的通道凭据变量，App 再构造命令环境；这不是操作系统沙箱或任意业务秘密的自动隔离机制。

Windows 交付与验收使用 PowerShell 7；默认查找 `pwsh`，不自动回退到 Windows PowerShell 5.1。管理员负责正确配置 `shell_path`；worker 不探测任意所配可执行文件的产品版本。

| `limits` 字段 | 默认值 | 行为 / 校验 |
| --- | --- | --- |
| `max_concurrent_sessions` | `8` | 每 worker 的活动执行上限；已完成执行释放活动名额 |
| `session_buffer_bytes` | `1048576`（1 MiB） | 每个 session 的待领取输出缓冲；允许 `4`–`67108864` 字节 |
| `max_cmd_bytes` | `65536`（64 KiB） | 单次 `cmd` 的 UTF-8 字节上限 |
| `max_input_bytes` | `65536`（64 KiB） | 单次 `chars` 的 UTF-8 字节上限 |
| `max_output_tokens` | `20000` | 单次请求预算的本机上限；可配置值最多 `40000`，以保持帧大小有界 |
| `max_runtime_ms` | `1800000`（30 分钟） | 从本次进程启动起计算的最长执行时间，独立于每次等待窗口 |
| `idle_timeout_ms` | `600000`（10 分钟） | 无获准 session 操作的闲置期限；后台输出不续期 |
| `finished_session_ttl_ms` | `600000`（10 分钟） | 已结束但尚未领取终态的句柄最多保留时间 |
| `max_finished_sessions` | `32` | 已结束且无人操作的未领取 session 上限；超出时淘汰较早结束者 |

所有限额必须为正整数，不支持用 `0` 禁用。配置变更需重启 OIC 重新交付，原 session 不迁移。示例配置见 [Linux OIC](../../../examples/oic.linux.yaml) 和 [Windows OIC](../../../examples/oic.windows.yaml)。

以下为当前 worker 的固定实现边界，没有对应 App YAML 配置字段：最多 64 个在途 Action；已完成调用去重最多 128 个、保留最多 10 分钟；输入写入期限 1000 毫秒；确认主进程结束后的输出排空窗口 1000 毫秒。宿主的单帧、ready、调用和关闭边界见 [App 运行时](app-runtime.md)。

## 6. 输出消费与结束收尾

worker 自进程启动起持续读取输出，不等待 Agent 轮询才采集。普通管道把 stdout/stderr 合并到同一输出通道；TTY 读取终端输出。接口不提供两条流各自的精确顺序，也不用于二进制文件传输。

每次成功提交 Action 结果时，worker 在短临界区取得待输出快照，按预算保留头尾并添加截断提示，将完整响应放入该 `call_id` 的缓存，同时消费整个快照。预算极小时只保留可容纳的前缀。元数据不占正文预算，截断提示可占少量额外空间。

**本次因预算省略的中间内容也已经被消费，下一次不会补读。** 缓冲容量超限时丢弃最旧内容、保留最新内容，并累计原始丢弃字节数；下一次交付将其与显示省略量合并报告。UTF-8 字符可跨读取边界保留，非法编码以替代字符显示，不改变已知原始省略字节的计数。

进程退出后仍须收尾输出：主进程退出状态已确认，并且输出读取结束或排空窗口到期，才能形成终态。到期强制关闭读取时设置 `output_incomplete`。仅观察到输出 EOF 或排空超时都不能替代进程退出确认。

主进程结束后，App 回收其管理范围内仍存活的后代。命令执行接口不承诺通过 `&`、后台化或子进程继承输出句柄长期托管服务。终态领取后 session 从执行表删除；同一 `call_id` 在缓存有效期内仍可复用已提交响应，但新的调用 ID 不能重新领取原终态或省略的历史输出。

## 7. 终端、身份与并发

| 行为 | `tty=false` | `tty=true` |
| --- | --- | --- |
| 后端 | 普通输出管道 | Linux PTY / Windows ConPTY |
| 初始 stdin | 空输入，立即 EOF | 保持终端输入端 |
| 空 `chars` | 等待并读取 | 等待并读取 |
| 非空 `chars` | `STDIN_NOT_WRITABLE` | 原样提交，随后等待读取 |
| 控制字符 | 不作为取消机制 | 由目标终端和程序解释；不保证无条件结束进程 |

Linux `read` 的代表输入为 `"loom\n"`；Windows ConPTY `Read-Host` 的 Enter 为 `"loom\r"`。App 不自动把 LF 改成 CR，也不把 Ctrl+D 定义为跨平台关闭 stdin 的公共 API。

session 绑定原 App worker 运行周期及可信 `access_identity`、`vmcp_id`。每次续接验证归属；新 worker、另一个平台、不同 runtime 或不同访问范围都不能续接它。匿名访问同一 vMCP 时共享匿名范围，不能仅凭客户端自报 Bearer 或 session 字符串形成用户隔离；访问模式见[配置参考](configuration.md)。

同一 session 同时只允许一个操作；并发读也受此限制。第二个调用返回 `SESSION_BUSY`，既不写入也不消费输出。后台采集独立于此锁，不同 session 可以并行。

闲置时间从创建或上次获准进入 session 操作开始计算；拒绝的调用、重复缓存响应和后台输出不续期。合法读写正在等待时不触发闲置回收，最长执行期限仍独立生效。因此持续轮询可让十分钟无输出命令继续运行，但不能突破 30 分钟默认运行期限。

## 8. 取消、断连与资源回收

取消目标是一次 `call_id`，不是新增的 `cancel_session` Action。MCP 显式取消、REST 服务端请求期限与内部帧的映射见 [OIS API](ois-api.md)。App 按响应提交点决定行为：

| 发生阶段 | 行为 |
| --- | --- |
| `exec_command` 尚未启动 | 阻止本次启动 |
| `exec_command` 已创建执行，结果未提交 | 请求回收这次新执行，返回取消错误；已发生的业务副作用不撤销 |
| `write_stdin` 结果未提交 | 停止本次等待或尚未完成的输入；不杀原 session，不消费未提交输出；已写入字节不回滚 |
| `result/error` 已提交 | 保留既有结果、去重缓存和输出消费；迟到取消无效 |

取消与结果提交串行裁决。一个未收到确认的取消请求不能证明进程已停止；写入后等待被取消也不能报告成“没有发送输入”。

意外网络失联不自动重跑命令、不重发输入，也不切换到另一 App。原 worker 仍存活、访问范围仍有效且固定通道恢复时，已知 session 可以继续使用；结果未知且没有拿到句柄的创建请求，不保证有恢复查询入口。

首版没有独立强制停止、session 列表、resize 或输出游标 Action。非 TTY 执行在句柄已提交后，依靠正常结束、运行/闲置限额或本地管理回收；终端执行可尝试适用的控制字符，但必须继续观察是否真正退出。

卸载、OIC 停止、worker EOF 或崩溃会回收托管执行；旧句柄不可迁移或自动重建。Windows 在执行用户逻辑前把进程加入 Job Object，Linux 通过进程组及独立 subreaper 监督普通后代；PTY/ConPTY 资源也在相应关闭路径处理。Linux 管理终止有 2 秒等待后再强制回收；Windows Job 回收可直接终止进程，不承诺两平台都有完全相同的优雅退出信号序列。主动逃离普通托管范围的外部服务不在完整隔离保证内。

## 9. 调用例子

Linux 短命令，传给 `linux_exec_command`：

```json
{"cmd":"pwd; ls -l","yield_time_ms":1000}
```

Windows 交互命令，传给 `windows_exec_command`：

```json
{"cmd":"$name = Read-Host 'name'; Write-Output ('hello ' + $name)","tty":true,"yield_time_ms":1000}
```

取得句柄后传给 `windows_write_stdin`，CR 按原样交付：

```json
{"session_id":"s_example","chars":"loom\r","yield_time_ms":1000}
```

Linux 十分钟无输出命令的首次调用：

```json
{"cmd":"sleep 600; printf 'done\\n'","yield_time_ms":1000}
```

Windows 对应命令为 `Start-Sleep -Seconds 600; Write-Output 'done'`。之后使用各自的续接工具顺序轮询：

```json
{"session_id":"s_example","chars":"","yield_time_ms":30000}
```

中间 `output` 可为空；收到 `session_id` 就继续，收到 `exit_code` 即完成。不能通过再次 `exec_command` 代替续接。

## 10. 实现依据

- Bash：[配置](../../../apps/bash/internal/terminal/config.go)、[Action 与会话](../../../apps/bash/internal/terminal/engine.go)、[输出](../../../apps/bash/internal/terminal/output.go)、[协议入口](../../../apps/bash/internal/terminal/serve.go)、[PTY](../../../apps/bash/internal/terminal/process_linux.go)。
- PowerShell：[配置](../../../apps/powershell/internal/terminal/config.go)、[Action 与会话](../../../apps/powershell/internal/terminal/engine.go)、[输出](../../../apps/powershell/internal/terminal/output.go)、[协议入口](../../../apps/powershell/internal/terminal/serve.go)、[ConPTY](../../../apps/powershell/internal/terminal/process_windows.go)。
- 测试：[Bash 运行契约](../../../apps/bash/internal/terminal/engine_test.go)、[Linux 信号](../../../apps/bash/internal/terminal/signal_linux_test.go)、[Bash 后代回收](../../../apps/bash/internal/procgroup/group_test.go)、[PowerShell 运行契约](../../../apps/powershell/internal/terminal/engine_test.go)、[PowerShell 后代回收](../../../apps/powershell/internal/procgroup/group_test.go)。

这些链接用于定位当前实现和已有测试，不表示维护本文时重新执行了功能验收。返回[规范入口](../README.md)。
