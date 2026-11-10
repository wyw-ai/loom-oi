# loom-oi beta 首版验收报告

本文件保留首轮显式 HTTPS+Token 部署的历史验收证据。随后用户要求默认 HTTP、无 Token，当前入口与访问策略见[接入配置](../../guides/access-configuration.md)；以下旧 HTTPS 地址、认证前提和包校验值不作为当前部署快照。

2026-11-10，Asia/Shanghai。**A01–A13 全部通过，约定的首版开发、部署和验收目标已完成。**

代码位于 `beta` worktree，交付为工作区修改，未自动提交或推送。基线提交为 `8230fba2d5bf02c9950ae8a6c181cf323575791c`。main、alpha 和其他 worktree 未修改。

## 部署与当前状态

| 项目 | 实际结果 |
| --- | --- |
| Linux | `admin@121.199.175.151`，部署目录 `/home/admin/loom-oi-beta`；OIS、OIC、Bash App |
| Windows | 当前 beta worktree 的 `.local/windows`；OIC、PowerShell 7 App |
| MCP | `https://121.199.175.151:25100/mcp/workbench`，协议 2025-11-25 |
| 工具 | `linux_exec_command`、`linux_write_stdin`、`windows_exec_command`、`windows_write_stdin` |
| 最终登记 | 2 通道、2 Host、2 App、4 Action、4 Tool，均在线可用；1 个 workbench |
| TLS | 专用 CA、服务器 IP SAN；OIC 与本机 MCP 客户端显式信任并验证证书，不跳过校验 |

服务已恢复并保持运行。验收临时 App、Tool、通道和 vMCP 均已清理。运行步骤见[实现说明](../../guides/build-and-run.md)。本版后台进程不安装系统开机自启。

## 验收结果

| 编号 | 结果与证据要点 |
| --- | --- |
| A01 | 同一 vMCP 发现四个固定 Tool；Host/Action 管理查询与两端绑定一致，无目标路由参数。 |
| A02 | 两端通过 MCP 输出唯一标记、真实 OS 和工作目录，exit_code=0。 |
| A03 | Bash/PowerShell 使用各自语法；显式 workdir 生效，新调用不继承上次 cd 或环境变量。 |
| A04 | Linux **600.050 秒**、Windows **600.430 秒**；每端通过原句柄续读 20 次，期间无输出，最终 done、exit_code=0。 |
| A05 | Linux 真实 PTY、Windows 真实 ConPTY；提示后输入成功。Linux 使用 LF，Windows Read-Host 使用 CR 提交，chars 不改写。 |
| A06 | 双向跨 App 句柄拒绝；另一个 vMCP 身份不能读取原会话。 |
| A07 | 数字字符串转换、对象输入拒绝、固定默认与显式映射；根 runtime 共享，非空 Action 覆盖独立，同名 handler 可省略。中心 Schema 与本地描述不同仍可加载。 |
| A08 | 超输出预算及超 1 MiB 缓冲均报告截断；省略部分不补读；非零退出与启动前错误分开，输出收尾完整。实际平台测试另验证 START_FAILED、信号及无效 UTF-8 边界。 |
| A09 | 离线调用未交付即报错；交付后 OIS 断开返回 CALL_RESULT_UNKNOWN，原命令完成且未重跑；取消读取不杀已有执行，已写终端输入不回滚。 |
| A10 | 相同定义保持同一 PID，变更要求先卸载；部分初始化失败的进程已回收；两端 OIC 重启恢复加载记录，旧句柄失效，新命令成功。 |
| A11 | 两平台 × unload / worker 强杀 / OIC stop，共六组。每组同时创建管道和真实终端的父子进程；按 PID 出生时间独立核验整个可见 worker 后代树退出，再恢复就绪。 |
| A12 | 管理查询、配置创建更新删除、引用冲突和不可换绑验证通过；在途调用保留绑定；OIS 重启后 API 配置仍在，未被旧 YAML 覆盖。 |
| A13 | 错误 Host、错误通道凭据、重复活动连接均拒绝；离线快照不可执行，迟到结果不覆盖新连接，无自动重放。 |

A11 的每组创建 2 棵命令执行树。Linux 每组跟踪 worker 及其可见后代共 7 个进程，Windows 共 9 个，包含终端/监督辅助进程。独立观察只检查本次 beta 所属 PID、路径和出生时间。

实际业务验收由 Windows PowerShell MCP 客户端发起。SSH 只用于部署、生命周期操作和独立系统观察，没有代替 MCP 执行业务测试。

## 自动化与原始证据

- Windows `go test -count=1 -timeout=180s ./...`、`go vet ./...` 通过。
- Linux151 `go test -race -count=1 -timeout=180s ./...` 通过，另执行真实 PTY、信号和进程树回归。
- 两平台可执行文件均构建成功；远端运行包的 SHA-256 与本机构建产物一致。
- 测试及部署脚本位于 `scripts/`，CI 配置位于 `.github/workflows/test.yml`。本轮验证为实际执行结果，不声称 GitHub 远端 CI 已运行。

原始记录保存在本机 `.local/reports`；时间戳使用 UTC，因此文件名为 `20261109`，对应上海时间 2026-11-10。可提交的[证据摘要](acceptance-evidence.json)保存每项状态、耗时、进程回收统计及原始记录 SHA-256，不包含凭据。

| 记录 | 内容 |
| --- | --- |
| `commands-20261109-195854.jsonl` | 最终部署上的发现、短执行、目录、TTY、隔离、参数与输出 |
| `long-20261109-194253.jsonl` | 两端实际 600 秒无输出执行的逐次结果 |
| `failures-20261109-195446.jsonl` | 缓冲溢出、取消、断连未知、无自动重跑 |
| `lifecycle-20261109-195326.jsonl` | 双端 runtime、加载、回滚、重启与旧句柄 |
| `reclamation-20261109-195635.jsonl` | 六组实际进程与终端回收及恢复 |
| `management-20261109-195755.jsonl` | API、身份、连接、重启持久化 |
| `windows-tests.txt` / `linux-race-tests.txt` | 两平台测试输出 |
| `final-state.json` | 最终配置与在线登记快照 |
| `binaries.json` / `linux-binaries.txt` | 构建和部署包校验值 |

本版边界保持冻结范围：不含 CU、同 Tool 多目标路由、热更新和跨重启执行恢复。输出预算是近似字节预算。没有未解决的首版验收阻断项。
