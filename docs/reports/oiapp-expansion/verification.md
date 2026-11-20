# Filesystem 与 Browser 开发验证 · 2026-11-20

## Windows 11 x64

Windows 11 Pro x64（10.0.26200）使用 Go 1.26.3、Node.js 24.13.0、Playwright 1.62.1 和配套 Chromium 151.0.7922.34（revision 1234）。测试启动隔离的 OIS 和 OIC，通过真实 WebSocket 和 worker stdio 调用。HTTP 测试站、文件根、状态目录和凭据均为本轮专用。对实际 `tools/list` 返回的 25 项，逐项核对了工具名及对应发行包的输入 Schema。

| 路径 | 实际观测 |
| --- | --- |
| Filesystem | 加载 Windows 包，创建文本、按名称和内容找回、读 revision、局部编辑后回读，覆盖全部 7 项。对已有 PNG 分别经 REST 和 MCP 读取局部，实际字节数、PNG 解码尺寸和元信息一致。 |
| Browser | 真实专用 Chromium 运行全部 18 项，包括新建与关闭标签、快照、填表、选择、勾选、按键、滚动与悬停。点击触发同步 confirm 后用独立调用处理弹窗，查询原操作终态。测试站只收到一次提交，页面读回包含字段值、选项值与勾选状态的成功文字。局部截图分别通过 REST 和 MCP 解码。 |
| 协议兼容 | 普通业务 JSON 即使带有 `content` 字段也按已有形式返回。测试旧连接缺少图像协商、重连、在途调用后重新登记、格式不匹配和响应过大。单次结果被拒后同一通道仍能执行下一项调用。 |
| 既有 PowerShell | 新 OIS/OIC 加载原 `powershell` 包并实际执行 `Write-Output 'loom-oi-legacy-powershell'`。REST 返回 HTTP 200 与退出码 0。MCP 的 `isError=false`，一个文本块和 `structuredContent` 均包含相同的终端结果。传入 `max_output_tokens: 0` 时，REST 返回 422，MCP 返回 HTTP 200 与 `isError=true`，两端均给出 `INVALID_ARGUMENT`。 |
| PowerShell MCP 客户端 | `Invoke-LoomTool` 在同一连接调用 `filesystem_view_image` 与 `filesystem_list_directory`。前者保留完整 MCP 结果，7×5 选区解码为 89 字节的 PNG，35 个像素逐一与源图匹配。后者仍直接返回目录条目、分页状态和 `_call_id`。 |
| 发行包与回收 | Windows Browser ZIP 校验 SHA-256 后，在独立目录解包，用包内 Node 与 Chromium 重跑上述 OIS/OIC 表单及截图用例。正常卸载释放临时 profile。另在真实进程检查中强制终止 worker，Windows Job 收回该实例的全部 7 个 Chromium 进程，下一次加载清理可核实归属的遗留 profile。 |

## 通过 MCP 读回小字

通过 `ois start`、`oic start` 和 `oic load` 加载 Windows Filesystem 包和从 Windows Browser 发行 ZIP 解出的清单。Browser 归档的 SHA-256 为 `73356bbb6ffc870d126a06fe0ef73d2ac4bd18e07913ca2b522399ee9a67bc71`。测试端分别生成 1280×720 与 1920×1080 的文件图片和网页 canvas，为每一份独立生成随机批号。每个样本使用 12 像素批号和 14 像素金额。两个 App 分别以 `crop` / `clip` 请求 228×64 区域，获取 MCP 原生 image 块。客户端将同一块中的 PNG 交给视觉模型，先记录读到的文字，再与单独保存的真值核对。

| Action | 源尺寸 | PNG 字节 | 模型读回的批号 | 与真值核对 |
| --- | --- | ---: | --- | --- |
| `view_image` | 1280×720 | 4,766 | AUC8VE3 | 一致 |
| `screenshot` | 1280×720 | 4,394 | 99DRMFV | 一致 |
| `view_image` | 1920×1080 | 4,907 | YW6VNCK | 一致 |
| `screenshot` | 1920×1080 | 3,931 | JJFTVJ9 | 一致 |

这四例验证了指定区域的原像素交付及该次视觉读取。页面截图使用 `scale: css`，文件图片使用方向校正后的源像素。不同消费者仍受其图像输入处理方式影响。

## Linux Filesystem

在 Windows 主机上以 QEMU 11.1.3 TCG 分别启动 amd64 和 arm64 的 Alpine 3.22.8 虚拟机。两台均使用 Linux 6.12.115-0-virt 和独立的 virtio 块设备，挂载为可写 ext4。测试文件和 `TMPDIR` 位于该文件系统。Linux worker 与测试程序使用 Go 1.26.3 构建，启动后在对应架构的 Linux 内核上实际执行。

| 架构 | 独立模块 | 真实 worker | OIS/OIC 消费路径 |
| --- | --- | --- | --- |
| amd64（x86_64） | 12 项通过 | 7 个 Action 全部通过 | `TestFilesystemPackage` 通过，0.69 秒 |
| arm64（aarch64） | 12 项通过 | 7 个 Action 全部通过 | `TestFilesystemPackage` 通过，0.62 秒 |

两个架构均检查了特殊文件与链接处理、修改后执行位保留、并发版本冲突、目录和文本游标、图像尺寸与裁剪。通过 OIS/OIC 的用例执行了查找、创建、读回和修改文本，并从 REST 与 MCP 两次解码同一选区。两个 guest 的测试进程退出码均为 0。

随后使用 `CGO_ENABLED=1`、Clang 和 glibc 2.36 重新构建四个模块测试程序。每个架构各执行 13 项 `-race` 测试，包含等待写锁时取消，均通过且竞态检测器没有报告。再将各自的 ext4 测试盘写满，提交一次 120 KiB 的条件编辑，实际触发 `ENOSPC`，worker 返回 `mutation_state=not_applied`。读回的原文件字节与 revision 均未改变，释放测试空间后再次跑通七项和跨层调用。

## Ubuntu Browser

在 Windows Hypervisor Platform 支持的独立 Ubuntu 24.04.5 LTS x86_64 虚拟机中，以 UID 1000 运行。系统安装了 Chromium 所需共享库。AppArmor 规则只对测试位置和解包位置的 Chromium 允许用户命名空间，浏览器保持沙箱开启。

锁定 Node.js 24.13.0、Playwright 1.62.1 和 Chromium 151.0.7922.34。运行 Browser 的五组原生测试通过，在 Ubuntu 生成 `browser-linux-x64.tar.gz` 并按包内 `checksums.txt` 校验。归档大小为 227,092,822 字节，SHA-256 为 `9fdc70bd90f81c83d378ddfbf8d17e04060dc1efa04d6b878dbdd23a3f8e91b0`，复制回工作树后再次核对摘要。

使用解包后的 `app.yaml` 和随包 Node、Chromium，通过 `TestBrowserPackage` 跑通 18 个 Action、同步确认框之后只提交一次的表单、REST 和 MCP 局部截图。夹具显式将状态目录设置为 0700 后，在访客默认 `umask 0002` 下复验通过（1.68 秒）。正常卸载后该会话的 11 个 Chromium 子进程全部退出。独立测试强制终止本次 worker，子进程也被回收，随后加载 App 时清除了遗留临时 profile。

Linux 归档在从本地工作树复制出的独立目录中构建。`release-lock.json` 记录了源码内容摘要 `32f73bd9dbddea38ac9d0de1406ff95142ec15cd14dab681a149ebd7ec6a3203`，此次未提交的代码对应的 `source_commit` 字段为空。

## 复验入口与范围

分别按 [Filesystem](../../../apps/filesystem/README.md) 和 [Browser](../../../apps/browser/README.md) 构建目标平台包。将 `OI_FILESYSTEM_MANIFEST` 和 `OI_BROWSER_MANIFEST` 设置为两个包中 `app.yaml` 的绝对路径，随后在仓库根目录执行以下命令。测试自行创建临时配置、受控目录、回环服务和测试页面。

```text
go test -count=1 ./...
go test -tags oiapps -count=1 -v ./internal/integration
```

Windows 和 Ubuntu 的原生 Browser 包测试，以及 Filesystem 三个平台的构建与运行均已加入 [CI 配置](../../../.github/workflows/test.yml)。本记录为本地实测，本轮未触发远程 CI。仍须逐项验收的故障注入包括文件发布瞬间外部改名或主机意外掉电。[发布验收清单](../../goal-shaping/current/oiapp-expansion/acceptance.md)
