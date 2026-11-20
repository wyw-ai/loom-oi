# Browser oiapp 详细技术设计

本稿基于 beta `9f935d8`。本轮确认 Filesystem 7 项、Browser 18 项。Browser 采用 Node.js + TypeScript、Playwright 和专用无头 Chromium。17 项返回普通 JSON，`screenshot` 通过共享 `content.v1` 返回当前视口或指定局部的图片。方案按 6 → 11 → 1 交付。网页上传下载和原文件传输暂缓。本文是实现和验收依据，正式发行待完成。

| 实施时需要知道什么 | 入口 |
| --- | --- |
| 各 Action 输入、成功/待处理回执、错误、控件语义 | [18 个 Action 的逐项说明](browser-actions.md)、[可机器校验的 JSON Schema](browser.schemas.json)、[完整请求与结果样例](browser-flow.example.json) |
| 会话归属、动作调度、过期引用、事件保留与恢复 | [会话与事件](browser-lifecycle.md) |
| 平台包、版本来源、构建命令、验收矩阵及证据 | [Windows / Linux 打包验收](browser-packaging.md) |
| 既有接入基础、图像扩展与三阶段验收 | [当前 worker 协议](../../../../spec/reference/app-runtime.md)、[共享 content.v1 设计](filesystem-design.md)、[运行与验收](browser-runtime.md) |

## 1. 工程边界和目录

Browser 是独立 App，不扩展 OIC 为浏览器驱动。两平台共享全部业务接口和 TypeScript 源；平台差异位于发行包、系统依赖、进程托管和 profile 目录管理。消费者不传 os、CDP 地址、profile 路径或浏览器启动参数。部署者先安装包，再加载 App；加载不下载依赖。浏览器通过正文、控件快照和当前视口截图交付观察结果，不承担原文件传输。发行布局见打包附件，拟建源码目录如下：

~~~text
apps/browser/
  README.md
  package.json / package-lock.json / tsconfig.json
  contract/
    schemas.json                 # 本稿 browser.schemas.json 的实施副本
    actions.json                 # 18 个名称、说明、handler、结果格式和 Schema 引用
  src/
    main.ts                      # stdin/stdout 生命周期，启动与 EOF
    protocol/{frames,writer,dispatch,cancellation}.ts
    config/{parse,paths,ownership,budgets}.ts
    runtime/{launch,session,tab,frame,cleanup}.ts
    runtime/{scheduler,operations,events,resources}.ts
    observations/{snapshot,targets,handles,screenshot,image-worker}.ts
    actions/{sessions,tabs,navigation,forms,wait,dialogs}.ts
  scripts/{manifest,package}.mjs
  build.sh / build.ps1
  test/{contract,worker,browser,oi}/
  test/site/                     # 本地站点：登录、弹窗、表单、iframe、canvas、文件交互拒绝
  examples/                      # 分平台 OIC 配置、OIS tools/vMCP；不含真实凭据
  dist/                          # 构建输出，不进源码
~~~

dispatch 只按固定表找 handler，读取可信 context，做输入校验并取得会话。业务层不直接写 stdout；one writer 只输出完整的协议帧，stdout 不接浏览器日志或裸文件字节，截图只进入规定的 Base64 图像结果。每个已有 invoke 只结算一次；后台事件不会多发 result。诊断走 stderr，只记标识、数量、错误码与耗时，不记录网页正文、URL 查询串、表单值、cookie、图像字节或 Base64 内容。

## 2. 配置分别由谁维护

**App manifest。** 平台包生成一份 app.yaml。下例是 Linux 包的最小节选；Windows 只换 Node 入口后缀。完整注册由 contract/actions.json 生成，恰好 18 项，每个 Action 都继承根 runtime；不能为了分文件实现而逐项配置 runtime。17 项使用 json，screenshot 显式声明 result_format=content.v1，并按共享协议进行能力协商；下例只展示普通 JSON Action，不代表完整 manifest。

~~~yaml
version: 1
app: {code: browser}
runtime:
  command: [./runtime/node, ./worker/main.cjs]
actions:
  list_sessions:
    description: List browser sessions owned by this caller.
    input_schema: {type: object, properties: {}, additionalProperties: false}
~~~

**OIC 本地实例配置。** 最小运行只需专用 state_dir，即可用临时会话浏览。下面的 Linux 例子另外允许一个可信身份使用专用持久 profile；Windows 使用本机绝对路径，例如 `D:\oi\browser\state`。示例是加入现有 OIC 配置的片段。

~~~yaml
app_config:
  browser:
    state_dir: /srv/oi/browser/state
    headless: true
    profiles:
      - alias: reports
        path: /srv/oi/browser/profiles/reports
        allowed_callers:
          - {access_identity: 'consumer:reports:REPORTS_TOKEN', vmcp_id: reports}
~~~

`allowed_callers` 比较 OIS 注入的两个标识，不比较 Token 明文。上例要求名为 reports 的 vMCP 配置相应 credential_env；同一 vMCP/凭据范围内的客户端共享身份。含登录态的 profile 默认不授权匿名身份。多个允许主体共用一个 profile 即**共享该网站的登录态**，不能当作多租户隔离；优先每主体一个别名和目录。同目录不同别名必须识别为冲突。实例配置只在 OIC 启动时重新读取；换 profile 配置需重启 OIC，不能用 list_sessions 修改。

在 ready 前检查私有 state_dir、profile 根的所有权或 ACL，以及实际目录身份；绝对路径必须属于当前平台。这些目录包含浏览器运行状态或网站登录态，不作为供 Filesystem 或其他调用方访问的交换目录。仅支持本地受支持文件系统，目录不存在时明确报配置错误，不回退到 HOME；不从业务输入构造目录。

| 配置组 | 默认行为与必须校验的关系 |
| --- | --- |
| state_dir | 唯一必填项，由主机管理员准备专用目录并授权给运行账户；保存临时会话状态，不作为文件传输入口。 |
| viewport、headless、network_policy | 1280×720 CSS 像素，device_scale_factor=1；只支持 headless=true、environment 网络策略。网络边界来自部署环境，不由第一次 URL 检查决定。 |
| profiles | 默认空数组：临时会话可用，持久 profile 不可用。每个已配置别名关联专用目录和 allowed_callers；重复别名、归属不明或实际目录重叠有越权风险即拒绝配置。 |
| limits：会话与动作 | 默认 4 个会话、每会话 16 页、16 个普通在途调用。普通操作 10 秒，空闲会话 15 分钟。阻塞操作与 dialog 的 5 分钟期限是**内部状态寿命**，绝不让一次 OIC 调用等 5 分钟。 |
| limits：观察与恢复 | 默认每 tab 保留至多 8 份快照、10 分钟；会话事件环 512 项，单回执至多 20 项；终态操作记录容量默认 128、保留窗口 10 分钟。到达容量时按生命周期规则回收/拒绝，不能使正在接续的操作消失。 |
| limits：帧 | `limits.max_result_frame_bytes` 限完整 worker 帧，默认 768 KiB，各跳仍检查 1 MiB。截图的 384 KiB 是图像编码字节的默认目标；原尺寸 PNG 和 JPEG Q80 都按含 Base64、元数据、协议封装及 JSONL 换行的实际帧大小判定可交付性。 |

范围和可选字段见 Schema；JSON Schema 的 default 是注解，worker 另行填充默认值并检查跨字段关系。调用者可缩小范围或等待时间，不能扩大提供方限额。拒绝未知配置而非默默启用其他浏览器路径或策略。

**OIS 暴露能力。** App 加载成功不会自动创建模型工具。部署者在 tools 中固定来源，例如：

~~~yaml
tools:
  reports_browser_list:
    description: List this caller's sessions on the reports browser.
    source: {channel: reports-host, app: browser, action: list_sessions}
    input_schema: {type: object, properties: {}, additionalProperties: false}
vmcps:
  reports:
    tools: [reports_browser_list]
    access: {require_https: true, credential_env: REPORTS_TOKEN}
~~~

这是单工具绑定示例；实际按任务绑定所需的整组，弹窗恢复至少要可用 list_tabs 与 handle_dialog。未开放相应控制工具时，运行时仍须在期限内处置阻塞资源，不能让浏览器永久卡住。

## 3. 从 Schema 到可调用的接口

设计工件是 JSON Schema Draft 2020-12 的命名定义集合。分别选取 config、指定 Action 的 input/output 或业务 error 定义验证；验证整个集合根对象不等于验证请求。成功输出指 worker result 的 **result 值**；错误走 type:error，不另造 ok:false。17 项返回普通业务 JSON；screenshot 的 output 是 content.v1 内容，result_format 声明位于协议外层，不能把图像混进普通 JSON 再指望模型自行解码。

构建时从同一契约生成三份投影：① worker 的完整校验器和类型；② OIC 可接受的本地 input_schema，根必须为 object，有分支的 target 等字段保留基础 type，不让旧绑定器决定 oneOf；③ OIS Tool 的完整业务输入 Schema，把跨文件引用内联或随每个 schema 带齐本地定义，避免悬空引用。禁止在 app.yaml 伪造 output_schema。构建检查注册名无重复、每项恰有一个输入/成功输出，handler 与 ready 对应。

生成投影时尤其要保留“缺省”和“分支不允许”之间的区别。例如 navigate 的 goto 必须有 url，back/forward/reload 禁止 url；不能在 OIC 顶层向所有分支自动填充 url。变体特有默认值只在 worker 识别分支之后补，worker 对非法字段报错，不静默删除。输入与输出都选用兼容 Draft 2020-12 的校验器；输出进入 writer 前检查，再按 Action 声明的结果格式封装。

例如 `click` 的输入、成功/待处理输出分别引用 `#/$defs/click_input`、`#/$defs/click_output`；其余 17 个 Action 使用同样的命名。实例配置用 `#/$defs/config`，业务错误用 `#/$defs/error`。超时、会话所有权、是否同一 DOM 元素、实际图像字节与元数据是否一致不是 JSON 形状，列入 worker 和端到端测试。

OIC 仍按 beta 规则做类型适配；worker 严格验证**适配后的**对象，包括未知字段、枚举、互斥组合、整数安全范围和字符串 UTF-8 字节预算。原始 HTTP JSON 的重复键/非法 Unicode 若已被上游解码归一化，worker 无法还原；需要保证原始输入严格时在第一解码点校验，不能记作 worker 已保证。禁止将上游不可信对象直接展开进 Playwright launch options。

## 4. 观察与操作如何落到 Playwright

| 动作 | 实现与主要限制 |
| --- | --- |
| 创建/找回/关闭、开关标签页 | 临时会话由 worker 建立独立环境；指定 profile 时使用专用 user-data-dir 并独占。新页先登记和安装监听，再导航。列表读本地注册表，dialog 阻塞时不能通过页面 JS 取标题。 |
| navigate、wait_for | 区分 HTTP 响应状态和网络异常；URL、文档加载、文字/元素条件有不同判断器。等待使用独立 deadline，不长期占住修改锁，也不以 networkidle 代表业务完成。 |
| snapshot | 分 frame 读取有界可见正文、控件及状态。在同一文档观察中保存实际 ElementHandle 并赋 ref；快照文本的行号不能作为句柄。实现按访问节点数、字符、frame、总时间分别设预算，不能先把整份 HTML 传回再截断。不可观察 frame 或达到预算时显式标记。密码值不回显，页面文字永远只作数据。 |
| ref / role / label | ref 按先前保存的元素检查 document 和连接状态；role/label 使用固定版本 getByRole/getByLabel，在指定文档和 frame 唯一匹配。不使用 first() 消歧；最终派发仍需保持原文档身份，避免等待中跨导航点中同名控件。 |
| click、fill、select_option、set_checked、press_key、hover、scroll | 使用对应浏览器输入与可操作性检查。替换与逐字输入分开，复选/选项按目标状态设置。滚动只作用于目标容器或视口。独立返回操作回执，不把网站提交结果推断为成功。 |
| handle_dialog | 与[生命周期](browser-lifecycle.md)中的控制通道关联；同步 confirm 等阻塞原操作时，返回可查询的 operation_id，响应 dialog 后查询原操作的实际终态。 |
| screenshot | 记录当前文档与视口，捕获一次 CSS 像素 PNG。整屏依次尝试原尺寸 PNG、同帧原尺寸 JPEG Q80，必要时返回标明尺寸的概览。传 `clip` 则从同一帧提取指定区域并以原像素 PNG 交付。 |

快照实现建议：用固定、随包的只读浏览器端遍历代码，逐批提取可见文本节点；用固定的可访问名称/角色计算实现对标准控件求语义，区分不可解析与空名称。每个被引用的元素连同其元信息在同一轮捕获、由短寿命句柄数组保留；若期间发生导航，放弃该 frame 的结果。不要用整页 DOM 的第二次选择器查询按顺序配对。语义定位交给 Playwright 的严格模式；ref 被替换后不尝试语义回退。用嵌套 label、aria-labelledby、多个同名按钮、iframe 换文档及无障碍信息不全的 canvas 作为固定测试夹具。不承诺实现浏览器完整原生可访问性树。

## 5. 首次登录、能力边界与恢复

**专用 profile 的首次登录。** 提供方准备独立空目录和授权别名；消费者 create_session 指定该别名后，使用 navigate、snapshot、fill、click、wait_for 完成网站普通登录表单，并通过登录后的页面状态确认结果。登录所需的凭据或验证码由已授权调用方按其现有方式提供，Browser 不新增凭据保管接口，也不在工具描述、配置样例或日志中放入凭据。关闭后重新以同一别名建会话，验证站点允许保留的登录态仍有效；网站会话到期时正常重新登录，不承诺永久有效。

本版没有人工可视接管、个人浏览器附着、cookie 导入或 profile 迁移流程。若站点必须使用 CAPTCHA、硬件安全密钥、设备授权或其他当前语义操作无法完成的步骤，则明确停在该步骤，不宣称登录成功，也不把复制个人 profile 作为默认补救。首次登录验收使用本地测试站点的可操作表单及服务端会话状态；另有无法完成的登录夹具，验证能明确报告边界并关闭自己的会话。

**网页文件交互不支持。** 普通和持久 context 都显式设置 acceptDownloads=false。worker 不调用 setInputFiles、FileChooser.setFiles、Download.saveAs 或 createReadStream，不暴露上传根、下载目录、传输票据或文件字节。可事先识别或明确归因的文件输入操作返回 UNSUPPORTED_INTERACTION，并如实说明已经发生的输入效果；脚本或其他控件触发的 file chooser 只记录有界的不支持提示并释放句柄，不产生可继续上传的 chooser_id，不清空既有控件。被动 download 事件按后端能力取消并记录，不生成可获取的下载产物。无法归因的异步事件不把已经完成的旧操作改判失败，挂接与清理不新建公开资源；具体事件形状以 Action/生命周期契约为准。

这不承诺禁止网页普通网络请求、媒体加载或浏览器缓存；acceptDownloads=false 约束的是浏览器文件下载接收能力。验收包括直接文件控件、脚本触发选择器、立即及延迟下载、随后继续普通页面操作，并检查没有向调用方返回文件路径或原件。截图可补充纯图像或 canvas 的视觉观察，但不新增坐标点击或执行代码；页面可能看得见而无法通过现有语义目标操作，须如实报告。

**截图输入与捕获。** `screenshot` 接收 `session_id`、`tab_id`，可选 `clip` 和 `timeout_ms`。`clip={x,y,width,height}` 使用当前可见视口左上角为原点的整数 CSS 像素。x、y 为非负数，宽高为正数；在触发截图前验证 `width <= viewport.width - x`、`height <= viewport.height - y` 及原点范围，越界返回 `INVALID_ARGUMENT`。省略时 `source_rect` 为整个视口；显式给出矩形时，哪怕覆盖全屏，也遵守局部无损语义。坐标与当前文档滚动量无关。

取图只调用一次 `page.screenshot({type:"png", scale:"css", fullPage:false, omitBackground:false})`，返回内存 Buffer。截图期间不滚动，也不改设备倍率或视口；从捕获前后检查主文档世代以及视口尺寸。若文档更换就丢弃并报 `STALE_DOCUMENT`。像素尺寸与采样的视口不符则丢弃并报 `SCREENSHOT_FAILED`。本次观察绑定一次实际画面，页面可以在之后继续变化。

**选图顺序与预算。** 384 KiB 是默认编码字节目标。所有候选都先预算 `4 × ceil(encoded_bytes / 3)` 的 Base64 长度及完整回执，再按实际序列化的 UTF-8 帧核对。原 PNG 超过 384 KiB 仍须尝试实际帧，不能直接缩小。`max_result_frame_bytes` 默认 768 KiB（786432 B），各跳物理硬限 1 MiB。若调用预算调低，按实际生效的完整帧限额执行，小于 384 KiB 的候选也可能装不下。

1. 显式 `clip`：在上述 CSS 像素 PNG 中按整数矩形复制像素，用 `pngjs=7.0.0` 编为原尺寸无损 PNG；全视口矩形可以直接复用原 PNG。帧放得下就返回，`resized=false`。放不下则返回 `IMAGE_PREVIEW_TOO_LARGE`，提示缩小 `clip`，不隐式转换为 JPEG 或缩小。
2. 整屏：优先返回原尺寸 PNG。原 PNG 装不进帧时，从这一帧解码的 RGBA 使用 `jpeg-js=0.4.4` 在内存按固定质量 80 编码，宽高保持原尺寸并再次核对帧。显式 `omitBackground:false` 以浏览器的默认不透明背景取图；对后端异常保留的透明像素，JPEG 编码前按该默认白色背景合成，避免直接忽略 alpha 导致黑色边缘。PNG 分支保留采集像素。
3. 两种原尺寸编码都装不下时，只对整屏生成缩小概览。候选长边按上一档的 0.8 向下取整，最后尝试 `min(原长边,768 px)`；另一边等比取整且至少为 1。每一档从同一原始帧双线性重采样，依次试该档 PNG 和 JPEG Q80。首个符合完整帧限额的结果返回 `resized=true`。到下限仍放不下返回 `IMAGE_PREVIEW_TOO_LARGE` 并提示指定局部 `clip`。小字细节由原像素局部和实测验收保障。

单实例只允许一个图像处理任务。超过槽位返回 `CAPACITY_EXCEEDED`。需要转换时使用 Node worker_threads 隔离有界解码、区域复制、重采样和编码，取消或到期终止线程并回收缓冲。整个请求仅捕获一次页面，不通过第二次 JPEG 截图切换编码。

**回执。** 成功结果恰有一个简短 text 块和一个原生 image 块，`mimeType` 取实际的 `image/png` 或 `image/jpeg`，`data` 是完整无前缀 Base64。`structuredContent` 含 `session_id`、`tab_id`、`document_id`、完整 `viewport`、实际 `source_rect`、`preview_width`、`preview_height`、`mime_type`、`encoded_bytes`、`resized`。预览宽高以解码结果为准，`resized` 比较预览与 `source_rect`，局部裁剪本身不计缩放。OIS 在 MCP 中映射为 image 与元数据，REST 内联返回同一内容。图像仅在内存周转，日志可记尺寸、字节数和错误码。

**图像兼容。** 与 Filesystem 的 view_image 共用 content.v1 实现及验证规则，但不要求安装 Filesystem App。新 OIC 与旧 OIS 连接仅该连接上的 screenshot 不可用，其余 17 项 JSON Action 和本地 worker 仍 Ready；派发时再次检查连接能力。旧 OIC 加载声明新格式的 manifest 显式失败。第一切片可使用仅登记已实现 JSON Action 的开发 manifest；正式 18 项包须包含格式协商和真实图像消费验收，不把普通 JSON 中的 Base64 文本冒充可见图片。

**调用恢复也要留出输出空间。** list_tabs 必须能给出当前待处理事件的 ID；先给每个 tab 的标识、状态、资源 IDs 和指定 operation 的终态预留空间，再分配给标题/摘要/可选 frame 明细。快照能返回的最大正文不等于列表里的单项也可使用同样预算。生成终态时就限制可嵌入的 mutation receipt 大小（例如实例帧预算的 1/4）；避免原始回执能返回，套进 list_tabs 却永远超帧。对必需身份和完整 URL 不截成另一标识；超出观察预算的 URL 应使用明确的未提供语义，不能把截断前缀作为可导航 URL。若契约不能表达某必要值缺失，返回 RESULT_TOO_LARGE，不生成形状不合法的成功结果。发布前用满配额、长标题和同时待处理的 dialog 检查恢复路径，而不只测空列表。

## 6. 开发顺序和交付判断

第一切片的 6 项通过 OIS → OIC → worker 操作本地表单，验证独立 snapshot/ref 链路。第二切片增加 11 项，验收标签管理、受限输出、控件、等待、会话找回及当前视口截图。第三切片增加 handle_dialog，完成阻塞/晚到弹窗的可接续流程；基础事件监听、不支持文件交互的处理与取消调度自第一切片存在。完整分组见[运行与验收](browser-runtime.md)。

两个原生平台均须完成独立包、18 项契约、真实 HTTP/MCP 消费、专用 profile 初次登录与复用、文件交互拒绝及资源回收验证。用 1280×720、1920×1080 的图文页面覆盖超过软目标的原尺寸 PNG、同帧 JPEG Q80、低帧预算概览和局部无损 PNG。在支持图像的 MCP 视觉客户端读回只存在于像素中的 12/14 px 随机中文短句、批号和数字。答案仅放测试真值，DOM、可访问名称及元数据不含答案。REST 解码核对 MIME、区域、尺寸、字节数；通过前逐平台记录剩余验证。Browser 可独立安装，图像验收需支持 content.v1 的 OIC/OIS 及图像消费者。

既有 Windows smoke 仅证明系统 Chrome 下语义操作、旧元素失效、dialog 等基础行为；不证明独立包、双平台或 OI 调度已经实现。本次产出是可评审的设计与契约工件，不改产品代码。
