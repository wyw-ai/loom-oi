# Bash / PowerShell 独立 App 重构验收

2026-11-10，在 `beta` 工作区完成。基线为 `62aaefc`，本报告对应尚未提交的独立 App 重构；原始记录保存在忽略提交的 `.local/reports/` 与 `.local/app-independence/`，可提交的结果和校验值见[证据摘要](app-independence-evidence.json)。历史首版报告与访问模式报告保持各自当时的条件。

## 交付结果

- [Bash](../../../apps/bash/README.md) 和 [PowerShell](../../../apps/powershell/README.md) 分别位于 `apps/bash`、`apps/powershell`，自带 YAML、独立 Go 模块、依赖、worker、测试、构建入口和使用说明。
- 删除主模块的 `cmd/oiapp-terminal`、`internal/terminal` 以及原 `examples/apps` 定义；主模块移除 PTY 依赖。OIC 保留通用加载、输入适配、转发与进程托管。
- App 自有命令、终端、会话、输出与子进程回收实现，不依赖主工程内部包、`go.work`、本地 `replace` 或另一个 App。JSONL protocol 1、App code、两 Action 和 OIS 固定绑定保持兼容。
- 构建脚本生成两套 OIS/OIC，以及 `dist/apps/bash`、`dist/apps/powershell` 运行包；CI 将每个 App 源码复制到仓库外，在对应平台独立构建和测试。
- 未实现 CU，未引入 App 注册中心或公共 SDK。

## 仓库外构建与现有 OIC 兼容

复制 App 源码到系统临时目录，排除所有已有 runtime 产物，在 `GOWORK=off` 下重新构建。Bash 从 Windows 交叉编译 Linux amd64 worker 和测试二进制，随后在 Linux151 执行真实 PTY、信号、会话及后代回收测试；PowerShell 在 Windows 外部目录执行构建、测试及 vet，覆盖真实 ConPTY 和 Job Object 回收。

保留并校验重构前 OIC，在不替换 OIC 的情况下直接 `unload` 旧 App、`load` 外部目录的新 App YAML，观察到两 App 均 ready / registered。MCP 短执行和双端真实 600 秒命令均在这个组合上通过。之后从正式运行包部署并继续故障回归。

| 校验对象 | SHA-256 |
| --- | --- |
| Linux OIC，重构前 / 重建 / 最终部署一致 | `c9a6859b933186686bc708d1069f5b23c359fb52bb3715c37ae18223d7278e15` |
| Windows OIC，重构前 / 重建 / 最终部署一致 | `cb957a5eb6c33a63039e37756ea3c7903152203ebb2686f3aae57e8cc9a74ac2` |
| Bash，仓库外 / 正式运行包 / 最终部署一致 | `7ee8462c5a94c6ee63ae86763ce582b7e771da3e11cd896a486794fb4b59bf21` |
| PowerShell，仓库外 / 正式运行包 / 最终部署一致 | `f1cad71d1efd7e4a38acf51570c65ceb15478e38bb5b11d8f3db24656ee9ff35` |

## 回归结果

| 验证 | 结果 |
| --- | --- |
| 主模块 Windows `go test` / `go vet` | 通过 |
| 主模块 Linux 实机测试 | 5 个测试包、42 个顶层测试（含 helper），0 失败、0 跳过 |
| 两独立 App 的构建、vet、对应平台运行测试 | 通过 |
| A01/A02/A03/A05/A06 与 A07/A08 参数、输出部分 | 两端通过；正式部署和故障测试后再次通过 |
| A04：双端 600 秒无输出命令与持续续读 | Linux 约 600.0 秒、Windows 约 600.4 秒；句柄稳定、最终输出 `done`、退出码 0 |
| A08/A09：缓冲溢出、取消续读、已写输入不回滚、断连结果未知、不自动重跑 | 缓冲/取消两端通过；断连场景经 Windows 工具通过 |
| A07/A10：参数映射、默认值、runtime 继承与覆盖、重复 load、失败回滚、重启 | 两端通过；旧 session 失效、新执行恢复 |
| A11：卸载 / worker 崩溃 / OIC 停止 | Linux、Windows 共六组通过；普通管道与真实终端的父子 PID 均已回收 |
| PowerShell 脚本语法、构建包路径、文档链接与结构化示例 | 通过 |

本轮使用 Go 1.26.3；Linux 实机没有新安装 Go 工具链，执行的是交叉编译测试二进制。本轮未在本地重跑 Linux race、A12/A13 完整管理与四种访问模式部署矩阵；Linux 根模块与 Bash race 已保留在 CI，远端 CI 尚未触发。本报告只声称以上实际执行的验证。

## 最终运行状态与迁移

2026-11-10 09:18 UTC 观察到 2 台 Host 在线、2 个 App 可用、4 个 Action 和 4 个 Tool 可用。Linux App 加载自 `/home/admin/loom-oi-beta/apps/bash/app.yaml`；Windows App 加载自本工作区 `.local/windows/apps/powershell/app.yaml`，不再依赖临时验证目录。配置和持久定义保留，临时测试 Tool 已清理。

MCP 地址保持 `http://121.199.175.151:25100/mcp/workbench`，默认 HTTP、无 Token；公开工具仍为 `linux_exec_command`、`linux_write_stdin`、`windows_exec_command`、`windows_write_stdin`。

其他部署迁移时先构建新运行包，再卸载原 App 并加载新 YAML。卸载会结束旧执行，不能迁移旧 session；OIS 绑定无需改名。独立构建与加载说明见 [apps](../../../apps/README.md) 和[运行指南](../../guides/build-and-run.md)。
