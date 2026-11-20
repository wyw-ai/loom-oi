# Browser oiapp · 18 个 Action 的接口设计

本轮确定 Browser 提供 18 项 Action。其中 17 项使用普通 JSON，`screenshot` 使用共享图像结果，支持当前视口和局部细节。网页文件上传、下载管理与跨机文件传输暂缓。下列参数、回执与错误用于实现和验收。[主设计](design.md)

推荐独立 Node.js worker，以固定版本 Playwright 驱动随发行包管理的 Chromium。首版只创建临时隔离会话或使用提供方配置的专用、独占 profile；不附着个人浏览器、任意 CDP endpoint，不接受可执行文件路径、任意启动参数或个人 user-data-dir。具体打包、进程拓扑、网络策略和平台能力由运行时设计规定，不由业务入参改变。

## 1. 公共输入、身份与预算

- worker 严格校验经过 OIC 适配的对象；输入拒绝未知字段和显式 null，可选值用省略表达。原始 UTF-8、重复键和孤立代理项必须由各自第一解码点检查，不能指望 worker 还原上游已丢失的信息。默认值由 worker 按分支应用。JSON Schema 的 maxLength 按码点；另有明定的 UTF-8 字节预算和 1 MiB 帧限制，运行时也要执行。
- `session_id`、`tab_id`、`document_id`、`frame_id`、`snapshot_id`、`ref`、`dialog_id`、`operation_id` 都是 worker 生成的不透明标识，不是浏览器底层编号，不由调用方分配。worker 重启后不复用旧标识。
- 资源绑定实际 App 实例及可信 `access_identity`、`vmcp_id`；这些归属不接受业务参数提供。不存在、跨身份或跨会话的标识统一返回 `RESOURCE_NOT_FOUND`，避免泄露其他调用方的资源。已授权且仍保留的关闭记录可报告 `SESSION_CLOSED`、`TAB_CLOSED`。
- profile 仅能使用提供方配置的别名，且别名需授权给当前可信身份；知道别名不等于获准使用。同一持久 profile 同时只允许一个会话占用。
- 下文标为 `Page` 的输入必含 `session_id`、`tab_id`；标为 `Session` 的输入必含 `session_id`。未另列的参数不支持。
- 一般动作 `timeout_ms` 省略为 10,000，范围 1..30,000；create_session 省略为 30,000。它是本次调用等待预算，不承诺网页服务或原生弹窗在该时限内结束。实际有效值受提供方更低上限限制。可接续 dialog 的底层操作期限独立于该调用预算，见第 3 节和生命周期设计。
- 17 项成功结果为普通 JSON；screenshot 返回含 Base64 图像的 `content.v1`，经过格式协商后交付原生 image block。默认完整 worker 返回帧不超过 768 KiB，保留链路封装余量；各 hop 仍检查自己的最终帧。文本和事件按完整元素裁减；不得从 JSON、UTF-8 字符或 Base64 中间截断。
- URI 只允许运行时支持的 HTTP/HTTPS 和内部空白页。`javascript:`、`data:`、`file:`、浏览器内部配置页及任意协议调用不通过 navigate/open_tab 暴露。内网与 localhost 可达性由提供方网络策略决定，不能用一次 URL 字符串检查冒充完整重定向/子资源网络隔离。

本候选不新增服务端 batch、JS evaluate、Python 或任意宿主代码执行。Agent 可在自己的执行环境中顺序组合多个 Action；每项都有独立回执，整组不是事务，失败后不得自动重放先前成功项。

## 2. 统一目标 target

首版只提供下列三个变体。没有 CSS、XPath、testId、任意 JS、模糊匹配或坐标目标。

| kind | 必填字段 | 可选字段 | 解析规则 |
| --- | --- | --- | --- |
| `ref` | `snapshot_id`、`ref` | 无 | 解析该快照记录的具体元素身份及 frame；不重新用文字或 selector 猜一个类似节点。 |
| `role` | `document_id`、`role`、`name` | `exact=true`、`frame_id` | 以当前文档的可访问性 role/name 解析；role 必须来自所锁定后端支持的 ARIA role 集合，name 非空且最多 512 字节。 |
| `label` | `document_id`、`text` | `exact=true`、`frame_id` | 按关联 label/可访问标签寻找控件；text 非空且最多 512 字节。 |

三个变体均显式含 `kind`。role/label 的文本匹配沿用固定版本 Playwright 的空白归一化和 exact 语义；不支持正则。省略 frame_id 表示主 frame；子 frame_id 来自 snapshot/list_tabs，绑定该 frame 的文档世代，导航、替换或脱离后失效，不能用整数 frame index 猜测。

目标动作要求在明确作用域内恰好匹配一个元素；0 个返回 `TARGET_NOT_FOUND`，多个返回 `AMBIGUOUS_TARGET`。重试期间始终遵守此要求，不使用 first/nth 默认消歧。role/label 可在同一文档内重新解析，适合重复表单；ref 只指原先那个节点。新建相似节点不能继承旧 ref。

文档导航返回 `STALE_DOCUMENT`，引用或 frame 失效返回 `STALE_TARGET`；Agent 重新观察后再形成动作。局部 DOM 改变不必使所有 ref 一律失效，但必须确认原节点仍连接到原文档。snapshot 的生命周期有界，过期返回 `SNAPSHOT_EXPIRED`。

除 wait_for 的存在性条件外，执行前检查目标类型、唯一性及该动作所需可操作性。首版不暴露 `force` 绕过可见性/遮挡检查，不在失败后自动切换成坐标或脚本点击。

## 3. 统一动作回执、异步事件与恢复

### 3.1 PageReceipt

改变页面、焦点或资源的动作返回下列公共信息及各自结果；列表、快照等只读动作使用相应专用结果。

```json
{
  "session_id": "session_opaque",
  "tab_id": "tab_opaque",
  "document_id": "document_opaque",
  "operation_id": "operation_opaque",
  "status": "completed",
  "page": {"url": "https://example.test/form", "title": "表单"},
  "events": {"cursor": 18, "items": [], "has_more": false, "truncated": false},
  "result": {}
}
```

- `completed`：该浏览器动作已经完成；不代表订单、提交等网站业务成功。
- `unchanged`：已明确观察目标处于所请求状态，未再发送相应变更。例如复选框本来已选中。
- `pending`：已开始的操作因已识别的 dialog 等支持接续的条件等待，返回 `operation_id`、`blocked_by` 和 `deadline_at`，不伪装完成或要求重新 click。不支持的文件交互不产生 pending。

`operation_id` 对应的记录状态为 `running/blocked/completed/failed/unknown`。pending 原调用结束返回后，该记录仍由 worker 维护；用 `list_tabs(session_id, operation_id)` 查询最终回执或错误。只允许在来源会话内查询。终态记录至少保留至会话关闭或配置保留期限；若已淘汰，明确 `OPERATION_RESULT_EXPIRED`，不得用当前页面猜测旧调用的最终状态。

弹窗处理、list_tabs、关闭资源走控制路径，不能被等待该弹窗的页面操作锁阻塞。普通后续变更不能越过尚未结束的同 tab 操作；返回 `TAB_BUSY`。后台操作到期但无法确认停止时记 unknown，tab 标为 `interrupted`；仍允许观察、控制和关闭，不再接受普通变更，不能让可能迟到的旧输入与新输入交错。

返回 pending 后继续保留原后端操作，不能让它沿用本次调用的 10 秒 timeout 而先行失败，也不能在 handle_dialog 后重新发 click/navigate。后端必须具有覆盖 `deadline_at` 的独立期限；worker 在该期限前持续观察原操作结果。底层已经超时或失败则记录实际终态，不承诺后续处理弹窗可以恢复已终止的调用。

超时/断连不能推导零副作用。顶层错误 details 包含已有 operation_id、`effect_state`（`not_applied/partially_applied/applied/unknown`）和已知关联资源。逐字输入中途失败可以是 partially_applied。没有证据时是 unknown；调用方先观察，不自动换会话重跑。

### 3.2 事件与待处理资源

worker 从会话建立时持续监听 popup/tab、navigation、dialog；监听先于任何触发动作。每条公开事件有单调递增 `sequence`、类型、tab_id、对应资源 ID；操作关联仅在能确认时提供，不声称同一时间窗口里的事件都由某次点击造成。

回执 `events` 默认最多 20 项。`cursor` 表示本次已交付位置，`has_more` 表示仍有后续缓存项；`truncated=true` 表示请求位置之前的事件被淘汰。`list_tabs(after_sequence=cursor)` 可继续读有界事件，无须新增 events Action。

即使事件已淘汰，list_tabs 也必须返回当前待处理 dialog，以及 running/blocked operation 的 ID；不能只依赖一次触发回执发现它们。

取消一次请求只是请求停止等待/执行，不自动撤销浏览器已接受的操作。关闭会话与关闭 tab 分别有明确语义，不能相互替代。

## 4. 会话与标签页

### 4.1 create_session

**输入：** `profile?`（配置别名，省略为临时 profile）、`timeout_ms?`。不接受 URL、浏览器进程参数或磁盘目录。

**输出：** `session_id`、`state=ready`、`profile=null|alias`、`persistent`、初始 `tab_id`/`document_id`、运行时实际 viewport 与可用能力摘要。初始页为 about:blank；导航由 open_tab/navigate 完成。`capabilities` 描述当前 worker 后端具备的能力。能否调用 screenshot 还取决于该 vMCP 的固定绑定及 OIC/OIS 连接协商；后台支持 `screenshots` 不能作为当前消费者一定能收到图像的依据。

profile 授权、占用和资源额度先于启动检查。初始化失败不返回 ready；如果已经分配会话资源，错误 details 给出可恢复的 session_id 和 `state=failed|starting|unknown`，由 list_sessions 找回，不能无限留下无归属进程。

**主要错误：** `PROFILE_NOT_ALLOWED`、`PROFILE_BUSY`、`CAPACITY_EXCEEDED`、`BROWSER_START_FAILED`、`TIMEOUT`。临时会话与持久 profile 不自动相互回退；有登录态的 profile 不可用时不能悄悄换成空白浏览器。

### 4.2 list_sessions

**输入：** 无。

**输出：** 当前可信身份可见的 `sessions[]`，每项含 session_id、state（starting/ready/interrupted/closing/failed）、profile、persistent、创建时间、存活 tab 数和有界故障说明。会话数由运行时硬限制，返回完整现存列表，不列其他身份资源或 profile 磁盘路径。

该接口找回现存句柄，不恢复 worker 重启前的浏览器控制关系。历史已关闭会话不混入正常列表。

### 4.3 close_session

**输入：** Session、`timeout_ms?`。

**输出：** session_id、`status=closed|already_closed`、被关闭的 tab ID，以及清理 warnings。

关闭该会话拥有的资源。持久 profile 保留，临时 profile 按运行时策略清理。关闭不回滚网站变化，也不关闭其他会话。超时返回已知关闭状态；状态未知时可通过 list_sessions 观察，不虚报 closed。

### 4.4 list_tabs

**输入：** Session、`operation_id?`、`after_sequence?`（非负整数）。

**输出：** `tabs[]`、`events`，以及请求 operation_id 时的 `operation`。每个 tab 含 tab_id、document_id、url/title、state（ready/busy/interrupted/closed）、frame 摘要、当前 `dialogs[]`、`pending_operation_ids[]`。列表包含页面自行打开的 tab；稳定 ID 不因选中顺序改变。

dialog 摘要含 dialog_id、type、message、允许的 responses、prompt 默认值。文本按预算裁剪并标记；不通过执行可能被 dialog 阻塞的页面 JS 才能取得这些状态。

operation 查询返回 running/blocked 的已知状态，或终态的原始有界回执/错误。after_sequence 只控制增量事件，不影响当前待处理状态表；省略时不重放全部历史。等待中的操作无须占住此查询调用。预算不足时优先保留待处理资源 ID 和原操作终态；可省略的 URL、标题或 frame 摘要须显式标记，不能让过长的页面标题堵住弹窗恢复。operation 在未请求时为 null。

### 4.5 open_tab

**输入：** Session、`url?`（省略 about:blank）、`wait_until=domcontentloaded`（commit/domcontentloaded/load）、`timeout_ms?`。

**输出：** 新 tab 的 PageReceipt，result 含 requested_url、actual_url、HTTP status（若有）、导航阶段。

新 tab 先登记并安装事件监听，再进行导航。导航失败时保留已创建 tab，并在错误 details 返回 tab_id；不因失败再创建第二个 tab。初始空白页不执行网络导航。

### 4.6 close_tab

**输入：** Page、`timeout_ms?`。

**输出：** session_id、tab_id、`status=closed|already_closed` 和 warnings。

关闭明确指定的 tab，不把“最后一个 tab”解释为关闭整个 session；之后可再 open_tab。首版按资源关闭执行，不运行网页 beforeunload 脚本；文档须明确这会放弃该 tab 尚未提交的表单。用户希望继续网页离开确认流程时使用 navigate，再 handle_dialog。

## 5. 导航、观察与等待

### 5.1 navigate

**输入：** Page、`mode=goto`（goto/back/forward/reload）、`url?`、`wait_until=domcontentloaded`（commit/domcontentloaded/load）、`timeout_ms?`。

goto 必须有 url；其他 mode 禁止 url。返回 PageReceipt，result 含请求模式、实际 URL、HTTP status 或 null、已达到的导航阶段；HTTP 4xx/5xx 页面本身可完成导航，返回真实 status，不伪装成网络连接失败。

明确的 reload 才表达重新加载意图；调用方发现已经在目标 URL 时应直接观察，不机械重复 goto。back/forward 无历史可退进时返回 unchanged、reason=no_history。同文档锚点/History API 导航可以改变 URL 而不更换 document_id；完整文档替换必须换 document_id 并使旧 ref 失效。

beforeunload/dialog 出现时按 pending 流程返回可接续状态。导航已提交但后续 load 等待超时，错误必须报告 committed=true 及当前文档；不能说页面未变化。

### 5.2 snapshot

**输入：** Page、`scope?`（ref/role/label，省略主文档）、`max_chars=12000`（1..32000）、`max_nodes=400`（1..1000）、`timeout_ms?`。

**输出：** session_id、tab_id、document_id、snapshot_id、page、`text`、`elements[]`、`frames[]`、`truncated`、`truncation_reasons[]` 和当前待处理资源摘要。元素含 ref、role、name、必要状态（disabled/checked/selected/expanded/editable 等）；frame 列表给稳定的文档归属，不要求 Agent 猜 iframe 序号。

同时保留正文、链接及控件语义，不能只列按钮。只提取当前页面可见内容及后端可访问的 frame；无法访问的 frame 记录 omission，不暗示已读其内容。密码控件的值不返回；不读取 cookie、localStorage、隐藏脚本状态或浏览器 profile 文件。不是 HTML 全量导出工具。

达到任一文字、节点、frame、时间或完整帧预算就明确标记不完整；不返回 total_count 假装知道全文规模。首版不输出 diff，不提供跨页面快照分页。可通过 scope 缩小范围。保留下来的 ref 必须有对应可验证元素记录，不能截半个条目。

### 5.3 screenshot

**输入**为 Page（`session_id`、`tab_id`），可选 `clip` 和 `timeout_ms`。省略 `clip` 请求当前视口概览。显式传入 `{"x":48,"y":96,"width":360,"height":72}` 表示逐像素观察指定区域。四项均为整数，原点是当前可见视口的左上角，单位是 CSS 像素。x、y 非负，宽高为正，右边和下边为开边界。整个矩形都须在当前视口内，超出即返回 `INVALID_ARGUMENT`。使用已有 `scroll` 后，应按新的视口坐标取图。

worker 按 CSS 像素取当前视口的一帧，使页面保持原滚动位置和尺寸。clip 从这帧取像素，避免把文档坐标误当视口坐标。已有未结算页面变更时返回 `TAB_BUSY`，弹窗独立控制槽仍可使用。捕获期间与新页面变更短暂互斥，开始和结束的 document_id 不同时丢弃图像并返回 `STALE_DOCUMENT`。

**输出**为 `content.v1`，`content` 中有一个简短 text 和一个原生 image。image 的 `mimeType` 为实际的 `image/png` 或 `image/jpeg`，`data` 是完整标准 Base64，无 data: 前缀。图像字节只在该块中出现一次。`structuredContent` 包含 `session_id`、`tab_id`、`document_id`、`viewport`、`source_rect`、`preview_width`、`preview_height`、`mime_type`、`encoded_bytes` 和 `resized`。`viewport` 始终反映完整视口。全屏的 `source_rect` 从 (0,0) 覆盖整个视口，局部图的 `source_rect` 等于调用者传入的矩形。`resized` 比较预览与这个取样区域的尺寸，所以原尺寸 JPEG 和原像素裁剪均为 false。

384 KiB 是编码后图像的默认传输目标。Base64 占用 `4 × ceil(encoded_bytes / 3)` 字节，完整 JSONL 帧另含调用 ID、格式、文本、元数据和换行。实例帧预算默认且最高为 768 KiB，各跳还须检查实际封装。即使在软目标内也须验证完整帧。超过软目标的原尺寸 PNG，只要完整帧仍装得下，就直接返回。

| 请求 | 编码与预算处理 |
| --- | --- |
| 整个当前视口 | 先试原尺寸 PNG。完整帧放不下时，从同一帧的像素生成原尺寸 JPEG Q80。两者均放不下，再从原始像素按比例逐档缩小概览，每档先试 PNG 再试 JPEG Q80，回执标 `resized=true`。长边下限为 min(原长边,768 px)，到达下限仍无法交付则返回 `IMAGE_PREVIEW_TOO_LARGE`，提示请求较小的 clip。 |
| 显式 clip | 返回该矩形的原尺寸无损 PNG。若完整帧放不下，返回 `IMAGE_PREVIEW_TOO_LARGE` 并提示缩小 clip。即使请求的矩形恰好覆盖整屏，仍执行这个分支。 |

### 5.4 wait_for

**输入：** Page、`condition`（下表之一）、`timeout_ms?`。

| condition.kind | 字段 | 语义 |
| --- | --- | --- |
| `element` | target、state=visible（attached/detached/visible/hidden） | 按状态等待；非唯一语义目标不默认挑一个，返回 AMBIGUOUS_TARGET。 |
| `text` | text、state=present（present/absent）、scope? | 在可见文本中按归一化连续空白后的字面包含关系判断；text 非空、最多 2048 字节。 |
| `url` | url、match=exact（exact/prefix） | 比较当前实际 URL；不是导航命令或 URL 访问权限判断。 |
| `load` | state=domcontentloaded（domcontentloaded/load） | 等待当前文档达到状态；不以 networkidle 默认代替网站业务完成。 |

**输出：** session/tab/document、`status=matched|timed_out|blocked`、elapsed_ms、满足时的有界 observation，以及 events/待处理资源。timed_out 是预期等待结果，不作为顶层执行错误；参数、目标世代、断连等异常仍返回顶层错误。blocked 给出 dialog/operation 归属，先处理后可重新等待。

element 的 detached/hidden 条件允许目标已经消失并立即满足，但前提是明确原文档/原 ref 身份；不能把跨文档的 stale ref 当作条件成立。role/label 的 attached/visible 可从 0 个候选开始等待；若后来出现多个候选仍报歧义。仅 presence 等待不会持有阻止 handle_dialog 的变更锁。

## 6. 页面交互

### 6.1 click

**输入：** Page、target、`click_count=1`（1/2）、`button=left`（left/middle/right）、`modifiers=[]`（Alt/Control/Meta/Shift 的无重复数组）、`timeout_ms?`。

**输出：** PageReceipt；result 含 click_count/button 和已知触发的导航、新 tab、dialog ID。按浏览器输入点击，不调用 element.click 的页面脚本替代真实输入。可操作性检查失败时不自动 force；右键不因此承诺能继续操作浏览器原生菜单。

点击成功只证明输入动作完成。若触发原生 dialog 并阻塞原调用，返回 pending；若网络请求仍进行但点击本身完成，返回 completed，业务结果由 wait_for/snapshot 确认。

### 6.2 fill

**输入：** Page、target、`text`（允许空，最多 16 KiB）、`mode=replace`（replace/type）、`timeout_ms?`。

replace 清空并替换支持的 input/textarea/contenteditable 内容；type 聚焦后逐字发送，不清空现有内容。需要“清空后逐字输入”的调用方先 replace 空字符串，再 type；可在客户端编排，但不是服务端事务。首版不允许自定义每字符延迟。

**输出：** PageReceipt；result 含 mode、请求字符数和已观察到的执行状态，不回显输入内容或密码。没有隐式 Enter、blur、提交；若网站要求失焦触发验证，调用方明确 press_key(Tab) 或点击下一控件。逐字输入中途失败报告 partially_applied/unknown，不能盲目重试以免重复内容。

### 6.3 select_option

**输入：** Page、target、`values`（字符串数组，最多 32 项，合计最多 8 KiB）、`timeout_ms?`。

只操作原生 select；每个值是 option.value，不猜显示文字与 value 的对应关系。单选恰好一个值；多选允许空数组表示清空，并明确是状态变更。先校验控件类型、多选能力、选项存在且无歧义，再执行一次设置。

**输出：** PageReceipt，result 含实际 selected_values；已处于目标集合时 unchanged。自定义菜单返回 `UNSUPPORTED_TARGET`，由 snapshot+click 操作，不冒充原生 select。

### 6.4 set_checked

**输入：** Page、target、`checked`（boolean）、`timeout_ms?`。

首版支持后端可验证状态的原生 checkbox/radio；自定义 ARIA 控件不自动以盲目 click 模拟。radio 不接受 checked=false；checkbox 按目标布尔状态设置。执行前后检查状态；本来一致则 unchanged，不发 toggle。

**输出：** PageReceipt，result 含 actual_checked；操作已发生但目标状态无法确认时报告 `POSTCONDITION_FAILED` 及 effect_state，不声称可安全重复。set_checked 不是表单提交。

### 6.5 press_key

**输入：** Page、`key`（最多 128 字节）、`target?`、`document_id`（省略 target 时必填）、`timeout_ms?`。

key 使用已固定后端支持的按键/组合键语法，例如 Enter、Escape、Tab、ArrowDown、Control+A、Meta+A、Shift+Tab；不接受未知键或脚本。有 target 时先聚焦该控件；无 target 时作用于该文档当前焦点，不作用于桌面全局键盘。与 target 同时给 document_id 时必须一致。

**输出：** PageReceipt，result 含 key 和目标焦点信息（可用时）。组合键可能触发站点行为或导航；返回状态不能把按键发送成功等同于网站操作成功。不通过该入口承诺浏览器窗口快捷键、系统菜单或跨应用剪贴板能力。

### 6.6 hover

**输入：** Page、target、`timeout_ms?`。

**输出：** PageReceipt。移动浏览器内指针到唯一、可见、可命中的元素，触发正常 hover；不自动点击。菜单/提示是否出现由 wait_for/snapshot 确认。没有坐标输入，不承诺桌面 hover。

### 6.7 scroll

**输入：** Page、`mode=by`（by/into_view）、`target?`、`delta_x=0`、`delta_y=0`、`timeout_ms?`。

by 可省略 target 表示主页面，否则指向实际可滚动容器；delta 单位 CSS 像素，各在 -2000..2000，至少一个非 0。into_view 必须有 target 且禁止 delta 字段，只把该元素滚入可视范围，不发点击。

**输出：** PageReceipt，result 给出实际可观测的滚动前后位置及 changed；到达边界、已可见时可以 unchanged。懒加载是否完成单独观察，不把一次滚动返回当作所有新内容已加载。不可滚动控件返回 `UNSUPPORTED_TARGET`，不改滚整个页面。

## 7. dialog 与不支持的文件交互

### 7.1 handle_dialog

**输入：** Page、dialog_id、`action=accept|dismiss`、`prompt_text?`、`timeout_ms?`。

dialog_id 必须是本 tab 当前待处理实例。prompt_text 仅允许 prompt+accept，省略表示空字符串；其他组合拒绝。候选 Node/Playwright 后端可分别处理 alert/confirm/prompt/beforeunload；运行时在 dialog 摘要中报告实际允许的 responses，不能根据别的后端文档假定全部支持。

**输出：** PageReceipt，result 含 dialog_id、type、action、handled；不声称触发该 dialog 的原 operation 也已完成。调用方可用 list_tabs(operation_id) 获取原操作结算结果。重复处理不作用于后来的新 dialog；已经处理或消失的 ID 返回 `DIALOG_NOT_PENDING`。

提供方可设有界 dialog 保留期限；自动处置必须有明确政策、事件和原因，不能静默 auto_accept。首版默认等待显式响应；超时采用 dismiss 并记录 `DIALOG_EXPIRED`，blocked operation 据实结算。

### 7.2 文件交互的首版边界

首版不提供文件选择、上传或下载管理 Action，不接受本地路径，也不建立文件资源票据、状态表或公开事件。直接操作已识别的 input[type=file] 在发送输入前返回 `UNSUPPORTED_TARGET`。不得通过 fill/press_key 把路径输入伪装为上传。

普通按钮、脚本或导航仍可能间接触发文件交互。会话建立时禁用下载接收；内部监听只用于抑制文件选择、拒绝/取消下载与清理资源，不等待后续文件 Action，不返回文件造成的 pending，也不调用 setFiles([]) 清空已有选择。无法确认停止时按实际副作用和中断规则处理，不让资源无限等待。

原输入已完成则仍报告输入动作 completed；它不表示文件选择、上传或下载成功。若不支持的交互使原操作失败，返回 `UNSUPPORTED_INTERACTION`，message 说明原因，details 保留已分配的 operation_id 和实际 effect_state。无法关联到原操作的迟到事件不改写已交付终态。调用方通过 snapshot/wait_for 观察网页反馈，不自动再次 click 或重发导航。

## 8. 错误、输出完整性与实现验收

| 错误类别 | 代表错误 | 调用方处理 |
| --- | --- | --- |
| 参数 | INVALID_ARGUMENT、UNSUPPORTED_URL、UNSUPPORTED_KEY | 修正请求；未开始动作。 |
| 归属/生命周期 | RESOURCE_NOT_FOUND、SESSION_CLOSED、TAB_CLOSED、BROWSER_DISCONNECTED | 找回真实资源或明确重建；不自动切换到别的会话。 |
| 目标 | STALE_DOCUMENT、STALE_TARGET、SNAPSHOT_EXPIRED、TARGET_NOT_FOUND、AMBIGUOUS_TARGET、UNSUPPORTED_TARGET | 重新观察或选择正确控件，不猜 ref。 |
| 可操作性 | TARGET_NOT_ACTIONABLE、TAB_BUSY、POSTCONDITION_FAILED | 检查遮挡、禁用、待处理操作和实际页面结果。 |
| 异步接续 | DIALOG_NOT_PENDING、OPERATION_RESULT_EXPIRED | 通过 list_tabs 当前状态恢复，不能重复旧副作用。 |
| 资源/配置 | PROFILE_NOT_ALLOWED、PROFILE_BUSY、CAPACITY_EXCEEDED、RESULT_TOO_LARGE、IMAGE_PREVIEW_TOO_LARGE | 缩小范围、处理现存资源或由提供方调整配置。 |
| 执行 | TIMEOUT、CANCELLED、NAVIGATION_FAILED、SCREENSHOT_FAILED、UNSUPPORTED_INTERACTION、INTERNAL_ERROR | 同时检查 effect_state、operation_id 及可恢复资源；失败不等于没发生。 |

错误走既有 worker error 帧，不将失败嵌进成功结果的 ok:false。wait_for 的 timed_out 是正常查询结果。写网页的结果未知时，先观察再决定，不因 HTTP/MCP 调用超时自动换 call_id 重放。

必要验收至少覆盖：

1. 同身份重用 session/tab；跨身份 ID/profile 别名拒绝；worker/browser 重启后旧句柄不复活。
2. role/label 多匹配、ref 原节点被替换、主 frame/iframe 导航、SPA 同文档更新，均不误操作相似节点。
3. click 导致 dialog，源调用及时返回 pending；超过原 10 秒调用预算再 handle_dialog 仍接续同一个后端操作；list_tabs(operation_id) 能取到原调用终态。迟到 dialog 能从 list_tabs 发现；底层已失败时不重发原动作。
4. 直接文件控件在输入前拒绝；按钮间接或延迟触发文件选择/下载不会留下票据、等待死锁或发布文件，不清空已有选择，不重放点击。
5. fill replace/type 的差异、逐字输入部分失败、select 多选与清空、set_checked 重复调用不 toggle、scroll 边界不误滚其他容器。
6. 用真实 MCP 图像消费者验收 screenshot，解码核对 PNG/JPEG 的 MIME、尺寸和字节数，读取只写进像素的中文短句、批号和数字。分别覆盖超 384 KiB 仍可原尺寸交付的 PNG、原 PNG 放不下而原尺寸 JPEG Q80 能交付、两者均超硬限时缩小的概览。检验滚动后和高 DPR 下的 clip、非零原点、恰好贴边、越界以及局部 PNG 装不下的错误。导航换页时丢弃旧帧。事件淘汰有 truncated，操作记录淘汰有明确错误。

按 6→11→1 分段验收：第一段 create_session/close_session/navigate/snapshot/fill/click；第二段 list_sessions/list_tabs/open_tab/close_tab/select_option/set_checked/press_key/hover/scroll/wait_for/screenshot；最后 handle_dialog。完整确认框样例覆盖 click pending、handle_dialog、原 operation 查询，不能把它改成第二次 click。

## 9. 参考与解释范围

主要参考本机官方 bundled Browser `26.1111.21308` 的静态资料：[持久句柄与观察指引](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/api-use-behavior.md)、[AX 与组合调用指引](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/accessibility.md)、[受控 API](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/api.json)、[有界事件 cursor](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/capabilities/tab/cdp.md)。这些是资料来源，不是本 App 运行时依赖；安装版本变化后需重新核验。

18 个 Action 与 operation 恢复记录是 OI 的接口。[机器 Schema](browser.schemas.json) 定义可静态校验的形状；[详细设计](browser-design.md) 和[生命周期](browser-lifecycle.md) 定义归属、时序和预算等必须在运行时检查的约束。示意 PageReceipt 中的 result 代表动作专有数据，不是所有 Action 都接受空对象；完整实例如[确认框的请求与回执](browser-flow.example.json)。
