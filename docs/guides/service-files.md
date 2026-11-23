# 用 workbench Service 验收 Host A 的截图

验收完成时，agent 应能读出 **Host A 当前屏幕上临时展示的文字**，并描述文字所在的窗口。截图原件由 Host A 的 Computer Use 写入工作目录，File Store 将原件上传至服务端 AList，再下载到新的本机路径。Filesystem 从下载路径读取图片，返回 agent 能看的 MCP image。OIS 的 workbench Service 只组合并发布这六个 Action。

## 构建

以下命令从包含本次实现的 beta 分支仓库根目录运行。构建机使用 Windows x64、Go 1.26+、PowerShell 7.1+。本例把 OIS 和 AList 部署在 Linux amd64 服务器，Host A 为已登录且桌面解锁的 Windows 会话。

```powershell
./scripts/build.ps1
./apps/computer-use/build.ps1
./apps/file-store/build.ps1
./apps/filesystem/build.ps1 -TargetOS windows -Arch amd64

$example = './docs/spec/reference/oiapps/examples/service-files'
$run = './.local/service-files'
New-Item -ItemType Directory -Force -Path $run | Out-Null
Copy-Item "$example/oic.windows.yaml" $run
```

将 `dist/linux-amd64/ois` 和示例 `ois.yaml` 放入服务器的专用部署目录。Windows 构建机与 Host A 为同一台时，后续命令可直接使用仓库中的 App 包。异机部署时，把 `dist/windows-amd64/oic.exe`、Computer Use 的 `app.yaml` 和 `runtime/`、File Store 和 Filesystem 的完整发行目录，以及 Host 配置复制到 Host A，并按实际位置调整加载命令。[OIS 配置](../spec/reference/oiapps/examples/service-files/ois.yaml) · [Host A 配置](../spec/reference/oiapps/examples/service-files/oic.windows.yaml)

## 配置服务端 AList 与 OIS

1. 在服务器使用独立的 AList v3.66.0 实例和持久化目录。新安装可解压官方 Linux amd64 发行包，运行 `./alist admin random --data ./data` 初始化管理员密码，在 `data/config.json` 中设 `scheme.address` 为 `127.0.0.1`、`scheme.http_port` 为 `5244`，再以同一账户运行 `./alist server --data ./data`。已有实例可以复用一个专用挂载。[AList v3.66.0](https://github.com/AlistGo/alist/releases/tag/v3.66.0)
2. 在 AList 管理页面添加 **Local** 存储。挂载路径设为 `/oi`，根文件夹指向服务器上 AList 进程可读写的专用目录。在管理界面创建 `oi-host-a` 用户，授予 WebDAV 读写、创建目录或上传、重命名和删除临时文件的权限。后端须支持 WebDAV 的 `LOCK`、`UNLOCK` 和同目录 `MOVE`。本次实测的可写 WebDAV 入口为 `/dav/oi/`，对应服务器上实际配置的 `/oi` 挂载。用专用账户在该入口完成下面的上传、查回和下载，再确认部署可用。对 `/dav/` 的目录查询成功只能证明该地址可读。[AList 用户权限](https://github.com/AlistGo/alist/blob/v3.66.0/internal/model/user.go)
3. 为 OIS 准备三个独立凭据。管理员使用 `OI_ADMIN_TOKEN`，Host A 与 OIS 共享 `OI_CHANNEL_TOKEN`，workbench 客户端与 OIS 共享 `WORKBENCH_TOKEN`。从部署目录启动 OIS。首次运行用新的状态目录，服务配置会在 `server.data_file` 首次创建时导入。已有实例通过管理接口更新 Service，入口为 `PUT /api/v1/services/workbench`。

```bash
chmod +x ./ois
for key in OI_ADMIN_TOKEN OI_CHANNEL_TOKEN WORKBENCH_TOKEN; do
  read -rsp "$key: " value
  echo
  export "$key=$value"
done
unset value
./ois --config ./ois.yaml start
./ois --config ./ois.yaml status
```

本例用 SSH 将两个服务转发到 Host A 的回环地址。在 Host A 单独的终端运行下列命令，把 `operator@your-server` 换成实际 SSH 目标，验收期间保持连接。之后可在 Host A 的 `http://127.0.0.1:25244` 管理 AList。已有 HTTPS 部署可直接在配置中填写对应的 OIS 与 WebDAV 地址。[HTTPS 与凭据配置](access-configuration.md)

```powershell
ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:25220:127.0.0.1:25220 -L 127.0.0.1:25244:127.0.0.1:5244 operator@your-server
```

## 在 Host A 加载三个 App

Host A 的三个 App 共享同一个已存在的根目录。Computer Use 在这里创建截图，File Store 读写原件，Filesystem 只读。示例使用 `C:\oi\exchange`。若需换目录，须一起修改 `output_root` 和两个 `root`。OIC 为 worker 隔离环境变量，File Store 从 `password_file` 读取 AList 密码。先把用于存放密码的独立目录设为仅 OIC 运行账户可访问，再将单行 UTF-8 密码写入文件。该文件的绝对路径须位于工作根之外。

在 Host A 已登录的交互桌面打开 PowerShell，从同一仓库根目录运行以下命令。将密码和通道 Token 分别输入对应提示。

```powershell
$config = './.local/service-files/oic.windows.yaml'
$oic = './dist/windows-amd64/oic.exe'
New-Item -ItemType Directory -Force -Path 'C:\oi\exchange', 'C:\oi\secrets' | Out-Null
$principal = [Security.Principal.WindowsIdentity]::GetCurrent().Name
icacls 'C:\oi\secrets' /inheritance:r /grant:r "${principal}:(OI)(CI)F"
if ($LASTEXITCODE -ne 0) { throw '无法设置凭据目录权限' }
[IO.File]::WriteAllText('C:\oi\secrets\alist-password.txt', (Read-Host -MaskInput 'AList password'), [Text.UTF8Encoding]::new($false))
$env:OI_CHANNEL_TOKEN = Read-Host -MaskInput 'OI_CHANNEL_TOKEN'
& $oic --config $config start
& $oic --config $config load ./apps/computer-use/app.yaml
& $oic --config $config load ./apps/file-store/dist/file-store-windows/app.yaml
& $oic --config $config load ./apps/filesystem/dist/filesystem-windows-amd64/app.yaml
& $oic --config $config apps
```

检查三个 App 均已就绪且已登记。File Store 在 Windows 上加载打包目录内的清单。workbench 的成员绑定到 Host A 的具体 App 和 Action，工具参数说明来自它们的登记信息。

| workbench 工具 | 来源 Action | 如何衔接 |
| --- | --- | --- |
| `capture_a` | `computer-use.screenshot` | 传 `{}`，获得本机相对路径 `path` 和像素尺寸。 |
| `put_from_a` | `file-store.put_file` | 使用上一步的 `path`，指定逻辑 `store_path`，返回 `file_ref`。 |
| `list_store_a` | `file-store.list_directory` | 只知道目录时传 `store_dir`。结果 `entries[].store_path` 可用于查引用，有 `next_cursor` 时传回 `cursor` 继续取页。 |
| `lookup_on_a` | `file-store.lookup_file` | 传完整 `store_path`，取回 `file_ref`。 |
| `get_on_a` | `file-store.get_file` | 传完整 `file_ref` 和新的本机相对路径 `dest`，校验原件后返回 `path`。 |
| `view_on_a` | `filesystem.view_image` | 传下载结果的 `path`，获得 MCP 原生图像。 |

## 验收同一张图

1. 在 Host A 打开记事本，使用足够大的字号写一行当场选择的短句，保持窗口可见。操作者记住短句，稍后用来核对。给 agent 的任务、工具参数和文件名均不包含它。
2. 在单独的验收终端运行以下脚本。若验收终端在 Host A，可先最小化终端或将记事本置顶，以保留验收画面。脚本经 `/mcp/workbench` 获取六个成员，逐一调用，核对上传、按路径查找和目录列举的结果，将原件下载到新的 Host A 路径，再用该路径读取 MCP image。

```powershell
$env:WORKBENCH_TOKEN = Read-Host -MaskInput 'WORKBENCH_TOKEN'
./scripts/acceptance-service-files.ps1 -McpUrl 'http://127.0.0.1:25220/mcp/workbench'
```

3. 脚本在 `.local/reports/service-files/<run_id>/` 下写入 `run.json` 和该次 MCP 响应中的 `mcp-preview.*`。在能读取图像的 agent 客户端连接同一个 workbench MCP 入口，传入同一消费凭据，要求它对 `run.json` 的 `readback_path` 调用 `view_on_a`，描述窗口并读出短句。若客户端通过另一个机器访问，那里也需要可用的 OIS 转发或 HTTPS 地址。保留 agent 的回答，与此次显示的内容核对。只有它能正确描述真实画面，才通过视觉验收。[验收脚本](../../scripts/acceptance-service-files.ps1)

`run.json` 记录调用 ID、截图尺寸、存储路径、文件引用和预览摘要。原件完整性用 `file_ref.size_bytes`、`file_ref.sha256` 核对。Filesystem 可能为 MCP 生成缩放预览，预览的摘要单独记录。若小字难以辨认，可对同一个 `readback_path` 调用 `view_on_a`，传入源像素坐标的 `crop` 获取局部原图。[Filesystem 图像参数](../../apps/filesystem/README.md)

## 让 Host B 接收文件

为 Host B 添加自己的 Channel，在它的 OIC 加载 File Store，配置可访问同一 AList 挂载的账户、相同的逻辑 `store`，以及 B 自己的本机工作根。把 B 的 `get_file`、`lookup_file`、`list_directory` 以各自唯一的工具名加入同一个 workbench Service。若 B 也要预览，加载 Filesystem 并将 B 的 `view_image` 加入 Service。

A 可交付 `file_ref`、完整逻辑路径或目录。B 持引用时直接下载，持路径时先查引用，只知道目录时先列举再查找。下载使用 B 本机的 `dest`。两个 Host 须在同一服务身份及消费身份的授权范围中访问这份文件，B 只读时其 AList 账号只需能读取相同的存储数据。实机验证中，Linux B 依次调用 `list_on_b`、`lookup_on_b`、`get_on_b`，下载后的字节数和 SHA-256 与 Windows A 的截图原件一致。[Host A 视觉验收与 Host B 取回记录](../reports/service-files/acceptance.md)
