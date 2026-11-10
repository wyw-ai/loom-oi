# beta 实现与运行

本文提供构建、部署、调用和验证步骤。同一个 `workbench` vMCP 暴露四个固定工具；项目当前契约由[规范入口](spec/README.md)维护，完成判据见[首版验收规格](goal-shaping/current/beta/acceptance.md)。

## 模块

```text
cmd/ois                    OIS CLI + HTTP/可选 HTTPS 监听
cmd/oic                    OIC CLI
internal/servicecmd        后台启动、回环鉴权管理、停止和状态
internal/ois               持久配置、管理 API、MCP、固定 WS/可选 WSS 通道
internal/oic               YAML、参数绑定、worker 托管、连接与登记
internal/protocol          JSONL/WS/WSS 公共消息
cmd/oiapp-terminal         终端 App 可执行文件
internal/terminal          exec_command/write_stdin、输出、会话和期限
internal/procgroup         Linux 子进程监督 / Windows Job Object
examples                   OIS/OIC 与两种 App 配置
scripts                    构建、部署、证书、PowerShell MCP 客户端与验收
```

OIS 不解析 Bash/PowerShell，OIC 不管理命令会话。两种 App 使用同一个终端实现二进制，由本机 `app_config` 选择 shell。Linux 使用 Bash 和 PTY；Windows 使用 PowerShell 7 和 ConPTY。

Linux 的私有监督进程作为 subreaper 回收普通后代，包括 PTY 创建的新 session；Windows 在恢复子进程前将其放入 Job Object。命令身份、输出和输入状态由本次 App worker 持有，不跨 worker 重启恢复。模块交互与源码职责详见[系统架构](spec/architecture/system.md)及[目录说明](spec/reference/repository.md)。

## 构建和目录

要求 Go 1.25 或更新版本；本次在 Go 1.26.3 验证。Windows 运行要求 PowerShell 7 和支持 ConPTY 的系统。Linux 使用 Bash。构建命令：

```powershell
./scripts/build.ps1
```

产物为 `dist/windows-amd64` 和 `dist/linux-amd64` 下的 `ois`、`oic`、`oiapp-terminal`。将终端 App 二进制放进对应 App 包的 `runtime/`，配置相对该 App YAML 解析：

```text
deployment/
  bin/ois, oic
  ois.yaml
  oic.yaml
  tls/                       # 显式启用 HTTPS 时放置证书/私钥
  apps/bash/app.yaml
  apps/bash/runtime/oiapp-terminal
  workspace/
  state/ois/config.json
  state/oic/loaded.json
```

Windows 的 App 包为 `apps/powershell`，二进制带 `.exe`。OIC 配置中的 `app_config.<code>.default_cwd` 应设为已存在的绝对目录。

## CLI

以下是常用操作；全部命令、参数、默认值和返回边界见 [CLI 参考](spec/reference/cli.md)，配置来源与持久化规则见[配置参考](spec/reference/configuration.md)。

```text
ois --config ois.yaml start [--foreground]
ois --config ois.yaml status
ois --config ois.yaml stop

oic --config oic.yaml start [--foreground]
oic --config oic.yaml load apps/bash/app.yaml
oic --config oic.yaml apps
oic --config oic.yaml unload bash
oic --config oic.yaml status
oic --config oic.yaml stop
```

`start` 默认后台运行，并返回 JSON 状态。`--foreground` 适用于控制台或外部服务管理器；本版不安装系统开机启动服务。`--config` 可放在子命令前后，默认读取当前目录的同名 YAML。

本地控制地址随机分配在 `127.0.0.1`，凭据和 PID 存在 `state_dir`；CLI 自动读取，不在状态中输出 token。停止会先回收资源，等待服务进程退出。OIC `stop` 保留 App 加载记录；`unload` 删除对应记录。再次 `start` 会加载记录，不恢复执行句柄。已退出 worker 不自动重启。

OIS 首次导入 YAML 中的 channels/tools/vmcps；之后以 `server.data_file` 为权威。管理 API 的修改持久保存，正常重启不重新覆盖。启动地址、TLS 与凭据环境引用仍由启动配置和进程环境提供。

## 本次部署

`scripts/deploy.ps1` 构建两平台产物，并部署到 `admin@121.199.175.151:/home/admin/loom-oi-beta` 和当前 beta worktree 的 `.local/windows`。重复部署会先停止本次 beta 服务并保留持久状态。它不修改 main、alpha、其他 worktree 或远端其他服务。

```powershell
./scripts/deploy.ps1
```

默认部署不要求 Token，也不生成凭据。显式 `-RequireToken` 时脚本生成或复用 `.local/credentials.json`，Linux 环境文件为 `secrets.env`（0600）。管理、vMCP 和两通道的凭据独立；App worker 和命令使用受控环境，不继承 OIS/OIC token。

默认使用 HTTP，不需要证书。显式启用 HTTPS 时可加载公共 CA 签发的证书；只有使用私有 CA 才需额外配置客户端信任。`scripts/devcert` 为显式 HTTPS 测试模式提供独立 CA，不改变系统信任、不关闭校验。证书、监听和 Token 的完整配置见[接入配置](access-configuration.md)。

本版 MCP 验收入口为 `http://121.199.175.151:25100/mcp/workbench`，默认无需认证头或 CA 文件。HTTP API 消费入口为 `POST /api/v1/exec`，请求格式见 [HTTP 执行参考](spec/reference/ois-api.md#3-rest-执行)。下面通过 PowerShell 模块验证 MCP 连接：

```powershell
Import-Module ./scripts/McpClient.psm1
$mcp = Connect-LoomMcp -Url 'http://121.199.175.151:25100/mcp/workbench'
Invoke-LoomTool $mcp linux_exec_command @{ cmd = 'ls -l' }
Invoke-LoomTool $mcp windows_exec_command @{ cmd = 'Get-ChildItem' }
$mcp.Client.Dispose()
```

独立重启 Windows OIC：`.local/windows/bin/oic.exe --config .local/windows/oic.yaml start`。只有显式配置 `ois.credential_env` 时才需先设置相应环境变量。Linux 同理；启用 Token 的部署可通过 `set -a; . ./secrets.env; set +a` 导入配置环境。

## 调用语义

本节提供使用摘要；全部参数、结果字段和执行边界见[终端 Action](spec/reference/terminal-actions.md)，MCP/REST 报文见 [OIS API](spec/reference/ois-api.md)。

`exec_command` 接收 `cmd`，以及可选 `workdir`、`tty`、`yield_time_ms`、`max_output_tokens`。每次创建独立 shell；工作目录通过进程属性设置，命令正文原样交付。`write_stdin` 接收 `session_id`、可选 `chars` 和相同等待/输出预算参数。

返回本轮 `output`；仍在运行时返回 `session_id`，完成时返回 `exit_code`，并提供 `output_truncated`、`omitted_bytes`、`output_incomplete`。命令退出非零是正常工具结果。预算按约 4 个 UTF-8 字节/token，非模型 tokenizer。后台持续收集，缓冲最多 1 MiB；领取时省略的输出不会再补读。

`tty=false` 的输入是 EOF，非空 `chars` 会报错。`tty=true` 输入字节不被自动替换；Linux `read` 用 `\n`，Windows ConPTY 中 `Read-Host` 用 `\r` 模拟 Enter。控制序列可能出现在原始终端输出中。

十分钟任务先 `exec_command`（如等待 1000 ms），再用原组 `write_stdin` 空输入、等待 30000 ms 续接，直到退出。输出不续期，无调用闲置 10 分钟会回收；正常持续轮询可跨越此期限。单次请求 60 秒期限与执行最长 30 分钟分开。

MCP 使用 2025-11-25：initialize → initialized 通知 → tools/list、tools/call。工具结果位于 `structuredContent`，调用 ID 位于工具结果 `_meta.call_id`；PowerShell 测试模块为便于证据记录，把这个 ID 复制到返回对象的 `_call_id`。MCP session header 与命令的 `session_id` 是不同概念。

显式 MCP `notifications/cancelled` 映射内部 cancel。取消一次续读不杀已有执行，已写入内容不能回滚；取消尚未提交结果的新执行会回收它。REST 的服务端请求期限会触发 cancel，没有单独的公开取消 API。普通 HTTP/WS/WSS 失联不证明已取消，也不自动重放命令；交付后失联返回 `CALL_RESULT_UNKNOWN`。

## 验证

```powershell
go test -count=1 -timeout=180s ./...
go vet ./...
./scripts/acceptance-commands.ps1
./scripts/acceptance-commands.ps1 -LongRunningOnly
./scripts/acceptance-failures.ps1
./scripts/acceptance-lifecycle.ps1
./scripts/acceptance-reclamation.ps1
./scripts/acceptance-access.ps1 # 验证四种访问模式，最后恢复默认匿名HTTP
```

长测必须先完成，再运行会停止或重启主服务的验收。`acceptance-management.ps1` / channelprobe 保留为显式 HTTPS+Token 部署的回归工具。实际业务调用全部从本机经同一个 vMCP 发出；SSH 用于部署、服务生命周期和独立进程核验。原始 JSONL 证据留在 `.local/reports`，不含 token。Linux 另运行 `go test -race`；Windows 的真实 ConPTY 和进程回收在 Windows 实机验证。
