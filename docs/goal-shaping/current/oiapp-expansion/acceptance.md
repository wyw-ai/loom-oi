# Filesystem 与 Browser 首版验收项

以下为 2026-11-20 封板的发布验收清单。每条保留目标平台、App 包版本、OIC/OIS 版本、消费方式和观测结果。Schema 验证与运行验收分别记录。[扩展目标](../../../spec/goals/oiapp-expansion.md) · [开发契约与测试样例](../../../spec/reference/oiapps/README.md)

本轮已在 Windows 验证两个发行包的 25 个 Action、实际 `tools/list` 输入 Schema、原 PowerShell 成功与错误回执，以及 REST/MCP 图片和局部小字盲读。Filesystem 另在 Linux amd64 和 arm64 的 ext4 上完成 7 项与 OIS/OIC 端到端验证。Ubuntu 24.04 x64 已用包内 Chromium 在非 root 沙箱运行 18 项、通过 REST/MCP 交付图片并验证进程回收。以下条目作为完整发布判据使用，单个平台通过的部分见[分平台运行记录](../../../reports/oiapp-expansion/verification.md)。

## 消费路径与范围

- [ ] 从独立发行包装载两个 App，由 OIS 绑定实例和 Action。用 HTTP API 和一个经授权的 vMCP 调用同一能力。服务返回的 action 名称、参数、行为与本版 Schema 一致。换身份后无法找回其他身份的会话或使用其游标。
- [ ] 确认已批准的 Filesystem 7 项与 Browser 18 项能通过各自 Schema 校验。`view_image` 和 `screenshot` 走通用图像结果，其余 23 项沿用普通 JSON。
- [ ] 对发行包中的两份 App 清单、实际 `tools/list` 返回的 25 个工具以及[规范中的参数样例](../../../spec/reference/oiapps/examples/arguments.yaml)逐项核对工具名、绑定和输入 Schema。分别从两个入口传入同一组参数，验证条件必填、参数互斥、运行时边界和相应回执。
- [ ] 普通业务 JSON 即使含有 `content`、`image` 或 `data` 也按普通 JSON 交付。通过 HTTP、MCP 调用既有终端工具，检查它们的成功和错误回执。

## 两项图像接口共同验收

| 用例 | 通过条件 |
| --- | --- |
| 软目标与硬预算 | 对两个 Action 分别构造一张编码后大于 384 KiB、完整结果仍可放入默认 768 KiB worker 帧的 PNG。检查实际返回原像素图。再调低实例帧限，覆盖完整帧刚好容纳、超过 1 字节，以及编码数据不到 384 KiB 但完整帧仍然过大的情况。每一跳按各自的实际封装检查预算，Base64、元数据及换行均计入对应帧。 |
| HTTP/MCP 图像交付 | 通过两种入口分别解码。MCP 顶层 `result.content` 恰有一个图像块。检查块的 MIME、真实文件头、解码尺寸和 Base64 之前的图像字节数（`encoded_bytes`），与 `structuredContent` 一致。整图和选区各测一例，图像数据只出现在图像块。 |
| 真实客户端读小字 | 分别准备 1280×720、1920×1080 的图文混排报表，一份是已有图片，一份由浏览器 canvas 绘制。保留 12/14 px 中文、细线以及随机批号和数字，真值只在测试端另存。调用方分别用 `view_image(crop)`、`screenshot(clip)`，由真实 MCP 视觉消费者逐字读回并与真值比对。记录实际 MIME、来源区域、输出尺寸、字节数和识读结果，以便定位失败。 |

## Filesystem

| 用例 | 可观察的通过条件 |
| --- | --- |
| 从发现文件到修改 | 在测试根中用 `list_directory`、`find_files` 和 `search_content` 定位目标。用 `read_file` 取 revision，调用 `edit_file` 修改两处，再读确认修改及周围 BOM、换行等原始字节。 |
| 读取边界 | 在 UTF-8 长行、CRLF 和多字节字符处触发分页，连续读取能还原正文。小文件 revision 对得上原始内容的 SHA-256。大文件触发流式路径，revision 为 null。游标失效有明确结果。 |
| 建文件与冲突 | `write_file` 创建只作用于不存在的目标。replace 或 edit 使用过期 revision 时目标字节保持原样。精确替换零命中、多命中或片段重叠时全部拒绝。只读实例拒绝写入。 |
| 预算与异常 | 目录翻页期间增删名称可检出。搜索触及限额时返回 complete=false 和原因。构造包含多层长名称的 create_parents 输入，回执预算容不下时在首个 mkdir 前失败。逐个入口送入原始重复成员或错误编码，验证在丢失原始信息前拒绝。 |
| 根目录和平台属性 | 根外访问、路径别名、链接/重解析点按平台设计处理。验证 Linux 的权限和同目录发布，验证 Windows NTFS 的共享占用、DACL 和失败回执。在发布点前后中断，分别核对文件、已创建的目录和回执中的副作用。 |
| 整图与裁剪 | 默认配置打开长边超过 1600 px 的合规静态图，原尺寸在硬预算内就保持原尺寸。配合 EXIF 方向和带透明像素的图，按校正后的原图坐标请求 `crop`，直接比对输出 PNG 解码后的像素、透明度及选区宽高。覆盖贴边、越界和宽高为零。有效选区过大时返回 `IMAGE_PREVIEW_TOO_LARGE`，改小后成功。 |
| 整图回退与安全限额 | 用无法原尺寸交付的整图观察编码选择和缩小概览，核对 `reencoded`、`resized` 与实际处理一致。用很小的 crop 读取源字节或解码像素已经超限的文件，仍应遵守源图安全限制。若提供方显式配置了输出硬限，检查该限额在帧可容纳时也生效。 |
| 看图失败和兼容 | 测损坏、超过像素预算、预览超限等情况，确认返回有界错误且同连接仍可用。新 OIC＋仅 JSON 的旧 OIS 只将图像 Action 标为不可用，六项文本/目录可用。旧 OIC 遇新 manifest 明确报加载失败。在同一旧连接上 Browser 的 screenshot 同样不可用，17 个普通工具继续工作。完整混用矩阵见[Filesystem 技术设计](filesystem-design.md)。 |

## Browser

| 用例 | 可观察的通过条件 |
| --- | --- |
| 完成网页任务 | 在本地表单站 `create_session → navigate → snapshot → fill → click → wait_for`。用页面可见的成功标记和测试站实际提交次数确认提交一次，最后 `close_session`。这条表单用例只消费普通 JSON；再单独验证视觉观察。 |
| 标签与会话 | 用 `list_sessions` 找回会话，`open_tab` 新开页，`list_tabs` 发现该页及网站另开的标签。关闭指定 tab 和会话，核对其他会话仍可用、临时状态被清理、持久 profile 锁释放。用支持的网页表单走一次专用 profile 首登，再验证第二个会话能复用登录态。 |
| 识别目标与操作控件 | 匹配两个同名控件时报歧义。重新渲染同名节点后，旧 ref 失效。测试 `select_option`、`set_checked`、`press_key`、`hover`、`scroll`，再用快照或等待读回选中值、菜单、可见文本等可验证状态。 |
| 整屏截图的编码顺序 | 分别覆盖三个分支。原尺寸 PNG 能交付就返回 PNG，包括大于软目标的样本。PNG 放不下而同帧原尺寸 JPEG Q80 能交付时，核对 MIME 为 JPEG、尺寸为 CSS 视口、`resized=false`。两者均放不下，才接受标记 `resized=true` 的等比概览。缩至规定下限仍无法交付时返回有界错误。记录编码器参数或用后端测试观察 Q80，同次采集计数为一。 |
| 局部截图与页面状态 | 用 DPR 1 和 2、非零滚动位置、贴右下边缘的矩形验证 `clip`。原点为当前视口左上角。解码 PNG 并与确定性视口参考帧对应区域逐像素比对；`source_rect`、输出宽高正确，`resized=false`，页面位置不变。负值、零面积、非整数、越界分别被拒绝。局部 PNG 过大时得到 `IMAGE_PREVIEW_TOO_LARGE`，缩小 clip 后可重试。 |
| 导航与隔离 | 捕获时换文档需丢弃该帧；标签忙或已关闭有明确结果。核对错误不会中断其他正常调用。截图不产生文件、领取 URL 或包含像素正文的常规日志。 |
| 等待和事件 | 确认 `wait_for` 能分别返回 matched、timed_out 和 blocked。用有界事件游标找回延迟弹出的新页或弹窗。事件被淘汰后仍可从 `list_tabs` 得到活跃弹窗和未结操作。 |
| 同步 JS 弹窗 | 让一次 click 触发 confirm，先收到 pending。超过普通调用等待时间、在操作期限内，以独立控制调用 `handle_dialog` 处理，再用 `list_tabs(operation_id)` 取原 click 的终态。验证没有再点一次。另测操作到期、断连、worker 退出后对未知结果的处理。 |
| 网页意外触发文件交互 | 直接操作 file input 或间接触发选择器、下载时，验证对已识别情形给出有界拒绝或按策略收尾。不留下等待调用方处理的文件任务或可领取的路径，不重放原操作。页面业务结果仍按实际观察判断。 |
| 发行包 | Windows 11 x64 和 Ubuntu 24.04 x64 分别用随包 Node、Playwright、Chromium 验证浏览器可启动、Linux 非 root 沙箱可用、退出后子进程收尽。既有系统 Chrome 的本地 smoke 只作开发参考。 |

Filesystem 另以 Windows amd64（本地 NTFS）、Linux amd64 和 Linux arm64 分别运行文件与消费者链路。Browser 的两个 x64 验收包不能覆盖 Filesystem 的 arm64 目标。逐项包验收详见 [Browser 打包方案](../../archive/oiapp-expansion/2026-11-20-initial-scope/browser-packaging.md)。
