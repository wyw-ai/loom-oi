# Filesystem 与 Browser 官方 oiapp · 25 个 Action 与实现方式

本轮共识于 2026-11-20 封板。[开发规范](../../../spec/reference/oiapps/README.md)维护后续接口约定，[封板归档](../../archive/oiapp-expansion/2026-11-20-initial-scope/design.md)保留本轮选择及当时的设计材料。

本版目标是让 Agent 通过 OIS API/MCP，在指定机器上读写文件、操作网页，并能按需看见结果。已确认 **Filesystem 7 项、Browser 18 项**。其中 23 项返回普通 JSON，`view_image` 和 `screenshot` 共用有界图像结果，支持查看整图和读取局部小字。以下按 Action 列出主要输入、受控端执行方式和调用方可核对的结果。这是待实现的设计基线，发布情况以[端到端验收项](acceptance.md)的实际执行结果为准。

[YAML 与参数手册](examples/README.md)列出每个 Action 的必填、可选字段及组合约束，并附 [Filesystem 完整 app.yaml](examples/filesystem/app.yaml)、[Browser 完整 app.yaml](examples/browser/app.yaml) 和 [25 份调用参数](examples/arguments.yaml)。

提供方在受控机安装 App，配置实例后由 OIC 托管 worker。OIS 将 HTTP 工具和 vMCP 工具绑定到指定实例及 Action。根目录、浏览器进程和专用登录状态均由受控机管理，调用方使用同一套工具参数。控制端与受控端之间、两个受控端之间的原件传输，以及网页上传、网页下载，均已推迟。之前讨论的[在线文件传输方案](../../archive/oiapp-expansion/2026-11-20-initial-scope/file-exchange.md)留作以后评审。

例如，Agent 可先读取工作目录里的报告配置，再在网页中选择月份、提交筛选并等待状态文字更新。`snapshot` 提供按钮和选项的身份；页面用 canvas 绘制趋势图时，再调用 `screenshot` 看趋势线是否真的出现。用户提供的是受控机上的现有图表文件时，用 `view_image` 直接看文件，无需打开浏览器。三个观察入口各自对应一种具体任务。

## Filesystem · 根内操作

由受控机上的独立 Go worker 执行。每个实例持有一个固定的 `os.Root`，多个根目录用多个实例暴露。调用优先传根内相对路径，回执统一使用以 `/` 分隔的根内相对路径。可写实例用一把写锁串行处理修改，Windows 与 Linux 共用 Action 和 Schema，文件属性检查与发布按平台处理。

| Action | 主要输入 | 受控端实现 | 调用方得到什么 |
| --- | --- | --- | --- |
| `list_directory` | `path`（默认 `.`）、`limit`、`cursor` | 读取一层，包括隐藏项。有界收集名称后按 UTF-8 字节排序，只读本页条目的属性。续页时核对名称集合的摘要。 | `entries`、`next_cursor`、`complete` 和读取问题。增删名称使旧游标失效。 |
| `find_files` | 路径 glob `pattern`，可指定起始 `path`、深度及隐藏项 / ignore 规则 | 在根内递归遍历普通文件，对相对于起始目录的路径匹配 glob。递归过程跳过链接，按条数、深度、扫描量和时间停止。 | 根内文件路径 `files` 和 `status`。查到限额会说明未查全，调用方可缩小范围再查。 |
| `search_content` | `query`，可指定 `path`、`mode`、`file_glob`、大小写和上下文行数 | 复用文件遍历，先验证候选文件为有效 UTF-8，再逐行执行字面匹配或 Go 正则匹配。每个匹配行记录首次命中的字节范围。 | `matches` 给出文件、行号、字节位置、片段及可选上下文。`status` 说明扫描范围是否查完。 |
| `read_file` | `path`，可用 `start_line` 起读或用 `cursor` 续读，可限行数及字节数 | 默认可编辑上限 2 MiB。限内对完整原始字节计算 SHA-256，再截取请求范围。较大文件流式读取，分页保留 UTF-8 字符及 CRLF 边界。 | 正文、范围、BOM 标志、`eof`、续读游标。可编辑文件有 `revision`，流式大文件为 `null`。 |
| `write_file` | `path`、`content`。`create` 可设 `create_parents`，`replace` 必须给 `expected_revision` | 默认新建，已有目标则拒绝覆盖。替换时在写锁内复核版本。创建目录前预留回执空间，写到同目录临时文件后再发布。 | 修改状态、新旧 revision、写入字节数及实际创建的目录。版本冲突时拒绝覆盖。 |
| `edit_file` | `path`、`expected_revision`、`edits`（每项含 `old_text`、`new_text`） | 在同一版原文中定位所有片段。每段须恰好命中一次、各段互不重叠，全部检查通过后一次发布。保留原 BOM、换行和未改内容。 | 与写入共用修改回执，含新 revision。漏匹配、多匹配或重叠时原文件保持原样。 |
| `view_image` | `path`，可选 `crop`，坐标基于方向校正后的原图 | 校验静态 PNG/JPEG/WebP 的文件头、尺寸和像素数，校正方向。整图优先保留原尺寸与可用源编码，显式裁剪则按选区原像素编码无损 PNG。 | 一个原生图像块，以及来源、尺寸、实际 MIME、编码字节数和缩放标志。选区过大时提示缩小 `crop`。 |

`read_file` 和 `list_directory` 支持续读。`find_files` 和 `search_content` 用完整性标记报告未查完的范围。一次 `write_file` 的正文或一次 `edit_file` 的新旧文本合计默认最多 128 KiB，与源文件的可编辑大小分别计量。[Filesystem 详细设计](filesystem-design.md) · [输入与输出 Schema](filesystem.schemas.json)

## Browser · 操作页面并按需查看渲染结果

受控机运行 Node.js / TypeScript worker，通过锁定版本的 Playwright 驱动专用无头 Chromium。会话按可信调用身份和 vMCP 归属。下表中，操作一个会话须带 `session_id`，操作一个页面还须带 `tab_id`。表格列出各项特有的主要参数。

### 会话、标签页与导航 · 7 项

| Action | 主要输入 | 受控端实现 | 调用方得到什么 |
| --- | --- | --- | --- |
| `create_session` | 可选 `profile` 别名，默认临时会话 | 检查身份、配额及持久 profile 的独占锁，创建隔离的浏览器环境，登记初始空白页并安装事件监听。 | 就绪的 `session_id`、首个 `tab_id` 和 `document_id`、实际视口及能力。 |
| `list_sessions` | 空对象 | 从 worker 会话表按身份、vMCP 过滤，读取仍保留的会话和状态。 | 可找回的会话 ID、状态、profile 和存活标签数。 |
| `close_session` | 会话 ID | 结束该会话的未结操作，关闭所属页面和浏览器资源，清理临时状态。持久 profile 保留并释放锁。 | `closed` 或 `already_closed`、关闭的标签 ID 及清理警告。 |
| `list_tabs` | 会话 ID，可选 `after_sequence`、`operation_id` | 读取标签表、增量事件及待处理弹窗。传操作 ID 时另查原操作记录，即使页面被弹窗阻塞也可查询。 | 当前标签、文档、弹窗及未结操作 ID。指定操作则额外返回状态、最终回执或错误。 |
| `open_tab` | 会话 ID，可选 `url` 和 `wait_until` | 创建标签，先登记并安装监听，再按需导航。省略 URL 时打开空白页。 | 新标签 ID、实际 URL、导航阶段及可得的 HTTP 状态。导航失败时可凭已建标签 ID 继续处理。 |
| `close_tab` | 页面 ID 组合 | 按 ID 关闭指定页，释放其句柄与快照引用。资源关闭跳过网页的离开确认。 | 关闭状态及警告。关掉最后一个标签后，仍可在原会话中开新页。 |
| `navigate` | `mode`、`url`、`wait_until` | 将 `goto/back/forward/reload` 映射到相应导航操作，只有 `goto` 传 URL。等待指定阶段，完整文档替换后轮换文档 ID。 | 实际 URL、HTTP 状态、已到达的阶段。无历史可前进或后退时返回 `unchanged`。 |

### 页面观察与等待 · 3 项

| Action | 主要输入 | 受控端实现 | 调用方得到什么 |
| --- | --- | --- | --- |
| `snapshot` | 可选 `scope`、`max_chars`、`max_nodes` | 逐 frame 提取可见正文、控件角色、名称及状态，将短期 `ref` 绑定到本次观察到的真实 DOM 节点。控制遍历量和输出量。 | 页面文字、可操作元素、快照 ID、frame 与截断标记。替换同名节点后旧 ref 失效，须重新观察。 |
| `screenshot` | 可选 `clip={x,y,width,height}`，单位为当前视口 CSS 像素 | 一次捕获可见视口。整屏依次尝试原尺寸 PNG、同帧原尺寸 JPEG Q80，均放不下才缩成概览。显式 clip 提取原像素无损 PNG。 | 原生图像、实际 `source_rect`、输出尺寸、MIME 和 `resized`。局部放不下时提示请求更小区域。 |
| `wait_for` | `condition`、可选 `timeout_ms` | 在期限内检查元素状态、可见文字、URL 或文档加载阶段。等待期间为弹窗处理保留独立控制路径。 | `matched`、`timed_out` 或 `blocked`，附耗时、观察结果或阻塞信息。 |

### 页面交互与弹窗 · 8 项

`target` 可用快照 ID + ref，或指定文档内唯一的 role/name 或 label。语义目标用 Playwright 在指定 frame 中解析，多个匹配时报歧义。普通动作返回本次输入的回执；确认网站业务结果时，调用方再用 `wait_for` 或 `snapshot` 检查页面。

| Action | 主要输入 | 受控端实现 | 调用方得到什么 |
| --- | --- | --- | --- |
| `click` | `target`，可指定鼠标键、点击次数和修饰键 | 校验唯一性与可操作性，发送浏览器点击输入，记录可归属的导航、新标签和 JS 弹窗。 | 动作回执及相关事件。弹窗阻塞时返回 `pending` 和原操作 ID。 |
| `fill` | `target`、`text`、可选 `mode` | `replace` 替换控件全文。`type` 聚焦后逐字输入，保留已有内容。回车或失焦由调用方显式触发。 | 输入模式、请求字符数及状态。回执省略正文，逐字输入中断时报告已知副作用。 |
| `select_option` | `target`、`values` | 对原生 select 校验实际的 `option.value`，设置一次，再读回选中值。支持清空多选。 | 实际 `selected_values`，已符合目标时返回 `unchanged`。自定义菜单可用快照和点击操作。 |
| `set_checked` | `target`、`checked` | 读取原生 checkbox/radio 状态，按期望值设置再复核。checkbox 支持勾选和取消，radio 支持选中。 | `actual_checked`，已满足时返回 `unchanged`。 |
| `press_key` | `key`，给 `target` 或 `document_id` | 目标明确时先聚焦再发按键或组合键，省略目标时校验文档并作用于该页当前焦点。 | 按键、可观察的焦点和动作回执。 |
| `hover` | `target` | 把浏览器内指针移到唯一、可见且可命中的元素，触发页面悬停行为。 | 悬停回执。可继续用 `wait_for` 验证菜单或提示已出现。 |
| `scroll` | `mode`，按分支传 `target` 或 `delta_x/delta_y` | `by` 按 CSS 像素滚动主视口或指定容器。`into_view` 将指定元素滚入视口。执行前后读取可观察位置。 | 实际前后位置及是否移动，到边界或元素已可见时可返回 `unchanged`。 |
| `handle_dialog` | `dialog_id`、`action=accept/dismiss`，接受 prompt 时可给 `prompt_text` | 走独立控制路径，核对当前弹窗实例并响应 alert、confirm、prompt 或 beforeunload。 | 弹窗处理回执。用原 `operation_id` 查询触发它的点击或导航最终是否完成。 |

例如提交表单时，`click` 可能因同步确认框返回 `pending`。此时按顺序调用 `handle_dialog`、`list_tabs(operation_id)` 和 `wait_for`，即可处理确认框、查回原点击、确认页面显示提交成功，整个过程只点击一次。[可校验的调用样例](../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-flow.example.json)

## 两项图像接口的最终约定

**384 KiB 是图像编码后的默认传输目标。** 是否能交付，要把 Base64、文本、元数据、协议封装及 JSONL 换行一并计入实际帧长。worker 完整结果帧默认 768 KiB，各跳遵守 1 MiB 限额及实例配置的更小硬限。举例说，518.7 KiB 的 PNG 转成 Base64 约 691.5 KiB，若完整帧容得下，仍可按原尺寸交付。[小字与帧预算实测](../../archive/oiapp-expansion/2026-11-20-initial-scope/image-preview-review.md)

| 调用 | 观察区域 | 成功时的图像 |
| --- | --- | --- |
| `view_image(path)` | 受控机文件，先校正 EXIF 方向 | 源字节、像素数及提供方硬上限允许时优先原尺寸。可直接使用合适的源编码。需重新编码时优先保护图文线条和透明度，不透明照片可用 JPEG。整图需要缩小时返回实际尺寸及 `resized=true`。 |
| `view_image(path, crop)` | 校正方向后原图左上角为原点，单位为原图像素 | 无损 PNG，宽高与 `crop` 相同。选区放不下时返回 `IMAGE_PREVIEW_TOO_LARGE`，调用方可缩小选区。 |
| `screenshot(session_id, tab_id)` | 当前滚动位置的可见视口，单位为 CSS 像素 | 同一帧依次尝试原尺寸 PNG、原尺寸 JPEG Q80。两者均超出实际硬预算后，才尝试有尺寸标记的缩小概览。 |
| `screenshot(session_id, tab_id, clip)` | 当前可见视口左上角为原点，单位为 CSS 像素 | 从同一次捕获中裁出指定区域，按原 CSS 像素返回无损 PNG。放不下时返回 `IMAGE_PREVIEW_TOO_LARGE` 并提示减小 `clip`。 |

两个矩形都使用整数 `{x, y, width, height}`，宽高为正，须完全落在对应边界内。输出元数据报告实际 MIME、预览尺寸、编码字节数及 `resized`。裁剪只改变所取区域，按选区原尺寸返回时 `resized=false`。浏览器整屏原尺寸 JPEG 同样是 `resized=false`，其有损编码由 `mime_type=image/jpeg` 表明。

### 两个图像 Action 共用的 OIC/OIS 改动

MCP 的 `tools/call` 允许把图像作为顶层 `result.content[]` 的一个块交付。示意回执如下，`data` 在实际回执中是完整的 Base64 预览字节。[MCP Image Content 规范](https://modelcontextprotocol.io/specification/2025-11-25/server/tools#image-content)

```json
{
  "result": {
    "content": [
      {"type": "text", "text": "当前标签页的视口预览"},
      {"type": "image", "mimeType": "image/png", "data": "<完整的预览图 Base64>"}
    ],
    "isError": false
  }
}
```

仓库的 [OIS MCP 适配器](../../../../internal/ois/mcp.go)目前把结果序列化到 text，对象另放入 structuredContent。只发布 worker 会将图像当成普通业务数据。方案要求 worker 显式声明结果格式 `content.v1`，OIC 协商并按 Action 验证，OIS 保留格式并将图像块写入顶层 content，只在 structuredContent 放元数据。该格式名属于 OI 内部。REST 调用者仍从同一次 HTTP 响应中拿到预览。普通 JSON 即使有同名字段也走原行为。

预览字节会经过 OIS 到达调用端，随一次有界的结果返回，无需上传接口、下载 URL 或 OIS 文件库。只有支持图像工具结果并把图交给视觉模型的消费者能完成看图任务。新 OIC 对仅支持 JSON 的旧 OIS 连接停用两个图像 Action，其余 23 项按原 JSON 结果使用。旧 OIC 加载声明新结果格式的 App manifest 则会失败，发布时需要清楚标出最低宿主版本。详细契约见 [Filesystem 技术设计](filesystem-design.md)和 [7 项 Schema](filesystem.schemas.json)。

### screenshot 调用示例

例如报表批号落在当前视口的一个 360×96 区域时，调用方可以直接请求该块原像素。

```json
{"session_id":"s1","tab_id":"t1","clip":{"x":80,"y":220,"width":360,"height":96}}
```

`clip` 与 `timeout_ms` 均可省略。worker 以 `scale: "css"` 将当前视口捕获一次，同一帧供裁图和重编码使用。页面不会因截图而滚动。前后核对文档身份，换页则丢弃结果。没有传 `clip` 时，只有原分辨率 PNG 和 JPEG Q80 都放不下才提供缩小的概览，其长边不低于原长边与 768 px 的较小者。概览仍然超限时提示请求更小的 `clip`。[Playwright 的内存截图](https://playwright.dev/docs/screenshots#capture-into-buffer)

成功回执的 `content` 含简短文本及一项原生图像。`structuredContent.viewport` 描述捕获时的视口，`source_rect` 标明实际取图区域。上例成功时 `preview_width=360`、`preview_height=96`、`mime_type=image/png`、`resized=false`。完整图像数据只出现在 image 块，随本次响应交付。

逐项参数与回执见 [18 项 Action 说明](../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-actions.md)和 [Browser Schema](../../archive/oiapp-expansion/2026-11-20-initial-scope/browser.schemas.json)。[Browser 技术设计](../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-design.md)、[事件与会话管理](../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-lifecycle.md)及[双平台打包验收](../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-packaging.md)列出具体实现和平台条件。

## 实施顺序

实施顺序先用 6 项跑通表单，即 create_session、navigate、snapshot、fill、click、close_session。然后补齐 11 项会话、标签、控件、等待及视口截图，最后验收 handle_dialog。截图这一步依赖通用图像结果接通，其他 17 个 Browser Action 的开发可先行。会话监听和独立控制路径从第一阶段就接入，保证同步 confirm 在后续阶段可以接续。意外触发文件选择或下载时，运行时按能力拒绝或清理；对调用者不产生可续办的文件任务。
