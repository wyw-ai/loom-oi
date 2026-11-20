# Browser 运行与验收（实施提案）

本轮确认 Filesystem 7 项与 Browser 18 项，Browser 含 17 项普通 JSON 和一项全视口或视口局部截图。网页上传下载及原文件传输暂缓。按 6 → 11 → 1 实施，依照[技术设计](browser-design.md)、[会话与事件](browser-lifecycle.md)和[平台打包验收](browser-packaging.md)检查实际交付。

Node.js + TypeScript worker，固定 Playwright 和配套 Chromium，正式启动 headless=true、channel=chromium、chromiumSandbox=true。每个平台单独打包 Node、worker、浏览器并说明 Linux 系统依赖。截图使用随包 `pngjs=7.0.0`、`jpeg-js=0.4.4` 和 Node 图像处理线程，结果走共享 `content.v1`。OIC ready 前不下载或启动浏览器。每会话独立临时 profile；持久 profile 只接受按可信身份授权的别名，独占打开。

18 项共用 app.yaml 根 runtime.command 并共享 worker 状态。所有字段与组合在 worker 校验。启动后同时读取 invoke 与 cancel；结果按 call_id 串行写 stdout。17 项沿用普通 JSON 结果，screenshot 返回 content.v1。当前宿主不接受主动 event 帧，所以事件常驻 worker 有界缓存，list_tabs 查询晚到的事件和 operation_id。所有句柄绑定 access_identity、vmcp_id 和 worker 周期；匿名同 vMCP 无法区分为多个用户。worker 重启后重新创建 session。

按 tab 串行修改，只运行一个在途修改；dialog、操作状态和关闭通道可独立调度。默认 10 秒，单次等待不超过 30 秒，为 OIC 固定 60 秒留余量。dialog 的内部保留期限不延长单次调用：原调用返回 pending，再通过 handle_dialog/list_tabs 接续。固定控制容量、记录保留及未知结果处理按生命周期稿执行，不能自动重放网页提交。

普通和持久 context 均显式关闭下载接收；文件选择器或被动下载只能产生不支持提示及必要清理，不提供可接续文件句柄、产物路径或上传字节。页面网络请求与缓存仍属正常浏览器行为，不把“没有文件接口”夸大成没有任何本地浏览器状态。

`screenshot(session_id, tab_id, clip?, timeout_ms?)` 从当前 CSS 视口捕获一帧 PNG。省略 `clip` 时先试原尺寸 PNG，超过 384 KiB 默认目标仍可在完整帧内交付。原尺寸 PNG 放不下才从同一帧编码原尺寸 JPEG Q80，两者都放不下才生成标记 `resized=true` 的有界概览。`clip` 是以视口左上角为原点的整像素矩形，须完全位于视口内。显式 `clip` 只交付原像素无损 PNG，放不下报 `IMAGE_PREVIEW_TOO_LARGE` 并提示更小区域。截图不滚动或重抓页面。回执包含实际 `source_rect`、完整视口、最终宽高、编码字节数、MIME 与相对源区域的 `resized`。worker 完整帧默认限 768 KiB，各跳继续检查 1 MiB；流程、超限及取消见[截图实现设计](browser-design.md)。

| 切片 | 实现 | 发布门槛 |
| --- | --- | --- |
| A：6 | create_session、close_session、navigate、snapshot、fill、click | 经 OIS 打开测试页、读到控件、填表、提交、读取成功结果。旧 ref 在同名元素替换后不得误点。 |
| B：11 | list_sessions、list_tabs、open_tab、close_tab、select_option、set_checked、press_key、hover、scroll、wait_for、screenshot | 两会话隔离，找回弹出页，HTTP/MCP 确认异步表单结果；两种视口的图文页面验证原尺寸 PNG、同帧原尺寸 JPEG Q80、局部无损和低预算概览，真实视觉消费者逐字读出像素小字。 |
| C：1 | handle_dialog | 同步 confirm 的源调用及时返回 pending；控制动作可执行；查询原 operation 得到真实终态。取消、过期和断连可区分。 |

专用 profile 的联合验收从空目录开始：按授权别名建会话 → 在本地测试站点完成普通登录表单 → 验证服务端登录态 → 关闭 → 同别名重新创建并验证仍登录；另一主体不能使用该别名。登录到期应呈现重新登录状态。依赖 CAPTCHA、硬件设备或人工可视操作的流程不在本版支持范围；不能用个人 profile 拷贝或 cookie 导入掩盖首登缺口。

文件交互边界单独验收：直接文件控件、脚本触发 chooser、立即/延迟下载均没有上传或成品交付；可明确归因的操作返回 UNSUPPORTED_INTERACTION，不能归因的异步事件不改判旧操作。随后仍能正常观察、操作或关闭页面，不留下等待文件输入的公开资源。另验证 upload_files/downloads 不可调用，旧文件配置以及 screenshot 的 target/full_page 输入被拒绝。`clip` 越界、空矩形、小数均应报 `INVALID_ARGUMENT`。

共享 content.v1 兼容性单独验收：新 OIC+旧 OIS 仅该连接的 screenshot 不可用，另外 17 项仍可调用，本地 worker 保持 Ready；旧 OIC+新 manifest 加载失败。支持图像的新链路分别核对 MCP 原生 image 和 REST 内联 PNG/JPEG；用只存在于像素中的中文短句与批号检查实际消费效果。

发布前分别验证干净 Windows 11 x64、Ubuntu 24.04 x64 主机的安装、OIC load、OIS 调用和关闭回收；两个平台均通过才满足本轮双平台 Done。真实 HTTP 客户端与支持图像的 MCP 消费者完成页面闭环和截图验收，不要求文件 SDK 或安装其他 oiapp。历史 Windows smoke 使用系统 Chrome，仅证明基础可行性，不能替代独立发行包、18 项契约或 OI 链路验收。
