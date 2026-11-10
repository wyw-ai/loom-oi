# loom-oi beta 首版验收规格

本文件保留首版完成标准；本轮讨论已于 2026-11-10 归档，后续开发入口为[项目规范](../../../spec/README.md)。同日收尾时用户确认：多 runtime 按每个 worker 分别等待最多 10 秒 ready，依次初始化，不要求整次 load 总共 10 秒内完成；部分失败仍须回收本次资源。

基线日期：2026-11-10，Asia/Shanghai；同日按用户新要求更新访问配置。用户已授权完整开发、部署与验收，并明确把现有 151 与本机部署切换为默认 HTTP/WS、无 Token。运行步骤见[实现说明](../../../guides/build-and-run.md)，访问配置见[配置说明](../../../guides/access-configuration.md)，产品边界见[当前共识](design.md)。

## 1. 验收拓扑

历史执行状态：A01–A13 已在启用 HTTPS 和独立 Token 的配置下于 2026-11-10 通过，证据见[验收报告](../../../reports/beta/acceptance-report.md)。该记录继续有效，但不能据此宣称新默认部署或以下新增访问配置场景已经通过；迁移与新场景须单独记录实测结果。

访问配置变更的独立验证：HTTP 无 Token、HTTP 有 Token、HTTPS 无 Token、HTTPS 有 Token 四种实际部署均已通过；新建 vMCP 省略 access 后可匿名调用两端工具，无 HTTPS 入口时 require_https=true 返回 400。已恢复 HTTP/WS 匿名主部署，保留两通道、两 App、四工具和一个 workbench。独立证据与结果见[访问配置验证](../../../reports/beta/access-verification.md)；Windows 全量测试/vet 与 Linux 全量 race 均通过。

```text
本机 Windows：MCP 验收客户端（PowerShell 7）
              │ 默认 HTTP、无 Token，单一 /mcp/workbench
              ▼
121.199.175.151：OIS（25100/TCP）
              ├─ linux_* 两工具 → linux-151 通道 → 同机 OIC → bash App → Bash
              └─ windows_* 两工具 → windows-local 通道 → 本机 OIC → powershell App → pwsh
```

两个 OIC 都主动连接 OIS。这里的两组工具分别有固定绑定，不存在一个工具按 host_id 在两台机器间选路。结果仍沿原调用链返回。

| 项目 | 固定值/要求 |
| --- | --- |
| Linux 部署机 | 121.199.175.151，SSH 账户 admin；具体部署目录在实施时选用户目录中的独立 loom-oi beta 目录 |
| Windows 部署机 | 用户当前本机，Windows x64，使用 PowerShell 7 的 pwsh.exe |
| OIS 计划服务端口 | 25100/TCP，属于用户允许的 25000–27000 范围；不自动占用整个区间 |
| REST base | http://121.199.175.151:25100/api/v1 |
| MCP endpoint | http://121.199.175.151:25100/mcp/workbench |
| OIC 接入口 | ws://121.199.175.151:25100/api/v1/oic/connect |
| MCP 首版验收协议 | 2025-11-25，Streamable HTTP；其他协议版本不作为首版验收前提 |
| 客户端 | 在 Windows 通过 PowerShell 7 驱动的 MCP 协议验收脚本；IDE/Agent 集成可复用同一入口，但不以某个 IDE 为唯一判据 |

默认部署无需 Token、证书或 CA 配置。可通过 server.https 增加共享 HTTPS/WSS 入口，默认附加监听端口为 25101；启用时由管理员提供 CA 签发、匹配地址或域名的证书，客户端验证证书，不跳过验证。OIS 不自动申请、签发或续期证书。vMCP 的 access.require_https 独立决定消费入口是否强制 HTTPS，多个 vMCP 共用该监听；旧 tls_cert/tls_key 配置继续表示 HTTPS-only，不能与 server.https 混用。Windows OIC 只需出站连接 OIS，本地管理入口绑定回环地址或本地 IPC，不公开到外网。

SSH 用于后续安装、日志和独立核验，不是产品的命令执行通道；不能用 SSH 直接执行结果代替 MCP 验收。

## 2. 四个固定 Tool

| Tool 名称 | channel | expected_host_id / OIC host.id | app | action | cmd 语法 |
| --- | --- | --- | --- | --- | --- |
| linux_exec_command | linux-151 | linux-151 | bash | exec_command | Bash |
| linux_write_stdin | linux-151 | linux-151 | bash | write_stdin | 已创建执行的输入 |
| windows_exec_command | windows-local | windows-local | powershell | exec_command | PowerShell 7 |
| windows_write_stdin | windows-local | windows-local | powershell | write_stdin | 已创建执行的输入 |

两组使用相同的参数/结果形状，但 PowerShell 不能照搬 Bash 的启动参数、路径或退出码解释。创建命令和后续读写必须回到本组同一 App/runtime 实例。Windows 使用 pwsh，不把 Windows PowerShell 5.1 自动当作等价后端。

host.id 是管理员显式配置、随部署配置持久保存的稳定标识，不取自 hostname/IP 自动识别；OIS 通道配置包含 expected_host_id。OIC 申报 host.id/channel.id，服务端始终核对绑定并拒绝同通道第二个活动连接；通道 credential_env 非空时额外核验凭据，匿名 ID 匹配本身不作为机器身份认证。重装时保留原配置才能沿用登记标识，不做自动迁移或覆盖；稳定 ID 不恢复已重启 worker 的旧句柄。

## 3. 本次采纳的首版基线

| 方面 | 冻结规则 |
| --- | --- |
| 定义权威 | OIS 首次从 YAML 导入 Tool/通道/vMCP，后续 API 修改持久配置；正常重启不覆盖。OIC 本地 YAML 维护 App/Action/Schema/runtime。Host/Action API 只查询登记。 |
| CLI | OIS start/status/stop；OIC start/status/load/apps/unload/stop。start 默认后台，--foreground 前台；全局 --config，缺省读取当前目录对应 YAML。 |
| 托管 | 两个命令 App 各自使用共享 worker；UTF-8 JSON Lines 经 stdio 通信。语言/打包方式是工程选择，协议和行为不能因此变化。 |
| runtime | 根默认，Action 更具体配置优先；顶层覆盖，数组/对象整体替换。继承根 runtime 的两个 Action 共用根 worker，非空 Action 覆盖不自动共享实例。 |
| 参数 | 同名传递、可选显式改名、固定默认、基础类型适配；不做中心和本地 Schema 一致性门禁，错误在副作用前报告。 |
| load | 全部本地入口 ready 才算加载成功；OIS 登记独立为 pending/registered。相同有效定义重复 load 无操作，变更必须先 unload。初始化部分失败回收本次资源，不公布部分能力。 |
| unload/stop/start | unload 回收执行并删除加载记录；stop 回收执行但保留记录；start 重新校验并加载记录，不恢复旧 session。worker 意外退出标记不可用，不自动重启。 |
| 结果 | 每次 Action 返回本轮 output，仍可继续时返回 session_id，最终返回 exit_code；命令非零退出属于业务结果。 |
| 输出 | 后台持续收集、有界缓冲、分批领取；截断明确标记，被省略部分不在下一次补读。 |
| 输入与停止 | tty=false 的 stdin 为 EOF，只有空输入读取；tty=true 支持输入与适用的控制字符。没有独立强制 kill/resize 工具。 |
| 生命周期 | 每实例并发基线 8，缓冲 1 MiB，最长执行 30 分钟，无调用活动 10 分钟回收；输出不算调用活动。限额可由本机配置调整。 |
| 断连 | 离线不排业务队列；交付后失联结果未知，不自动重试或换目标；原实例仍存活且身份有效时，恢复同一通道后可继续已知句柄。 |
| 取消 | MCP 显式取消与 REST 请求取消/超时映射内部 cancel；执行阶段按详稿处理。意外断连不证明取消已送达或进程已停；已写入输入不能回滚。 |
| 请求期限 | 上层执行请求期限基线 60 秒，Action 等待窗口 0–30000 毫秒；与本地最长执行期限分开。 |
| 更新 | Tool source 不原位换绑，被引用资源不级联删除；新配置影响新请求，已接受调用保留当次绑定。 |
| 权限 | 默认无 Token；admin.token_env、通道 credential_env、vMCP access.credential_env 分别非空时启用。显式引用缺值必须拒绝，不降级匿名；不同已启用凭据不能互相替代。匿名同 vMCP 共享身份，任意 Bearer 不改变身份。 |
| HTTPS | server.https 提供共享监听和管理员提供的证书；vMCP access.require_https 默认 false，true 必须存在 HTTPS 入口，MCP/REST 仅接受真实 TLS，HTTP 返回 403 HTTPS_REQUIRED。 |

其余字段和默认值采用[命令执行详稿](../../archive/beta/2026-11-10-first-flow/bash-action-design.md)、[OIS API 规格](../../archive/beta/2026-11-10-first-flow/ois-api-design.md)中已采纳主方案。与本文件的两平台绑定冲突的旧单 Bash 示例不作为验收配置。无需在首版实现 Tasks、离线任务系统、输出历史回放、热更新或执行迁移。

## 4. 从零准备到调用

1. 在两个平台准备隔离的 beta 部署目录及可执行文件/App 包，记录版本、配置路径、进程与工作目录。
2. 在 Linux151 配置 OIS 的 HTTP 入口和持久数据目录，使用 25100 启动 OIS；默认不配置 Token 和证书。
3. 配置 linux-151、windows-local 两条通道及 expected_host_id；配置四个 Tool 和 workbench vMCP。
4. 在各机配置自己的 host.id、channel.id、HTTP OIS URL 及本机 shell_path/default_cwd；分别启动 OIC 并加载 bash 或 powershell App。启用通道 Token 或 HTTPS 的测试场景再提供对应凭据/CA。
5. 本地 apps 显示 ready/registered；OIS 管理 API 显示两个 Host、两个 App、四项 Action 登记及固定来源。
6. Windows MCP 客户端完成初始化及协议需要的通知，再 tools/list，得到四个名称和正确的 Bash/PowerShell 参数描述。
7. 后续所有业务测试都由这个 Windows 客户端经该 vMCP 发起，不在进程执行阶段临时指定 host_id/channel_id。

2025-11-25 MCP 的 HTTP 头、初始化与会话约定由选定版本规范处理；MCP 协议会话标识与 App 的 session_id 不混用。[MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)

## 5. 必须通过的验收场景

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| A01 | 发现与绑定 | Windows 客户端在同一 vMCP 发现四个固定 Tool；OIS 管理查询能对应到两条通道、Host 和本地 Action，消费参数不要求选择机器。 |
| A02 | 两端短命令 | 分别输出唯一标记、OS 信息和实际工作目录，确认 Linux 与 Windows 上各执行一次，返回 output 和 exit_code=0。 |
| A03 | 工作目录和语法 | Linux 使用 Bash 语法和 Linux 路径，Windows 使用 PowerShell 语法和 Windows 路径；新 exec 不继承上次 cd/export/变量状态。 |
| A04 | 十分钟无输出长执行 | 两端都执行至少 600 秒、期间不输出的命令；首轮得到句柄，持续空输入读取，最终收到 done 和 exit_code=0。记录实际耗时，不用短命令替代。 |
| A05 | 两端终端输入 | tty=true 时先读提示，再通过本组 write_stdin 写入带换行的内容，收到预期结果；Windows 必须使用真实终端适配，不能仅因管道可读就算通过。 |
| A06 | 会话隔离 | Linux/Windows 句柄不可跨组；不同可信访问范围不能读写原执行。匿名同 vMCP 共享范围，随意更换 Bearer 不能伪造另一个身份或用户隔离。 |
| A07 | 参数与 runtime | 数字字符串能适配；无法转换对象在执行前报错；根 runtime 默认、Action 级覆盖和同名 handler 省略可验证，不因为中心/本地 Schema 不相等拒绝 load。 |
| A08 | 输出和非零退出 | 超预算/缓冲丢弃有明确标志且不重复补读；非零命令退出和启动失败分开；最终结果在确认退出和输出收尾后返回。 |
| A09 | 断连与取消 | 未交付时依赖不可用明确报错；交付后失联标为结果未知且无自动重跑；取消一次读取不误杀已有执行，已写入输入不声称回滚。 |
| A10 | 加载与重启 | 重复相同 load 不重复启动 worker，改变定义须先卸载；部分 runtime 初始化失败不残留可调用的半个 App；OIC 重启恢复加载记录但旧句柄失效。 |
| A11 | 资源回收 | 正常卸载、worker 崩溃、OIC 停止都清理普通托管范围内的进程和终端；两平台分别验证实际 PID/后代退出，而不是只看 CLI 返回成功。 |
| A12 | 配置与管理 API | 已列管理接口可完成查询和配置；API 修改后 OIS 重启不被旧 YAML 覆盖；引用删除、禁止原位换绑和在途调用处理符合契约。 |
| A13 | 身份与接入 | 始终核对 expected_host_id、拒绝重复活动连接、将断连快照标为不可执行，旧连接消息不得覆盖新状态。启用通道 Token 时才要求错误/缺失凭据被拒绝；默认匿名通道允许无 Token 接入。 |

访问配置追加验收：默认 HTTP/WS 无 Token 能完成管理、登记及两平台四工具调用；启用各自 Token 后，错误、缺失及显式引用未设置的凭据均不得降级匿名；配置 server.https 后验证证书、共享入口和 endpoint 选择；require_https=true 的 MCP 与 REST 对 HTTP 返回 403 HTTPS_REQUIRED，Forwarded/X-Forwarded-Proto 不能绕过检查，真实 HTTPS 按独立 Token 设置访问。API 更新后的访问配置重启仍保留，移除 HTTPS 入口不能静默降级持久化的 require_https=true vMCP。原 TLS-only 配置兼容、新旧 TLS 配置混用拒绝也应覆盖。

`acceptance-management.ps1` 与 `channelprobe` 保留为显式 HTTPS+Token 配置的回归探针，不作为匿名默认部署可直接执行的入口。默认匿名管理由集成测试和实际 API 检查覆盖，启用模式与默认模式的结果分别记录。

长执行的命令正文为：

```text
Linux:   sleep 600; printf 'done\n'
Windows: Start-Sleep -Seconds 600; Write-Output 'done'
```

首次 exec 可用 yield_time_ms=1000；后续各组用 chars=""、yield_time_ms=30000 持续等待，每次返回未结束句柄就继续。这两次执行可以并行，但每个 session 的读取依次发起。允许中间 output=""；不能另起 exec 代替续接，不用定时日志输出来绕过无输出场景。每端至少一轮实际 600 秒测试是最终验收要求，不能以缩短超时的单元测试代替。

TTY 的代表命令为：Linux 的 read -r name; printf 'hello %s\n' "$name"；Windows 的 $name = Read-Host 'name'; Write-Output "hello $name"。实际 MCP 参数作为字符串传递，不通过客户端 shell 二次求值；PowerShell TTY 后端不得以禁用交互输入的启动选项破坏本场景。[pwsh CLI](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_pwsh?view=powershell-7.6)、[Windows ConPTY](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session)

平台实测补充：chars 原样交付。Linux 的 read 使用 LF（`\n`）提交；Windows ConPTY 的 Read-Host 使用 CR（`\r`）模拟 Enter，不在 App 内改写输入。

闲置回收另行测试：无人调用会触发本地期限，命令持续输出不续期。可在专用测试配置中缩短限额验证机制，同时核对交付默认值；这与 A04 的实际十分钟执行分别记录。压力、异常和回收场景只操作本次 beta 托管的进程与文件，不影响其他服务。

## 6. 证据与完成判定

每项记录时间、平台、调用 Tool、call_id/session_id（可脱敏）、输入、输出、退出状态、实际耗时及相关 OIS/OIC/App 日志。保留测试报告与失败项；凭据不进入 Git、命令日志或报告正文。命令运行位置通过各端独立观察核对，不能仅凭客户端伪造标签判定。

完成意味着 A01–A13 及本次追加访问配置场景有对应配置和实际结果记录，现有 151/Windows 部署已按授权切换，公开配置与启动步骤可复现，实质偏差已修正。历史启用 HTTPS/Token 的结果不重写为匿名模式结果；文档冻结、构建成功或健康检查不能替代功能验证。

## 7. 冻结阶段只读环境检查记录

2026-11-10 检查：

- SSH 使用已有 known_hosts 校验并以 BatchMode 成功登录 admin@121.199.175.151；目标为 Linux 6.8.0-63-generic x86_64，存在 /usr/bin/bash、/usr/bin/python3。未安装、复制或启动服务。
- 远端监听快照未发现 25100 占用；这是当时的本机监听观察，不证明公网端口已放行或未来仍空闲。公网连通、TLS 和防火墙由部署阶段实测。
- 本机为 Windows x64、系统 build 26200；PowerShell 7.6.9、Go 1.26.3、Python 3.14.0 可调用。工具存在不等于选择这些语言，也不证明 Windows TTY 和回收实现已完成。
- 未创建证书、修改信任、调整防火墙、启动 OIS/OIC、安装 App 或执行上述十分钟测试。

以上是冻结阶段的历史检查记录。随后用户已明确授权开发与部署，新的运行记录单独保存，不将只读检查当作功能通过证据。
