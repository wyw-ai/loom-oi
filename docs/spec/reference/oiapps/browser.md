# Browser oiapp

Browser 让 Agent 在受控端建立独立浏览器会话，用页面快照找到控件，操作网页，再观察页面或查询原操作的结果。已确认首版共 18 个 Action，17 个返回普通 JSON，`screenshot` 返回图片。本分支已在 Windows 11 x64 与 Ubuntu 24.04 x64 使用包内 Chromium 跑通 18 项和 OIS/OIC 端到端调用。[源码与构建](../../../../apps/browser/README.md) [官方 App 索引](README.md) · [版本目标与验收](../../goals/oiapp-expansion.md)

## App 的安装和配置

一个安装包提供一个 `app.code: browser`，18 项共用根 `runtime.command` 和一个常驻 Node.js / TypeScript worker。每个会话独占受管理的无头 Chromium 进程及浏览器上下文。源码规划在 `apps/browser/`，其中 `contract/` 保存契约和 Action 注册表，`src/protocol/` 负责 JSONL 收发，`src/runtime/` 管会话、事件和操作记录，`src/observations/` 管快照、元素身份与图像，`src/actions/` 实现各项操作。OIC 启动 worker、传递可信上下文与配置，OIS 按 `channel/app/action` 固定绑定提供给调用方的工具。[完整 Linux app.yaml](examples/browser/app.yaml) · [App 运行时协议](../app-runtime.md)

OIC 的 `app_config.browser.state_dir` 是唯一必填项，须为服务账户有权读写的本机专用绝对目录，由提供方预先创建。默认视口为 1280 × 720 CSS 像素，设备像素比为 1，`headless=true`，`network_policy=environment` 沿用部署环境的网络边界。持久登录使用提供方配置的 `profiles[].alias/path/allowed_callers`，名单同时匹配可信的 `access_identity` 和 `vmcp_id`。同一实际 profile 目录同时只允许一个会话，获准共用目录的主体共享网站登录态。省略别名即使用临时 profile。[配置结构与限额](browser.schemas.json)

## 18 个 Action

表中 `Page` 表示必填的 `session_id` 和 `tab_id`，问号表示可省略。除 `list_sessions` 和 `list_tabs` 外，均可传 `timeout_ms`，默认 10 秒，`create_session` 默认 30 秒，单次请求最长 30 秒，并受实例配置约束。每个 Action 在机器契约中各有一组 `$defs.<action>_input` 和 `$defs.<action>_output`。调用参数拒绝未知字段和显式 null，worker 补齐分支默认值，并校验跨字段关系、字节预算及实时页面状态。[输入输出 Schema](browser.schemas.json) · [逐项调用参数](examples/arguments.yaml)

| Action | 业务输入 | 实现及成功结果 |
| --- | --- | --- |
| `create_session` | `profile?` | 校验授权、配额及独占锁，启动浏览器，安装监听后返回 `ready`、初始 `about:blank` 页的 session/tab/document ID、视口及后端能力。 |
| `list_sessions` | `{}` | 列出当前身份仍可查询的会话及状态，可找回创建中或中断的会话。 |
| `close_session` | `session_id` | 关闭所属标签和进程，释放锁，清理临时目录，保留持久 profile。返回 `closed` 或 `already_closed`、关闭的 tab ID 和告警。 |
| `list_tabs` | `session_id, after_sequence?, operation_id?` | 返回标签、frame 摘要、待处理 dialog、在途操作和增量事件。指定操作时另给出其状态、原回执或错误。页面自行打开的标签也可找回。 |
| `open_tab` | `session_id, url?, wait_until?` | 先登记新页并监听事件，再导航，默认打开空白页。回执含新 tab、实际 URL、HTTP 状态与导航阶段。建页后导航失败时错误保留 tab ID。 |
| `close_tab` | `Page` | 直接关闭该标签，跳过 beforeunload，未提交表单可能丢失。返回关闭状态与告警。关掉最后一个标签后仍可用同一会话开新页。 |
| `navigate` | `Page, mode?, url?, wait_until?` | 支持默认 `goto`、`back`、`forward`、`reload`，仅 `goto` 接收且要求 URL。返回实际 URL、HTTP 状态、阶段及是否已提交。无可前进或回退的历史则返回 `unchanged`。 |
| `snapshot` | `Page, scope?, max_chars?, max_nodes?` | 读取可见正文、链接、控件角色与状态、可访问的 frame。默认最多 12,000 字符、400 节点。返回 `snapshot_id`、带 `ref` 的元素、裁减原因和待处理摘要。密码值不进入快照。 |
| `screenshot` | `Page, clip?` | 从当前可见视口捕获一帧，返回 `content.v1` 图像和区域、尺寸、编码元数据。整屏与显式局部遵循下文不同的保真规则。 |
| `wait_for` | `Page, condition` | 等待元素状态、可见文字出现或消失、精确或前缀 URL、文档加载阶段。返回 `matched`、`timed_out` 或 `blocked`，含耗时、观察及待处理资源。 |
| `click` | `Page, target, click_count?, button?, modifiers?` | 对唯一可操作元素派发单击或双击。回执记录已知的新标签、弹窗及导航提交，业务结果可以再用快照或条件等待确认。 |
| `fill` | `Page, target, text, mode?` | 默认 `replace` 替换字段，`type` 在焦点处逐字输入。回执给出模式、请求字符数和输入完成状态。空字符串可清空字段，不回显输入或隐式提交。 |
| `select_option` | `Page, target, values[]` | 按原生 select 的 option.value 设置已选项。单选要求一个值，多选允许空数组以清空。回执返回实际 `selected_values`。 |
| `set_checked` | `Page, target, checked` | 设置并复核原生 checkbox/radio。radio 只接受 true。已满足时返回 `unchanged`，回执含 `actual_checked`。 |
| `press_key` | `Page, key, target?, document_id?` | 有目标时先聚焦，无目标须提供文档 ID，按键发送到该文档当前焦点。回执含按键与可观察的焦点信息。 |
| `hover` | `Page, target` | 将页内指针移到唯一、可见、可命中的目标，返回 `hovered`。后续菜单或提示可用 `wait_for` 观察。 |
| `scroll` | `Page, mode?, target?, delta_x?, delta_y?` | 默认 `by` 按 CSS 像素滚动页面或指定容器，每轴 −2000 至 2000，至少一轴非零。`into_view` 要求目标并禁止位移参数。回执含前后位置与 `changed`。 |
| `handle_dialog` | `Page, dialog_id, action, prompt_text?` | 在独立控制通道上按该弹窗允许的响应执行 `accept` 或 `dismiss`。仅接受 prompt 时可带文本。回执确认此弹窗已处理，原动作的终态通过其 operation ID 查询。 |

导航只开放 HTTP、HTTPS 与 `about:blank`。`wait_until` 可选 `commit`、`domcontentloaded`（默认）或 `load`。网页返回 HTTP 4xx/5xx 也可能构成已提交导航，回执报告实际状态码。网页文件选择、上传、下载管理与跨端文件传输均在本版范围之外。worker 在输入前拒绝直接操作文件控件，对间接触发的 chooser/download 执行抑制或清理，不设置文件、不交付文件路径。首版也不提供任意 JS、CDP、宿主命令、个人浏览器附着接口。

## 元素定位与会话恢复

**目标身份。** `target.kind=ref` 用 `snapshot_id + ref` 指向快照中的原节点。`role` 用 `document_id + role + name`，`label` 用 `document_id + text`。后两种允许指定 `frame_id` 和 `exact`，默认主 frame、精确匹配，要求唯一命中。页面完整换文档就更换 `document_id`，同一 DOM 的 SPA 更新沿用原世代。旧节点脱离或 frame 被替换时返回 `STALE_TARGET`，文档变化返回 `STALE_DOCUMENT`，快照淘汰返回 `SNAPSHOT_EXPIRED`。调用方须重新观察，不会误点与旧元素同名的新节点。

**归属与存活。** session、tab、frame、snapshot、operation、dialog 等标识都绑定当前 App 实例、worker 生命周期及可信身份组合。跨主体查询或猜测 ID 统一返回 `RESOURCE_NOT_FOUND`。同一匿名 vMCP 上的调用共享现有身份语义。传输短暂断开后可用 `list_sessions`、`list_tabs` 找回仍存活的资源。worker 重启后旧句柄失效，获授权的持久 profile 可显式建立新会话。默认空闲会话 15 分钟后回收。

**弹窗接续。** 同一标签同时至多执行一个尚未结算的普通变更。以网页 confirm 为例，`click` 返回 `status=pending`、`operation_id`、`blocked_by` 和 `deadline_at`。调用方用 `handle_dialog` 处理该实例，再用 `list_tabs(session_id, operation_id)` 查询原点击的终态。原后端操作独立于本次请求的 10 秒等待预算。默认 dialog 5 分钟到期后执行 dismiss 并记录 `DIALOG_EXPIRED`，再按实际效果结算。保留独立的查询、接续和关闭容量，普通变更遇到未结算操作返回 `TAB_BUSY`。

**事件与不确定结果。** worker 从建页前开始监听 popup、导航和 dialog，按会话维护单调递增的 `sequence`。默认缓存 512 条，每次最多交付 20 条，用 `after_sequence=cursor` 继续，`has_more` 和 `truncated` 区分尚有后续和历史已淘汰。当前弹窗和在途操作 ID 独立于历史保留。默认保留终态记录 10 分钟，会话关闭即结束访问，已淘汰的操作查询返回 `OPERATION_RESULT_EXPIRED`。超时、取消或断连可能发生在输入产生效果之后，错误附已知 `operation_id` 和 `effect_state`。不能确认底层输入已停止时将 tab 标为 `interrupted`，可观察、查询与关闭。调用方先核对页面和原操作，再决定后续输入，避免重复提交。

## 截图的清晰度与返回

`clip={x,y,width,height}` 以当前可见视口左上角为原点，四项均为整数 CSS 像素，坐标非负、宽高为正，整个矩形须位于视口内。滚动后使用新视口坐标。worker 按 `scale: css` 捕获一次视口 PNG，保持页面位置，裁剪、转码与缩放均用同一帧。若捕获期间更换文档，丢弃该图并返回 `STALE_DOCUMENT`。已有未结算变更时返回 `TAB_BUSY`。

- **整屏。** 先试原尺寸 PNG。384 KiB 是编码图像的默认目标，超过目标仍可交付装得进完整帧的原 PNG。装不下才试同帧原尺寸 JPEG Q80，两者均超限才逐档等比缩小概览，每档依次试 PNG 与 JPEG Q80，长边下限为 `min(原长边, 768 px)`。降至下限仍超出预算时提示改用局部。
- **显式局部。** 只返回所选矩形的原像素无损 PNG。即使矩形覆盖整个视口，也遵循同一规则。完整帧放不下时返回 `IMAGE_PREVIEW_TOO_LARGE`，提示缩小矩形。
- **消费。** `content` 恰有一个简短 text 块和一个原生 image 块，其 `data` 为完整标准 Base64，`mimeType` 为实际 PNG/JPEG 类型。`structuredContent` 含 session/tab/document ID、`viewport`、`source_rect`、预览宽高、MIME、`encoded_bytes`、`resized`。后者相对取样矩形计算，裁剪或原尺寸 JPEG 均为 false。MCP 交付原生 image，REST 响应内联，图像只在内存周转。worker 对含 Base64、元数据、协议封装和换行的完整帧执行默认且最高 768 KiB 限额，各跳另验 1 MiB 上限。[共享结果格式](../app-runtime.md) · [截图返回 Schema](browser.schemas.json)

## 打包与待验收项

Ubuntu 24.04 的部署需允许包内 Chromium 在非 root 账户下启动自身的沙箱。开启 AppArmor 用户命名空间限制时，管理员可按受保护安装目录中实际 Chromium 可执行文件的路径配置授权。[Ubuntu 24.04 用户命名空间说明](https://documentation.ubuntu.com/release-notes/24.04/#unprivileged-user-namespace-restrictions)

目标发行包为 Windows 11 x64 普通用户的 `browser-windows-x64.zip` 和 Ubuntu 24.04 x64 非 root 的 `browser-linux-x64.tar.gz`。包内携带 Node、编译后的 worker 与生产依赖、匹配的 Chromium、版本锁、校验和及许可证。两端入口分别为 `[./runtime/node.exe, ./worker/main.cjs]` 与 `[./runtime/node, ./worker/main.cjs]`。部署提供本地状态目录、系统库和可用的 Chromium sandbox。加载时只初始化 worker，创建会话才启动浏览器，无需临时联网下载浏览器。

本次 Windows 和 Ubuntu 构建均使用 Node.js 24.13.0、Playwright 1.62.1、Chromium revision 1234（151.0.7922.34）、pngjs 7.0.0 和 jpeg-js 0.4.4。正式版本以制成包的版本锁、校验和与双平台回归为准。两端已使用配套 Chromium，通过 OIS 与 OIC 交付截图和弹窗回执，正常卸载和 worker 异常退出后的进程回收也已分别验证。[分平台记录与归档校验值](../../../reports/oiapp-expansion/verification.md)

两个目标平台均须通过以下验收，并记录运行版本与包 SHA-256。[完整版本验收条件](../../goals/oiapp-expansion.md)

1. 干净主机安装后通过 OIS → OIC → Browser 执行表单闭环。覆盖 role/label 歧义、旧 ref、iframe 导航、表单状态和页面自行打开的标签，确认文件交互不生成可领取资源。
2. 专用 profile 从空目录完成登录，同别名关闭重建后复用登录态，其他身份无法访问。验证并发独占、闲置释放、EOF 和强制退出后的进程树、临时目录回收。
3. 普通并发与操作记录上限均设为 1，触发 confirm 后超过原调用的等待预算再处理，原操作只派发一次，调用方可查真实终态。覆盖事件淘汰、取消、超时、断连与恢复。
4. 用 1280 × 720 与 1920 × 1080 的本地页面覆盖原尺寸 PNG、JPEG Q80、低预算概览和局部原像素 PNG。用真实 MCP 图像消费者读出仅存在于像素中的 12/14 px 随机中文、批号和数字，REST 解码核对 MIME、尺寸和字节数。覆盖高 DPR、滚动、边界和捕获中导航。
5. 共享 `content.v1` 升级后，新 OIC 连旧 OIS 时仅该连接的 `screenshot` 不可用，17 项 JSON 和本地 worker 仍就绪。旧 OIC 加载声明新结果格式的 manifest 应显式报错。
