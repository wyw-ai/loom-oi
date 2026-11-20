# Browser：生命周期、调度与恢复（候选技术设计）

本文与 [Action 契约](browser-actions.md)、[Schema](browser.schemas.json)、[运行与验收](browser-runtime.md)共同使用。字段和公开状态以 Schema 为准；以下状态转换、固定控制容量和资源处理是待实现方案，不代表已经实测。本稿候选范围为 18 项，不新增事件推送、通用代码执行或自动重放接口。

## 1. 资源与可信归属

一个 App 根 runtime 对应一个常驻 Node/TypeScript worker，18 项共享会话管理器。一个 session 拥有独立 Chromium 进程/context、profile、tab、观察记录和操作记录。所有句柄绑定 `access_identity`、`vmcp_id`、App 实例及 worker 运行周期；浏览器内部编号不作为外部永久 ID。匿名同 vMCP、相同认证范围的多个客户端共享现有身份语义，不能宣称按客户端隔离。

创建前先验证配额和 profile 的 `allowed_callers`，再以实际目录取得独占锁。默认使用临时 profile；持久 profile 只能使用配置别名，授权必须同时匹配可信身份与 vMCP。锁随进程存活，不能仅按超时删除正在使用的锁；不同别名若解析到同一实际目录仍互斥。`network_policy=environment` 仅说明沿用部署环境网络，不承诺过滤网络目标。

所有列表和错误都先做归属检查，不能靠猜 ID 读到另一范围的状态或 profile 路径。旧 ID 不复用；查不到归属的资源返回统一不可用结果。关闭幂等性依赖绑定原归属的有限墓碑，不能用任意不存在 ID 返回 already_closed。

## 2. 会话与标签页状态

| 资源状态 | 进入与退出条件 |
| --- | --- |
| session `starting` | 创建额度/profile 锁已登记，浏览器尚未完成启动；此时可由 list_sessions 找回并关闭 |
| session `ready` | 浏览器、初始 about:blank 页面和监听器准备完成；只有此时 create_session 返回成功 |
| session `interrupted` | 浏览器断连或活动状态无法可靠控制；允许查询和清理，不自动附着另一个浏览器继续 |
| session `closing` | 已开始关闭拥有的资源；拒绝新普通动作，关闭与查询仍可用 |
| session `failed` | 启动或清理确定失败，保留有界 fault；待回收的进程/锁仍计入额度 |
| tab `ready/busy/interrupted/closed` | 分别表示可接收普通变更、原操作尚未结算、无法排除迟到输入、已经关闭 |

会话关闭成功后从活动列表移除；`closed/already_closed` 是 close_session 的回执状态，不新增到 session_record 枚举。启动异常的 error.details 可报告未知结果；列表使用 `interrupted`/fault 表达失控，不扩展公开会话枚举。创建尚未交付成功时超时或取消，worker 继续有界清理；若暂不能确认回收，保留可查询的 session_id，不留下无归属进程。

close_session/close_tab 关闭指定资源，无文件传输相关的关闭前置条件。关闭 tab 不关闭整个 session，即使它是最后一个 tab；之后仍可 open_tab。关闭按资源清理执行，不触发 beforeunload 脚本；需要网页离开确认时走 navigate/handle_dialog。持久 profile 不因关闭偷偷删除。

## 3. 普通执行与独立控制通道

JSONL 输入循环持续处理 invoke/cancel，登记 call_id 后异步执行；输出用单一串行通道，避免帧交错。不能用浏览器等待阻塞协议读取。worker 不向 OIC 输出主动 event 帧，事件只保存在本地并随结果/查询交付。17 项成功结果为普通 JSON；screenshot 通过协商的 `content.v1` 交付，不能以普通 Base64 文本冒充原生 image。

普通调用受 `max_inflight_calls` 限制（默认 16，范围 1..64）；普通操作记录受 `max_operation_records_per_session` 限制（默认 128，范围 1..1024）。同 tab 只允许一个尚未结算的普通变更；其他普通变更返回 TAB_BUSY，不排进无界队列。wait_for/观察不持有等待 dialog 关闭才能释放的排他锁，短时状态读取与浏览器异步等待分开。

**固定控制预留不占上述普通配额：** worker 额外保留 4 个控制执行槽，其中 2 个用于短查询/关闭/取消调度，2 个用于 dialog 接续。每个 tab 另有 4 个控制操作回执槽，保存 handle_dialog 产生的 PageReceipt，独立于普通操作记录。以上是候选内部固定边界，不新增配置字段；控制执行槽等待有界，不能变成无限请求队列。close_session/close_tab 与列表不创建 PageReceipt 操作记录。

控制路径包括 list_sessions/list_tabs、关闭资源、handle_dialog、内部 cancel。普通页面变更不能借此绕过 TAB_BUSY。close/cancel 的底层等待不长期占用短查询槽，先登记状态再异步收敛；额外控制槽仍受 OIC 全宿主 256 在途上限约束，不能保证宿主已被无关调用占满时仍可送达。

running/blocked 记录、已向调用方交付 pending 后等待恢复的原记录，以及其关联尚未结算的控制记录不能为新控制动作腾空间而淘汰。控制回执也返回独立 operation_id，并能通过 list_tabs(operation_id) 查原回执；它不替换原 click/navigate 的记录。普通和控制终态至少按 `operation_retention_ms`（默认 600000）或会话关闭保留；容量满且没有可到期回收的记录时拒绝新的相应副作用，始终保留查询和关闭路径。有限容量不意味着可以无限接续不断产生的新 dialog。

最小配额验收必须设置两个普通上限都为 1：click 占一个普通调用/记录，出现 dialog 后原调用返回 pending；handle_dialog 使用控制执行槽和控制回执槽；list_tabs 查询原 operation 得到终态，原记录不丢失。handle_dialog 完成只说明该弹窗已处理，不能代替原操作结算。即使控制记录暂满，list_tabs 和 close_tab/session 仍可执行，不能被记录分配失败一并堵住。

## 4. 操作状态与请求期限

call_id 关联一次宿主请求；operation_id 关联已登记的页面操作，二者不能互换。PageReceipt 的 `completed/unchanged/pending` 与 operation_record 的状态分别使用：

| operation 状态 | 含义与转换 |
| --- | --- |
| `running` | 已接受并执行，可转 blocked/completed/failed/unknown |
| `blocked` | 已识别 dialog 等接续条件；返回 pending，必须含 blocked_by、deadline_at；处理后回 running 或直接结算 |
| `completed` | 浏览器动作确定完成，保存匹配原 action 的 completed/unchanged 回执；不等于网站业务成功 |
| `failed` | 已确认错误，保存原错误与已知 effect_state，不把失败等同零副作用 |
| `unknown` | 到期/断连后无法排除已执行或迟到输入；保存原未知结果，tab 进入 interrupted |

终态回执一次提交、不可由后续页面状态倒推改写。unknown 后即使收到迟到回调也不自动把原调用重放或解除 tab 隔离；允许观察、控制、关闭，由调用方明确关闭并重建需要继续变更的 tab。普通 DOM 更新并不把已完成动作改回 running。

一般 `timeout_ms` 省略为 10000，create_session 省略为 30000，公开范围为 1..30000；实际等待还受提供方较小上限限制。worker 应用 Action 默认，输入 Schema 不注入统一 default。OIC 固定 60 秒是外层请求期限，不能靠调大 OIS 超时延长；worker 单次等待必须先自行结算。

只有已识别的、受支持接续条件才返回 pending；耗时长或网站请求未结束不能随意当作 pending。返回 pending 后原宿主调用结束，后台 operation 不依赖其 call context。接续预算 `blocked_operation_timeout_ms` 默认 300000；deadline_at 不因查询或反复弹出 dialog 自动延长。

**调用等待与后端执行使用两套期限。** 派发可能触发 dialog 的页面变更前，登记原操作和监听器；把后端硬期限 H 固定为“派发时刻 + 本次有效 timeout_ms + blocked_operation_timeout_ms”。支持 timeout 参数的 Playwright 变更/导航调用显式使用这一完整上限，不继承通用 10 秒默认值；worker 仍在本次调用预算内返回。第一次出现支持接续的阻塞时，deadline_at 取 H、首次阻塞时刻加接续预算、该 dialog 的绝对到期时刻三者的最早值。处理 dialog 后继续等待同一个后端 promise，仍受该 deadline_at 约束，不重新派发输入。后续 dialog 可以缩短期限，不能延长它。

调用预算耗尽但没有已识别阻塞时，不制造 pending；按真实结果返回 TIMEOUT/unknown 并隔离可能仍有迟到输入的 tab。任何后端 promise 都由操作管理器持有至结算，不能用 Promise.race 返回后直接丢弃。到接续期限时先执行到期 dialog 的 dismiss 并收集已知结果；接续已到期或后端硬期限到达而原操作仍不能结算，worker 请求停止/关闭拥有的 tab。无法确认停止则原操作记 unknown、tab 记 interrupted，进行有界关闭与进程回收，不能继续收普通变更。不能依赖一个无期限的 timeout=0 调用维持恢复能力；不支持可控底层期限的路径必须由同一看门狗负责关闭。底层若提前失败，立即保存 failed/unknown；handle_dialog 可以处理仍存在的弹窗，但不恢复或重放已终止的原操作。

请求取消只请求停止本次执行/等待，不回滚网站变化。error.details 按证据报告 `effect_state=not_applied/partially_applied/applied/unknown`，并带已分配 operation_id 与资源 ID。wait_for 的 timed_out 是正常结果。宿主发出 cancel 并不等于已停止；取消发生在 pending 回执已提交之后，也不能借旧 call_id 擅自关闭整个 session。

## 5. 事件、dialog 与不支持的文件交互

会话/新 tab 在允许动作前安装 popup、navigation、dialog 监听；不支持的文件交互另由内部拒绝与清理策略处理，不产生文件资源公开事件。事件 sequence 在会话内单调递增；只在可确认时填写操作关联，不把邻近时间出现的全部事件归因于 click。事件环默认 512 项（max_events_per_session），单次 events.items 默认最多 20 项；cursor 代表已交付位置，has_more 表示后续缓存，truncated 表示请求位置前已有事件淘汰。

事件历史与当前资源状态分开维护。list_tabs(after_sequence) 继续事件；无论历史是否淘汰，都返回全部当前 dialog 和 running/blocked operation ID。operation_id 查询返回原结果，过期返回 OPERATION_RESULT_EXPIRED；不能通过重发 click 来“找回”。未请求 operation_id 时 operation=null。

dialog 默认等待显式响应，期限由 `dialog_timeout_ms`（默认 300000）控制；到期按契约 dismiss，记录 DIALOG_EXPIRED，再按实际结果结算阻塞操作，不能静默 accept。处理必须匹配原 dialog_id 和允许的 responses；旧 ID 不作用于新 dialog。

首版不维护文件选择票据或下载记录。直接命中文件输入控件时在输入前拒绝；普通页面动作间接触发文件选择时，内部监听抑制原生选择流程并释放后端对象，不设置文件、不调用 setFiles([])、不等待不存在的后续 Action。创建 context 时禁用下载接收，迟到的下载事件仅用于拒绝/取消和清理，不 saveAs 或发布文件。已确认与当前操作有关且造成失败时用 UNSUPPORTED_INTERACTION 与实际 effect_state 结算；已完成输入不改写成文件成功，无法关联的迟到事件不修改原操作终态。无法停止时隔离并关闭相应资源，不制造文件导致的 pending 或自动重放。

输出预算优先保留恢复标识、状态和允许响应，再分配描述文字。tab_record 的 url/title 预算不足置 null 并记录 omitted_fields；frames 可省略摘要并标记。dialog 文字裁减使用自己的 truncated 字段，不能删除 dialog/operation ID。接收新资源前保证恢复集合能放入有效帧预算；无法容纳时拒绝新动作或关闭新增资源并明确报错，不能把 session 变成不可查询状态。

## 6. 文档世代与快照

主页面提交新文档时分配新 document_id，frame 导航/替换/脱离时失效该 frame 世代；保留真实 DOM 的同文档 SPA 更新不会无条件失效所有引用。ref 绑定快照记录中的原节点及文档，不重新按相似文字查找替代节点。节点脱离或 frame 失效为 STALE_TARGET，主文档不符为 STALE_DOCUMENT；身份无法确认时拒绝动作。

snapshot 默认最多保留每 tab 8 份，TTL 为 600000（max_snapshots_per_tab/snapshot_ttl_ms）；过期或容量淘汰返回 SNAPSHOT_EXPIRED。快照是观察结果，不锁住网页；执行时必须重新确认原节点连接与可操作性。role/label 是明确文档/frame/范围内重新解析的唯一匹配条件，多匹配报 AMBIGUOUS_TARGET，与 ref 的身份语义不同。href 只交付完整值，预算不足为 null/省略，不能输出看起来可导航的截断 URL。

snapshot/screenshot 不使后台操作自动完成。同 tab 有未结算页面变更时，screenshot 返回 `TAB_BUSY` 并保留 dialog 的独立控制槽。捕获期间短暂互斥，锁内读取 document_id、视口，验证可选 `clip` 是完全位于可见视口的整数 CSS 像素矩形，捕获一帧 CSS 尺寸的内存 PNG，核对主文档世代与源尺寸，再释放锁。期间导航替换文档返回 `STALE_DOCUMENT`，尺寸失配返回 `SCREENSHOT_FAILED`。截图不滚动或改变 viewport。后续裁剪、转码与缩放都在同一帧上完成，动态页面不会被再次捕获。

384 KiB 为默认图像编码目标，能否交付取决于完整回执是否装入 `max_result_frame_bytes`（默认 786432 B）以及各跳帧限制。全视口优先原尺寸 PNG，再试同帧原尺寸 JPEG Q80，两者都不合限才尝试有界缩小概览。显式 `clip` 只返回指定矩形的原像素无损 PNG，放不下即 `IMAGE_PREVIEW_TOO_LARGE` 并提示缩小区域。完整视口和实际 `source_rect` 随回执返回，`resized` 相对源矩形判断。编码工作在线程槽中执行，过期、取消和退出须释放资源。没有后台截图任务、产物文件或可领取的 URL。

## 7. 关闭与回收

会话空闲期限默认 900000（session_idle_timeout_ms）；仅获准的消费者操作更新活动时间，网页自行刷新和后台活动不续期。到期进入受控关闭，终止仍活动的等待并留下原因；不能无限累积不可访问进程。显式关闭与闲置回收的原因分别记录。

EOF 时停止接收普通调用，清理会话和未完成资源、释放 profile 锁并结束响应。OIC 给 5 秒退出宽限后进行进程树回收；App 不能依赖浏览器自行退出。清理失败保留有界诊断，不虚报 closed。临时 profile 与后端意外产生的临时文件按实际资源所有权清理；持久 profile 保留，不清理其他会话或其他 App 的目录。

## 8. 断连、重连与必须验证的过程

OIS—OIC 暂时断连不自动卸载 worker，既有会话可继续；重连后重新登记能力，由 list_sessions/list_tabs 找回仍存活资源。HTTP 流断开不等于取消。外部重试产生新 call_id，不获得“恰好一次”保证；没有拿到 operation_id 时也不能承诺找回每次最终回执，先观察真实状态而不自动重放。

浏览器断连只影响其拥有会话并转 interrupted；worker/OIC 重启导致全部旧句柄失效。持久 profile 可供明确创建新 session 使用，但不恢复原 tab、operation 或快照身份。OIC 当前不自动重启故障 worker，同定义重复 load 也不是重启，应按既有 unload/load 或 OIC 恢复流程处理。

最低验收包括：两个普通容量设为 1 仍可处理 dialog 并查询原终态；控制回执饱和仍能查询/关闭；超长 title/URL 不阻塞恢复；迟到 dialog 与事件淘汰；在原 10 秒调用预算之后、接续期限之前处理弹窗，证明后端操作未先超时且只派发一次；底层提前失败、blocked 到期与迟到输入均不重放；同名元素替换与 iframe/SPA 导航；截图中导航不错配、图像线程被取消仍可控制、同帧转码和裁剪、原像素局部真实 MCP 小字识读；间接文件交互不挂起、不发布文件、不清空原选择；EOF 和 worker 崩溃回收；跨身份句柄/profile 拒绝。默认值与输入/输出正反例按 [browser.schemas.json](browser.schemas.json)检查，确认框恢复顺序见 [完整样例](browser-flow.example.json)，平台真实行为按 [browser-runtime.md](browser-runtime.md)的 6+11+1 顺序验证。

现有 beta 约束依据：[worker.go](../../../../../internal/oic/worker.go)的 ready/stdio/取消/关闭流程、[host.go](../../../../../internal/oic/host.go)的 256 在途与 60 秒调用期限、[server.go](../../../../../internal/ois/server.go)的 authorizeAccess，以及 [App runtime](../../../../spec/reference/app-runtime.md)。这些来源说明当前宿主边界，不表示本文 Browser 状态机已经实现。
