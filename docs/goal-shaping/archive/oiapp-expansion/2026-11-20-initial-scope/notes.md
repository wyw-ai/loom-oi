# 官方 oiapp 扩展 · 沟通笔记

- 主题：`oiapp-expansion`
- 讨论标识：`initial-scope`
- 文档父目录：`docs/goal-shaping/`
- 本轮范围：Filesystem、Browser 两个官方 oiapp 的目标、使用过程、能力边界和 Done。
- 状态：已于 2026-11-20（Asia/Shanghai）封板归档。采纳 Filesystem 7 项、Browser 18 项和共享图像结果的设计。跨机原件传输和网页上传下载推迟。
- 启动确认：2026-11-16（Asia/Shanghai），用户明确回复“启动目标塑形并维护文档”。
- 工作分支：`codex/oiapp-expansion`。
- 基线提交：`9f935d8cc460fb8268c423ef677557d7d491b992`，基于获取后的 `origin/beta`。
- 已读取相关基线：`docs/goal-shaping/current/beta/design.md`、`docs/spec/goals/beta.md`、`docs/spec/principles/design.md`、`docs/spec/architecture/system.md` 及 App runtime、终端接口。原 beta 首轮讨论已结束，本主题另建讨论目录。
- 本主题初始无 current 或活跃草稿；本次创建 current、候选设计和笔记，并在文档导航加入入口。

## 沟通记录

### 2026-11-16：调研与工作环境

用户希望参考 pi、OpenClaw、Codex、Hermes 等开源 Agent 的运行环境相关内置工具，理解 loom-oi 的定位及 Bash 作为首个 App 的意义，判断下一版本适合增加哪些官方 oiapp。

调研发现 beta 已有 Bash（Linux）和 PowerShell（Windows）两个独立 App，均提供 exec_command/write_stdin。main 主要保留长期架构材料；本轮以 beta 判断已有能力，不将 main 的 Node、Lease 等长期设计当成已实现前提。

Agent 建议优先 Filesystem 与 Browser，Computer Use 预览或后续、持久 Python 后续。理由是文件和浏览器分别拥有真实环境归属与独立操作语义，能补齐终端之外的常见需求。建议曾用“修改文件、运行程序、浏览器验证并取回结果”说明组合价值；这只是代表性场景，不是用户限定只能面向开发用途。

用户要求创建新的分支和 worktree 后查看本次目标。已基于 beta 创建 codex/oiapp-expansion 分支及独立 worktree，并挂接当前聊天。

### 2026-11-16：启动及范围确认

用户明确确认启动目标塑形并维护文档。随后 Agent 再次询问希望补齐的使用体验，用户纠正：“之前我们不是聊过了吗，就按照你推荐的，这次的目标就是filesystem和browser就可以，还是说你有别的补充了？”

据此已确认本轮交付对象为 Filesystem 和 Browser，不再重开候选 App 选型。Computer Use、Python 及其他 App 不进入本轮目标。Agent 已说明没有新增方向补充，之前的问题重复。

文件、截图及下载产物的可消费交付作为必要配套进入候选设计；具体机制、平台、Action 集合及 Browser 是否接入已有登录态仍需细化。用户没有逐项确认这些技术选择，不能把目标名单确认扩大成所有实现细节定案。

同步记录：已将明确目的与两个 App 的范围写入 current/oiapp-expansion/design.md，其余方案整理到本轮 design.md。原 beta 共识及 docs/spec 保持为既有实现基线。此次启动和范围确认不自动触发 commit、push、部署或功能实现。

### 2026-11-16：Filesystem Action 提案

用户询问“filesystem打算做什么action”。Agent 在既有 Filesystem 范围内提出 9 个首版候选：list_directory、stat_path、find_files、search_content、read_file、write_file、apply_patch、upload_file、download_file。

划分理由：目录浏览与单路径属性查询分别处理；路径查找与内容搜索分别处理，避免参数和返回语义混杂；文本读写编辑与保留原始字节的完整文件传输分别处理。写入可按参数创建父目录，mkdir/move/delete 等独立目录整理 Action 暂缓是 Agent 的收敛建议，未获用户确认。

传输业务必须能跨环境交付实际文件，不能仅返回受控机本地路径或要求模型手工搬运 Base64。分块、产物获取及结果表示尚待技术设计，不在这轮提案中宣称已有完整传输方案。

本次仅更新候选设计及笔记，未把用户的询问视作对 Action 清单的确认；current 中已确认的两个 App 目标保持不变。

### 2026-11-16：对照实际 Agent 工具，修正提案依据

用户追问为何首版选择这些 Action，要求列出当前助手自己的工具与已调研 Agent 的实际工具。此问题是在要求说明依据，不表示认可上一轮 9 个 Action 或要求重新选择 App。

本轮重新核对事实：当前会话主要通过 exec_command/write_stdin、apply_patch、view_image 操作本地执行与文件，读取文本和搜索实际调用 shell 的 Get-Content、rg 等；另外有 Node REPL 和浏览器/桌面控制工具，属于其他运行环境能力。

Pi 核心工具默认包含 read/bash/edit/write，可选 grep/find/ls/powershell；read 支持文本及图像，edit 是旧文本到新文本的精确替换。OpenClaw 当前核验版本的核心文件组实际包括 ls/read/write/edit/apply_patch，此前概述漏列 ls；apply_patch 当前默认开启，不能套用早期“实验且默认关闭”的说法。Hermes 的 file 工具组仅 read_file/write_file/patch/search_files；search_files 合并 files/content 两种目标，patch 默认替换并按模型开放补丁形式；read_file 不读取图片，图像由 vision_analyze 处理。

据此修正候选设计：优先表达读取、写入、局部编辑、列目录、路径查找、内容搜索六类核心能力；图片读取或查看应明确纳入考虑。精确替换与补丁接口需分别说明；stat 是否单列、文件传输是否以 upload/download 工具呈现属于 oi 的增量设计，不能声称由各 Agent 的共同清单直接推出。

上一轮“read_file 仅处理文本”的候选限制已被更新；“首版 9 个”不再作为固定建议。文件完整交付目标仍保留，具体交付机制待设计。本次变更是 Agent 对证据和提案的修订，未写入已确认共识。

补充证据：[Pi read](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/core/tools/read.ts)、[Pi edit](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/core/tools/edit.ts)、[Hermes 文件实现](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/tools/file_tools.py)。

OpenClaw 的额外文件传输工具也已核对：模型可调用 file_fetch、dir_list、dir_fetch、file_write，属于官方随包且默认启用的 file-transfer 插件，但调用仍受节点和路径策略约束；底层有 file.stat，不等于对模型单列 stat。来源：[核心工具表](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/src/agents/core-tool-facts.ts)、[补丁契约](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/docs/tools/apply-patch.md)、[传输插件定义](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/extensions/file-transfer/openclaw.plugin.json)、[传输工具接口](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/extensions/file-transfer/src/tools/descriptors.ts)。

### 2026-11-16：认可核心能力并讨论 view_image

用户回复“这几个可以，view image你打算怎么做？”。结合上一轮明确推荐的六类核心能力，已同步确认读取、写入、编辑、列目录、路径查找和内容搜索；图像查看继续进入具体设计。此确认不扩大为旧 9 项清单或所有编辑/传输技术选择获准。

Agent 建议独立 view_image：path 指向受控环境中的图片，可选 crop 按校正方向后的原图坐标裁剪；worker 读取并生成有界预览，返回图像及源图/裁剪/预览尺寸信息。由 OIC/OIS 的通用结果表示承接，MCP 输出原生 image 块，HTTP 保持等价结果，Browser 截图复用。图片理解由调用方模型完成，不默认新增视觉推理服务。

已再次核对代码：internal/protocol/protocol.go 的 MaxFrameBytes 为 1 MiB；internal/ois/mcp.go 当前将业务结果统一编码为 text，并为对象结果附 structuredContent。MCP 2025-11-25 官方工具规范支持包含 mimeType 和 Base64 data 的 image 内容块。候选需要通用结果扩展及兼容旧 JSON 结果，不能将业务字段误当成协议标记。

有界预览策略需检查最终序列化大小、解码像素和原始文件预算，保留源文件；需要细节时裁剪，不截断图像。支持格式、裁剪参数、缩放默认值及通用结果字段目前仅为 Agent 提案。验收对象须实际支持图像工具结果进入视觉上下文，不能用只返回路径作为完成证据。

本次 current 已同步六类核心能力；其余 view_image 方案只写入候选设计，待后续讨论。

### 2026-11-16：说明图像返回结构

用户询问返回的数据是什么样子。Agent 在候选设计中补充 MCP tools/call 的 result 示例：content 包含简短 text 说明及 image 项（mimeType、完整 Base64 data），structuredContent 包含受控机 path、源图和预览尺寸、crop、resized/reencoded，isError 表达结果状态。图像数据只出现一次。

示例是消费端拟接收的结果，不是已实现结果，也不将 MCP 外层结构直接定为 worker 内部协议。1920×1080 到 1280×720 仅用于展示缩放情况，具体尺寸预算未冻结。本次是接口说明，尚未收到用户对这一数据结构的确认。

### 2026-11-17：按实现复核 Filesystem 设计

用户要求查看现有 Agent 的实现思路，对比本轮 Filesystem Action 是否合理。本轮读取四个项目的固定版本源码及相关测试，并对照 beta 的 worker、Schema 适配和 MCP 转换；没有执行上游测试。调查结论与调整仍是 Agent 建议，不扩大为用户已确认事项。

推荐保留六类能力，加独立 view_image，共七个候选 Action；局部编辑优先收敛成单文件 edit_file，同一原文多段替换，全部匹配成功后提交，不默认引入多文件 apply_patch。list/find/search 保持分开；read/list 可续取，find/search 先做有界结果与未查全标记，不将完整搜索分页作为首版硬要求。

重要事实纠正与依据：

- **Pi**：此前记录“精确替换”过于简化。实现先 exact 再做 Unicode、空白和标点归一化，唯一性判断也会使用归一化文本；多段替换以同一原文为基准并拒绝重叠。read 为计总行数仍可能扫描整文件，UTF-8 解码不严格；write/edit 的同路径队列仅是进程内协调，实际 writeFile 不是临时文件替换。ls/find/grep 有 limit，不提供完整分页游标。图片默认 Base64 预算为 4.5 MiB，不适用 oi 现有帧限额。证据：[编辑匹配](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/core/tools/edit-diff.ts#L245-L293)、[整文件计行](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/core/tools/read.ts#L213-L226)、[写入](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/core/tools/write.ts#L33-L82)、[find 限额](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/core/tools/find.ts#L328-L342)、[图像预算与降质](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/utils/image-resize-core.ts#L32-L165)。
- **OpenClaw**：read 支持图像块和有界文本读取；ls 按完整条目分页，适合借鉴。edit 具备修改计划与写后核对，但队列不能约束外部写入；多文件 patch 预检后逐项提交，无普遍回滚保证。完整原文件传输由独立插件处理。证据：[ls 游标](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/src/agents/sessions/tools/ls.ts#L60)、[有界读取测试](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/src/agents/sessions/tools/read.large-files.test.ts#L63)、[edit](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/src/agents/sessions/tools/edit.ts#L144)、[patch 提交](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/src/agents/apply-patch.ts#L596)、[写入队列](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/src/agents/sessions/tools/file-mutation-queue.ts#L11)。
- **Codex**：工具组合偏向 shell + apply_patch + view_image；补丁支持多文件操作，匹配也有空白/标点容错。提交逐项执行，失败记录累计变更；源码明确指出写入失败也可能已经改变目标。view_image 读取目标执行环境的文件，转成真实 InputImage；模型相关图像处理由自身 runtime 管理，不适合搬成 oi worker 的模型分支。证据：[补丁容错](https://github.com/openai/codex/blob/710bee73a89385aaf2afb41d885edf82473d941f/codex-rs/apply-patch/src/seek_sequence.rs#L35-L110)、[逐项提交及失败变化](https://github.com/openai/codex/blob/710bee73a89385aaf2afb41d885edf82473d941f/codex-rs/apply-patch/src/lib.rs#L391-L469)、[图像读取与输出](https://github.com/openai/codex/blob/710bee73a89385aaf2afb41d885edf82473d941f/codex-rs/core/src/tools/handlers/view_image.rs#L155-L273)。
- **Hermes**：read 的分页续读值得借鉴，但长行尾部可能无法由 offset 取回。先读后写及重复读取缓存依赖 Agent 身份、已读范围、上下文压缩状态；远端后端还有不同检查，不能照搬到独立文件服务。patch 名称覆盖替换和 V4A 模式，后者预检全部通过也仍可能提交到一半失败。search_files 合并路径/内容搜索，但路径 glob 实现会取 basename，不是完整相对路径匹配。证据：[分页与长行](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/tools/file_tools.py#L67-L115)、[读取跟踪](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/tools/file_tools_read_tracking.py#L192-L244)、[覆盖检查及远端例外](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/tools/file_tools_write_guards.py#L613-L653)、[补丁部分成功](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/tools/patch_parser.py#L353-L400)、[glob 实现](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/tools/file_operations_search.py#L1079-L1188)。

设计已补充：显式创建/覆盖意图、候选版本条件及其保证边界；不要强制每次分页算全文件哈希；实际扫描与输出分别有界；不省略“重复读取”的正文；固定实例路径基准、统一目录和链接规则。可信身份由现有 OIS context 提供，但匿名同 vMCP 共享身份。read/list 的续取、find/search 的有界查询和有损片段应分别描述，不能把所有情况笼统称为分页。

view_image 的独立接口及通用图像输出建议保持。crop 是 oi 面向预览预算和局部细节提出的设计，不是声称 Pi/OpenClaw 已有的参数。原图字节交付另行设计；不为满足帧预算无限缩小到失去使用价值。

本次只更新候选设计和笔记；current 已确认的六类核心能力保持原状，未实现、提交或推送功能代码。

### 2026-11-17：Filesystem 详细技术设计

用户明确要求详细设计 Filesystem oiapp 的目录结构、配置、Schema、实现语言、跨系统和内部逻辑，并授权补充遗漏事项。本轮继续同一主题，不重新询问目标塑形，也不将设计请求解释为开始功能实现。

新增技术附件 [filesystem-design.md](filesystem-design.md) 和机器可读 [filesystem.schemas.json](filesystem.schemas.json)，主 design.md 保留整体目标并链接附件；current 不吸收未确认的具体取舍。用户可以从技术附件独立理解完整 Filesystem 候选方案，根文档导航同步更新。

关键候选选择：独立 Go worker，一个实例固定一个 root；七个 Action 共享实例，多个目录使用不同 app.code。Filesystem 独立模块采用 Go 1.26.0 基线，使用当前已有的 Go 1.26.3 开发/CI；不改动根模块或既有 App 的 go 1.25 声明。正式目标为 Linux/Windows，macOS 因当前 OIC 托管未覆盖而不宣称完整支持。

Schema 包含 29 个定义：配置、七个输入、七个动作使用的输出结构及共用类型/错误。worker 深度验证参数，不能依赖 OIC 对 items、enum、范围或未知字段执行检查。结果 Schema 是契约测试材料，不是现有 App YAML 已支持的 output_schema。

默认 2 MiB 以下的文件可全文有界校验并返回真实 SHA-256；大文件流式读取、revision=null，首版拒绝超限源文件的 edit/replace。write 输入或 edits 文本合计默认最多 128 KiB，与可编辑旧文件大小分开。replace/edit 必须带 expected_revision；创建使用 no-clobber，所有修改在单 worker 写临界区串行，不宣称跨进程 CAS。长行用字节游标无损续读；目录采用明确 2 万项上限的排序分页；搜索提供有界结果及是否查全，不提供搜索快照。

搜索采用 Go 自有遍历和 os.Root 访问层；doublestar/go-git 库只做匹配，不调用其独立文件遍历或全局配置读取。图片采用标准库与 x/image，方向、裁剪、编码预算明确；依赖通用 content.v1 结果扩展，包含 App 显式 result_format、worker 和 OIS/OIC 能力协商、旧端不支持时明确不可用，以及既有 JSON 结果兼容。

新增核验来源：[Go os.Root 的路径边界](https://go.dev/blog/osroot)、[Go Root/文件 API](https://pkg.go.dev/os)、[Go regexp](https://pkg.go.dev/regexp)、[doublestar](https://pkg.go.dev/github.com/bmatcuk/doublestar/v4)、[go-git ignore](https://pkg.go.dev/github.com/go-git/go-git/v5@v5.19.3/plumbing/format/gitignore)、[x/image WebP 解码](https://pkg.go.dev/golang.org/x/image/webp)、[x/image v0.48.0 工具链要求](https://raw.githubusercontent.com/golang/image/v0.48.0/go.mod)、[go-git v5.19.3 工具链要求](https://raw.githubusercontent.com/go-git/go-git/v5.19.3/go.mod)、[PNG EXIF](https://www.w3.org/TR/png-3/#eXIf)、[WebP 容器](https://developers.google.com/speed/webp/docs/riff_container)。具体代码仍需实施和平台验证；本轮不把源码调查作为运行验收证据。

本轮设计复核已修正：图像输出必须恰好有一个 image；搜索取消有独立 stop_reason；BOM/EOF/长行坐标统一；目录游标增加完整有界名称集合摘要；大小限制之间校验；Windows 明确使用 Root.Rename 并界定属性支持。协议设计区分本地 ready 与某条连接的格式可用性，避免禁用图片时连带禁用六个文本 Action；另补齐原始 JSON 字符串检查及发送前完整帧检查。

已完成设计工件检查：29 个 Schema 定义结构有效，25 个输入/输出正反例及 3 个查询换行约束用例通过；9 个 JSON/YAML 示例可解析，配置及真实 read_file 响应示例通过对应 Schema；30 个本地文档链接存在。验证库仅装在系统临时目录，项目依赖未改。没有运行尚不存在的 Filesystem 实现，也没有将这些检查当成 Windows 文件发布或真实 MCP 图像链路验收。

### 2026-11-17：确认 Filesystem，提出 Browser Action

用户明确回复“可以这个方案可以，再看下browser，这次打算做什么action”。据此接受上一轮 Filesystem 技术方案及接口设计，转入 Browser 讨论；没有要求开始功能编码、提交或发布。

已将 Filesystem 完整技术方案与 29 个 Schema 定义同步到 current/oiapp-expansion，更新 current/design.md 的已确认范围；草稿技术附件标为已确认的讨论副本，导航指向 current 基线。主草稿移除过时的“Filesystem 接口待定”表述，保留完整方案链接与简要说明。

Browser 新建议为 20 个窄职责 Action：create_session、list_sessions、close_session；list_tabs、open_tab、close_tab；navigate；snapshot、screenshot；click、fill、select_option、set_checked、press_key、hover、scroll；wait_for；handle_dialog；upload_files、downloads。list_sessions 用于恢复当前身份的会话句柄；navigate 合并 goto/back/forward/reload；downloads 合并 list/status/cancel。Browser 名单尚未获得用户确认，不写入 current 共识。

候选默认是 App 管理的专用 Chromium，可使用提供方配置的持久 profile 保留登录；同一个 profile 不能并发占用。操作显式带 session/tab，ref 绑定页面观察身份；不增加全局切换“当前标签页”。snapshot 同时提供正文和控件，事件监听从会话创建起建立，dialog/chooser/download 能在触发动作后接续。下载落到受控机不等于传回 Agent，完整文件交付仍为待细化配套。

已定向复核：OpenClaw 固定提交 202514ee 的对外入口是一个 browser 工具，其 action/act 内包含诸多子操作，不能称为19或20个独立工具；Hermes 固定 aa6d201 实际有 exec/cdp/dialog/vault 四种浏览器工具，其中部分按配置开放。exec 会执行宿主 Python，且明确要求 terminal toolset，不能只因工具少就视为与结构化浏览器 Action 等价。Playwright MCP 的官方目录提供较细粒度交互工具；本轮仅参考能力和事件处理，不照搬全部调试/网络/凭据工具。

来源：[OpenClaw 顶层动作](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/extensions/browser/src/browser-tool-action-facts.ts)、[act 子操作](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/extensions/browser/src/browser/act-kinds.ts)、[已有会话模式限制](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/docs/tools/browser/existing-session.md)、[Hermes 工具表](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/website/docs/reference/tools-reference.md#L15-L26)、[Hermes exec 的 terminal 条件](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/tools/browser_use_cli.py#L1030-L1032)、[Playwright MCP 工具目录](https://github.com/microsoft/playwright-mcp/blob/main/README.md#tools)（本轮查阅 main，2026-11-17）。未运行这些上游工具或接管任何真实浏览器。

### 2026-11-17：接受 Browser 能力方向，主要参考 Codex

用户回复“这个可以主要学一下codex，codex的浏览器操作做得很不错”。据上下文接受上一轮 20 项 Browser 能力方向，并明确主要参考 Codex 的浏览器操作体验；不据此声称后续新提出的 target Schema、后端、SDK 或坐标回退也已确认。已同步 current/design.md 的能力清单与参考方向，Filesystem 基线未变。

本轮定向读取本机官方 bundled Browser 26.1111.21308 的静态文档/API 声明，并核对公开官方 Browser 说明与已固定的 Codex 仓库工具注册。不控制真实浏览器、不读取浏览历史或 profile，不将静态调查称为运行验证。

主要核验及设计推导：

- Codex 桌面 Browser 与公开 CLI 默认工具不同。固定提交的注册逻辑区分 core、MCP、extension executors 与 dynamic tools；不能把桌面 API 当作开源 CLI 的一组默认浏览器工具。依据：[官方 Browser](https://learn.chatgpt.com/docs/browser)、[固定提交注册流程](https://github.com/openai/codex/blob/710bee73a89385aaf2afb41d885edf82473d941f/codex-rs/core/src/tools/spec_plan.rs#L150-L174)。
- 本机文档强调持久 Browser/Tab 句柄、一次客户端执行可组合多个动作，以及操作后选择必要的状态或截图。OI 保留 20 个明确 Action，让客户端按自身能力组合；不因此新增宿主 JS/Python 执行服务。是否提供薄 SDK 仍待定。依据：[API 使用指引](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/api-use-behavior.md)、[AX 工作流](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/accessibility.md)。
- AX 指引在相应 API 可用时推荐索引定位、刷新状态与差量输出；但 documents.json 条件注入该指引，api.json 的 Tab.ax 标注后端默认不可用性，不能泛称所有 Codex 浏览器都默认 AX 优先且可用。OI 定义自己的有界 snapshot，并暂缓差量输出。依据：[条件文档](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/documents.json)、[API 声明](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/api.json)。
- Playwright 包装接口有语义 locator、frame、受限只读 evaluate，不等于完整 Playwright，也不是自动证明底层使用 Playwright MCP。建议在 ref 外加入结构化语义 target，唯一匹配，避免隐式选第一项；重复任务可重新解析同一语义条件。浏览器视口坐标是本轮新增候选回退，绑定截图及坐标映射，不扩展到桌面操作。服务端 batch、任意 evaluate 仍不列首版 Action。
- 条件等待和事件先于触发动作建立的方向保留；补充 beforeunload 类型、可用弹窗响应、chooser 多文件能力及有界事件缓存遗漏。设置文件、网站上传成功、下载落盘与交付消费者分别表述。公开 Browser 说明与本机通用上传 API 的适用范围存在差异，不据本机接口声称所有 Codex 后端支持上传。依据：[上传指引](C:/Users/c6321/.codex/plugins/cache/openai-bundled/browser/26.1111.21308/docs/file-uploads.md)、[公开能力说明](https://learn.chatgpt.com/docs/browser#browser-use)。

工作设计已吸收上述候选改进；current 仅同步用户实际接受的能力范围与主要参考方向。没有新增功能实现、提交或推送。下一步仍是 Browser 技术设计，非重新选 App 或重开目标塑形。

## 2026-11-18：Browser 本地最小 smoke

使用本机已有 Node v24.13.0 与 Codex bundled runtime 中的 Playwright 1.62.1，显式启动 Chrome 157.0.8103.79 的临时无头实例，通过临时 localhost HTTP 页面验证导航、标题读取、role locator 填写与点击、结果等待、ARIA snapshot 和内存 PNG 截图；约 2 秒全部通过，截图为 8,070 字节。smoke 未安装依赖、未修改项目、未使用个人 profile，浏览器与 HTTP 服务已关闭。Playwright 配套 Chromium（chromium-1234）未安装，项目默认模块解析也找不到 Playwright；本次仅验证 Windows + 系统 Chrome 的基础链路，尚未覆盖配套 Chromium、Linux、oiapp JSONL 接入、持久会话及上传/下载/dialog。独立 oiapp 发行仍需自行定义依赖，不能依赖 Codex 安装目录。

## 调研依据与可行性约束

2026-11-18 补充 Windows 本地验证：同名按钮重新创建后，旧 ElementHandle 的 click 报 detached，新按钮点击数为 0。同步 confirm 出现后，原 click 在 100 ms 检查时仍 pending，另一路执行 dialog.accept 后原 click 才结束且页面显示 Accepted。以上验证 Playwright + 系统 Chrome 行为，不代替 oi 的 ref 和跨调用调度端到端验收。

已有调研在同一会话完成，不需要重新广泛搜索。需要验证会影响设计的事实时，再定向核对。

| 来源 | 对本轮的主要启发 |
| --- | --- |
| [Pi 工具实现](https://github.com/earendil-works/pi/blob/22fc026b872b4a7df22ffa94e8fa00b873128054/packages/coding-agent/src/core/tools/index.ts) | 默认文件读写编辑与 Bash 分开，另有搜索/目录工具；相关文件操作可以组成一类 App。 |
| [OpenClaw 工具](https://github.com/openclaw/openclaw/blob/202514ee61a09f929f587f43820eb125cc51aed8/docs/tools/index.md)、[文件传输](https://docs.openclaw.ai/nodes/file-transfers) | Browser 管理真实实例与状态；文件传输不能依赖被截断的 shell 输出。 |
| [Codex 工具注册](https://github.com/openai/codex/blob/710bee73a89385aaf2afb41d885edf82473d941f/codex-rs/core/src/tools/spec_plan.rs) | 持续进程、文件修改、图像读取各有明确接口；不同客户端扩展不等于核心默认能力。 |
| [Hermes 工具参考](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/website/docs/reference/tools-reference.md)、[Browser](https://github.com/NousResearch/hermes-agent/blob/aa6d201dfdd530b4bcc4df377181b6e7170e17eb/website/docs/user-guide/features/browser.md) | 文件、浏览器和执行后端的状态归属清晰；托管浏览器与已有 profile 接入需区别表达。 |

在当前 beta 中，[worker 协议](../../../../spec/reference/app-runtime.md)规定单帧上限 1 MiB、OIC 单次调用期限 60 秒；[MCP 转换](../../../../../internal/ois/mcp.go)当前生成 text 和 structuredContent，未直接输出 image 块。这些是实现可行性约束，不是用户要求维持的永久产品限制，也不自动授权建设完整 Artifact 平台。

## 2026-11-18：确认实施路径，详细设计 Browser

用户明确接受前一轮的专用无头 Chromium、20 项、三种 target、可接续事件和三阶段交付，并要求比照 Filesystem 编写详细技术方案。已同步到 current 的是上述决策；机器 Schema、文件夹和模块、精确配置、双平台构建与测试矩阵作为本次设计附件，不代表已有实现。见[技术设计入口](browser-design.md)。不调整已确认的 Filesystem 设计。

工件检查：Draft 2020-12 元 Schema、89 个命名定义及本地引用有效，20 项逐项说明与输入/输出定义一一对应；已校验 20 组合法输入/输出。另用持久化的 8 次调用样例复核 16 个载荷、13 个应拒绝的分支和配置样例。设计补入控制通道保留席位、长页面描述可省略但 pending ID 不可丢、文件选择与上传成功分别表述。此处均为契约验证，不是平台包或 OI 端到端验收。

## 当时的未决问题（保留历史）

### 本轮 shaping 缺口复核

用户要求参照已有 shaping 复核 Filesystem 与 Browser 还差什么。以 current 的范围、草稿的联合 Done、机器 Schema 和 beta 代码为准，仅同步复核发现与建议，未修改已确认设计或产品代码。

1. 目前明确的是受控机内的文件操作。完整原件双向跨机交付仍未定义；原生 image、下载绝对路径、SHA-256 元信息都不是原件交付机制。先区分组件 Done、同机联合 Done、跨机交付 Done，避免因一个目标未定卡住其他可独立实现的能力。共同场景和登录首用见[候选设计](design.md)。
2. Filesystem 允许 64 层 create_parents 以及 65536 字节的实例帧预算，回执又需包含实际创建的目录。内存测算：64 个各 50 ASCII 字节的层名，目标相对路径 3265 字节，仅前缀目录 JSON 数组便有 106209 字节（未创建文件）。在第一个 mkdir 前校验成功和失败帧的最坏长度，不能只在目标发布前检查。另需统一原始重复键检查：现有 [MCP tools/call](../../../../../internal/ois/mcp.go) 和 [HTTP 入口](../../../../../internal/ois/invoke.go) 将 arguments 解成 map，worker 不能检测上游已丢掉的重复字段。
3. Browser 草稿默认每文件上传 50 MiB、合计 100 MiB，全部提交 buffer；锁定的 Playwright 1.62.1 以所有 buffer 的总字节数比较，达到门槛就拒绝。见[判定及调用点](https://github.com/microsoft/playwright/blob/v1.62.1/packages/playwright-core/src/client/elementHandle.ts#L232)、[常量](https://github.com/microsoft/playwright/blob/v1.62.1/packages/playwright-core/src/client/fileUtils.ts#L19)。建议保留简单 buffer 路径、总额默认 48 MiB 且配置硬限低于 50 MiB（含单文件约束），或如确需维持现额度，使用私有暂存副本并验收清理；不重新打开原路径。补合计恰好达阈值和两个各 30 MiB 的用例。此外以超过 10 秒再处理 confirm 验证底层操作期限与 5 分钟 pending 生命周期确实可分别控制。
4. content.v1 已有设计，缺实现与真实消费者验证。当前仓库只有 Bash、PowerShell manifest，未见上述两个产品 App；现有 Windows 系统 Chrome smoke、JSON 示例、平台矩阵分别是可行性证据、契约检查和计划，均非发布验收。发布证据应按平台/能力标明；Filesystem 的 Linux arm64 也不能用 Browser 的 Linux x64 验收代替。

- 详细契约需按附件完成实现及双平台端到端验证；系统 Chrome 的已有 smoke 不能代替发布包验收。
- content.v1 的通用格式协商和真实模型可见图像仍待实施；完整原文件跨机器交付仍为独立配套。

### 原件交付提案

用户追问文件部分如何做。提出同机专用工作根先行、跨机可选 put/get 配套随后交付，用户侧客户端搬运字节；明确 CLI/SDK 运行位置、REST 固定绑定、身份隔离、续传及无覆盖发布。见[文件交付提案](file-exchange.md)。未将机器传输入口列为已确认范围，未开始实现。

### 重新界定为 OIS API/MCP 服务

用户否定以专用 CLI/SDK 增加消费端负担的方向，明确希望既支持控制端与受控端双向、也支持受控端互传，并倾向不由 OIS 存储文件。上一段为已被替代的历史建议。修订草稿推荐 OIS 提供统一 API/MCP，通过已有 OIC 出站控制连接协商、独立 HTTP 数据请求在线中转；不建议为此引入中心文件库、端对端入站端口或模型可见的分块。此架构为建议，并非已确认实现。

### 2026-11-19：评估首版暂缓文件传输和 Browser 截图

按用户提出的前提进行影响分析：若跨机原件传输和网页上传下载均暂缓，同时不做 screenshot，Browser 从 20 项减到 17 项；6 项基本任务不变，原第二阶段保留 10 项，再加 handle_dialog。snapshot 是正文、控件及引用的结构化观察，保留。受影响的是共享回执和配置：移除 chooser/download 资源、事件、计数、关闭参数、图像和文件能力声明；保留新标签、导航、dialog、pending operation 的恢复。网页意外触发文件交互时不选文件、不交付下载件、不留不可处置的 pending，不因此重放点击。发布包仍需 Node/Chromium，但不再为上传打包文件 helper，Browser 不再依赖 content.v1。验收改为读页、填表、提交并读到结果，含确认框与新标签；精确视觉布局不在该验收范围。Filesystem 的 view_image 是查看已有图片，不是网页截图；若要一并暂缓图像结果，才需将 Filesystem 从 7 项收缩为 6 项并整体推迟 content.v1。本次为条件评估，未重写已确认基线或两份 Schema。

### 2026-11-19 · 最终评审范围与图像结果

用户先确认 Filesystem 7 项、Browser 17 项、推迟文件传输与网页上传下载，并要求核实 MCP 如何返回图片。OI 当前实现将业务对象编码进 text；本仓库协商的 MCP 版本 2025-11-25 已允许顶层 result.content 中出现 type=image、mimeType、data。仅安装 App 不足以让当前 OIS 输出原生图像。随后用户提出截图也可沿用 Base64，并委托重新评估本版目标。

本次建议保留现有 24 项，再增加只截当前标签可见区域的 screenshot，合计 7 + 18。图像协议改动、帧检查与真实视觉消费者验收由两项共用。Browser 的文件资源、网页上传下载、跨机原件交付继续暂缓。已将这套待最终评审的设计同步到[本版总览](../../../current/oiapp-expansion/design.md)、[验收项](../../../current/oiapp-expansion/acceptance.md)和对应技术附件。尚无两个 App 的正式发行或 OIC/OIS 图像链路的运行验收。

本轮工件验证覆盖 3 份 Draft 2020-12 Schema（Filesystem 两份副本的定义一致）、所有本地定义引用、评审表中的 25 个 Action 名称，以及 [Browser 流程样例](browser-flow.example.json) 9 次调用的 18 个输入/输出。样例截图含可解码的 1280×720 PNG，编码 3766 字节，与元数据一致。该样例属于契约工件，正式发行包与模型通过 MCP 实际看见图像另按验收项执行。

### 2026-11-19 · 对照 Codex 核实图像预算与小字可读性

核对了本机桌面版匹配的开源 view_image、浏览器插件的显式截图路径和真实 MCP 图像返回，比较 1280 与 1920 宽度的中文报表。图文页的 1920×1080 PNG 按原草案会缩到 785×442；同页原尺寸 JPEG Q80 为 242.8 KiB，文字区原像素 PNG 为 18.7 KiB。建议以 384 KiB 作默认传输目标，优先保留像素，再提供局部原尺寸观察。此为待评审建议。证据、对照图、适用版本和验收建议见[图像预览预算复核](image-preview-review.md)。

### 2026-11-19 · 确认并同步两个图像 Action

用户确认 384 KiB 为默认目标，截图优先原尺寸，PNG 超出有效帧预算时用 JPEG Q80，并支持 clip 查看局部小字。已将两个图像接口的行为、配置、Schema 和发布用例同步到[最终设计](../../../current/oiapp-expansion/design.md)与[验收项](../../../current/oiapp-expansion/acceptance.md)。Filesystem 的 crop 使用方向校正后的文件像素；Browser 的 clip 使用当前视口的 CSS 像素。显式区域成功时均按原尺寸返回无损 PNG，超限提示请求更小区域。整屏回退使用同一捕获帧。

本轮复核了两份 Filesystem Schema 和一份 Browser Schema，三份均通过 Draft 2020-12 元校验及引用检查。Browser 的 11 次调用、22 个输入输出样例均通过校验，整屏 PNG、整屏 JPEG、局部 PNG 的字节和尺寸均可实际解码。额外用 531,099 B 的原尺寸 PNG 验证两项接口都允许超软目标，两个示意完整帧均小于 768 KiB。另有同帧编码实验见[打包验证记录](browser-packaging.md)。正式 OIC/OIS、真实视觉客户端及 Windows/Linux 发行包的发布验收尚待实现。

### 2026-11-19 · 逐项 YAML 与参数评审

根据逐项查看 YAML、支持参数和两个 App 形态的要求，补齐[两份完整清单、双平台 OIC 配置、OIS 绑定和 25 份参数样例](../../../current/oiapp-expansion/examples/README.md)。每组调用参数在原始合同、App 清单、OIS Tool 三处的输入 Schema 上分别通过验证。仓库 Go 配置读取器成功读取两份 OIC 配置和一份 OIS 配置。当前 OIC 对原始新清单报未知字段 `result_format`。在临时测试副本中仅移除图像格式声明后，两份清单通过当前 OIC 结构解析，各使用一个根 runtime。此次属于设计工件验证，正式 worker、图像协议和双平台运行仍待发布验收。

### 2026-11-20 · 目标封板，转入开发规范

用户确认结束本轮并归档。采纳的范围为 Filesystem 7 项和 Browser 18 项，其中 `view_image` 与 `screenshot` 共用有界图像结果。384 KiB 为默认编码目标，整屏优先保留原尺寸，文件的 `crop` 和网页的 `clip` 可查看局部小字。历史草案中的文件交换服务、网页上传及下载均留待以后讨论。后续开发以[扩展目标](../../../../spec/goals/oiapp-expansion.md)和[接口契约](../../../../spec/reference/oiapps/README.md)为入口。

本轮完成设计及配置样例校验。两个 worker、宿主与中心的图像协商和交付、目标平台发行包、真实 MCP 视觉消费者读回小字均待实现和验收。依赖锁定版本与哈希以实际构建结果为准。
