# 独立 oiapp

这里收录随项目维护的独立 App。每个目录都是完整源码包：YAML、worker 实现、依赖、构建入口、测试和使用说明放在一起，可以整体复制到主工程之外构建和交付。

| App | 目标平台 | 构建入口 | 运行包 |
| --- | --- | --- | --- |
| [Bash](bash/README.md) | Linux | App 目录下 `sh ./build.sh` | `app.yaml` + `runtime/oiapp-bash` |
| [PowerShell](powershell/README.md) | Windows | App 目录下 `./build.ps1` | `app.yaml` + `runtime/oiapp-powershell.exe` |
| [Filesystem](filesystem/README.md) | Windows amd64、Linux amd64/arm64 | `go run ./cmd/package -os <windows或linux> -arch <amd64或arm64>` | `dist/filesystem-<os>-<arch>/`，一份清单和一个 Go worker |
| [Browser](browser/README.md) | Windows 11 x64、Ubuntu 24.04 x64 | 在目标平台运行 `npm ci`、`npm run install-browser`、`npm run package` | `dist/browser-*.zip` 或 `dist/browser-*.tar.gz`，解压后加载随包 Node、worker 与 Chromium |
| [Computer Use](computer-use/README.md) | Windows x64 交互桌面 | App 目录下 `./build.ps1` | `app.yaml` + `runtime/oiapp-computer-use.exe` |
| [File Store](file-store/README.md) | Windows、Linux | App 目录下 `./build.ps1` 或 `sh ./build.sh` | 对应平台的 `app.yaml` 和 worker，Windows 包位于 `dist/file-store-windows/` |
| [Loom OI](loom-oi/README.md) | Windows、Linux | App 目录下 `./build.ps1` 或 `sh ./build.sh` | 在管理 OIC 加载，按调用身份查询目录和管理 Service。 |
| [Skills](skills/README.md) | Windows、Linux | App 目录下 `./build.ps1` 或 `sh ./build.sh` | 在目标 Host 加载，分层读取工作区和用户目录中的 Skill。 |

各 Go App 有独立的 `go.mod` / `go.sum`。Browser 使用自己的 npm lockfile。根目录的构建脚本组合交付 OIS、OIC 与两个终端 App。其余 App 使用上表的独立构建入口。运行普通 `go test ./...` 时，各 App 模块需分别测试。CI 配置另将 App 源码复制到仓库外构建和测试。

要在 workbench Service 中组合 Computer Use、File Store 和 Filesystem，把 Host A 截图经服务端 AList 上传、下载并交给 agent 辨认，请使用配套的构建、部署与验收示例。[Host A 截图验收指南](../docs/guides/service-files.md)

## OIC 与 App 的边界

OIC 从 YAML 读取 Action、输入 Schema 和 runtime，托管 worker、适配输入、转发调用与取消、向 OIS 登记状态。App 自己实现能力并持有执行状态；Bash / PowerShell 的命令、终端、会话、输出缓冲和子进程回收均在各自 App 内。OIC 不导入 App 代码，也不识别 Bash 或 PowerShell 命令。

各 App 通过 YAML 和 JSONL worker 协议接入。Filesystem 和 Browser 共用声明式 `content.v1` 结果。读取根内文件、操作标签页以及管理进程和会话的代码都在各自的 App 中。第三方 App 可使用独立仓库。[App runtime](../docs/spec/reference/app-runtime.md)

## 使用与迁移

构建完成后，在目标机器执行：

```text
oic --config /path/to/oic.yaml load /path/to/app/app.yaml
oic --config /path/to/oic.yaml apps
```

将完整发行包复制到受控机。`load` 以 YAML 所在目录为基准解析 runtime，直接启动包内程序。Browser 需要一并保留包内的 Node、worker 依赖和 Chromium。访问凭据和连接地址留在 OIC 配置中。

从旧 `oiapp-terminal` 迁移时，先构建新 App，卸载原 App，再加载新路径；两个 `app.code`（`bash` / `powershell`）、Action 名、参数和 worker 协议版本保持不变，OIS 的固定绑定无需修改。卸载会结束旧会话，无法将它们迁移到新 worker。仅替换二进制后重复 `load` 不会热更新。

### 本地试用 Filesystem 与 Browser

1. 用本分支的 OIS 和 OIC，复制示例中的 `ois.yaml` 和对应平台的 `oic.*.yaml` 到新的本地配置目录。示例监听回环地址，首次启动时导入 25 个工具。按 OIS YAML 开头的说明设置三个独立的 Token。[OIS 配置](../docs/spec/reference/oiapps/examples/ois.yaml) · [Windows OIC](../docs/spec/reference/oiapps/examples/oic.windows.yaml) · [Linux OIC](../docs/spec/reference/oiapps/examples/oic.linux.yaml)
2. 把 `app_config.filesystem.root` 改为已有工作目录，按需设置 `read_only`。把 `app_config.browser.state_dir` 改为运行账户专用的私有目录。首次试用临时会话时可省略示例中的 `profiles` 段。[Browser 目录权限](browser/README.md)
3. 用 `ois --config <ois.yaml> start`、`oic --config <oic.yaml> start` 启动两个进程，再按上文的 `load` 命令分别加载两个发行包的 `app.yaml`。用 `oic --config <oic.yaml> apps` 确认两个 App 均已就绪和登记。
4. 客户端以 `OI_WORKBENCH_TOKEN` 的值发送 Bearer 凭据。通过 `POST /api/v1/exec` 提交以下请求即可列出工作目录。MCP 入口为同一 OIS 上的 `/mcp/workbench`，可以调用 `browser_create_session` 获取首个标签的 ID。[各 Action 参数样例](../docs/spec/reference/oiapps/examples/arguments.yaml) · [分平台实测](../docs/reports/oiapp-expansion/verification.md)

```json
{"vmcp":"workbench","tool":"filesystem_list_directory","arguments":{"limit":20}}
```

使用仓库的 PowerShell 客户端时，`Invoke-LoomTool` 遇到图像会返回完整 MCP 结果。以下示例从工作根内的 `report.png` 取一个选区，在内存中取得可解码的 PNG 字节。终端、目录列表等普通 JSON 工具仍可直接读取业务字段。

```powershell
Import-Module ./scripts/McpClient.psm1
$mcp = Connect-LoomMcp -Url 'http://127.0.0.1:25100/mcp/workbench' -Token $env:OI_WORKBENCH_TOKEN
try {
    $preview = Invoke-LoomTool $mcp filesystem_view_image @{ path = 'report.png'; crop = @{ x = 0; y = 0; width = 120; height = 32 } }
    $image = $preview.content | Where-Object { $_.type -eq 'image' }
    $png = [Convert]::FromBase64String($image.data)
} finally { $mcp.Client.Dispose() }
```
