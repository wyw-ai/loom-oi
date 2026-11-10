# loom-oi beta：首版系统设计

> 本轮讨论已于 2026-11-10 结束。本文作为归档快照保留；持续维护的当前契约见[项目规范](../../../../spec/README.md)，讨论结束与取舍见[笔记](notes.md)。

状态：2026-11-10 规格冻结，用户随后授权完整开发与部署验收。用户指定 Linux151 与本机 Windows 的验收方式，并采纳此前推荐主方案。权威定义见[当前共识](../../../current/beta/design.md)，完成判据见[验收规格](../../../current/beta/acceptance.md)，实现步骤见[运行说明](../../../../implementation.md)。本轮已按用户明确要求结束并归档，冻结阶段保留活跃目录的安排已完成。

## 产品范围与职责

首版实现 Linux Bash、Windows PowerShell 7 两端命令接入，各提供 exec_command/write_stdin。采用固定链路：Agent → vMCP → Tool → App.Action 引用 → 通道 → OIC → oiapp → cmd。OIS 中的 vMCP、Tool 和 Action 引用是逻辑定义，不分别部署服务。

| 模块 | 职责 |
| --- | --- |
| OIS | Tool 定义、消费权限、vMCP 组合、固定 source、REST/MCP 转换及调用转交 |
| OIC | 主动连接、加载 YAML、轻量参数适配、托管 runtime、传递可信上下文 |
| oiapp | 业务 Action、目标 shell、实际进程/终端、输入输出、session 和资源回收 |
| Agent/客户端 | 发现工具、提供业务参数、根据句柄继续读取或输入 |

公共 OIS/OIC 不编写 Bash/PowerShell 业务分支。Host 是宿主机管理身份，通道是指定 OIC 的通信入口；两机部署不引入同 Tool 的多目标选择。

## 部署与固定绑定

Linux admin@121.199.175.151 部署 OIS、Linux OIC 和 bash App；本机 Windows 部署 Windows OIC、powershell App，并作为 MCP 调用端。OIS 默认在用户允许范围内的 25100/TCP 提供 HTTP/WS，无 Token；单一入口为 http://121.199.175.151:25100/mcp/workbench。用户已明确授权现有部署切换到该默认。此前 HTTPS 与独立 Token 的部署及 A01–A13 通过记录作为历史启用配置保留，不替代新默认验证。

| Tool | 通道 / Host ID | App.Action |
| --- | --- | --- |
| linux_exec_command | linux-151 | bash.exec_command |
| linux_write_stdin | linux-151 | bash.write_stdin |
| windows_exec_command | windows-local | powershell.exec_command |
| windows_write_stdin | windows-local | powershell.write_stdin |

每组两个 Tool 到达同一个 App 后端。Agent 选择工具名后只提交业务参数，不提供 host_id/channel_id。Windows 组明确执行 PowerShell 7 语法，不宣称 Bash 兼容。两机的目录、配置与完整示例见[从 App 到客户端的流程](app-to-agent-walkthrough.md)。

## App 与运行契约

一个 App 使用一份 app.yaml，包含 App code、以名称为键的 Actions、description、基本 input_schema 和 runtime。handler 省略时使用 Action 名；同名入口简写不省略参数 Schema。可选 input_mapping 只做显式改名，不引入表达式语言。

runtime 根默认、Action 更具体配置优先；顶层覆盖，数组和对象整体替换。未声明或空 Action.runtime 继承根 worker，非空覆盖使用独立实例，不按 command 内容相同自动共享。两端命令 App 的两个 Action 分别共用各自根 worker，跨进程不会自动迁移 session。

OIC 经 UTF-8 JSON Lines 的 stdio 托管 worker，使用 start/ready/invoke/result/error/cancel。stdout 只写协议帧，诊断写 stderr，命令输出进入 App 缓冲。启动 config 与每次 input/context 分开；业务参数不能覆盖 runtime、handler、身份或本机配置。语言、最终可执行文件名和打包方式留给工程实现，不能改变公开契约。

## CLI 与加载生命周期

OIS 提供 start/status/stop；OIC 提供 start/status/load/apps/unload/stop。start 默认后台，支持 --foreground；全局 --config 定位同一本机实例配置，缺省读取当前目录对应 YAML。本地控制使用 IPC 或回环入口，不向公网暴露。

load 以全部本地入口 ready 为成功条件，多个 runtime 依次初始化、每个 worker 分别等待最多 10 秒 ready，不设整次 load 总共 10 秒的截止时间。中心登记另报 pending/registered；中心离线不撤销本地加载，连接恢复后重新登记。加载不执行用户 cmd，不要求中心与本地 Schema 相等。相同有效定义重复加载无操作，变更先卸载再加载，不监听文件热更新。多个 runtime 初始化部分失败时回收本次资源，不公布半个 App。

unload 停新调用、撤登记、回收执行并删除加载记录；stop 回收执行但保留记录；start 重新校验加载记录，旧 session 不恢复。worker 意外退出标记不可用，不自动重启。两平台分别验证普通托管范围内的实际进程/终端回收，不只检查 worker PID。

## 配置、身份与管理

采用[OIS API 规格](ois-api-design.md)中的管理、执行和登记查询接口。OIS 首次从 YAML 导入，此后管理 API 修改持久配置，重启不被旧 YAML 覆盖。OIC 本地 YAML 是 App/Action/Schema/runtime 的权威；Host/App/Action 为管理只读登记视图，不自动覆盖 OIS Tool 定义。

OIC 显式配置 host.id/channel.id；OIS 始终核对通道 expected_host_id，并拒绝重复活动连接与旧连接覆盖新登记。默认关闭 Token：admin.token_env、通道 credential_env、vMCP access.credential_env 分别为空时匿名，非空则启用对应鉴权；引用变量未设置或为空时拒绝，不能降级匿名。启用各自 Token 后才核对对应凭据，匿名 ID 匹配不宣称机器身份认证。

匿名调用者在同一 vMCP 内共享 anonymous:<vmcp_id> 身份，任意 Bearer 不会改变这个范围；启用 Token 时使用凭据对应身份。共享匿名范围或同一凭据不隔离成不同用户。稳定标识不按机器名/IP 自动归并，也不迁移已有 session。

server.listen/public_url 默认表示 HTTP；server.https 可增加共享 HTTPS/WSS 监听，包含 listen/public_url/cert_file/key_file。证书由管理员提供并由 CA 签发，OIS 不自动申请、签发或续期。多个 vMCP 共用该监听和证书。旧 tls_cert/tls_key 保留 HTTPS-only 兼容，不能与 server.https 混用。

vMCP access.require_https 默认 false，与 Token 开关独立。设为 true 前必须有 HTTPS 入口，endpoint 选择该 HTTPS 地址；MCP 和 REST /exec 仅接受真实 TLS，HTTP 返回 403 HTTPS_REQUIRED，不信任转发头。false 使用根 public_url 作为 endpoint。详见[访问配置](../../../../access-configuration.md)。

Tool source 不原位更换；引用中的资源不能级联删除。新配置影响新请求，已接受调用保留当次绑定；删除 vMCP 不自动结束命令，实际执行由本地生命周期回收。不迁移旧 session。

## 通信、调用与结果

OIC 默认主动连接 WS /api/v1/oic/connect，选择 HTTPS 入口时使用 WSS；JSON 消息承载按配置鉴权的登记、invoke、result/error、存活与内部 cancel。心跳 10 秒，30 秒无法确认存活则不可用；只向固定在线通道即时投递，不排离线业务队列，不换目标。

OIS 检查消费权限和请求结构；OIC 按本地 YAML 完成显式改名、固定默认、基础类型转换和必填判断。可转换数字字符串应交付，结构无法转换时在副作用前报错；业务语义和动态默认留在 App。两份 Schema 不要求一致。

exec_command 接受 cmd，以及可选 workdir、tty、yield_time_ms、max_output_tokens；write_stdin 接受 session_id，以及可选 chars、yield_time_ms、max_output_tokens。空 chars 只读取，非空先输入再读取。每次 exec 创建独立执行，不继承另一执行的目录或 shell 变量。

一次 Action 返回一份本轮 output，仍可继续时有 session_id，最终有 exit_code。call_id 标识一次请求，session_id 标识原 App 实例中的持续执行；跨组和错误身份句柄被拒绝。命令非零退出属于业务结果，最终结果需确认退出及输出收尾。

采用[命令执行详稿](bash-action-design.md)的字段和默认基线：等待窗口 0–30000 毫秒；exec 默认 10000，write 空输入 5000/非空 250；输出预算默认 10000、上限 20000。每实例并发默认 8、每 session 缓冲 1 MiB、最长执行 30 分钟、无调用活动 10 分钟回收。它们可配置，但不得混淆等待窗口与进程期限。

输出持续采集、分批领取，截断明确标注；被省略部分不在下次补读。tty=false 的 stdin 为 EOF，只允许后续空输入读取；tty=true 的 Linux PTY 和 Windows 实际终端适配分别验收。首版没有独立 kill、resize 或历史游标。控制字符只是中断请求，不等于确认退出。

## MCP、REST 与取消

Windows PowerShell 7 驱动的验收客户端使用 MCP 2025-11-25 Streamable HTTP，完成 initialize、notifications/initialized、tools/list、tools/call。按该版本处理 HTTP 头、通知、会话与响应；MCP 会话不等于命令 session。首版不依赖 Tasks 或 stdout 订阅。[协议规范](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)

POST /api/v1/exec 接收 vmcp/tool/arguments，与 MCP 共用消费权限和固定绑定处理。两个入口通过后续 write_stdin 获取输出，不增加任意 App.Action 旁路。

MCP 显式取消通知映射内部 cancel。REST 不新增公开取消端点；处理器明确中止/服务端请求期限触发时尽力转交同一规则，普通 HTTP(S)/WS(S) 意外断开只保留结果未知，不假定取消已送达。上层请求期限基线 60 秒，与 Action 等待和本地最长运行期限分别设置。

取消 exec 尚未启动时阻止启动，已启动但结果未提交时请求回收本次新执行；取消 write 只停止本次等待/未完成输入，不终止原执行，已写入内容不回滚；迟到取消不撤销已提交结果。交付后失联不自动重跑或重新输入。原 App 存活且身份有效、同一通道恢复时可接续已知句柄；worker/OIC 重启后旧句柄失效。去重只覆盖当前运行周期和有界缓存。

## Done 与后续范围

[验收规格](../../../current/beta/acceptance.md)中的 A01–A13 是完成判据，涵盖单一 vMCP 四工具、两端短执行、真实十分钟无输出执行、TTY、跨组句柄拒绝、参数/runtime、输出、断连/取消、加载/重启、回收、管理 API 与身份接入。必须记录实际结果，不能用文档检查、SSH 连通或短测试替代。

2026-11-09 审查提出的七组决定已收口；2026-11-10 新要求将默认访问改为 HTTP/WS、无 Token，并保留显式启用选项。新增访问配置及现有部署迁移须单独验证；TLS/凭据拒绝场景按启用配置验收，不能作为匿名默认的前提。

CU、无独立 Host 远程 App、多 Host 合并/选路、热更新、执行迁移、完整输出历史、跨重启执行恢复和 App 商店后置。历史与取舍见[沟通笔记](notes.md)。文档冻结与实际实现分开记录，验收以真实运行证据为准。
