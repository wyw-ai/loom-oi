# Filesystem 与 Browser 官方 oiapp · 封板快照

本轮于 2026-11-20 结束。此目录保留当时的方案、样例、实验与讨论。后续开发与验收的规范入口见[扩展目标](../../../../spec/goals/oiapp-expansion.md)和[接口契约](../../../../spec/reference/oiapps/README.md)。

已确认 **Filesystem 7 项 + Browser 18 项**。两种视觉观察都在同一次 API/MCP 调用内交付有界图片，384 KiB 为默认目标。查看文件用 `view_image`，观察当前网页用 `screenshot`。跨机原件传输与网页上传下载已推迟。

[逐项能力与实现思路](../../../current/oiapp-expansion/design.md)列出全部 25 项和调用路径。[发布验收项](../../../current/oiapp-expansion/acceptance.md)列出消费者可观察的结果。以下是对应的工程依据。

## Filesystem

每个 Go worker 固定一个受控机根目录，可在 OIC 配置多个实例。Agent 通过 `list_directory`、`find_files`、`search_content` 定位文件，使用 `read_file` 取得正文和可用的 revision，再用 `write_file` 创建或条件替换，用 `edit_file` 完成单文件内的精确修改。`view_image` 读取根内已有静态图，校正方向，优先保留整图原尺寸；显式 `crop` 按校正后的原图坐标返回原像素 PNG。

- [当前 Filesystem 技术设计](../../../current/oiapp-expansion/filesystem-design.md)及[配置与接口 Schema](../../../current/oiapp-expansion/filesystem.schemas.json)约定目录结构、平台差异、提交边界和预算。
- [本轮技术设计稿](filesystem-design.md)及[对应 Schema](filesystem.schemas.json)用于复核当前基线。

## Browser

Node.js / TypeScript worker 使用固定版本 Playwright 和专用无头 Chromium。目标定位限定为快照 ref、唯一 role/name 和 label。会话归调用身份和 vMCP 所有。所有页面操作都可用结构化回执查证，截图由调用方按需请求。

| 实施阶段 | Action | 验证目标 |
| --- | --- | --- |
| A · 6 项 | create_session、navigate、snapshot、fill、click、close_session | 在测试站创建会话，找到控件并提交表单，从页面文字读回结果。 |
| B · 11 项 | list_sessions、list_tabs、open_tab、close_tab、select_option、set_checked、press_key、hover、scroll、wait_for、screenshot | 找回会话、操作多个标签与常见控件；视觉客户端按需核实当前视口的画布或布局。 |
| C · 1 项 | handle_dialog | 用独立控制请求处理阻塞弹窗并查回原动作的最终回执，服务端只收到一次提交。 |

[Browser 技术设计](browser-design.md)包含目录、配置与实现模块；[18 个 Action](browser-actions.md)与[机器 Schema](browser.schemas.json)定义输入输出；[流程样例](browser-flow.example.json)可校验。生命周期细节见[会话与事件](browser-lifecycle.md)，发版细节见[平台构建验收](browser-packaging.md)及[实施切片](browser-runtime.md)。

## 共同依赖与本版交付边界

OI 内部协商 `content.v1`，OIS 在 MCP 顶层 `result.content` 产生原生 image 块，HTTP 响应也可直接取得预览。该公共能力实施一次。Filesystem 与 Browser 独立安装，两个生成端分别负责根内图片解码和视口像素捕获。每次只发送一个有界预览，不需要中心文件库、下载 URL 或客户端另建文件服务。Browser 的 screenshot 增加视觉观察，不增加坐标点击、全页拼接或网页上传下载。

实施须验证新旧宿主混用、预算受限时的行为及真实客户端能否将图像交给视觉模型。Filesystem 分别验证 Windows amd64、Linux amd64/arm64。Browser 分别验证 Windows 11 x64 与 Ubuntu 24.04 x64。此前 Windows 系统 Chrome 的 smoke 记录在[沟通笔记](notes.md)，两个正式 App 和端到端发布验收仍待实现。

[跨机器文件交换提案](file-exchange.md)保留供以后评审，独立于这次图像结果的交付。

[图像预算与小字实测](image-preview-review.md)记录了这次决策的依据。Browser 优先交付原尺寸 PNG，放不下时用同帧原尺寸 JPEG Q80。`clip` 以当前视口左上角为原点，以 CSS 像素为单位，返回原像素局部 PNG。完整结果帧决定图像能否交付。
