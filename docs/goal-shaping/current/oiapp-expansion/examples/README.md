# 两个 oiapp 的 YAML 与调用参数

本页及同目录的 YAML 保留 2026-11-20 封板时逐项评审的版本。后续实现和接口维护以[规范目录中的 Schema、清单及配置示例](../../../../spec/reference/oiapps/README.md)为准。

Filesystem 在受控机的指定目录读写文本、查找文件、看图。Browser 在同一台受控机启动专用 Chromium，让 Agent 打开网页、操作控件、读回页面状态或查看截图。下面给出两个包的完整清单，以及把这 25 项能力接入同一个 vMCP 的一套配置。

| 要看什么 | 文件 | 重点 |
| --- | --- | --- |
| Filesystem 包声明了什么 | [filesystem/app.yaml](filesystem/app.yaml) | 一个 Go worker，7 个 Action 的完整输入 Schema |
| Browser 包声明了什么 | [browser/app.yaml](browser/app.yaml) | 一个 Node.js worker，18 个 Action 的完整输入 Schema，含共用的元素定位、等待条件和截图区域 |
| 每个 Action 调用时怎么填 | [arguments.yaml](arguments.yaml) | 每项都有一份可用作 `arguments` 的参数样例。示意 ID 由真实运行回执替换 |
| 受控端如何配置 | [oic.linux.yaml](oic.linux.yaml) · [oic.windows.yaml](oic.windows.yaml) | 工作根目录、写权限、预算、浏览器视口及可选的登录状态 |
| 服务端怎样提供 API/MCP 工具 | [ois.yaml](ois.yaml) | 25 个工具名、各自的输入 Schema、绑定的通道 / App / Action，以及 vMCP 的开放清单 |

## 一个包怎样成为工具

每个发行包只有一份 `app.yaml`。`app.code` 标识实例，`runtime.command` 指定 OIC 在包目录启动的进程，`actions` 下每个键声明一个可调用的 Action。这两个包各自共用一个根 worker，OIC 默认把 Action 名作为 handler 名，直接传递同名参数。下面节选 Browser 清单中的一个完整 Action。

```yaml
version: 1
app: {code: browser}
runtime:
  command: [./runtime/node, ./worker/main.cjs]
actions:
  list_sessions:
    description: 列出当前调用身份可见的浏览器会话和状态。
    input_schema:
      type: object
      properties: {}
      required: []
      additionalProperties: false
```

两个包的部署形态如下。表内是目标发行布局，完整源码目录和打包内容见各自设计。

| App | 包内运行文件 | 目标平台 |
| --- | --- | --- |
| Filesystem | `app.yaml` 和 `runtime/oiapp-filesystem`。Windows 包入口为 `runtime/oiapp-filesystem.exe`。Go 编译成独立可执行文件 | Linux amd64 / arm64、Windows amd64。[目录与构建](../filesystem-design.md) |
| Browser | `app.yaml`、`runtime/node`、`worker/main.cjs`、`worker/node_modules/` 和 `browsers/`。随包提供 Playwright、图片编解码依赖和配套 Chromium。Windows 包入口为 `runtime/node.exe` | Windows 11 x64、Ubuntu 24.04 x64。[源码目录](../../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-design.md) · [发行包目录与验收](../../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-packaging.md) |

OIC 的 `app_config.filesystem` 和 `app_config.browser` 分别传给对应 worker。示例把 Filesystem 显式设为可写，根为 Linux 的 `/srv/oi/workspace` 或 Windows 的 `D:\oi\workspace`。部署时选用已存在的工作根目录。Browser 状态目录单独配置，默认开临时会话。示例另为有权使用 `workbench` 的指定身份配置了持久 profile 别名 `reports`，需要复用登录态时传 `profile: reports`。

发行包就位后，在运行 OIC 的机器上分别执行 `oic --config <oic.yaml> load <包目录>/app.yaml`。OIS 用 `tools.<工具名>.source` 将这台机器的通道、App 和 Action 绑定起来，再通过 `vmcps.workbench.tools` 暴露。示例 OIS 监听本机回环地址，从环境变量读取令牌。首次创建状态文件时导入该 YAML 的绑定，已有 OIS 实例通过管理接口更新。[OIC 加载命令](../../../../spec/reference/cli.md) · [OIS 配置规范](../../../../spec/reference/configuration.md)

## Filesystem 的 7 个 Action

路径优先写成实例根目录内的相对路径，如 `reports/status.txt`。可选参数可直接省略。调用时只传本行列出的参数。字符串长度、数组大小、数值上下限及条件约束见[逐字段输入 YAML](filesystem/app.yaml)，结果和错误结构见[输入输出 Schema](../filesystem.schemas.json)。

| Action | 必填参数 | 可选参数 | 调用约定 |
| --- | --- | --- | --- |
| `list_directory` | 无 | `path`, `limit`, `cursor` | 默认从 `.` 列出一层，每页 200 项。续页用回执里的游标 |
| `find_files` | `pattern` | `path`, `respect_gitignore`, `include_hidden`, `max_depth`, `limit` | `pattern` 匹配相对起始目录的路径，可用 `**`。默认最多 200 项 |
| `search_content` | `query` | `path`, `mode`, `case_sensitive`, `file_glob`, `respect_gitignore`, `include_hidden`, `max_depth`, `limit`, `context_lines` | 默认字面匹配、区分大小写、`file_glob="**"`、100 个匹配行。可选 Go 正则 `regex`，上下文 0–2 行 |
| `read_file` | `path` | `start_line`, `max_lines`, `max_bytes`, `cursor` | 首次默认从第 1 行取最多 200 行、65,536 字节。续读传 `cursor`，与 `start_line` 互斥 |
| `write_file` | `path`, `content` | `mode`, `create_parents`, `expected_revision` | 默认为 `create`，可选创建父目录。只有 `replace` 接受且必填原 revision，替换时 `create_parents` 只能省略或为 false |
| `edit_file` | `path`, `expected_revision`, `edits` | 无 | `edits` 有 1–64 项，每项需 `old_text` 和 `new_text`。在同一版原文中分别唯一匹配且互不重叠后一次提交 |
| `view_image` | `path` | `crop` | 打开根内已有的静态 PNG、JPEG 或 WebP。省略选区看整图，传选区看方向校正后的原图局部 |

## Browser 的 18 个 Action

下面的“会话”表示传 `session_id`，“页面”表示同时传 `session_id` 和 `tab_id`。`create_session` 返回初始页面的 ID，`list_tabs` 可找回当前标签、弹窗以及未完成的操作。`timeout_ms` 可用范围为 1–30,000 毫秒。支持该参数的 Action 默认 10,000 毫秒，创建会话默认 30,000 毫秒。[逐字段输入 YAML](browser/app.yaml) · [输入输出 Schema](../../../archive/oiapp-expansion/2026-11-20-initial-scope/browser.schemas.json) · [会话与事件](../../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-lifecycle.md)

| Action | 必填参数 | 可选参数 | 调用约定 |
| --- | --- | --- | --- |
| `create_session` | 无 | `profile`, `timeout_ms` | `{}` 创建临时会话，同时得到第一个标签。可用已配置并授权的 profile 别名 |
| `list_sessions` | 无 | 无 | 传 `{}`，列出当前身份可见的会话 |
| `close_session` | 会话 | `timeout_ms` | 关闭会话及其标签，释放 profile |
| `list_tabs` | 会话 | `after_sequence`, `operation_id` | 用事件序号增量读取，或用操作 ID 查原操作结果 |
| `open_tab` | 会话 | `url`, `wait_until`, `timeout_ms` | 省略 URL 打开空白标签。导航阶段可选 `commit`、`domcontentloaded`（默认）、`load` |
| `close_tab` | 页面 | `timeout_ms` | 关闭该标签并释放元素引用 |
| `navigate` | 页面 | `mode`, `url`, `wait_until`, `timeout_ms` | 默认 `goto`，必须传 URL。`back`、`forward`、`reload` 要求省略 URL。导航阶段同 `open_tab` |
| `snapshot` | 页面 | `scope`, `max_chars`, `max_nodes`, `timeout_ms` | 返回正文、控件和引用。可限定元素范围，默认最多 12,000 字符、400 节点 |
| `screenshot` | 页面 | `clip`, `timeout_ms` | 省略 `clip` 截当前可见视口。传 `clip` 取当前视口内的原像素局部 |
| `wait_for` | 页面、`condition` | `timeout_ms` | 等待元素、文字、URL 或加载阶段；四种条件的字段见下节 |
| `click` | 页面、`target` | `click_count`, `button`, `modifiers`, `timeout_ms` | 默认左键单击。支持 1 或 2 次点击、左中右键、Alt / Control / Meta / Shift 修饰键 |
| `fill` | 页面、`target`, `text` | `mode`, `timeout_ms` | `replace` 默认替换全值，空字符串可清空。`type` 逐字输入 |
| `select_option` | 页面、`target`, `values` | `timeout_ms` | 原生下拉框。传字符串数组，按 `option.value` 选择 |
| `set_checked` | 页面、`target`, `checked` | `timeout_ms` | `checked` 为布尔值，指定复选框或单选框的目标状态 |
| `press_key` | 页面、`key` | `target`, `document_id`, `timeout_ms` | 省略目标时须传文档 ID，向当前焦点发送按键。两者都传时校验属于同一文档 |
| `hover` | 页面、`target` | `timeout_ms` | 将指针移至目标元素 |
| `scroll` | 页面 | `mode`, `target`, `delta_x`, `delta_y`, `timeout_ms` | 默认 `by`，至少一个位移非零，各轴限 −2000 至 2000。`into_view` 必须给目标，且省略两个位移 |
| `handle_dialog` | 页面、`dialog_id`, `action` | `prompt_text`, `timeout_ms` | `action` 取 `accept` 或 `dismiss`。只有接受 prompt 时可传输入文字 |

### 定位元素、等待条件与选区

| 对象 | 参数形状 | 含义 |
| --- | --- | --- |
| `target` 或 `scope` | `{kind: ref, snapshot_id, ref}` | 指向一次 `snapshot` 里观察到的元素。复用快照回执中的两个值 |
| 同上 | `{kind: role, document_id, role, name}` | 按角色及可访问名称定位。可加 `frame_id`、`exact`，默认精确匹配 |
| 同上 | `{kind: label, document_id, text}` | 按控件标签文字定位。同样可加 `frame_id`、`exact` |
| `condition` | `{kind: element, target}` | 可选 `state` 为 `attached`、`detached`、`visible`（默认）或 `hidden` |
| 同上 | `{kind: text, text}` | 可选 `scope` 和 `state`。状态为 `present`（默认）或 `absent` |
| 同上 | `{kind: url, url}` | 可选 `match` 为 `exact`（默认）或 `prefix` |
| 同上 | `{kind: load}` | 可选 `state` 为 `domcontentloaded`（默认）或 `load` |
| `crop` 或 `clip` | `{x: 80, y: 120, width: 320, height: 96}` | 四个字段均必填且为整数。左上角为原点，坐标非负、宽高为正。整个矩形须落在源图 / 当前视口内。文件图用方向校正后的源像素，网页用 CSS 像素 |

## 从工具名到图像结果

以局部截图为例。会话 ID 和标签 ID 从 `create_session` 或 `list_tabs` 的回执取得。HTTP 消费者向本机示例的 `POST /api/v1/exec` 发送以下 JSON，使用 `workbench` 的 Bearer 令牌。

```json
{
  "vmcp": "workbench",
  "tool": "browser_screenshot",
  "arguments": {
    "session_id": "s_demo",
    "tab_id": "t_report",
    "clip": {"x": 80, "y": 220, "width": 360, "height": 96}
  }
}
```

MCP 客户端连接同一服务的 `/mcp/workbench`，在 `tools/call` 的 `params.name` 中填 `browser_screenshot`，在 `params.arguments` 中传同一对象。选区成功时得到 360 × 96 的无损 PNG。MCP 的 `result.content` 含一个 `type: image` 块，`mimeType` 和 Base64 `data` 随块返回。`structuredContent` 提供尺寸、区域、编码字节数等元信息。控件名称和状态用 `snapshot` 直接读取。

`view_image` 和 `screenshot` 在 manifest 显式声明 `result_format: content.v1`，其余 23 项使用普通 JSON。384 KiB 是图像编码软目标，默认硬预算按含 Base64、元数据及封装的完整 worker 帧计算（768 KiB）。整屏原尺寸 PNG 能放下就保留，放不下再试同帧原尺寸 JPEG Q80，两者都放不下才缩小。显式 `crop` 或 `clip` 成功时保留选区原像素和无损 PNG；若选区仍过大，回执提示缩小选区。[图像处理与结果](../design.md) · [小字与跨端验收](../acceptance.md)

## 设计与运行状态

两份 `app.yaml` 和 OIS binding 都给出完整的输入 Schema。输出结构由各 App 的 JSON Schema 描述，供契约测试和消费端使用。OIC 做基础类型绑定，worker 还需检查完整条件分支、实际 UTF-8 字节数、文件 revision、会话归属和选区边界。

这批 YAML 是设计样例。当前仓库的 OIC 严格读取 App 字段，遇到新的 `result_format` 会拒绝加载整份清单。两个 worker、OIC/OIS 对图像格式的协商以及 Windows/Linux 发行包仍待实现并做端到端验收。现有验证覆盖清单结构、25 份调用参数和 OIS 绑定；实际截图、本地读写和真实客户端显示属于发行验收。[逐项实现方式](../design.md) · [验收清单](../acceptance.md)
