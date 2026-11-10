# 构建、部署与调用

本文提供构建、部署、调用和验证步骤。同一个 `workbench` vMCP 暴露四个固定工具；项目当前契约由[规范入口](../spec/README.md)维护，完成判据见[首版验收规格](../goal-shaping/current/beta/acceptance.md)。

文中的 `./scripts/...`、`.local/...` 和 Go 命令从 beta 仓库根目录执行；CLI 示例按所在部署目录使用配置和 App 路径。文档位置不改变这些命令的工作目录。系统职责和实现结构见[系统架构](../spec/architecture/system.md)及[目录说明](../spec/reference/repository.md)。

## 构建和目录

要求 Go 1.25 或更新版本；本次在 Go 1.26.3 验证。Windows 运行要求 PowerShell 7 和支持 ConPTY 的系统。Linux 使用 Bash。构建命令：

```powershell
./scripts/build.ps1
```

主程序产物为 `dist/windows-amd64` 和 `dist/linux-amd64` 下的 `ois`、`oic`；独立运行包为 `dist/apps/bash` 和 `dist/apps/powershell`，各自包含 `app.yaml` 与 `runtime/` 二进制。部署时直接复制对应 App 包，配置相对该 App YAML 解析。源码、依赖和独立构建步骤见 [apps](../../apps/README.md)，不需要先构建或修改 OIC。

```text
deployment/
  bin/ois, oic
  ois.yaml
  oic.yaml
  tls/                       # 显式启用 HTTPS 时放置证书/私钥
  apps/bash/app.yaml
  apps/bash/runtime/oiapp-bash
  workspace/
  state/ois/config.json
  state/oic/loaded.json
```

Windows 的 App 包为 `apps/powershell`，二进制带 `.exe`。OIC 配置中的 `app_config.<code>.default_cwd` 应设为已存在的绝对目录。

## CLI

以下是常用操作；全部命令、参数、默认值和返回边界见 [CLI 参考](../spec/reference/cli.md)，配置来源与持久化规则见[配置参考](../spec/reference/configuration.md)。

下面的 CLI 示例从部署包根目录执行，使用已准备好的 `ois.yaml`、`oic.yaml` 和 App 包。程序名需位于 `PATH`，否则替换为可执行文件的实际路径。

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

`start` 默认后台运行；需要前台观察时加 `--foreground`。OIC 启动后用 `apps` 检查本地 ready 与中心 registered，不能只凭 `start` 成功判断工具已经可调用。

修改 OIC 启动配置后需要停止再启动。`stop` 保留 App 加载记录，`unload` 删除对应记录；重新启动不会恢复旧命令句柄。OIS 已导入的通道、Tool 和 vMCP 通过管理 API 维护，单改初始化 YAML 不会覆盖持久配置。完整规则见上面的 CLI 与配置参考。

## 本次部署

`scripts/deploy.ps1` 构建两平台产物，并部署到 `admin@121.199.175.151:/home/admin/loom-oi-beta` 和当前 beta worktree 的 `.local/windows`。重复部署会先停止本次 beta 服务并保留持久状态。它不修改 main、alpha、其他 worktree 或远端其他服务。

```powershell
./scripts/deploy.ps1
```

默认部署不要求 Token，也不生成凭据。显式 `-RequireToken` 时脚本生成或复用 `.local/credentials.json`，Linux 环境文件为 `secrets.env`（0600）。管理、vMCP 和两通道的凭据独立；App worker 和命令使用受控环境，不继承 OIS/OIC token。

默认使用 HTTP，不需要证书。显式启用 HTTPS 时可加载公共 CA 签发的证书；只有使用私有 CA 才需额外配置客户端信任。`scripts/devcert` 为显式 HTTPS 测试模式提供独立 CA，不改变系统信任、不关闭校验。证书、监听和 Token 的完整配置见[接入配置](access-configuration.md)。

本版 MCP 验收入口为 `http://121.199.175.151:25100/mcp/workbench`，默认无需认证头或 CA 文件。HTTP API 消费入口为 `POST /api/v1/exec`，请求格式见 [HTTP 执行参考](../spec/reference/ois-api.md#3-rest-执行)。下面通过 PowerShell 模块验证 MCP 连接：

```powershell
Import-Module ./scripts/McpClient.psm1
$mcp = Connect-LoomMcp -Url 'http://121.199.175.151:25100/mcp/workbench'
Invoke-LoomTool $mcp linux_exec_command @{ cmd = 'ls -l' }
Invoke-LoomTool $mcp windows_exec_command @{ cmd = 'Get-ChildItem' }
$mcp.Client.Dispose()
```

单独启动 Windows OIC：`.local/windows/bin/oic.exe --config .local/windows/oic.yaml start`。只有显式配置 `ois.credential_env` 时才需先设置相应环境变量。Linux 启用 Token 的部署在远端部署目录 `/home/admin/loom-oi-beta` 中执行 `set -a; . ./secrets.env; set +a` 导入配置环境，再从该环境启动对应服务。

## 接续长命令与终端输入

`exec_command` 返回 `session_id` 时，用同组 `write_stdin` 接续；`chars=""` 只等待并领取后续输出。十分钟任务的首次调用可设置 `yield_time_ms=1000`，再用原句柄、空输入和 `yield_time_ms=30000` 顺序轮询，直到返回 `exit_code`。不要另起 `exec_command` 代替续接；完整参数与示例见[终端 Action](../spec/reference/terminal-actions.md)。

需要后续输入时，首次调用设置 `tty=true`。输入按原样交付：Linux `read` 用 `\n` 提交，Windows ConPTY 的 `Read-Host` 用 `\r` 模拟 Enter。输出可能包含终端回显和控制序列。

PowerShell MCP 模块将工具结果中的 `_meta.call_id` 复制为返回对象的 `_call_id`，便于记录调用证据；原始 HTTP/MCP 响应格式及取消规则见 [OIS API](../spec/reference/ois-api.md)。

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

根目录 `go test ./...` 不包含 App 模块。还须按各 App README，在 Linux 的 `apps/bash` 和 Windows 的 `apps/powershell` 内分别执行 `go test -count=1 -timeout=180s ./...`、`go vet ./...`；这些命令也可在 App 的仓库外副本执行，使用 `GOWORK=off` 验证独立性。Linux 对根模块与 Bash 另运行 `go test -race`；Windows 的真实 ConPTY 和进程回收在 Windows 实机验证。

长测必须先完成，再运行会停止或重启主服务的验收。`acceptance-management.ps1` / channelprobe 保留为显式 HTTPS+Token 部署的回归工具。实际业务调用全部从本机经同一个 vMCP 发出；SSH 用于部署、服务生命周期和独立进程核验。原始 JSONL 证据留在 `.local/reports`，不含 token。

已执行结果保存在[beta 首版报告](../reports/beta/acceptance-report.md)、[访问模式验证](../reports/beta/access-verification.md)和[独立 App 验收](../reports/beta/app-independence.md)，报告中的部署状态按其记录时间解释。
