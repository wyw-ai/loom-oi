# beta 目标、交付范围与完成判据

## 产品目标

让 Agent 通过 oi 调用不同机器上的受控能力。oi（loom-oi）目前提供 HTTP API 和 MCP 两种消费接入方式，未来会加入更多消费方式；提供方通过本地 App 加载、停止和配置掌握接入范围。脚本和其他程序也可以通过现有接口消费能力。beta 重新定义产品，不默认继承 alpha 的职责划分或实现。

本版以 MCP 为入口验证完整命令执行流程：配置中心工具 → 在两端加载 App → Agent 从一个 MCP 入口发现工具 → 选择 Linux 或 Windows 工具执行 → 取得短命令结果，或通过原句柄持续读写直到长命令结束。HTTP API 已提供调用入口，并复用相同的访问规则和固定绑定；MCP 是本版完整流程的验收入口。

## 已完成的首版范围

| 范围 | 交付内容 |
| --- | --- |
| 中心 | OIS CLI、配置持久化、管理 API、MCP 与 REST 消费入口 |
| 受控端 | OIC CLI、YAML 加载、worker 托管、参数适配、连接及能力登记 |
| App | Linux Bash 和 Windows PowerShell 7；各提供 `exec_command`、`write_stdin` |
| 交互 | 短执行、长执行分批读取、真实终端输入、输出预算、执行句柄与访问范围 |
| 生命周期 | 重复加载、失败回滚、卸载、重启加载记录、断连/取消及资源回收 |
| 访问 | 默认 HTTP/WS、无 Token；显式启用 HTTPS 和各范围 Token |

实现及配置详细规则由[架构](../architecture/system.md)和[技术参考](../README.md#技术参考)维护。

## 首版验收拓扑

单一 `workbench` vMCP 暴露四个固定 Tool：

| Tool | 通道 / Host | App.Action |
| --- | --- | --- |
| `linux_exec_command` | `linux-151` | `bash.exec_command` |
| `linux_write_stdin` | `linux-151` | `bash.write_stdin` |
| `windows_exec_command` | `windows-local` | `powershell.exec_command` |
| `windows_write_stdin` | `windows-local` | `powershell.write_stdin` |

151 Linux 运行 OIS、Linux OIC 和 Bash App；本机 Windows 运行 Windows OIC、PowerShell App 和 MCP 验收客户端。部署默认入口是 `http://121.199.175.151:25100/mcp/workbench`，端口和地址属于本次验收部署，不是协议中写死的路由规则。

Windows 使用 PowerShell 语法，不承诺 Bash 语法兼容。业务验收经 MCP 完成，SSH 仅承担部署、服务生命周期和独立观察。操作步骤见[运行指南](../../implementation.md)。

## Done 与证据

完成标准保留在[首版验收规格](../../goal-shaping/current/beta/acceptance.md)：A01–A13 覆盖四工具发现、两端短执行、两端真实 600 秒无输出并持续轮询、TTY 输入、会话隔离、参数与 runtime、输出/错误、断连/取消、加载/重启、进程回收、管理持久化和身份接入。

2026-11-10 已记录 A01–A13 全部通过，详见[首版验收报告](../../acceptance-report.md)及[证据摘要](../../acceptance-evidence.json)。该轮使用显式 HTTPS/Token；随后追加的默认匿名访问、四种 HTTP/HTTPS × Token 配置、现有部署迁移与相应回归有[独立验证记录](../../access-verification.md)。两份证据按各自配置解释，不把旧模式结果改写为新模式结果。

本次规范整理依据上述已有记录和当前源码，不表示重新执行了测试或重新检查了部署在线状态。构建、健康检查和文档检查不能替代这些完成判据。

## 已明确的限制

- 首版不交付独立的消费端 CLI；无 CU、同 Tool 多目标路由、负载均衡或离线任务队列。
- 无命令白名单/黑名单、逐条审批或文件系统/网络沙箱。命令使用 OIC 运行账户的操作系统权限；资源限额不是命令策略。
- 无公开的独立强制停止、终端 resize、完整输出历史或回放游标；截断内容不会在下次补读。
- 无 App 热更新、worker 崩溃自动重启、跨 worker/OIC 重启的执行恢复或执行迁移。
- 默认匿名同 vMCP 共享访问身份，不能据此承诺各 Agent 独立用户隔离。
- CLI 提供后台运行，不安装操作系统开机自启服务；OIS 不自动申请、签发或续期公共 CA 证书。

## 后续方向

继续扩展 oi 的消费接入方式，使 Agent 和其他消费者能按自身使用场景接入。当前已交付 HTTP API 与 MCP，其余消费方式的具体形态和交付安排留待后续确定。

CU 的桌面控制权与失联处理、无独立 Host 的远程接口 App、多目标合并/路由、执行恢复和 App 分发均需另起讨论。其中 CU 已有“同一桌面同一时段一个控制者”的方向，其余完整机制仍未确定；这些意图没有变成首版能力或后续版本的交付承诺。

本轮讨论归档保留被替代方案及取舍，未来变更应以本规范为当前基线重新明确范围与 Done。
