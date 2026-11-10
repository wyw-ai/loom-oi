# loom-oi beta：当前设计共识

本轮 `first-flow` 已于 2026-11-10 结束并归档；这里保留已确认的设计意图，后续开发查阅[项目规范](../../../spec/README.md)。本轮最终补充：多 runtime 初始化按每个 worker 分别等待最多 10 秒，详见下文加载规则。

状态：已确认的首版设计，包含 2026-11-10 用户追加的默认 HTTP/WS、无 Token 及可选 HTTPS/Token 配置。用户已明确授权现有 151 与本机部署切换到新默认。此前启用 HTTPS 与独立 Token 的 A01–A13 结果保留为历史验收；新访问模式与迁移另有实测记录，二者不互相替代。配置规则见[访问配置](../../../access-configuration.md)，运行步骤见[实现说明](../../../implementation.md)，实际结果与标准见[验收规格](acceptance.md)及[验收报告](../../../acceptance-report.md)，讨论来源见[沟通笔记](../../archive/beta/2026-11-10-first-flow/notes.md)。

## 目的与范围

让 Agent 通过 oi 调用不同机器上的受控能力，同时保留提供方的控制权。oi（loom-oi）当前提供 HTTP API 和 MCP 两种消费接入方式；本版以 MCP 为入口验证完整使用流程，未来会加入更多消费方式。脚本和其他程序也可通过现有接口消费能力。

beta 从头定义产品。alpha 用于理解原始意图，不自动继承其职责划分、实现和技术方案。先明确目标、概念和首个完整流程，再设计、实现和验证。本期通过 HTTP API 与 MCP 消费能力，不交付独立的消费端 CLI。

首版实现命令执行接入：Linux 使用 Bash App，本机 Windows 使用 PowerShell App，不实现 computer-use。两组共享 exec_command/write_stdin 交互契约，命令语法及平台执行行为由各自 App 适配。保持 OIS 定义、OIC 接入与 App 业务实现的解耦。当前版本采用固定调用链：Agent → HTTP API / MCP 接入 → vMCP 范围 → Tool → Action → 通道 → OIC → oiapp → 对应目标 cmd。中间按配置直接转交，不增加多 Host 路由、同 Tool 多目标合并、动态选路或负载均衡。

## 核心概念

| 概念 | 含义 |
| --- | --- |
| Host | 提供受控功能的宿主机；宿主机统一使用 Host 命名 |
| App | 一组相关 Action 的组织与接入单位，由一份 YAML 描述，例如 bash、computer-use |
| Action | App 中具体可调用的操作，例如 exec、click、screenshot |
| vMCP | 面向消费者的 MCP 入口，将选定 Action 映射成 Tool，并组成工具集 |
| 通道 | 固定调用链中到达指定 OIC 的通信入口，以配置 ID 标识，经 WS 或显式启用的 WSS 长连接承载调用；Token 按通道配置启用 |

App 包含 Action，Host 承载 App。Host 仍表示实际宿主机，但首版不作为 Agent 需要发现、选择的路由参数。Tool 已固定绑定目标 Action 及其通道，执行位置随绑定确定；这不要求整个系统只能存在一个 OIC。

Tool 是 OIS 面向消费者配置的操作呈现，可经 HTTP API 或 MCP 调用；执行侧的 App、Action 标识与消费侧的 Tool 展示名称分开。原先用于宿主机的 Node 名称不再使用。未来如引入负载池或逻辑路由目标，需要另行定义，不再用 Node 混指宿主机。

## OIC 与 App 描述

OIC 保留为轻量 CLI 宿主，负责受控侧接入，加载 App YAML，识别本机提供的 App、Actions、参数 Schema 及相应执行配置。收到 OIS 转发的数据后，OIC 根据已加载的 YAML 识别目标 Action、处理参数并转发到目标 oiapp 的实现。接入已有应用也是统一接入模型的变体。

一个 App 一份 app.yaml，配套实现代码和 runtime 入口。首版 App code 为 bash 和 powershell，均提供 exec_command、write_stdin。actions 目录表达各动作的实现模块，代码可以随 worker 打包，不要求每个动作自建网络服务；实现语言属于工程选择。其他 App 类型后续接入。

App YAML 需要包含 Action 和参数 Schema 的基本信息，以及 Action 与实际执行入口的对应关系。OIS 维护面向消费者的 Tool 定义和描述，本地 Schema 描述接入实现所需的参数；两者不要求严格一致。“轻量适配”不表示省略本地参数声明，也不意味着将业务实现写进 YAML。

Action 采用以名字为键的映射结构；省略 handler 时默认使用 Action 名，需要不同入口名时再显式声明 handler。参数使用 input_schema 的 type/properties/required/default/description 等基本信息；可选 input_mapping 只做显式改名，不引入表达式语言。省略同名 handler 不等于省略 Schema。

runtime 可以配置在 App 根节点，也可以配置在某个 Action 内。根节点提供默认执行配置，Action 上更具体的配置优先。按顶层字段覆盖，command 数组和对象字段整体替换，不做隐式递归合并。只有 Action 级配置也可表达入口。继承根 runtime 的 Action 共用本次加载的根 worker；非空 Action.runtime 覆盖使用独立实例，不因命令文本相同自动共享。

项目统一接入和调用方式，各 Action 仍可接受不同参数。OIC 根据 Action 的参数 Schema 与有效 runtime 对接实际入口，执行时进行必要的参数适配。首版托管 worker 使用 UTF-8 JSON Lines 经 stdin/stdout 交换 start/ready/invoke/result/error/cancel；诊断写 stderr，stdout 不混入命令原始输出。

OIS 和 OIC 都提供 CLI，start 默认后台运行，--foreground 用于前台观察；OIC 的 load/apps/unload 管理本地 App。首版两个命令 App 各使用共享 worker，保存各自进程、终端及输出状态；公共 OIC 不出现按 Bash/PowerShell 名称编写的业务分支。逐调用进程等其他运行模式后置。

load 以所有本地入口 ready 为成功条件，中心登记单独显示 pending/registered。多个 runtime 依次初始化，每个 worker 的 ready 等待上限为 10 秒，不设整次 load 总共 10 秒的截止时间。初始化部分失败时回收本次资源，不发布半个 App。同一有效定义重复加载无操作，定义变化必须先卸载；不自动监听文件热更新。unload 停止新调用、撤登记、回收执行并删除加载记录；stop 回收执行但保留加载记录；start 重新校验并加载记录，旧 session 不恢复。worker 崩溃标为不可用，不自动重启。

## OIS 与消费侧映射

OIS 提供管理 API、HTTP 执行入口和 MCP 入口。首版配置 Tool 对应的 Action 及其固定通道，并把 Tool 组成 vMCP。一次调用沿既有绑定转交到对应 OIC，不从多个执行目标中作选择。具体接口按[API 规格](../../archive/beta/2026-11-10-first-flow/ois-api-design.md)实现，Host/App/Action 登记是管理只读视图。

OIC 提供 Action、参数 Schema 及执行入口的本地接入，不负责组装 MCP。OIS 维护工具定义、参数描述、展示信息和 vMCP 组合；本地 Schema 的存在不意味着 OIS 必须由它自动生成或覆盖 Tool 定义。执行侧根据 YAML 承接调用，不将两侧参数定义完全一致作为加载或组合的前提。

一个 vMCP 可以包含多个 Tool，每个 Tool 都有自己的确定绑定。多个 Tool 的组合不意味着单个 Tool 同时映射多个可选执行目标。一个 MCP Server 与一个 App 没有固定一一对应要求。

OIS 首次初始化从 YAML 导入配置，此后由管理 API 修改持久配置，正常重启不由旧 YAML 覆盖。Tool 的 source 不原位更换，引用中的资源删除返回冲突；配置更新只影响新请求，已接受调用保留当次绑定。删除 vMCP 不等于结束已经启动的命令，不迁移旧 session。

## 参数映射与执行时适配

加载时不强制检查本地参数与 OIS 工具定义是否匹配；发现可识别的差异时至多警告，不以这种差异阻止加载。YAML 可读取、执行映射可解析等基础要求与跨层参数匹配是不同问题。

真正调用时才根据实际入口所需参数做轻量匹配和序列化、反序列化，尽可能适配传递。可转换的字符串与整数不应只因类型不同而被拒绝；实际无法适配的结构，例如对象或数组没有可用映射却要求作为整数输入，应返回参数错误。JSON 是承载格式，这里的结构不匹配指实际值，不是所有 JSON 编码的值都不能转换为整数。

系统不承担复杂的跨 Schema 一致性验证。OIC 依次完成显式改名、固定默认、基本类型转换和必填判断；缺少可选字段才使用默认，显式 null 不视作省略。目标字符串可接收字符串或整数，整数可接收完整可解析的整数值/字符串，布尔可接收布尔或 true/false 字符串；不将对象/数组强转为这些标量。动态默认、路径和命令业务语义留在 App。详细转换和错误契约采用命令执行详稿基线。

## 首版固定绑定与调用参数

Agent 通过 HTTP API 或 MCP 选择 vMCP 范围内的 Tool，直接提供业务参数；不再查询目标列表或传入 host_id、通道 ID 选择执行位置。OIS 根据 Tool 已绑定的 Action 和通道转发，通道到达对应 OIC，再交给 oiapp 的实现执行。本版完整流程以 MCP 接入进行验收。

首版不实现同一 Tool 多 Host 合并、list_targets、允许目标列表、自动切换目标或额外调度层。此前多 Host 的讨论保留为后续背景，不再作为本版实现与验收要求。

中间环节只承担定义查找、连接转交、轻量参数适配、结果返回与必要错误处理，不增加业务编排或自动重执行逻辑。Tool 的 source 固定为 channel/app/action；Agent 的 arguments 是扁平业务对象，内部 invoke 使用 input 和独立可信 context。POST /api/v1/exec 接收 vmcp/tool/arguments，经过与 MCP 相同的消费权限与绑定处理。

## OIS—OIC 连接与执行边界

OIC 主动连接配置的 OIS，默认使用 WS /api/v1/oic/connect 承载 JSON 调用协议；启用 HTTPS 入口时也可使用 WSS。首版只通过既定在线通道投递，通道不可用则返回错误，不切换到其他 OIC，也不积压离线调用等待恢复后执行。重连后重新登记能力；心跳基线为 10 秒、30 秒无法确认存活则不可用。一个通道只接受一个活动 OIC，旧连接的迟到消息不能覆盖新登记。

OIS 管消费方能使用哪些已配置 Tool，OIC 只执行本机已加载、启用的 Action；本地停用后拒绝新调用。默认关闭 Token 鉴权；admin.token_env、通道 credential_env、vMCP access.credential_env 分别为空时关闭对应鉴权，显式填写环境变量引用后独立启用。已填写引用却未设置变量或值为空时必须拒绝，不能降级为匿名。业务输入不能覆盖身份或执行入口。

匿名消费者在同一 vMCP 内共享 anonymous:<vmcp_id> 身份，任意 Bearer 内容不会产生新身份；启用 Token 后沿用凭据对应的可信身份。共享匿名范围或同一凭据的多个 Agent 不隔离为不同用户。OIC 始终显式配置 host.id 和 channel.id，OIS 核对 expected_host_id 并拒绝重复活动连接；通道启用 Token 时再校验对应凭据。匿名模式中的 ID 匹配是配置约束，不是机器身份认证。首版不做硬件识别、自动归并或身份迁移。

OIS 根 server.listen/public_url 默认提供 HTTP。可用 server.https 配置额外的共享 HTTPS 监听、public_url、cert_file、key_file；证书由管理员提供并由 CA 签发，OIS 不自动申请、签发或续期。客户端验证证书与目标名称。多个 vMCP 复用这个 HTTPS 入口，不逐个创建监听或证书。保留旧 tls_cert/tls_key 的 HTTPS-only 配置兼容，与新 server.https 块不能混用。

创建或修改 vMCP 时通过 access.require_https 决定是否强制 HTTPS，默认 false；它与是否启用 Token 独立。true 必须已有 HTTPS 入口，endpoint 使用 HTTPS 地址；REST /exec 和 MCP 都按真实 TLS 连接检查，普通 HTTP 返回 403 HTTPS_REQUIRED，不信任 Forwarded 等头绕过限制。false 的 endpoint 使用根 public_url，同时可在已配置的 HTTPS 入口调用。

MCP 首个验收版本采用 2025-11-25 Streamable HTTP，Windows PowerShell 7 客户端脚本完成标准初始化、工具发现及调用。不依赖 Tasks 扩展或自定义 stdout 通知。MCP 请求的显式取消通知转为内部 cancel；普通网络断开不能视作可靠取消。REST 请求取消/超时尽力转交同一内部规则；执行请求上层期限为 60 秒基线，与不超过 30 秒的 Action 等待窗口分开。

## 首版命令执行的交互模型

Linux Bash 与 Windows PowerShell 均按 exec_command / write_stdin 两动作设计：

- exec_command 启动一次执行并等待一段时间；已结束则返回输出和退出码，仍在运行则返回已有输出及 session_id，供后续继续操作。
- write_stdin 使用该 session_id；chars 为空时等待并获取后续输出，非空时写入输入并继续获取输出，直到执行结束。

yield_time_ms 表示本次等待时间，不是执行期限；max_output_tokens 表示本次返回输出的近似预算。采用命令详稿中的默认基线：exec 等待 10000 毫秒，write 空输入 5000/非空 250 毫秒，等待范围 0–30000；输出预算默认 10000、本地上限 20000。并发 8、每执行 1 MiB 待输出缓冲、最长运行 30 分钟、无调用活动 10 分钟回收等作为可配置基线。输出截断必须标明，被省略部分下一次不补读。执行程序由本机 App 配置确定，消费者不能覆盖 runtime。

session_id 标识同一次持续执行；新的 exec_command 不自动继承上次 shell 的目录或变量。同一存活的交互 shell 可以通过后续输入继续操作。该 ID 与原 App 实例及访问者范围关联；每组两个 Tool 到达同一执行后端，后续 write_stdin 沿固定绑定接续，跨组句柄拒绝使用。call_id 标识单次调用，有限运行周期内去重，不承诺跨重启恰好执行一次。

tty=false 的 stdin 为 EOF，后续仅允许空输入读取；需要输入或控制字符时创建 tty=true 执行。Linux 使用 PTY，Windows 使用实际终端适配；两端都必须验收，不能用普通管道冒充。首版没有独立强制停止/resize 工具或公开输出游标。非 TTY 句柄返回后依靠完成、限额或本地卸载回收；发送 Ctrl-C 不等于确认退出。

显式取消在 exec 尚未启动时阻止启动；已启动但本次结果未提交时请求回收新执行；取消 write_stdin 只取消本次等待/尚未完成输入，不终止旧执行，已写入内容不回滚。已提交结果不被迟到取消撤销。交付后失联返回结果未知，不自动重跑；仅当原 App 实例仍存活、身份有效并恢复同一固定连接时，已知句柄才可继续。资源回收必须在所选平台验证，不能只因 worker 退出就宣称全部子进程已停。

## 后续 Computer-use 的控制范围

CU 不属于首版实现范围。后续首个 CU 场景限定同一桌面同一时段只有一个控制者，控制范围覆盖截图、操作和再次观察。仅对单个 Action 排队不足以保证整个操作过程不被其他 Agent 干扰。

桌面上下文标识与控制权分别表达；占用、释放和控制者失联后的处理规则仍需设计，不从单控制者方向推定已经确定通用会话或工作流机制。

## 没有独立实体 Host 的接入

App 描述层仍需保留。如果接入对象只是一组远程 HTTP 接口、没有独立受控机器，对应 YAML 可以放在 OIS。

这确定的是统一 App 描述及其可放置位置，不代表已经设计 OIS 执行器、通用 HTTP 适配字段、凭据管理或具体执行部署。完整 MCP 接入、单 Action 接入及多受控方组合也使用统一描述和映射思路，具体适配方式尚未确定。

## 工作方式与 Done

开发及设计文档使用 beta 分支的独立 worktree；main、alpha 和历史实验 worktree 保留，alpha 的未提交改动不修改或删除。

冻结阶段仅处理文档，随后用户已授权完整开发和部署验收。实现语言、库和存储介质是工程内部选择，须满足已冻结契约；这不再是未决产品目标。

验收部署与绑定：

| 位置/入口 | 职责 |
| --- | --- |
| admin@121.199.175.151 | Linux OIS、Linux OIC、Bash App |
| 本机 Windows | Windows OIC、PowerShell 7 App、发起 MCP 调用的客户端 |
| 151 的 25100/TCP | 默认 OIS HTTP 与 WS 服务入口；用户允许 25000–27000，启用额外 HTTPS 监听时再显式配置相应端口 |
| /mcp/workbench | 同一个 vMCP，暴露下表四个固定 Tool |

| Tool | 通道 / Host ID | App.Action |
| --- | --- | --- |
| linux_exec_command | linux-151 | bash.exec_command |
| linux_write_stdin | linux-151 | bash.write_stdin |
| windows_exec_command | windows-local | powershell.exec_command |
| windows_write_stdin | windows-local | powershell.write_stdin |

“一个 server、两组 bash”按一个 MCP 入口、两组命令执行工具落实，Windows 组明确使用 PowerShell 语法，不宣称 Bash 语法兼容。两台机器不引入同 Tool 的多目标选择。

Done 以[验收规格](acceptance.md)为准：Windows 客户端发现四个工具，分别验证 Linux 和 Windows 的短执行、十分钟无输出执行与持续轮询、TTY 输入、会话隔离、参数适配、失败/断连/取消、加载/重启及资源回收。只读检查或文档通过不等于这些功能通过。

## 后续范围与工程验证

CU 控制权、无独立 Host 的 App、多 Host 合并与负载均衡、热更新、执行迁移、完整输出历史和跨重启执行恢复均不进入首版。

Windows 终端/进程回收、Linux 后代清理和公网连通按验收规格实际验证；启用 HTTPS 时额外验证证书与信任。默认匿名访问、启用 Token、require_https 与现有部署迁移分别保留证据，不以此前 HTTPS/Token 验收替代新默认验证。实际可执行包、依赖版本和内部代码结构见实现说明。
