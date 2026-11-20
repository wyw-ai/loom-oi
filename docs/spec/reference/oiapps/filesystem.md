# Filesystem oiapp

Filesystem 为远端 Agent 提供一个固定目录内的文本读取、查找、条件修改和静态图像预览。Windows amd64（NTFS）、Linux amd64 与 arm64（ext4）的发行包均已通过 OIS/OIC 调用。[源码与构建](../../../../apps/filesystem/README.md) 完整文件传输和跨实例文件操作不在本轮范围。[扩展目标与验收范围](../../goals/oiapp-expansion.md) · [官方 App 设计索引](README.md)

## 安装、配置和路径

一个 App 实例对应一个 Go worker 和一个固定的 `os.Root`。七个 Action 共享根句柄、写入锁和预算。配置多个根时为每个根建立独立 `app.code` 实例。OIS Tool 用 `source.channel/app/action` 固定目标，调用方只提交业务参数。[App/worker 运行协议](../app-runtime.md)

| 层次 | 核查内容 |
| --- | --- |
| App 清单 | `version: 1`、`app.code: filesystem`。顶层 `runtime.command` 在 Linux 指向 `./runtime/oiapp-filesystem`，Windows 指向 `./runtime/oiapp-filesystem.exe`。各 Action 复用顶层 runtime，handler 默认同名。六个 Action 返回普通 JSON，只有 `view_image` 显式声明 `result_format: content.v1`。[完整 App YAML](examples/filesystem/app.yaml) |
| OIC 实例 | `app_config.<app.code>.root` 必填，必须是受控机器上已存在、可打开的绝对目录。`read_only` 默认为 `true`，提供方显式设为 `false` 才能写。`search`、`limits`、`images` 控制搜索和资源上限。配置由 `start.config` 注入，在 `ready` 前验证并打开根。修改实例配置须重启 OIC。 |
| 接口契约 | `$defs.config`、各 Action 的 `*_input`、`*_output`、共享 `mutation_output` 和 `error` 定义字段及限制。发行包和 OIS Tool 的 `input_schema` 从同一来源展开。输出 Schema 用于文档和契约校验。可选输入通过省略表示，worker 按 Action 应用默认值，拒绝未知字段、显式 null 和非法字段组合。[完整 JSON Schema](filesystem.schemas.json) · [调用参数示例](examples/arguments.yaml) |

优先使用根相对路径。目录操作省略 `path` 时取 `.`，显式空字符串无效。也可传本机根内绝对路径，返回值统一为以 `/` 分隔的根相对路径。Windows 同时接受两种分隔符，Linux 保留文件名中的反斜杠。根句柄约束实际 I/O，拒绝越界路径、异平台绝对路径、NUL 和 Windows 盘符相对、UNC、设备、ADS 等有歧义或特殊的路径形式。无法无损表示的非 UTF-8 文件名在枚举中记录为问题。

列目录展示链接本身。递归查找不跟随链接。读取文本和图像可经过 `os.Root` 支持的根内链接。修改拒绝最终目标为符号链接、重解析点或有多个硬链接的文件。读写对象须为普通文件。根限定的是该实例的路径访问能力，外部进程与同一文件系统上的其他入口仍能独立修改文件。

## 七个 Action

下表列出全部业务入参和成功结果。`?` 表示可省略。同名字段如 `path` 在同一行的输入、输出中分别表示请求路径和已归一化的结果路径。

| Action | 输入 | 成功结果及实现规则 |
| --- | --- | --- |
| `list_directory` | `path?`、`limit?`（200）、`cursor?` | 返回 `path, entries, next_cursor, complete, issues, issues_truncated`。读取一层，含隐藏项。按 UTF-8 名称字节排序后分页，每项含 `name, path, kind, size_bytes, modified_at`。记录取不到的属性。每页重扫名称并核验摘要，名称集合改变时报 `STALE_CURSOR`。 |
| `find_files` | `pattern`、`path?`、`respect_gitignore?`、`include_hidden?`、`max_depth?`、`limit?`（200） | 返回 `path, files, status`，每个文件含 `path, size_bytes`。从请求目录递归，仅返回匹配的普通文件。glob 相对请求目录，使用 `/`、区分大小写，支持 `**`。结果路径相对实例根。 |
| `search_content` | `query`、`path?`、`mode?`（`literal`）、`case_sensitive?`（true）、`file_glob?`（`**`）、`respect_gitignore?`、`include_hidden?`、`max_depth?`、`limit?`（100）、`context_lines?`（0–2，默认 0） | 返回 `path, matches, status`。完整验证候选文件为 UTF-8 且无 NUL 后逐行查找。支持字面值或 Go `regexp`，查询禁止实际换行。每个命中行返回路径、行号、首个命中的原文字节区间、片段起点、截断标记及前后文。 |
| `read_file` | `path`、`start_line?`、`cursor?`、`max_lines?`（200）、`max_bytes?`（65,536） | 返回 `path, text, source_size, bom, range, eof, next_cursor, revision`。首次从第 1 行或指定行开始，续页带相同路径和游标。`start_line` 与 `cursor` 互斥。大文件流式读取，`revision=null`。 |
| `write_file` | `path, content`、`mode?`（`create`）、`create_parents?`（false）、`expected_revision?` | 返回修改回执。`create` 独占创建，可按需创建父目录，禁止传版本。`replace` 要求已有目标且必须传版本，禁止 `create_parents=true`。逐字节写入调用方给出的合法 UTF-8 正文，保留指定换行，拒绝 NUL。 |
| `edit_file` | `path, expected_revision, edits` | 返回修改回执。`edits` 为 1–64 个 `{old_text,new_text}`。每个非空 `old_text` 在同一份原文中须恰好命中一次，所有匹配区间互不重叠。全部通过后一次提交，保留 BOM 和未修改的原始字节。无匹配、多匹配、重叠或版本冲突均在提交前失败。 |
| `view_image` | `path`、`crop?`（整数 `x,y,width,height`） | 返回 `content` 中的一张原生图像和 `structuredContent` 元信息。省略 `crop` 查看整图，指定时从方向校正后的原图裁出矩形，按原像素返回无损 PNG。 |

### 遍历、续读与预算

- `find_files` 和 `search_content` 共用受根约束的遍历。隐藏项和 Git 忽略规则默认取实例设置，默认均开启。读取根内沿途的嵌套 `.gitignore`，被排除的目录直接剪枝。提供方的 `exclude_globs` 始终生效于搜索。两项均无游标，返回的 `status` 含 `complete, stop_reason, visited_entries, scanned_bytes, excluded, issues, issues_truncated`。只有 `complete=true` 的空结果才表示规定范围内无匹配。触及条数、时间、扫描或输出预算时，交付完整条目和未完成原因。
- 目录单次默认最多 20,000 项，名称累计上限 8 MiB。超过上限返回 `DIRECTORY_TOO_LARGE`。搜索默认最多走访 50,000 项、深度 64、总扫描 32 MiB，单候选文件 4 MiB、单行 1 MiB。无法完整校验的候选或超长行不能提供伪完整命中。`limit` 以文件或匹配行计，实例默认最多返回 1,000 项。
- 文本行号从 1 开始，字节列从 0 开始，范围末端为开区间。自文本视图剥离文件开头 BOM，保留原 LF/CRLF；分页不拆开 UTF-8 字符或 CRLF。可编辑大小内先验证完整原始字节再求 SHA-256，摘要包含 BOM 和换行。超过实例 `max_edit_file_bytes` 时采用流式读取，首屏定位也受扫描预算约束。默认单页上限为 64 KiB、2,000 行。
- 游标带签名，绑定 worker 实例、Action、根内路径、调用身份、vMCP 和到期时间（默认 10 分钟）。小文件续读还绑定完整摘要，大文件绑定身份、大小、时间，目录绑定名称集合。变化返回 `STALE_CURSOR`，过期或重启后返回 `CURSOR_EXPIRED`。跨页的大文件读取和目录属性反映各次观察。

## 修改、并发与失败

成功回执包含 `path, state, created, bytes_written, previous_revision, revision, created_directories, warnings`。版本格式为 `sha256:` 加 64 位小写十六进制摘要。内容完全相同时 `state=unchanged` 且 `bytes_written=0`。提交新内容后 `state=applied`。建议的调用顺序是 `read_file` → 传入本次取得的 `expected_revision` 调用 `edit_file` 或 `write_file(mode=replace)` → 再读核对。遇到 `REVISION_CONFLICT` 应重新读取。默认编辑文件上限 2 MiB，一次新正文或编辑各段 old/new 合计上限 128 KiB。

例如，若 `src/main.go` 仅含 `package main` 和一个 LF，`read_file` 的业务结果如下。调用方可原样复制 `revision` 到下一次编辑的 `expected_revision`。

```json
{
  "path": "src/main.go",
  "text": "package main\n",
  "source_size": 13,
  "bom": false,
  "range": {"start": {"line": 1, "byte_column": 0}, "end": {"line": 2, "byte_column": 0}},
  "eof": true,
  "next_cursor": null,
  "revision": "sha256:df1d036cbbf3df46e2045071e082245ece204c7f53ecf0a4e022bff9bb228f47"
}
```

同实例的修改持有同一把可取消的写锁。首次创建目录或发布目标前，先按完整封装预留成功与失败的回执，包含最多 64 层可能创建的目录路径。无法容纳时先返回 `RESULT_TOO_LARGE`。锁内核对身份、类型、属性及版本，把新内容写入目标同目录临时文件，设置可支持的属性、Sync、Close，发布前再次检查身份、内容和取消状态。`create` 通过根内硬链接独占发布，已存在即 `FILE_EXISTS`。替换和编辑通过 `Root.Rename` 发布。外部编辑器可在最后一次检查后修改文件，这组检查只串行化本实例的写调用。

业务失败使用 worker `error` 帧的 `code, message, details`。修改错误还要报告 `details.mutation_state`（`not_applied/applied/unknown`）和已知的 `created_directories`。即使目标尚未提交，之前成功创建的父目录也可能存在。跨过提交点后须核实目标身份与字节，结果未知时先读回，避免自动重放。Windows 共享占用可返回 `FILE_BUSY`。

默认最多 16 个在途调用，共享写锁、图片解码槽和搜索重 I/O 槽各 1 个。等待者计入限额，可取消。默认调用期限 15 秒，配置上限 45 秒。worker 独立处理 stdio 输入，再串行输出完整 JSONL 帧。

## 图像的清晰度与返回形式

1. 仅解码实例根内已有的静态 PNG、JPEG、WebP。按文件头判断，先检查压缩源字节、尺寸和像素数，默认上限依次为 16 MiB、单边 16,384、2,000 万像素。解码前排除动画及不支持的容器。读取三种容器中的 EXIF 方向，覆盖全部八种方向。
2. 整图优先原分辨率。可直接复用且满足硬限的原编码优先，其次尝试同尺寸无损 PNG。仅对完全不透明的整图尝试同尺寸 JPEG Q90、85、80、75。JPEG 优先选达到目标的最高质量，否则选能满足硬限的最高质量。原尺寸候选均不合适时才保持宽高比缩小，回报 `resized=true`。默认概览最小长边为 768，小图不放大。
3. 显式 `crop` 采用方向校正后的源像素坐标，左上为原点，矩形须完全在界内。`width/height` 为正，右、下边界排除。裁剪结果保持原像素尺寸和透明通道，`resized=false, reencoded=true`。超硬限返回 `IMAGE_PREVIEW_TOO_LARGE`，调用方可缩小矩形；越界返回 `CROP_OUT_OF_BOUNDS`。
4. `images.target_encoded_bytes` 默认为 393,216 字节（384 KiB），指 Base64 之前的软目标。略超目标但符合所有硬限的同尺寸原编码或无损 PNG 可直接返回。提供方可另配 `max_encoded_bytes` 和 `max_preview_edge` 作为硬限。检查默认 768 KiB 的 worker 完整帧时，须计入 Base64、元数据、标识、转义及换行，各传输跳还要核对自身的 1 MiB 封装上限。
5. 两端协商 `content.v1` 后，MCP `result.content` 中的 `{type:"image", mimeType, data}` 携带单份 Base64，HTTP 返回 `{call_id,result_format,result}`。元信息放在 `structuredContent`，包含根内路径、源 MIME、方向校正后的源尺寸、`orientation_applied, crop, preview_width, preview_height, resized, reencoded, mime_type, encoded_bytes`。旧 OIC 对新版清单会明确加载失败。新 OIC 连接只支持 JSON 的 OIS 时，图像 Action 对该连接不可用，其余六项可调用。[运行协议](../app-runtime.md)

## 平台、打包与验收

独立 `apps/filesystem/` 模块以 Go 1.26.0 为基线。`cmd/worker` 提供 JSONL worker，`cmd/package` 由同一组 `contract/` 定义生成清单和 Tool 示例，`internal/filesystem/` 分担根访问、遍历、提交和图像处理。平台差异集中在 `*_linux.go` 和 `*_windows.go`。使用 `GOWORK=off`、`CGO_ENABLED=0` 产出平台包，随包交付 `app.yaml`、`runtime/` 中的二进制、配置示例、许可证和校验值。部署机直接加载，无需 Go、rg 或脚本运行时。

| 目标 | 文件系统与属性 | 本轮验证与剩余范围 |
| --- | --- | --- |
| Linux amd64 / arm64 | 普通本地文件系统须支持同目录 link/rename。替换保留普通权限和执行位，暂存文件与源 owner/group 不同时拒绝。 | 两个架构均已在 ext4 上加载运行包，验证链接、FIFO、执行位、并发版本冲突、等待写锁时取消，以及磁盘写满后的文件和版本。两边均已运行 Go 竞态检测。发布点前后的中断仍须逐一验收。 |
| Windows amd64 | 本地 NTFS 普通文件。保留支持的常规属性及经读回核对的 DACL 和保护状态。拒绝只读、命名数据流、重解析点、多硬链接及不能支持的属性。 | Windows 11 已从发行包调用七项，覆盖只读、命名数据流、共享占用及 DACL。提交后异常的回执与副作用仍须按发布清单验证。 |

已在仓库外独立构建并验证从发现文件到条件编辑、再读回的调用链。Windows 实测还用 MCP 视觉消费端从 1280×720 和 1920×1080 图片选区中读回随机批号。多实例同名路径、故障注入和各级预算边界按发布验收清单逐项记录。[分平台运行记录](../../../reports/oiapp-expansion/verification.md) · [版本目标与 Done 条件](../../goals/oiapp-expansion.md)
