# Filesystem 与 Browser 扩展目标

2026-11-20 封板。目标是让 Agent 通过既有 HTTP API 或 MCP，在提供方指定的机器上读写工作目录、操作网页，并按需看见图片或页面的渲染结果。设计与接口契约已确认。当前 beta 已交付 Bash 和 PowerShell；扩展版的两个 App 均已在 Windows 跑通。Filesystem 已在 Linux amd64 和 arm64 走通 REST 与 MCP，Browser 已在 Ubuntu 24.04 x64 上使用包内 Chromium 和非 root 沙箱跑通相同的端到端调用。[分平台开发验证](../../reports/oiapp-expansion/verification.md) [beta 已交付范围](beta.md) · [两个 App 的接口契约](../reference/oiapps/README.md)

## 能力与归属

| 交付对象 | 消费者要完成的事 | 提供方负责的范围 |
| --- | --- | --- |
| Filesystem，7 个 Action | 找到文件、读取正文和版本、创建或精确修改文本、查看根内已有静态图片和局部细节 | 在受控机配置固定根目录和读写权限，运行独立 Go worker。每个实例守住自己的访问范围。[Filesystem 契约](../reference/oiapps/filesystem.md) |
| Browser，18 个 Action | 从同一会话的首个标签开始，观察控件、填写并提交表单、处理弹窗、读回可见结果，必要时观察画布或局部文字 | 在受控机运行独立 Node.js / TypeScript worker，随包提供 Playwright 与专用 Chromium。由提供方配置可用的持久 profile 和访问范围。[Browser 契约](../reference/oiapps/browser.md) |
| 图像结果，供上述两个 App 共用 | 调用 `view_image` 或 `screenshot`，在同一次 API/MCP 响应中得到可解码预览、来源区域和尺寸 | worker 生成并预算图像，OIC 校验并协商结果格式，OIS 将图像交给消费端。其余 23 项沿用普通 JSON。[宿主协议](../reference/app-runtime.md) · [消费端结果](../reference/ois-api.md) |

本版暂缓控制端与受控端之间、两个受控端之间的原件传输，以及网页上传、下载。图像随响应有界传递，因此本版无需 OIS 文件库存储、领取 URL 或跨 App 文件交接服务。

## 完成判据

| 验收路径 | 可观察结果 |
| --- | --- |
| 接入与兼容 | 从独立发行包加载两个 App，经固定 Tool 绑定分别从 HTTP 和授权 vMCP 调用。核对 25 项名称、输入与结果，验证身份隔离、图像格式协商、混用新旧组件和既有终端工具回归。 |
| 文件 | 从目录、文件名或内容找到目标，读出 revision，一次修改同文件两处并回读确认。旧 revision、匹配冲突或只读配置时，目标内容保持原值。覆盖文件分页、路径越界、预算和平台错误。 |
| 网页 | 在测试站通过快照填写表单并提交。若遇同步确认框，处理后查回原操作回执，读到页面成功状态，测试站只收到一次提交。另验证多标签、等待、会话回收和事件恢复。 |
| 图像 | 文件整图和视口整屏优先保原尺寸，默认编码软目标为 384 KiB。整屏原尺寸 PNG 放不下时尝试同帧原尺寸 JPEG Q80。显式 `crop` / `clip` 成功时返回原像素无损 PNG。用真实 MCP 视觉消费者读回局部随机批号和小字，并核对完整帧预算。 |
| 发行 | Filesystem 在 Windows amd64 本地 NTFS、Linux amd64 及 arm64 实机或对应架构运行环境验证。Browser 分别在 Windows 11 x64 与 Ubuntu 24.04 x64 验证随包浏览器、Linux 非 root 沙箱和退出后的资源回收。 |

逐项执行条件、预期失败及证据要求集中在[发布验收清单](../../goal-shaping/current/oiapp-expansion/acceptance.md)。封板时已有 Schema、清单与示例的静态验证。后续的 Windows 运行与读图结果已单独记录，发行按各平台的执行证据判断。[封板快照](../../goal-shaping/archive/oiapp-expansion/2026-11-20-initial-scope/design.md)
