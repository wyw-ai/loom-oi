# loom-oi beta · 沟通笔记

- 主题：`beta`
- 讨论标识：`first-flow`
- 文档父目录：`C:/Users/c6321/GolandProjects/copilot-worktrees/loom-oi/beta/docs/goal-shaping`
- 本轮范围：beta 产品目标、核心概念及首个流程的连续讨论；当前同步默认 HTTP/WS、无 Token 与可选访问配置，并记录现有部署切换授权。
- 状态：已结束并归档（2026-11-10）；首版范围已采纳并完成实现与验收，后续访问配置已另行验证，当前项目规范维护于 `docs/spec/`。历史候选、被替代结论和当时的未实施描述保留。
- 启动确认：2026-11-07，用户明确要求“那就启动 goal-shaping，用它继续 loom-oi beta 的讨论。先把前面已确认的共识和候选方案整理清楚，再继续讨论 oicapp、bash.yaml 和 oic load 的行为。”该指令明确授权启动，本轮不重复询问。
- 初始基线：beta commit `90d681c4c26f4c6d801ab8e0a29fe9eb8416d6bb`；读取时工作区干净，仅初始化 README，无既有 goal-shaping 文档。
- 日期采用用户时区 Asia/Shanghai。前序讨论于 2026-11-06 开始，本轮于 2026-11-07 整理。

## 前序讨论回溯

本节归纳对话，不把助手建议当作用户决定。

### 重启范围与 alpha 的作用

用户要求 beta 从头定义产品，不默认继承 alpha 的职责和技术；先明确目标、概念及完整流程，再设计、实现和验证。所有开发限于指定 beta worktree；旧 main、alpha 和实验 worktree 保留，alpha 未提交改动不能修改或删除。

用户允许从 alpha 了解原始意图。已只读查看当前概念、目标、架构、使用手册、关键入口及历史讨论。由此形成“让分散环境中的能力被 Agent、脚本或程序发现、选中和使用，提供方保留控制权”的理解；用户随后以“对的”承接并提出 beta 方向。alpha 的纯连接方案未被重新采纳。

### OIS、OIC 与本期边界

用户明确提出：做一个 OIS；OIC 是很轻的胶水层，负责 OIS 与受控方的对接；本期可以不做 oi，只靠 OIS API。

用户随后给出示意：`POST /exec {"action":{"bash":"ls -l"}} → OIS → OIC → oicapp[bash] 实现 → Bash → ls -l`。

采纳范围：OIS API 入口、轻量 OIC、具体能力的 oicapp 实现与调用关系，以及本期不做独立 oi。HTTP 路径、动作载荷、进程边界和协议仍是示意或未决。

### 启动与加载的候选体验

用户提出“可能”具有 `ois start`、`oic start`、`oic load xxx.yaml`。这些记录为候选入口，不当作冻结命令。

助手补充了启动、加载、登记、API 调用、返回结果的流程，并提出显式目标选择。用户没有明确采用 target 字段、加载成功条件或任何具体登记协议。

### Bash 形态与配置的候选方案

用户要求以 Bash 讨论 oicapp 形态、bash.yaml 和 load 行为。助手推荐独立常驻 oicapp-bash，由 OIC 启动，经 stdio 通信，每次调用启动新 Bash；公共加载配置与应用业务配置分开解释；初始化时声明操作；OIS 确认登记后 load 报告可用。

助手给出 YAML 示例、超时和输出预算、stdout/stderr/退出码返回、target 字段以及每次调用不延续 cd 的语义。上述均为助手候选，用户尚未确认。没有创建程序、配置实现或运行验证。

### 技能发现与启动

用户先询问本地 goal-shaping 是否可见。已读取 `C:/Users/c6321/.agents/skills/goal-shaping/SKILL.md`，当时仅确认可读，没有启动方法或创建文档。

2026-11-07 用户明确要求启动并整理既有讨论。本轮读取文档管理规则和模板，采用项目根目录的 goal-shaping/，同一主题继续使用 first-flow，不创建空归档。

## 2026-11-07：整理与继续讨论

- 将已确认的产品意向同步到 current，明确它不是实现现状或验收结果。
- 同步后的共识基线：`current/beta/design.md` 尚未提交，SHA-256 为 `3d66b8b825eb5ae1b99d66058c79cdbba98f56f055806963144b64cb4e755785`。
- 将用户提出的交互示例和助手提出的运行方案保留在完整候选草稿；使用状态标记避免误认为整稿已采纳。
- 将独立进程方案的替代形态和成本补入草稿，保留独立进程方案为推荐候选。
- 当前建议聚焦 load 的产品承诺：只接入已有能力，还是启动并维持 oicapp。这个选择直接影响应用形态、配置内容和 OIC 的运行责任。
- 尚未取得对上述选择的进一步答复，不同步候选运行机制到 current。
- 已核对文档中的本地链接；独立复核未发现把候选误记为共识的实质问题。本轮仅保存设计文档与 README 入口，未提交、推送或开始实现。

## 2026-11-07：统一加载模型

- 用户明确回复：“我觉得是第一个，第二个其实也算是第一个的变体，没区别的。”语境中的第一项是启动并管理应用，第二项是接入已有应用。
- 已同步共识：OIC 加载并管理 oicapp；接入已有应用属于同一加载模型的变体。此前把二者作为互斥路线的讨论方式已被替代，前一节的首要未决项在这个层面得到解决。
- 同步后的共识基线：`current/beta/design.md` 尚未提交，SHA-256 为 `78e095508353e35956edd6ea2ca81dee4cff7335919e5477917ce55a1f2c762d`。
- 采纳不扩大为独立进程、stdio、自动重启或接管外部目标生命周期；建立外部连接的具体层次也没有因这句话单独确定。
- 草稿保留独立进程为候选，并加入下一步建议：将 oicapp 实现与配置加载实例分开，同一 Bash 实现可候选加载为多个命名实例，各有默认工作目录。这是助手的新提议，尚未确认。

## 2026-11-07：YAML 结构建议

- 用户询问“你觉得未来yaml里面怎么设计比较好”，尚未确认上一轮的多实例提议。
- 助手建议把加载配置组织为 version、name、app.ref、config 四部分，以明确的本地应用入口作为初期引用；按独立可执行程序候选给出 Bash 示例。
- 先前的 process.executable 是较低层的启动示例。本轮推荐改用应用引用表达加载对象，把业务配置交给应用；具体运行入口、启动参数与应用包形态仍未决定，没有把原方案当作已确认后再撤销。
- 建议区分格式版本与应用版本、实例名称与实现身份及动作名称；相对应用引用以 YAML 所在目录为基准。简单名称解析、版本选择、应用下载没有被隐含为既有功能。
- 草稿补充配置分层校验、由应用声明操作、公共字段仅承载 OIC 能负责的行为等建议；Computer Use 示例只用于说明扩展形状，不加入首版范围。
- 此轮更新候选草稿与笔记，current 未变化；共识基线仍为 SHA-256 `78e095508353e35956edd6ea2ca81dee4cff7335919e5477917ce55a1f2c762d`。

## 2026-11-07：从新链路推演终态

- 用户明确给出最新链路：`agent->mcp.json->mcp-server[vmcp]->原子能力A->绑定信息->机器A.oic.oiapp.能力A.yaml -> 能力A.oiapp -> bash -> ls -l`，并要求先不解释，由助手独立理解产品终态。
- current 同步这条原始链路及明确出现的概念。OIS 的原有 API 方向予以保留，但其在新链路中的位置列为未决；没有因省略 OIS 推定移除，也没有将 oiapp 擅自认定为 oicapp 的更名。
- 同步后的共识基线：`current/beta/design.md` 尚未提交，SHA-256 为 `0204f66cf78ce152a9d627da291bfe449c3899a8f20b60588f3cdf075b7ef8b6`。
- 助手推断：产品中心是面向 Agent 的能力及其具体执行绑定。vmcp 可能是可配置能力集合的逻辑 MCP 入口；能力定义、绑定和实际部署应分别理解。上述解释只进入草稿，尚未确认。
- 草稿围绕候选完整体验重整：提供能力、建立绑定、配置 MCP、Agent 调用、返回结果。原先四字段加载 YAML 保留为局部候选，不能代表尚未解释的能力A.yaml 全部职责。
- 能力 A 可能是通用脚本执行，也可能是列目录等更窄操作；同一 oiapp 能否承载多个能力、一个能力能否有多个部署均未从链路直接推出。
- 独立推演复核同样指出能力/绑定是主要变化，提醒不把点号链当作文件路径、不把 vmcp 自动视为某个既定项目、不补造自动调度或故障切换。
- 为核对 MCP 的一般工具概念，只读了[官方服务端概念](https://modelcontextprotocol.io/docs/learn/server-concepts)：工具可以提供操作说明与输入输出约定；它不解释本项目 vmcp、绑定、mcp.json 文件形式或 oiapp 的含义。未查找或调用任何用户外部账户资料。
- 本轮不要求用户解释或确认某个字段，不开始实现；先呈现对产品终态的完整理解。

## 2026-11-07：工具合并与显式目标路由

- 用户明确希望有路由与合并：机器 A/B 都提供 Bash 时，OIS 看到一个工具，内部保留两个映射；Agent 调用 Bash 时必须传入一个能够查询到的设备 ID。
- 用户允许考虑逻辑节点抽象：Agent 面向节点，未来不同工具可分配给不同节点，节点下可以配置负载均衡。此处是演进方向，尚不代表首版要实现负载均衡或已经采纳某个节点到设备的模型。
- 已同步至 current：同类工具合并、多个执行目标、可查询目标 ID、调用时显式必填目标，以及 OIS 的相应路由职责。必填 ID 不表示所有调用永远固定同一个目标。
- 同步后的共识基线：current/beta/design.md 未提交，SHA-256 为 `7d661c1e148aa87b7dc173158479941db20961afb762882ee53e49c5a5578cc9`。
- 此表达修正了此前草稿中“一个对外能力预先指向一个执行对象”的简化：现在同一工具可以有多个映射，每次按工具与目标 ID 共同路由。
- 助手推荐对外使用 node_id，首版节点可一对一映射设备，未来节点内再选执行成员；这仍为候选，不把参数名或节点实现写成共识。
- 草稿补充候选约束：合并依赖共同能力契约，不能只看同名；负载池内实现需要适合互换。该建议用于保持 Bash 操作环境的可理解性，尚未冻结兼容判断或负载均衡规则。
- 更新候选完整流程和 Done：Agent 看到一个 Bash，查询两个目标并分别调用；新增目标缺失或不匹配的候选验收。整体验收标准尚未确认。

## 2026-11-07：OIS 定义工具，应用声明实现；比较 HTTP 与 SDK

- 用户明确：Bash 工具本身在 OIS 创建；借助 OIC 的 YAML 机制开发和接入 bash-oic 这个 oiapp，指定工具名字，并暴露自己支持该工具。
- 已同步共识：中心工具定义和实现支持声明分开，多个机器上的实现挂到同一个 OIS 工具下。此前“同类工具合并”进一步澄清为已有工具下的多实现映射，不是从实现端同名声明自动推导中心定义。
- 同步后的共识基线：current/beta/design.md 未提交，SHA-256 为 `f8722efcca7d4bc68a5675b27a0140ee44248dcab1d967aac947b2e04ae896fd`。
- 用户担心前期 SDK 迭代压力，提出拆分及 OIC—OIS 采用 HTTP，也提出若做 SDK 可先考虑 Rust，并询问判断。这些选项仍在讨论，未当作语言、传输或 SDK 采用决定。
- 助手推荐先定义语言无关协议、保留独立 OIC、做一个 bash-oic 参考实现；Rust 可用于系统能力适配器，再从实际重复需求提取薄 SDK。SDK 暂按面向 oiapp 作者的接入库理解，不能假定等同于 OIS API 客户端或内嵌完整 OIC。
- 明确区分 OIS—OIC 和 OIC—oiapp 两个边界：HTTP 可改善语言与进程解耦，但不会自动定义工具兼容、调用投递、结果、取消和版本等语义。SDK 可以封装 HTTP，两者并非互斥。
- YAML 示例加入 provides 引用中心工具的字段候选，并将应用入口示例改为 bash-oic。工具输入输出、版本校验和提供声明的机制未冻结。
- 仍需判断 YAML 是否只是声明/配置，还是承担执行映射甚至实现逻辑；后者会影响 SDK 的对象与价值，尚未擅自解释。
- 只读参考：[Rust 进程模块](https://doc.rust-lang.org/std/process/index.html)、[系统扩展](https://doc.rust-lang.org/std/os/index.html)、[FFI](https://doc.rust-lang.org/nomicon/ffi.html)、[HTTP 语义](https://httpwg.org/specs/rfc9110.html)。这些支持语言与协议能力的判断，不构成本项目技术选型或实现验收。

## 2026-11-07：CLI、oiapp 形态与控制机制的连贯候选

- 用户再次明确 Bash 工具在 OIS 创建，bash-oic 通过 YAML 指定并声明支持它；本轮重点询问 OIC CLI 的形态、如何加载、如何控制应用逻辑，以及逻辑以何种形式存在。
- 读取时 current 已包含中心工具定义与实现声明的共识，以及 HTTP/SDK/Rust 选项；保留这些已有内容，没有覆盖独立补充。实际共识基线为 SHA-256 `f8722efcca7d4bc68a5675b27a0140ee44248dcab1d967aac947b2e04ae896fd`。
- 助手推荐一个连贯候选：一个 oic 程序提供常驻宿主和短时管理命令；oiapp 由 YAML 描述及独立程序组成；应用通过小型控制协议实现初始化、调用、取消和关闭。没有因推荐而同步为已确认形态。
- 草稿将 app.ref 的局部示例展开为 runtime.command/transport，加上 provides.tool/action 和应用 config，使“中心工具名如何落实到本地处理函数”可讨论。字段名和协议方法都未冻结。
- 推荐本地托管层先比较 stdio 的小协议路线，不将其扩展为 OIS—OIC 的传输决定；保留现有 HTTP 与 SDK 比较段落。
- 控制包含通用进程管理与应用主动处理协议两部分；应用负责具体 Bash 进程和结果。cancel 与 shutdown 分属调用和应用生命周期，不能混成杀进程即可证明业务取消。
- 建议先用 bash-oic 参考实现验证协议，再提取公共接入代码为薄 SDK；若需要纯 YAML 定义简单动作，可另由一种通用 runner oiapp 实现，不把所有业务逻辑塞入 OIC。
- 只读核对了 [JSON-RPC 2.0 规范](https://www.jsonrpc.org/specification) 的请求/响应、关联 ID 与传输无关特性；它不定义本项目 initialize/invoke/cancel/shutdown、分帧或业务停止语义。
- 本轮只更新候选草稿和笔记；current 内容与上述基线一致，未开始实现、提交或推送。

## 2026-11-07：读取最新跨会话讨论并对照原文

- 用户要求查看最新远程会话讨论并寻找启发。通过会话工具找到并完整读取[总结达成共识](https://chatgpt.com/c/6aeed28e-9454-83ea-8fec-5f729d2c4d5b)，再读取它附带的原始文字讨论，而非仅依据助手摘要。
- 远程会话中用户直接更正：“node改名为host了，以后不要有歧义了”。原始附件含 2026-11-07 12:04 至 15:22 的讨论文字，来源时间按附件保留；若干图片只有占位记录，未取得图片内容，不补猜图示。
- 附件由会话工具提供，文件名“粘贴的文本 (1).txt”，25,061 字节，SHA-256 为 `462b02f4694b16a69dd85a60de1b358c2f472bdeb364e2cc0c3c78aa1cb57207`。原文未复制进项目，仅保存必要结论及来源定位。
- 15:00–15:04 双方厘清：项目统一接入调用方式，Action 参数 Schema 需要声明；一个 App 一份 YAML，computer-use 的 click/screenshot 属于同一 App，不逐动作拆 App。15:12–15:13 明确 OIC 是加载 App YAML 的轻量 CLI，YAML 配套 actions 目录保存脚本。
- 15:07–15:10 采用 App/Action 术语并将 Action 到 Tool 的映射和 vMCP 组合交给 OIS。15:18–15:21 统一宿主机口径、保留 App 描述层，允许按 App、Host、Action 选择映射；仅有远程 HTTP 接口时 YAML 可位于 OIS。宿主机最终名称以远程会话中用户直接更正的 Host 为准。
- 已同步这些明确结论至 current；不把远程助手的所有推论自动视为共识。工具展示仍在 OIS，但旧的“执行 App 必须引用中心 Tool 名称”表述被新讨论细化为“App 声明 Action，OIS 配置 Tool 映射”；Action 参数结构不能仍被假设全部只在 OIS 定义。
- 之前建议的独立常驻适配进程、runtime.transport 和每 App 自建 initialize/invoke/cancel/shutdown 服务降为特殊实现备选，不再作为默认 App 形态。之前 node_id 命名建议也不再继续作为默认字段；未来路由池需要另行命名，不复用 Host 表示逻辑池。
- 助手新的推荐候选：OIC 提供统一 Action 运行能力，首个 Bash 按调用执行脚本；App 作者提供描述、Schema 和业务脚本。加载主要建立描述/入口索引并登记 App/Actions，不要求加载时执行业务或为每个 App 启动常驻服务。逐调用进程、输入输出通道和具体字段尚未确认。
- 新建议还包括：执行 Schema 与消费侧展示分工、系统目标 ID 与业务参数分开、结果契约需能表达非文本输出，以及 App 描述与执行位置分开。这些放入草稿，未当作已确定实现。
- 保留既有“同 Tool 多 Host 映射、Agent 显式目标 ID”方向；负载均衡仍未进入首版承诺。保留 Go/Rust/SDK/HTTP 的未定状态，不根据中途建议选择技术栈。
- 同步后的共识基线：current/beta/design.md 未提交，SHA-256 为 `2c7add6646fc0a2bc5f70473b709055db8222e6486676e8a4014693154b22300`。草稿按新概念重整，既有推演沿革留在本笔记；未开始实现、提交或推送。

## 2026-11-07：整理 Bash 与 computer-use 的完整设计文章

- 用户要求根据当前讨论，给出未来 YAML、目录设计，以及机器 A 提供 CU、机器 B 提供 Bash 时的 OIS 配置、配置后操作和 Agent 对接流程。
- 新增[从 App YAML 到 Agent 调用](app-to-agent-walkthrough.md)，作为本次讨论的独立阅读材料，并从 README 和候选设计总稿链接。文章沿用已确认的 Host/App/Action/Tool/vMCP 骨架；没有创建示例程序、真实部署配置或启动服务。
- 为使流程具体可评估，助手提出完整候选：App 的 config_schema/input_schema/output_schema/run，分离的 OIC 本机配置，OIC 统一逐次脚本输入输出，OIS Tool/vMCP 声明，目标查询 list_targets 和 host_id/input 参数分层。一个 Host 每个 App 一个实例、CLI 后台宿主、持久加载记录及凭据方案均是示例约束或新建议，未当作用户已确认结论。
- Bash 示例明确单次执行及 stdout/stderr/exit_code；CU 示例包含 screenshot/click/type_text，明确实际图像返回、桌面上下文校验、像素坐标和操作后的观察。未将 computer-use 同时纳入首版范围，也未承诺现有跨平台实现。
- 客户端例子核对了 [VS Code 官方配置参考](https://code.visualstudio.com/docs/agents/reference/mcp-configuration)，仅用于该客户端本地会话，不将其 mcp.json 格式当作所有 Agent 的通用格式。核对 [MCP Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools) 和[传输规范](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)，区分标准工具发现与本项目建议的目标发现工具，不从客户端传输推定 OIS—OIC 的协议。
- 本轮没有新增用户明确采纳的字段或机制，current 保持不变；共识基线仍为 SHA-256 `2c7add6646fc0a2bc5f70473b709055db8222e6486676e8a4014693154b22300`。未提交、推送或开始实现。
- 完成文档校验：5 段 YAML 和 12 段 JSON 均可解析，Host/App/Action/Tool 引用、MCP 地址及示例参数相互一致，本地文档链接有效；两次独立只读复核未发现必须修复的问题。这是文档一致性检查，不是功能运行或端到端验收。

## 2026-11-07：整体架构复核与实现前的缺口

- 前一轮按用户明确要求，将 README、已确认共识、候选稿、笔记及完整文章提交为 `cd7bdc5` 并推送至 `origin/beta`；本轮读取时工作区干净，HEAD 与该提交一致。
- 用户表示“感觉没什么问题了”，并询问整体架构还需要补什么。记录为对当前整体方向的认可及进一步复核请求，不扩大成所有 YAML 字段、运行机制或首版范围均已冻结，也不据此开始实现。
- 复核结论：现有概念及正常流程足以支撑首个闭环，主要缺口在连接投递、调用状态与断连、CU 跨 Action 控制权、配置及更新权威、提供方本地执行范围，以及首版验收边界。将最小建议补入候选设计总稿的“实现前需要补齐的运行约定”。
- 新建议包括 OIC 主动连接、离线不排队、贯穿调用 ID、断连结果未知时不自动重发、首个 CU 限制单一控制者、变更时处理在途调用、区分 OIS 消费方权限与 OIC 本地执行范围。这些均为候选；未选择通信协议、语言或通用会话框架。
- 另外明确：A 的 CU 加 B 的 Bash 验证跨 App 组合与图片返回；一个 Bash Tool 对应两台 Host 才验证合并和显式路由。建议按 Bash 后 CU 的顺序收束，先固定平台和客户端，具体发布范围仍未确定。
- current 已覆盖用户认可的概念主干，本轮未引入新的明确采纳机制，保持不变；基线为提交 `cd7bdc5`，共识文件 SHA-256 仍为 `2c7add6646fc0a2bc5f70473b709055db8222e6486676e8a4014693154b22300`。本轮仅更新候选稿和笔记，未开始实现或再次提交推送。

## 2026-11-09：确认连接与控制边界，简化参数定义和适配

- 用户明确“135都可以”，对应上一回复的第 1 项 OIC 主动连接、在线即时投递及重连登记，第 3 项 CU 同一桌面单控制者覆盖整个观察—操作过程，第 5 项 OIS 管消费方权限、OIC 管本地已启用 Action 范围。已同步这些范围到 current；不将它扩大为具体协议、控制权接口或凭据格式已经确定。
- 用户修正第 4 项：OIS 维护定义和工具描述，OIC 加载的 YAML 只含必要映射；无需强制本地参数与中心定义匹配，load 对可识别差异至多警告。真正执行时才做参数匹配与序列化、反序列化适配，可转换的 str/int 尽量传递，实际无法适配的值才报错；不希望在这里增加过多参数校验逻辑。
- 已替代旧的“App YAML 声明完整参数 Schema，OIS 从其继承工具定义”模型，以及加载/组合时强制 Schema 一致的建议。当前设计与候选稿均调整为中心维护工具定义、本地维护必要执行与参数映射。配套文章的 YAML 和完整流程同步修订，旧思路保留在此前历史笔记中。
- 将用户所说的 JSON 与 int 不匹配按实际值理解：例如对象/数组无法直接作为整数；JSON 编码本身可以承载整数。数字字符串可转换，无法解析的文本不能凭空填默认值。具体转换范围和目标类型来源仍未冻结；不假设 OIC 能自动推断任意脚本签名，也不因此引入完整映射语言或第二套 Schema。
- 第 2 项调用状态、断连结果、重复投递和取消语义未获本轮确认，继续保留候选。第 4 项下此前建议的运行中拒绝更新/卸载、YAML 与 API 编辑权威等，也没有因本次参数方向澄清而自动采纳。
- 读取时保留了上一轮候选稿与笔记的未提交修改。同步前基线为 `cd7bdc5`，current 文件 SHA-256 为 `2c7add6646fc0a2bc5f70473b709055db8222e6486676e8a4014693154b22300`；同步后 current 未提交，SHA-256 为 `83a4340364ef639fff0dceeb1c9d6cd612fc71cecc0a5195f570f14fe3fdec58`。本轮继续设计，不开始实现、提交或推送。

## 2026-11-09：首版只实现 Bash，按终态保持解耦

- 用户明确首版按终态设计架构，接入方只做 Bash、不做 CU，并要求应解耦的地方保持解耦。同时询问 MCP 的一次性调用与流式输出，以及如何实现完善的 Bash。
- 已将首版 Bash-only 范围和公共层/App 解耦要求同步到 current。CU 的既有单控制者共识保留为后续设计，候选稿及完整文章不再将 CU 是否进入首版标为未决；文章继续作为终态组合示例。
- 核对截至本日的 MCP 官方 latest（2026-07-28）：普通工具结果、可选 progress、SSE 传输与可补读命令输出是不同层次。Tasks 为需双方声明支持的官方扩展，不自动提供 stdout 分块或 stdin；不能把可用性建立在所有客户端都支持扩展的假设上。来源见候选稿的 MCP 出口段落。
- 助手新建议：Bash App 提供 exec/read/write/cancel/list，短命令直接返回，长执行返回 job_id，通过游标读取新增输出；wait_ms 与执行期限分开。建议首版补上可选 PTY 及 resize，持久 shell 则作为后续显式模式。上述具体范围、动作名和字段尚未采纳。
- 为保持解耦，建议 Bash App 的共享本地后端持有长进程、stdin、输出缓存和作业状态；OIC 只做通用托管与调用，OIS 只做工具定义、权限、路由和 MCP 适配。每次 Action 可继续使用一次输入/一次结果，不能让已经退出的脚本承担跨调用句柄；共享后端具体进程形态未定。
- 新增的游标、保留限额、作业查询、取消和断连恢复规则均是候选。已有线上投递共识不扩大为调用状态机已经确定；job_id、call_id、MCP taskId 与未来 shell session_id 分开理解。没有开始开发或引入通用持久任务平台。
- 本轮同步前 current SHA-256 为 `83a4340364ef639fff0dceeb1c9d6cd612fc71cecc0a5195f570f14fe3fdec58`；同步后未提交，SHA-256 为 `30062193cf4ce3c9204a0d1c6ddd4c0dee717f00e083e337db4ec7520982690a`。保留全部已有修改，未提交或推送。

## 2026-11-09：按当前命令工具收敛 Bash 两动作模型

- 用户要求“你自己的bash工具怎么设计的按照这个来设计”。据此采用本会话可见的 exec_command / write_stdin 交互模型，不将此前助手建议的五个动作原样采纳。
- exec_command 启动执行并等待，结束时返回 output/exit_code，仍运行时返回 output/session_id；write_stdin 使用同一 session_id，空 chars 用于继续等待输出，非空 chars 写入输入后收取输出。等待窗口、输出预算和可选终端模式进入已确认方向；默认值、边界限额和内部实现仍待细化。
- 核对了[官方 Codex Prompting Guide](https://developers.openai.com/cookbook/examples/gpt-5/codex_prompting_guide) 的公开说明，主要事实依据仍是本会话真实工具声明与观察。没有读取或声称知道当前工具的内部进程、缓存或恢复实现。
- 在 beta 工作目录用当前命令工具启动了一个无文件副作用的 Python stdin 回显进程，启用终端模式；观察到启动返回 session_id、空输入轮询仍保留 session、写入一行后返回回显和 exit_code 0。测试进程已经结束；这只验证当前工具的交互形状，不是 loom-oi 代码或 Bash 跨平台验收。
- 同步 current 和候选稿，将此前 exec/read/write/cancel/list 与 job_id/cursor 方案收敛为两个核心 Action 及 session_id。独立列表、强制停止、resize、可回放游标未从当前工具确认，不作为首版必选接口。session_id 表示同一次持续执行，新 exec_command 不自动继承旧 shell 状态。
- 保持终态解耦：建议由 Bash App 的共享执行后端持有进程与缓冲，OIC 进行通用托管，OIS 做定义、权限、路由及 MCP 适配。执行后端组织方式是本项目的候选实现，不是对 Codex 内部结构的复述。普通通用命令工具的 shell 选择和宿主专有选项也不直接照搬成 Bash App 参数。
- 配套文章同步调整 Bash 目录、两份 Action 映射、中央 Tool 定义、调用示例和工具计数。CU 仍为后续终态示例，首版只做 Bash。
- 同步前 current SHA-256 为 `30062193cf4ce3c9204a0d1c6ddd4c0dee717f00e083e337db4ec7520982690a`；同步后未提交，SHA-256 为 `802099289fc9243093e3f98ce5c4ec030e07ecbdb629286a0c5c0742e17e06f4`。保留已有修改，未开始 loom-oi 实现、提交或推送。

## 2026-11-09：展开 Bash Action、参数和运行机制详细候选

- 用户要求“action和参数还有机制，你给我一个详细的设计”。本轮继续已启动的 goal-shaping，范围是把已确认的两动作交互展开为可评审契约，没有取得开始实现或再次提交推送的指令。
- 新增 [Bash Action、参数与执行机制详稿](bash-action-design.md)，涵盖两个 Action 的字段与默认值、执行时轻量绑定、结果与错误、管道/PTY、输出消费与截断、并发、去重、身份、限额、App YAML、load、取消及断连，并给出完整调用示例和验收条件。
- 本轮建议 Bash 使用 OIC 直接托管的共享 worker，两个 Action 为进程内处理入口，runtime.command 及 actions.handler 只描述必要执行映射。固定 JSON Lines 的 start/ready/invoke/result/error/cancel 协议用于本地控制；EOF 用于关闭。普通无状态 App 的逐调用程序形式可后续支持，首个 Bash 不必同时完成全部执行模式。此候选替代此前两个短入口脚本再另连共享后端的推荐形态，不推翻 App/Action 组织与职责共识。
- 默认等待、近似输出预算、字符串 session_id、pipe 模式关闭 stdin、TTY 输入、运行与闲置期限、有界缓冲和缓存均为本轮建议，不视为用户已接受的固定值，也不声称是 Codex 内部实现。max_output_tokens 的近似计量与被显示截断内容不补读的语义明确写出。
- 补清 session_id 与 call_id、Action 与 handler、当前请求取消与已有执行停止的区别；去重受身份及 App 运行周期约束，必须核对载荷；处理中记录不能按已完成响应缓存淘汰；断连不自动重投，输出最终结果以确认退出为前提。
- OIS—OIC 的 WebSocket、心跳与超时仍是载体候选；不根据 Bash 参数反推通信选型已确认。Bash 路径和 Python 文件只用于示例，首个平台、语言与交付方式仍未冻结。
- README、候选总稿与完整组合文章同步入口、术语和 Bash 托管示意。只读复核重点检查参数与权限边界、关闭、输出、取消和去重，修订结果并入草稿。完成文档检查：45 段 JSON、10 段 YAML 可解析，18 个本地引用有效；详稿的 Action/handler/Tool/Host 及返回形状一致，git diff --check 通过。这是设计文档检查，不是功能实现验收。
- current 保持读取时内容，SHA-256 为 `802099289fc9243093e3f98ce5c4ec030e07ecbdb629286a0c5c0742e17e06f4`；既有未提交修改全部保留。本轮未创建业务实现、启动服务、提交或推送。

## 2026-11-09：采用同名入口简写，补充本地参数信息候选

- 用户认可 `actions: {exec_command: {}, write_stdin: {}}` 的简写，并追问为何此前示例没有放参数信息。已将同名入口省略 handler、默认使用 Action 名的规则同步到 current；空对象不表示没有参数。没有将该确认扩大为共享 worker、参数字段或整份 YAML 全部定案。
- 说明此前最小示例把参数绑定留在 Action 代码，遗漏了本地参数信息的具体表达。“不重复完整中心 Schema”并不禁止本地声明必要参数信息，既有共识本来就允许必要参数映射。
- 本轮新增候选：Action 下可选 input，以本地参数名为键，使用 from/as/required/default 表达来源、交付类型、必填和固定默认值；省略 from 默认同名，省略 input 原样交付。OIC 根据声明做通用轻量绑定，Bash 保留业务检查、动态默认和限额；不推断任意函数签名、不加入加载时的中心参数一致性门禁。
- 参数字段仍是助手建议，用户本轮尚未明确采纳具体写法。完整候选加入 Bash 详稿第 11 节，并同步候选总稿和组合文章；现有 CU 的 from/as 示例沿用同一方向。
- 绑定语义补清：仅缺少字段时采用固定默认，默认也走类型转换；from 改名消费源字段、从原始输入同时取值，歧义覆盖报错；未列出的字段继续交给 App；同一 call_id 保存原始请求与首次绑定结果，避免映射变动后重复执行。
- 同步前 current SHA-256 为 `802099289fc9243093e3f98ce5c4ec030e07ecbdb629286a0c5c0742e17e06f4`；同步后为 `1862048edbeb256086903e1c1c17a074b50eb7ce81eb469b3c434b0c1a3c62de`。仅继续设计及文档维护，保留已有改动，没有开始实现、提交或推送。

## 2026-11-09：明确本地 Action Schema 与 runtime 分层覆盖

- 用户指出此前只有 runtime.command 与 handler 的 YAML 偏离原意：YAML 应包含 Action 和参数 Schema 的基本信息；OIC 加载后，在收到 OIS 转发的数据时根据 YAML 识别并转交目标 oiapp 的实现。用户同时明确 runtime 可放根节点或 Action 内，根节点为默认，更具体的配置优先。
- 按最新纠正更新 current：本地参数 Schema 是 App 接入描述的一部分，OIC 根据声明进行必要适配和转交。“轻量”不再解释为不声明 Schema 或只提供可选的 from/as 映射。保留 OIS 维护消费侧 Tool 定义、两侧 Schema 无需强一致、load 差异至多提示、执行时宽松 str/int 转换的既有方向。
- 同名 handler 可省略继续成立，但不再用空 Action 对象作为完整 YAML 示例；省略 handler 与省略参数 Schema 是不同事项。用户本轮未要求新增 output_schema 或冻结完整 JSON Schema 支持范围。
- 具体候选采用 actions.<name>.description/input_schema，使用基本 type/properties/required/default 表达实际入口参数；需要改名时另设 input_mapping，按“本地目标名: 调用来源名”引用本次业务输入，不重复类型规则。OIC 先改名、补固定默认、做轻量转换，再转发到有效 runtime；App 处理业务语义和条件默认。
- runtime 的根默认与 Action 优先已确认；助手进一步建议只覆盖同名顶层字段，command 数组和对象字段整体替换，不拼接或隐式递归合并。这些合并细节仍为候选。Bash 的共享 worker 仍是具体执行方式建议，不上升为所有 App 的必选形态。
- 区分配置优先级与实例共享：本轮 worker 候选中，继承根 runtime 的两个 Bash Action 访问同一实例，非空 Action.runtime 覆盖使用独立实例；相同命令文本不自动意味着共享 session。此实例规则尚未由用户确认。
- 已同步候选总稿、Bash 详稿与组合文章，保留历史笔记以说明此前提议被替代。读取时 current 已有同名 handler 简写的独立补充，基线 SHA-256 为 `1862048edbeb256086903e1c1c17a074b50eb7ce81eb469b3c434b0c1a3c62de`；本轮同步后为 `803ad6f88b1663f9c2b0ebd777b2f16f5c226fabb0eace096a3884e002e5c308`。仅维护设计文档，保留原有修改，不开始实现、提交或推送。
- 完成一致性检查：45 段 JSON、12 段 YAML 可解析，17 个本地文档引用有效；三份候选稿的 Bash 参数集合、Schema、默认值和同名入口约定一致，git diff --check 通过。独立只读复核确认 Schema 适配与 runtime 配置/实例边界表述一致。这些属于文档检查，不代表运行机制已有实现。

## 2026-11-09：按最新 YAML 契约重新整理完整方案

- 用户要求重新生成当前 Action、参数和机制说明。本轮按最新的 Action/参数 Schema/runtime 共识连贯呈现完整流程，不沿用早期“本地不声明 Schema”的示例，也不把重新整理当作采纳此前全部候选。
- 在 Bash 详稿增加当前方案总览，将声明、加载、调用、业务执行、后续读写串起来；明确 OIS 消费定义与本地执行 Schema、runtime 优先级与实例共享的边界。参数默认值、共享 worker、具体协议和资源限额继续标为建议；不新增公开 Action 或选定语言、平台。
- 当前已确认共识未变，读取及保留的 SHA-256 为 `803ad6f88b1663f9c2b0ebd777b2f16f5c226fabb0eace096a3884e002e5c308`。仅整理已有设计和沟通记录，未实现、提交或推送。

## 2026-11-09：生成顶层与 Bash 生命周期交互图

- 用户明确要求使用 imagegen 绘制模块交互图，先看顶层，再按 Bash 的不同生命周期展开。使用内置 image_gen 生成五张图：顶层、加载登记、exec 启动、write_stdin 持续读写、结束/断连/回收。
- 图片与完整实际提示词保存于 [图集目录](diagrams/2026-11-09-v1/README.md)。按已有共识与候选详稿制图，共享 worker、等待参数和回收规则仍为候选，不因画成图片而确认为产品实现。
- 逐张检查中文、参数标识、调用与返回箭头及生命周期边界；定向修正顶层和持续读取的返回路径、yield_time_ms 名称、OIS/OIC 图例职责和异常图的重连条件，保留最终五张图片。生成原件未删除，未选用版本未复制进项目。
- 当前共识内容保持不变，SHA-256 为 `803ad6f88b1663f9c2b0ebd777b2f16f5c226fabb0eace096a3884e002e5c308`。本轮只生成设计图并补充索引，未实现、提交或推送，未修改 main、alpha 或实验 worktree。

## 2026-11-09：首版收缩为固定 Action 与通道的直接调用链

- 用户明确“这个版本简单点，不做多host路由”，指定链路为 Agent → vMCP → Tool → Action → 通道 → OIC → oiapp → 对应目标 cmd，中间不增加额外逻辑。已将本版固定调用链和多 Host 路由退出首版同步到 current，不把它解释成删除所有 Host 概念或平台只能存在一个 OIC。
- 每个 Tool 预先绑定一个 App.Action 和通道，Agent 不再发现目标、提交 host_id 或通道 ID；删除首版 list_targets、allowed_hosts、同 Tool 多目标合并、自动切换和负载均衡要求。Bash 两个 Tool 指向同一通道及执行后端，后续只需 session_id 和业务读写参数。
- 保留用户已确认的 YAML Action/参数 Schema、runtime 根默认与 Action 优先，以及必要的轻量序列化/参数适配。OIS 依固定定义转交，OIC 按本地声明对接实现，业务进程与执行状态仍由 oiapp 管理，不引入新调度服务或业务编排层。
- 通道目前按 OIS 对接指定 OIC 的通信入口理解；source.channel、channel.id、bash-local 标识和扁平业务参数是本轮具体候选，不冻结协议或连接恢复实现。此前公开参数的 host_id/input 分层不再用于首版，内部 invoke 的 input 封装可以保留。
- 同步候选总稿、Bash 详稿及首版配置文章；首版验收由两 Host 合并改成一个固定通道的 Bash 完整闭环。CU 和多 Host 的旧讨论保留为后续背景。
- 用内置 image_gen 新绘[首版固定调用链图](diagrams/2026-11-09-v2/README.md)，旧五张图保留且明确标为历史；完整提示词随图保存。图中未增加 Host 池、目标发现或额外业务处理模块。
- 同步前 current SHA-256 为 `803ad6f88b1663f9c2b0ebd777b2f16f5c226fabb0eace096a3884e002e5c308`，同步后为 `fb2a8140437ccbcc331372331947056ddbc16eb4b3fd99398438f8954764beb4`。保留已有未提交修改，本轮只维护设计及图片，没有开始实现、提交或推送。

## 2026-11-09：展开首版 OIC CLI 候选

- 用户要求查看 OIC 的 CLI 命令。本轮在既有 start/load/apps/unload/stop 草案上补充 status，建议默认后台启动、--foreground 前台运行、全局 --config 指定本机实例配置，默认读取当前目录 oic.yaml。
- 建议 status 区分本机运行、OIS 连接和通道状态，apps 区分本地入口就绪与远端登记；load 操作已运行 OIC，加载本身不执行 cmd。管理命令不增加多 Host 选择，也不提供替代 Agent 的通用消费入口。
- unload 回收该 App 的现有执行、stop 回收全部托管执行是本轮建议，明确对应 session 失效；没有将其视为用户已采纳的生命周期规则。CLI 名称、进程形态、控制协议、加载成功口径和重启后的加载恢复仍待冻结。
- 只更新候选总稿和笔记；当前共识不变，没有启动 OIC、实现命令、提交或推送。

## 2026-11-09：展开 OIS 首版 API 候选

- 用户要求列出 OIS API 文档。本轮新增 [OIS API 草案](ois-api-design.md)，围绕运行状态、通道配置与只读能力登记、Tool 定义、vMCP 组合、HTTP 执行和 MCP 出口列出端点、字段、示例和错误约定。
- 建议 POST /api/v1/exec 接受 vmcp/tool/arguments，必须经过与 MCP 相同的消费权限和固定绑定；不恢复按 Host 选择、任意 Action 旁路或作业队列。call_id 关联本次请求，session_id 仍归 Bash App；OIC 接入传输未定，只列逻辑消息。
- CRUD 为候选而非已实现能力；首次 YAML 导入后由 OIS 持久配置/API 维护、PUT 完整替换、固定 source 不原位变更、被引用资源禁止删除、变更只影响新调用等均为本轮建议，尚未得到用户确认。不新增复杂账号管理、远程 App 部署或会话管理服务。
- 核对官方 MCP Tools 与 Transports 文档，仅据其说明 tools/list、tools/call 和 MCP 传输边界；不从管理 API 的 REST 形状推定 MCP wire 协议。MCP 具体版本和客户端兼容范围仍待选定。
- 当前共识读取基线 SHA-256 为 fb2a8140437ccbcc331372331947056ddbc16eb4b3fd99398438f8954764beb4，保持不变。本轮只维护候选文档与索引，不实现服务、执行真实 API 请求、提交或推送。

## 2026-11-09：补充 Host 与 Action 管理查询候选

- 用户追问 Action 和 Host 的 API。此前接口清单遗漏独立 Host 视图，将 Action 仅嵌入通道的 App 列表，现已在 OIS API 草案中补齐管理查询；不把“不做多 Host 路由”误解为“不保留宿主机和能力信息”。
- 建议提供 /hosts 列表、详情、其 App/Action 清单；提供 /actions 过滤查询及按通道/App/Action 定位的详情。本地 Action 描述和 Schema 由 OIC 加载登记，Host 由认证接入登记，首版建议先提供只读视图，不增设中心编辑本地 runtime/Schema 或通过删记录停用执行的接口。
- Host 稳定身份、授权绑定和重复身份处理仍未定；文档不将上报名称或系统信息视作硬件身份。Action 清单每项保留 channel/app/action，不合并同名来源；连接失效时标明快照陈旧及不可用，不从名字相同推断 session 恢复。
- 本轮只是针对用户问题提出补充方案，未将具体 API、登记形态或只读选择同步为已确认共识。保持固定 Tool 绑定，Agent 不增加 Host 参数；没有实现、提交或推送。

## 2026-11-09：首版目标与未决事项收敛审查

- 用户要求判断当前版本目标是否收敛，以及是否仍有未决和遗留事项。复核 current、候选总稿、API、Bash 机制和使用文章，并分别核对管理边界、运行生命周期与验收范围。
- 审查判断：产品目标和主链已基本收敛，没有发现必须推翻已确认架构的冲突；实现规格尚未冻结。不能把“有详细候选”当作“用户已经采纳”，也不能把所有默认值或后续方向都视为开工阻塞。
- 将候选总稿的关键未决清单整理成七组：首发环境/交付、配置权威/管理范围、Host/通道身份、运行通信契约、load/卸载/重启、Bash体验/请求结束、配置变更影响。已有推荐和仍缺闭合方案分别表述，当前已确认共识未改。
- 真正缺少完整行为的重点为 Host 首次登记绑定、同 App 多 runtime 初始化部分失败、REST /exec 的断开与超时。建议 load 本地整体成功再发布、登记另报状态、重复同定义无操作、变更先卸载、失败回收本次资源；这些是审查建议而非新共识。
- 明确提示当前候选中的体验取舍：非 TTY 不支持后续输入或单独强制停止；被截断内容不补读；十分钟无人调用可能触发闲置回收，与持续轮询完成十分钟命令不同。取消等待、终止进程和网络失联也需分开。
- 从本版关键未决项移出 CU、多 Host 路由与无独立 Host 接入；旧“目标查询”措辞改为管理侧登记查询，避免重新引入 Agent 选目标。提出一组最小完整 Done 供后续采纳。
- current SHA-256 读取基线及保留值均为 fb2a8140437ccbcc331372331947056ddbc16eb4b3fd99398438f8954764beb4。仅整理候选稿与笔记，不开始实现、归档讨论、提交或推送。

## 2026-11-10：采纳主方案并冻结 Linux151 / Windows 验收

- 用户指定 admin@121.199.175.151 用于 OIS 和 Linux OIC 部署，本机用于 Windows OIC 运行；151 的 25000–27000 端口可用。最终从 Windows 发起 MCP 调用，在同一个 server/vMCP 中使用两组命令能力，分别执行 Linux Bash 和 Windows PowerShell，并明确“其他的都按照你说的来”。
- 经工作范围澄清，用户选择“先冻结文档和验收规格”。因此本轮只更新文档及进行只读环境检查，没有开始实现、部署、启动服务或运行验收。规格冻结不自动结束或归档讨论，也不自动 commit/push。
- 将上述两组落实为 workbench vMCP 的四个固定 Tool：linux_exec_command/linux_write_stdin → linux-151 → bash；windows_exec_command/windows_write_stdin → windows-local → powershell。Windows 语法明确为 PowerShell 7，不声称 Bash 兼容；这不是恢复同 Tool 多 Host 选路。
- 采纳此前推荐的配置权威、管理/消费边界、共享 worker/JSON Lines、runtime 覆盖与实例规则、轻量参数适配、CLI、加载/卸载/重启、输出截断/限额及分阶段取消等主方案。Host/Action 只读登记，OIS 管理 Tool 定义；重复 load 不重复启动，变更先卸载，初始化部分失败回收本次资源，重启不恢复旧 session。
- 依据用户对剩余工程选择的委托，明确本版端口为 25100/TCP，OIS HTTPS/WSS，OIC 接入口 /api/v1/oic/connect；首个 MCP 验收客户端为 Windows PowerShell 7 驱动的协议脚本，固定 MCP 2025-11-25 Streamable HTTP。TLS 证书、客户端信任和公网连通留部署阶段实际设置验证，不在本轮执行。
- Host 与通道身份采用显式稳定配置：host.id 和 channel.id 对应 linux-151/windows-local，OIS 以 expected_host_id 与各自凭据核对；拒绝错误身份和重复活动连接。不引入机器硬件识别或自动身份迁移。
- 新增 [首版验收规格](../../../current/beta/acceptance.md)，规定 A01–A13，包括四工具发现、两端短执行、两端实际 600 秒无输出并持续读取、TTY 输入、跨组句柄拒绝、参数/runtime、输出与失败、断连/取消、加载/重启、进程清理、管理持久化及身份接入。声明所有功能均待实现与实际测试。
- 只读检查：SSH 使用已有 host key 严格校验，admin 免交互连接成功；Linux x86_64 上存在 Bash/Python3，监听快照未见 25100 占用。Windows x64 build 26200、pwsh 7.6.9 可用。没有打开端口、修改证书信任、安装依赖、上传程序或启动服务。
- 同步 current、系统设计、API、命令机制、使用文章及 README；清除已采纳事项仍被列作未决的现行措辞，历史笔记保持原文。基线 commit 为 8230fba2d5bf02c9950ae8a6c181cf323575791c，current 同步前 SHA-256 为 fb2a8140437ccbcc331372331947056ddbc16eb4b3fd99398438f8954764beb4，同步后为 d15d02c9f8d964a9f8d346779aea77d7a894e8fb9f00f9cdb52c59269779a670。

## 2026-11-10 冻结阶段状态与后续工程事项（历史）

首版产品目标、模块职责、部署拓扑、主协议、管理边界及 Done 已冻结，不再将 2026-11-09 的七组建议列为待用户重新决定的产品问题。实现语言/库、存储介质、内部 IPC、最终二进制及依赖版本由工程阶段选择，须满足已冻结契约。

下一阶段需要实际实现与验证的事项：两平台终端适配和托管进程清理、TLS 与证书信任、25100 公网连通、MCP/REST/OIC 协议实现、加载持久化，以及 A01–A13 的测试证据。它们是尚未完成的工程工作，不是功能已实现的声明。

CU、无独立 Host App、多 Host 合并/路由、热更新、执行迁移和跨重启执行恢复属于后续范围。当前仍保留活跃设计目录；上述冻结阶段只处理文档，没有实施、提交或推送。

## 2026-11-10：授权完整实现与部署验收

- 用户明确要求“开始开发吧，直到完成本次目标为止”，工作范围从冻结文档转为实现、实际部署和 A01–A13 验收。仍仅操作 beta worktree；main、alpha 和旧实验保留。
- 工程选择为 Go 实现的 OIS、OIC 和独立终端 App；通用宿主通过 JSONL 协议运行 App。Linux 使用 PTY、私有 subreaper 监督进程；Windows 使用 ConPTY 和 Job Object。
- 公网 OIS 部署于 151 的 25100；Linux OIC 和本机 Windows OIC 主动 WSS 接入。专用 CA 带 IP SAN，客户端按本次 CA 验证，不绕过 TLS、不修改系统信任存储。部署配置、私钥和凭据保存在忽略提交的独立目录。
- 四个固定工具通过本机 PowerShell MCP 客户端实际调用。两端各 600 秒无输出命令均已通过原句柄续接完成，Linux 约 600.1 秒、Windows 约 600.4 秒；没有使用 SSH 业务执行替代 MCP。
- 实测确认输入原样传递：Linux read 使用 LF，Windows ConPTY 的 Read-Host 使用 CR 提交。此为终端平台语义，不新增 YAML 适配表达式。
- 实现步骤、目录和命令见[运行说明](../../../../implementation.md)。冻结阶段“未实施”的描述保留为历史记录。
- 最终 A01–A13 全部通过；两端重启、六组进程树回收、OIS 持久配置、身份与迟到消息均有实际证据。Windows 全量测试/vet、Linux 全量 race 通过。交付时仅两条主通道、两 App、四工具和一个 workbench，在线可用。详见[验收报告](../../../../acceptance-report.md)。未自动提交或推送。

## 2026-11-10：默认 HTTP/WS、无 Token 与可选访问配置

- 用户明确要求默认使用 HTTP、无 Token，HTTPS 和 Token 改为显式配置选项；新建 vMCP 时决定是否要求 HTTPS。用户同时明确授权将现有 151 的 OIS/Linux OIC 与本机 Windows OIC 切换到这个默认，不只是修改新部署示例。此要求替代前述所有部署强制 HTTPS/WSS 与独立 Token 的设计，原讨论和验收原文保留为历史。
- 已确认：admin.token_env、通道 credential_env、vMCP access.credential_env 各自为空/省略时关闭该范围的 Token 鉴权；显式填写引用后必须正确提供 Token，引用变量缺失或为空不能降级匿名。各处独立控制，Token 与 HTTPS 也互相独立。
- 已确认：匿名消费者在同一 vMCP 内共享 anonymous:<vmcp_id> 身份，不用任意 Bearer 内容制造新的身份。共享匿名范围或同一消费凭据均不隔离为不同用户。业务输入仍不能覆盖可信 context；Host/channel 匹配、重复连接拒绝、固定 Tool 绑定与原实例句柄约束保留。A13 的错误凭据拒绝改为启用对应 Token 时的验收要求。
- 已确认：server.https 增加共享 HTTPS/WSS 监听，管理员提供 CA 签发证书，OIS 不自动申请、签发或续期；多个 vMCP 复用监听和证书。旧 tls_cert/tls_key 保留 HTTPS-only 兼容，不能与新块混用。显式部署测试工具可生成/复用验收 CA，这不表示 OIS 默认自动签发证书。
- 已确认：vMCP access.require_https 默认 false；true 必须已有 HTTPS 入口，endpoint 使用相应 HTTPS 地址。MCP 与 REST 都检查真实 TLS，HTTP 返回 403 HTTPS_REQUIRED，不信任 Forwarded/X-Forwarded-Proto；该设置不自动启用 Token。API 配置持久化规则保持，重启不能恢复旧 YAML 的访问策略，也不能静默降级仍要求 HTTPS 的 vMCP。
- 同步 current/design、current/acceptance、系统总稿、API、命令执行详稿和使用文章，并链接[访问配置](../../../../access-configuration.md)。默认示例去掉 Token 和 TLS 前提；acceptance-management.ps1/channelprobe 明确保留为显式 HTTPS+Token 配置的回归探针，匿名默认管理使用集成测试和实际 API 检查，不宣称旧探针可直接用于新默认。
- 本次独立实际验证：HTTP 无 Token、HTTP 有 Token、HTTPS 无 Token、HTTPS 有 Token 四种部署全部通过；新建 vMCP 省略 access 后能匿名调用两平台工具，无 HTTPS 入口时 require_https=true 被 400 拒绝。已恢复 HTTP/WS 匿名主部署，两通道、两 App、四工具、一个 workbench。Windows 全量测试/vet 与 Linux 全量 race 通过。证据为 `.local/reports/access-modes-20261110-023050.jsonl`、`access-create-vmcp.json`、`access-final-state.json` 及两平台测试日志；这些是本次新记录，不改写此前 HTTPS+Token 的 A01–A13 历史证据。
- 同步基线 commit 为 8230fba2d5bf02c9950ae8a6c181cf323575791c；读取 current/design 的 SHA-256 为 f0e7b619f51910840f0acdc5082151fa4c2c52878b8de34a443b7b713ce6aee5，同步后为 c939a421fd0cc032a3a41c3d506e94d8984c17be3eb53f4eb756bf94a6b5dbca。本次文档整理不自动提交、推送或归档讨论。

## 2026-11-10 文档目录调整

- 按用户要求，将项目根目录的 `goal-shaping/` 整体迁至 `docs/goal-shaping/`，同步修正 README、实现说明和设计文档中的引用。讨论标识、状态和内部目录结构保持不变；前文采用根目录位置的记录保留为历史。

## 2026-11-10：规范整理与讨论归档

- 用户明确要求“那就用 spec-maintenance 整理一下规范，然后把这轮 goal-shaping 归档”，结束本轮 `first-flow`。结束原因：约定的首版使用流程已完成，转入成果沉淀；实现基线为 beta commit `79ea9ff5c3eb13a363718bcb054d9862065fc9fd`。
- 结果：首版固定调用链、两端命令 App、CLI/API、轻量参数适配与可选访问配置等已采纳内容已同步到 current；项目持续有效口径整理至 [docs/spec](../../../../spec/README.md)，覆盖概念、原则、架构、目标和技术参考。早期同 Tool 多 Host 路由等被替代方案不因归档重新采纳。
- 核对源码时发现旧详稿写成“load 初始化默认 10 秒未全部 ready 即失败”，实现实际上为多个 worker 依次初始化、每个分别等待 10 秒。用户明确选择“采用当前实现：每个 worker 10 秒（推荐）”；据此同步 current、验收规格、详稿和规范，不增加整次 load 总期限，不改执行代码。
- 首版完成依据保留 [A01–A13 验收报告](../../../../acceptance-report.md)和[后续访问模式验证](../../../../access-verification.md)。本次只核对源码、既有证据和文档，没有重新执行功能测试、部署或在线健康检查。
- 仍未解决的首版产品选择：无。后续 CU 桌面控制权、无独立 Host App、多目标路由、热更新、执行迁移与恢复等不在本次交付范围，完整机制仍待未来讨论。命令策略没有在本轮实现。
- 本次归档位置为 `docs/goal-shaping/archive/beta/2026-11-10-first-flow/`，包含本轮所有设计、笔记、图集与提示词。`current/beta` 保留已确认共识与完成标准；后续演进另起讨论，不继续改写此归档。
- 本次规范维护和归档不自动提交、推送或发布；main、alpha 和历史 worktree 保持原状。
