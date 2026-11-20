# Filesystem oiapp 技术设计（已确认）

状态：Filesystem 设计于 2026-11-17 确认，`view_image` 的原尺寸、软目标及局部细节规则于 2026-11-19 确认。本文记录设计基线，运行验收待实现后执行。范围见[主设计](design.md)，沟通过程见[笔记](../../archive/oiapp-expansion/2026-11-20-initial-scope/notes.md)。

本设计以 beta 提交 `9f935d8` 的 App/worker 边界为基础。交付一个独立 Go App，首版正式支持 Linux 和 Windows，提供七个 Action：read_file、write_file、edit_file、list_directory、find_files、search_content、view_image。

## 1. 关键选择和边界

| 项目 | 本设计选择 | 理由 |
| --- | --- | --- |
| 实现语言 | Go；本 App 独立 `go.mod` 使用 Go 1.26.0 基线 | 仓库 CI 和当前开发机已有 Go 1.26.3；独立模块可以使用满足新依赖要求的版本，不要求主工程或已有 App 一并升级。 |
| 部署 | 按 GOOS/GOARCH 构建的单 worker 二进制，无 CGO | 受控机器无需 Node、Python、Go、rg、fd 或 shell 来完成文件操作。 |
| 实例 | 一个 worker 对应一个固定 root，七个 Action 共享它 | 配置、路径和写入协调有明确归属；不继承终端最后一次 cwd。 |
| 多目录 | 不同 app.code 的多个实例，可复用同一二进制 | 沿用已有 OIC 实例规则，不引入全局 Workspace 或虚拟挂载体系。 |
| 搜索 | Go 遍历与读取，glob/ignore 库只做字符串匹配 | 所有真实 I/O 经过同一根目录访问层，结果与安装了哪个外部命令无关。 |
| 修改 | 创建 / 条件替换 / 单文件多段编辑 | 避免把多文件 patch 的格式、增删移和部分成功同时带入首版。 |
| 图片 | 根内已有静态图片的本地解码、方向处理、裁剪与预览；原生图像结果 | 经显式 OIC/OIS 扩展交付 MCP 图像块，由支持视觉的消费端使用；不调用额外视觉模型。 |

首版面向普通本地文件系统上的文本、目录和静态图片：Linux 要求支持同目录 link/rename，优先验证 ext4；Windows 限本地 NTFS。Linux 首发 amd64/arm64，Windows 首发 amd64；每个发行目标必须有实际运行验证。macOS 不列为本轮正式支持：当前 OIC 进程托管只有 Linux/Windows 实现，不能用 App 能交叉编译代替完整链路支持。

本轮不提供独立 stat/mkdir/move/delete、跨文件事务、动态 cwd、任意二进制写入、文件系统索引。父目录创建附属于 write_file。保留已确认的七个 Action，其中 view_image 仅查看实例 root 内已经存在的静态图片。完整原件传入/取回、Browser 网页上传下载及跨 App 联合文件流均暂缓，不作为本版配套或验收条件；图像预览和分页文本不构成任意文件传输协议。view_image 与 Browser 当前视口 screenshot 两项共享通用原生图像结果链路，分别保留各自输入范围和验收。

## 2. 源码与发行目录

~~~text
apps/filesystem/
  README.md
  go.mod
  go.sum
  app.yaml                         # 从 contract 生成的开发用 Linux manifest
  build.sh
  build.ps1
  cmd/
    worker/main.go                 # 参数、stdio、退出码
    package/main.go                # 生成平台运行包与部署样例
  contract/
    schemas.json                  # config、7组input/output、error
    actions.json                  # 名称、description、handler、result_format
  internal/
    protocol/
      frame.go                    # 本 App 自有线协议类型
      content.go                  # content.v1 类型与检查
    worker/
      serve.go                    # start/ready/invoke/cancel/EOF
      dispatcher.go               # handler查表与严格参数验证
      calls.go                    # call_id、并发槽、context取消
      writer.go                   # 串行发送与完整帧预算
    filesystem/
      config.go
      service.go                  # 持有Root、写锁、cursor密钥、资源限额
      errors.go
      paths.go
      paths_linux.go
      paths_windows.go
      cursor.go
      revision.go
      text.go                     # UTF-8、BOM、行/字节位置
      read.go
      write.go
      edit.go
      list.go
      walk.go
      ignore.go
      find.go
      search.go
      commit.go
      commit_linux.go
      commit_windows.go
      image.go
      image_metadata.go           # 有界容器元信息与EXIF方向
      *_test.go
  testdata/
    text/
    images/
    protocol/
  examples/
    oic.linux.yaml
    oic.windows.yaml
    ois.tools.yaml
  runtime/                        # 本地构建产物，忽略提交
  dist/                           # 平台运行包，忽略提交
~~~

继续保持与 Bash/PowerShell 一致的独立性：不导入主工程 internal、不使用本地 replace、不依赖 go.work。协议以版本化线格式约束，不因为三个 App 都用 Go 就引入尚未发布的共享 SDK。

`contract/schemas.json` 是接口定义源；已确认的接口工件见 [filesystem.schemas.json](filesystem.schemas.json)，包含 29 个定义。构建器展开输入 Schema 的本地 $ref，生成 app.yaml 和 OIS Tool 示例，防止两端手工维护漂移。输出 Schema 用于契约测试和文档；当前 App 格式没有 output_schema，不把它直接添加到 manifest。

当前开发用 app.yaml 与发行包 manifest 由同一份 actions/schema 生成。发行目录：

~~~text
filesystem-linux-amd64/
  app.yaml
  runtime/oiapp-filesystem
  examples/
  README.md
  LICENSES/
  checksums.txt

filesystem-windows-amd64/
  app.yaml
  runtime/oiapp-filesystem.exe
  examples/
  README.md
  LICENSES/
  checksums.txt
~~~

build.sh/build.ps1 调用同一个打包入口，设置 GOWORK=off、CGO_ENABLED=0，明确目标 OS/架构。打包器只生成不同 runtime.command，不复制两套业务 Schema。构建结果保留依赖清单、许可证和校验值；OIC load 只加载运行包，不现场编译或下载依赖。

## 3. 依赖与语言层分工

| 能力 | 推荐实现 |
| --- | --- |
| JSONL、配置、hash、签名、UTF-8 | encoding/json、crypto/sha256、crypto/hmac、unicode/utf8 等标准库 |
| 根目录访问 | os.Root，全部 I/O 以根句柄或根内已打开目录句柄为依据 |
| 正则 | Go regexp，逐行、线性时间匹配语义；不支持回溯引用和 look-around |
| 路径 glob | github.com/bmatcuk/doublestar/v4，只调用 ValidatePattern/Match |
| ignore | github.com/go-git/go-git/v5/plumbing/format/gitignore，只用 ParsePattern/Matcher |
| 图像 | image/png、image/jpeg；golang.org/x/image/webp 解码；x/image/draw 缩放 |
| Windows 文件元信息/发布补充 | golang.org/x/sys/windows，集中在平台文件中 |

候选兼容版本为 doublestar v4.10.4、go-git/v5 v5.19.3、x/image v0.48.0；后两者要求 Go 1.26。实施时写入 go.mod/go.sum 并实际构建、检查依赖，不使用“构建时自动取 latest”。这些版本目前只核对了 API/工具链要求，尚未在本 App 中构建。

不使用 glob 库自己的 Glob/Walk，也不使用 gitignore 库的全局配置/递归文件加载器；由 worker 经根访问层读取文件，再交给纯匹配函数。这使链接、范围与扫描预算始终由 App 统一控制。

不引入通用后端插件接口、云存储适配或数据库。filesystem 是本机能力，远程调用由现有 OI 链路完成。

## 4. 三层配置，避免职责混杂

### 4.1 App manifest：声明能力与入口

以下是发行 manifest 的节选，输入 Schema 展开后放入对应 Action：

~~~yaml
version: 1
app:
  code: filesystem
runtime:
  command: [./runtime/oiapp-filesystem]
actions:
  read_file:
    description: Read UTF-8 text inside this filesystem instance; use the returned cursor to continue.
    input_schema:
      type: object
      properties:
        path: {type: string, minLength: 1, maxLength: 4096}
        start_line: {type: integer, minimum: 1}
        max_lines: {type: integer, minimum: 1, maximum: 10000}
        max_bytes: {type: integer, minimum: 4, maximum: 131072}
        cursor: {type: string, minLength: 1, maxLength: 4096}
      required: [path]
      additionalProperties: false
  view_image:
    description: View a static image inside this filesystem instance, optionally cropped.
    result_format: content.v1
    input_schema:
      type: object
      properties:
        path: {type: string, minLength: 1, maxLength: 4096}
        crop:
          type: object
          properties:
            x: {type: integer, minimum: 0}
            y: {type: integer, minimum: 0}
            width: {type: integer, minimum: 1}
            height: {type: integer, minimum: 1}
          required: [x, y, width, height]
          additionalProperties: false
      required: [path]
      additionalProperties: false
~~~

`result_format` 是本轮提出的通用新增字段，当前 OIC 会拒绝它，须与第 10 节的协议适配一起实施。其余六个 Action 缺省采用既有 json 格式。所有 Action 继承根 runtime，不设置非空 Action runtime 覆盖，以免被 OIC 拆为不同 worker。

不写 runtime.cwd/env/platform；当前执行器不解释这些字段。平台在构建时选择，工作范围由 App 配置决定。

### 4.2 OIC app_config：提供方定义范围与预算

Linux 可使用：

~~~yaml
app_config:
  filesystem:
    root: /srv/workspace
    read_only: false
    search:
      respect_gitignore: true
      include_hidden: true
      exclude_globs: ['**/.git', '**/.git/**']
    limits:
      max_inflight_calls: 16
      operation_timeout_ms: 15000
      max_result_frame_bytes: 786432
      max_read_bytes: 65536
      max_read_lines: 2000
      max_scan_bytes: 33554432
      max_write_bytes: 131072
      max_edit_file_bytes: 2097152
      max_directory_entries: 20000
      max_search_entries: 50000
      max_search_file_bytes: 4194304
      max_search_line_bytes: 1048576
      max_results: 1000
      max_depth: 64
      cursor_ttl_ms: 600000
    images:
      max_source_bytes: 16777216
      max_pixels: 20000000
      max_dimension: 16384
      target_encoded_bytes: 393216
      min_preview_edge: 768
      # 可选的提供方硬限，只在需要更严限制时设置 max_preview_edge / max_encoded_bytes
~~~

最小配置仅需 root；read_only 默认 true，写入由提供方显式开启。上表展示完整默认预算，普通部署不必逐项填写。Windows 使用同一结构，将 root 改为例如 `'D:\workspace'`，使用 Windows 发行包。

images.target_encoded_bytes 默认 393216（384 KiB），用于选择编码候选。max_preview_edge、max_encoded_bytes 仅在提供方显式配置时分别限制输出长边和图像编码字节；缺省没有这两项额外硬限制，源图安全上限、完整 worker 帧和各跳上限始终生效。提供方设定更小 max_encoded_bytes 时，编码目标取两者较小值。已有配置中的 max_preview_edge: 1600 或 max_encoded_bytes: 393216 继续按硬限执行。

root 必须是当前机器能打开的现有绝对目录，缺失、非目录或不能打开时启动失败。配置内部不做环境变量插值。配置未知字段、负值、非法组合一律拒绝。显式设置 max_preview_edge 时，min_preview_edge 不能大于它。max_write_bytes <= max_edit_file_bytes <= max_scan_bytes，单文件搜索限额不能大于总扫描预算。

root、只读模式、资源上限不接受业务参数覆盖。调用中的 limit/max_bytes 是请求上限，实际有效值取请求、实例和完整帧预算的较小值；文本写入内容超限时拒绝，不截断后继续写。

配置经 start.config 注入，worker 在 ready 前解析、验证并打开 root。app_config 由 OIC 启动时读取，修改后须重启 OIC；unload/load 不会重新读取 oic.yaml。

### 4.3 OIS Tool：明确目标实例

~~~yaml
tools:
  work_read_file:
    description: Read files from the Linux work directory on worker-a.
    source: {channel: worker-a, app: filesystem, action: read_file}
    input_schema:
      type: object
      properties:
        path: {type: string}
        start_line: {type: integer, minimum: 1}
        max_lines: {type: integer, minimum: 1}
        max_bytes: {type: integer, minimum: 4}
        cursor: {type: string}
      required: [path]
      additionalProperties: false
vmcps:
  workbench:
    tools: [work_read_file]
~~~

这是一个工具的结构示意；发行样例生成七个工具的完整 Schema。App load/登记不自动创建 OIS Tool，不假定中心定义会自动同步。只需要只读能力的 vMCP 可只暴露五个只读 Action，worker 的 read_only 仍是实际写入检查。

多个目录使用不同 app.code，例如 filesystem-work 与 filesystem-assets，各有一份 manifest 和 app_config 键。两个 manifest 可指向同一个二进制；各自的七个动作共享本实例。相同 code 的第二份配置不会创建新实例，而会命中已有 OIC 的 APP_ALREADY_LOADED 行为。

## 5. 路径和跨平台规则

对外采用同一套 Action 与字段，不让模型传 os 参数。运行平台由构建产物决定；本地专用行为由 `*_linux.go` / `*_windows.go` 编译选择。

1. 推荐使用 root 相对路径，空路径拒绝；目录动作省略 path 时解释为 `.`。
2. 接受当前平台的 root 内绝对路径，经本机路径规则转为相对路径；异平台绝对路径、盘符相对路径、UNC、设备路径、Windows ADS 路径拒绝。不得把 `C:foo` 当作 `C:\foo`。
3. 返回 path 统一为 root 相对路径、以 / 分隔。Windows 输入可接受 / 和 \；Linux 的 \ 保持合法文件名字符，不擅自替换。
4. 按路径组件判断越界，不用字符串前缀；所有平台拒绝 NUL 和越界的 ..。Windows 额外拒绝保留设备名及会产生歧义的尾点/尾空格，Linux 不误拒绝这些合法名称。文件名的大小写保留，查找遵循所在文件系统；glob 规则固定区分大小写。
5. 启动时打开 os.Root，实际操作不退回裸 os.Open(root + userPath)。路径字符串校验用于报错，根句柄负责实际访问约束。
6. read/view_image 可通过 os.Root 允许的根内链接访问；根外链接拒绝。绝对符号链接等 os.Root 不支持的形式明确报错，即使字符串看起来仍在 root 内。
7. 递归 find/search 不跟随链接；list 展示链接本身。写操作观察到最终目标为符号链接/重解析点时拒绝，父目录通过根内句柄解析。通过打开句柄的 nlink/NumberOfLinks 检查多硬链接并拒绝修改，发布前再次检查身份与属性；外部程序仍可能在最后检查后更改，不能承诺不存在外部竞态。
8. 普通文件以外的管道、设备、socket 不读写；Linux 打开时采用非阻塞标志并在打开后检查类型，避免预检查后变为 FIFO 导致协议处理卡住。
9. 不支持无损表示的非 UTF-8 文件名不做替换字符转换；目录/搜索以问题记录说明遗漏，调用这种路径返回 UNSUPPORTED_PATH_ENCODING。
10. root 约束不是操作系统沙箱，不隔离硬链接、挂载或已经暴露的 Bash/PowerShell 权限。首版支持范围以本地普通文件系统为准，不承诺网络盘/异常设备上的硬实时取消。

| 差异 | Linux | Windows |
| --- | --- | --- |
| 正式构建 | linux/amd64、linux/arm64 | windows/amd64 |
| 分隔符 | / | 输入 / 或 \，内部 native，输出 / |
| 路径身份 | 目录/文件句柄、设备与 inode | 目录/文件句柄、volume/file ID |
| 文件属性 | 保留可支持的普通 mode，尤其执行位 | 不绕过只读属性；处理共享占用和 DACL |
| 发布 | 同目录发布；本地 POSIX rename 具备原子替换语义 | 独立发布实现；不宣称所有 Windows 文件系统都有同等原子保证 |
| 占用文件 | 打开的旧句柄可能继续读旧内容 | 可能因 sharing violation 返回 FILE_BUSY |

## 6. Schema 与统一返回约定

完整机器可读候选位于 [filesystem.schemas.json](filesystem.schemas.json)，采用 JSON Schema 2020-12，以 `#/$defs/<action>_input`、`..._output` 引用；write/edit 共用 mutation_output。配置引用 `#/$defs/config`。

worker 先严格解码并拒绝未知字段，再检查枚举、数组项、整数范围和组合规则，最后检查路径及资源预算。当前 OIC 仅执行部分 type/properties/required/default，不能依靠它验证 edits 数组、crop 或 additionalProperties。

Go encoding/json 会替换非法 UTF-8 或孤立的 UTF-16 surrogate 转义，并覆盖或合并重复 JSON 成员，不能只检查解码后的字符串/map 就宣称保留了原始输入。本轮需在 OIS 的 HTTP/MCP 原始 JSON 入口、OIS/OIC 帧及 worker JSONL 等各原始载荷的首次实际解码前，通用校验字符串编码并拒绝重复成员名，包括任意嵌套对象及经转义后同名的键；不同对象内使用相同键不算重复。合法的 U+FFFD 字符仍可使用。已被上游解码覆盖或替换的信息无法由 worker 还原，不能把上游原始重复键检测交给 worker。该校验不解释文件业务；业务字段名精确区分大小写，不利用 Go struct 解码的大小写容错代替 Schema 验证。

输入可选字段通过省略表达，不接受 null；输入默认值由 worker 应用，manifest 不为 start_line 之类互斥字段自动注入默认值。输出需要明确“没有”的字段使用 null。maxLength 是字符约束，UTF-8 字节限制仍独立检查。

| Action | 必填输入 | 可选输入与省略行为 | 输出重点 |
| --- | --- | --- | --- |
| read_file | path | start_line=1；max_lines=200；max_bytes=65536；cursor 与 start_line 互斥 | text、range、source_size、bom、eof、next_cursor、revision |
| write_file | path、content | mode=create；create_parents=false；replace 必须 expected_revision | state、created、bytes_written、revision、created_directories、warnings |
| edit_file | path、expected_revision、edits | 无隐式 replace_all/fuzzy/正则模式 | 与 write 相同 |
| list_directory | 无 | path=.；limit=200；cursor | 完整 entries、next_cursor、complete、issues |
| find_files | pattern | path=.；limit=200；ignore/hidden 沿用实例；max_depth 沿用实例 | files、status；无搜索分页令牌 |
| search_content | query | path=.；mode=literal；case_sensitive=true；file_glob=**；limit=100；context_lines=0 | 每匹配行一项、位置、上下文、status |
| view_image | path | 省略 crop 看整图；给 crop 取方向校正后指定区域的原像素 | content.v1 图像块、区域、实际 MIME、尺寸、resized、reencoded、encoded_bytes |

统一错误用 worker 的 type:error，code/message/details；不把业务失败包装为成功结果里的 ok:false。完整图片结果也不内嵌另一套 isError；MCP 的 isError 由 OIS 根据顶层错误生成。

文本结果的 limit 不是 token 数。所有结果先按业务预算形成，再检查完整序列化帧；路径、行号、图像数据不可从中间截断。能返回较少完整条目时返回较少条目并说明未完成；单个必要元素放不下时返回 RESULT_TOO_LARGE。write/edit 必须在任何目录创建或目标发布前，另行预留成功与失败的必要回执空间，不能把执行后的最终帧检查作为第一次预算检查。

## 7. 七个 Action 的逻辑

### 7.1 read_file：正文、续读与版本

示例：

~~~json
{"path":"src/main.go","start_line":1,"max_lines":80}
~~~

~~~json
{
  "path":"src/main.go",
  "text":"package main\n",
  "source_size":13,
  "bom":false,
  "range":{"start":{"line":1,"byte_column":0},"end":{"line":2,"byte_column":0}},
  "eof":true,
  "next_cursor":null,
  "revision":"sha256:df1d036cbbf3df46e2045071e082245ece204c7f53ecf0a4e022bff9bb228f47"
}
~~~

此处摘要对应示例的原始字节。下文其他调用中的摘要占位符需替换为实际 read_file 返回值。

算法：

1. 解析路径并打开普通文件；记录文件身份、大小和时间，验证读取范围。
2. 文件不超过 max_edit_file_bytes 时，限额内读完整字节，验证 UTF-8/非二进制并计算 SHA-256，然后切出请求页面。此路径最多扫描约 2 MiB，不是任意大小文件的全文预扫描。
3. 更大文件采用流式读取；revision=null。不为总行数、哈希或末尾信息读到 EOF；初次 start_line 定位也受 max_scan_bytes 和时间预算限制，不能到达时返回 READ_SEEK_LIMIT_EXCEEDED。
4. 行号从 1 开始，字节列从 0 开始；range.end 为 exclusive。保留原始 LF/CRLF，不添加行号、不改 Unicode、不静默替换非法 UTF-8。文件开头 UTF-8 BOM 不出现在 text 中，bom=true；hash 包含 BOM。
5. 到行数上限停在换行边界；到字节/帧预算时允许在长行中分段，但不切开 UTF-8 字符或 CRLF。next_cursor 记住字节偏移和行内位置，下一次不会丢长行尾部。
6. 小文件 cursor 绑定完整内容摘要；大文件只绑定文件身份/大小/时间观察值。检测到变化返回 STALE_CURSOR；大文件跨页不承诺快照，不能把观察值伪装成内容哈希。
7. 验证读取期间可检测的变化；文件被替换或修改时返回 FILE_CHANGED，避免把明显不同版本拼成一次稳定结果。

range 的坐标基于剥离文件开头 BOM 后的文本视图，第一处正文为 (1,0)，内部文件偏移仍包含 BOM；search_content 采用同一视图。空文件或仅有 BOM 时，text=""、start=end=(1,0)、eof=true、next_cursor=null，revision 分别计算各自原始字节。start_line 超过 EOF 时返回空正文，位置为实际 EOF，不伪造请求行号。LF 推进行号并清零列，CRLF 不拆分，孤立 CR 计作正文；从长行中间续读时剩余部分计为本页第一行。

cursor 使用随机 worker 实例标识及 HMAC，绑定 handler、归一化路径、access_identity、vmcp_id、续读位置、观察值和到期时间。路径及身份以固定长度摘要绑定，不能把允许的长路径直接 Base64 后塞进 4096 字符的令牌；调用仍传 path 供核对。重启后密钥变化，旧 cursor 返回 CURSOR_EXPIRED；无需每页持有文件句柄或永久缓存正文。cursor 不能被当作写入授权。

完整 revision 的格式为 sha256:<64 hex>。它标识读取到的字节，不表示读操作持有跨调用锁，也不阻止外部程序在读取期间原地修改。读取前后元信息校验用于发现通常的变化，无法把普通文件读取变成跨进程快照。

### 7.2 write_file：创建与条件替换

~~~json
{"path":"notes/result.txt","mode":"create","create_parents":true,"content":"完成\n"}
~~~

~~~json
{"path":"config.json","mode":"replace","expected_revision":"sha256:<读取结果中的摘要>","content":"{\n  \"enabled\": true\n}\n"}
~~~

create 只创建新文件，存在就 FILE_EXISTS；不接受 expected_revision。replace 要求目标存在、expected_revision 存在、目标在可编辑大小范围内；不接受 create_parents=true。不提供隐式 upsert。

content 为严格 UTF-8，不替调用方转换换行或自动添加 BOM；有 NUL 的二进制内容拒绝。完整正文最多默认 128 KiB，原因是调用仍经过 1 MiB JSON 帧，不能因可编辑源文件上限是 2 MiB 就允许 2 MiB 入参。实际 JSON 请求仍须满足完整帧上限。

进入写临界区后重新打开并读取源文件、计算 hash、校验期望值；不匹配返回 REVISION_CONFLICT，包含可用的 current_revision，不写目标。替换结果与原始字节相同则 state=unchanged。

具体提交与取消见第 8 节。需要创建父目录时，最多创建 64 层并记录实际创建目录；目录是副作用，后续失败不能声称完全没有变化，不自动删除可能已被别人使用的目录。在第一次 mkdir 之前，以本次允许创建的完整目录路径集合预检成功与失败回执的最坏序列化大小：包括 created_directories、目标路径、固定大小的摘要/状态/调用封装及有界错误说明余量，并满足实例完整帧预算。不能容纳就先返回 RESULT_TOO_LARGE，不创建任何目录；执行中不得超出预留集合继续创建。后续失败回执完整报告实际已创建的子集，提交后不能再因这份清单过大而丢失结果或截断路径。

### 7.3 edit_file：单文件多段精确替换

~~~json
{
  "path":"src/main.go",
  "expected_revision":"sha256:<读取结果中的摘要>",
  "edits":[
    {"old_text":"port := 8080","new_text":"port := 9090"},
    {"old_text":"debug := true","new_text":"debug := false"}
  ]
}
~~~

处理顺序固定：

1. 在写临界区读取不超过上限的完整文件，校验文件类型和 expected_revision。源文本、old_text/new_text 与最终文本都执行同一文本验证：合法 UTF-8、无 NUL；任何失败发生在提交之前。
2. 从文本视图暂时分离 BOM；对每一段非空 old_text 在同一份原文中查找，包括重叠出现的候选位置。
3. 每段必须恰好出现一次，各匹配区间不能重叠。空 old_text、不匹配、多匹配或区间重叠时全部不写。
4. 按位置生成新字节；未涉及区域、原 BOM 和换行字节原样保留。old/new 中的换行按字面匹配，不悄悄规范化 LF/CRLF、智能引号、Unicode 或空白。
5. edits 最多 64 段，old/new 合计输入默认不超过 128 KiB；编辑后完整文件不得超过 max_edit_file_bytes。新旧相同返回 unchanged。
6. 全部验证成功后通过同一个提交模块写入一次，返回新 revision 和实际修改状态。

结果不默认附带整份文件或完整 diff，避免写入成功后又因回显太大使结果失败。需要确认时 read_file；后续若增加有界变更摘要，应保持不会影响提交结果交付。

### 7.4 list_directory：有界排序分页

非递归，包含隐藏项，不应用 .gitignore。普通条目给 name/path/kind/size_bytes/modified_at；大小仅对普通文件有意义，无法取得的属性为 null 并记录问题。

首版选择“有界收集名字后排序”，按 UTF-8 名称字节顺序、区分大小写，不使用依赖系统 locale 的排序。最多接纳 max_directory_entries 个名字，允许再读一个条目或 EOF 以区别恰好达限与超限；全部原始条目都计数，包括随后因编码问题略过的项。然后截取 cursor 指定位置之后的一页。只对需要返回的条目读取属性，减少 stat；名字累计另限 8 MiB，防止仅按条目计数仍占用过大内存。

这不是无限目录的流式分页：超过目录扫描限额返回 DIRECTORY_TOO_LARGE，不返回一个声称有序完整的假页面。限制内的分页每次会重新扫描名字；接受这一实现成本，暂不引入长期目录句柄或快照存储。

cursor 绑定目录身份、有界完整排序名称集合的摘要、最后返回的完整名字及调用归属；每页重扫核对摘要，名称集合变化返回 STALE_CURSOR，重启或过期返回 CURSOR_EXPIRED。文件属性是本页的即时观察，子文件内容变化不要求使名称游标失效。跨页不是快照，枚举期间未检测到的外部修改仍可能造成遗漏或重复。普通分页 next_cursor!=null 时 complete=false；最后一页无观察错误才 complete=true。

### 7.5 find_files：路径 glob

path 必须是目录；pattern 相对于它匹配，例如 `src/**/*.go`，返回路径仍相对于实例 root。只返回普通文件，不悄悄混入目录或链接。

共享 walk 逐目录读取有限批次，通过 os.Root 打开目录/条目，累计条目数、深度、读取字节和期限；单次最多一个搜索执行进入重 I/O 槽。首版不保证匹配结果是跨调用稳定排序，也不提供搜索 cursor。

glob 使用 / 分隔、固定区分大小写；`*.go` 与 `**/*.go` 的作用范围不同。包含目录组件的模式不能降级成 basename 匹配。

ignore 的具体范围：

- respect_gitignore=true 时，从实例 root 到搜索 path 的父层、再向下读取根内 .gitignore，按层叠优先级匹配。
- 目录被排除时剪枝，不深入后再靠子级 ! 规则重新包含。
- 不读取 root 之外的仓库祖先、全局 Git 配置、core.excludesFile 或用户 HOME。
- .git/info/exclude、.ignore 和 .rgignore 不在首版自动识别范围。
- search.exclude_globs 是提供方的搜索排除项，始终生效；它不是 read/write 的访问控制。
- 单个 .gitignore 最多 256 KiB，读取量计入本次 max_scan_bytes；过大、不可读或编码无效时记录问题并将 complete=false，不能假装已遵守全部规则。

达到条数、输出、条目、深度、扫描字节或时间预算，返回已找到结果及 status.complete=false/stop_reason。权限和消失文件形成有界 issues；按明确 ignore/隐藏/链接范围主动排除不等于执行失败。

### 7.6 search_content：逐行文字查询

与 find_files 共用 walk、ignore 和路径规则。path 为目录，file_glob 限定候选普通文件。mode=literal 先 regexp.QuoteMeta(query)，mode=regex 使用原表达式；case_sensitive=false 时应用 Go regexp 的 Unicode simple-fold。匹配位置来自原始文本字节索引，不先把全文转小写。regex 中显式内联 flags 按 Go 规则生效。query 非空、最多 4 KiB、不能含实际 CR/LF；首版不支持跨行匹配。

观察大小超过 max_search_file_bytes 的文件整份跳过，记录 FILE_TOO_LARGE 并将 complete=false；其余候选最多读限额+1字节，增长超限也整份跳过。确认完整候选 UTF-8/无 NUL 后再逐行匹配，不返回未经完整候选验证的前缀命中。读取总量受 max_scan_bytes 限制，预算不足以完成候选就停止并说明未查全；二进制作为约定范围外文件计数排除。超长行整行跳过并记录遗漏，不能把半行当整行匹配。

按行检查，一个匹配行生成一条结果，limit 按匹配行计，不假称所有匹配次数。记录首个匹配的原行字节区间；片段优先保留匹配附近内容，最大 1024 UTF-8 字节，切在字符边界，snippet_start_byte 指明起点，text_truncated 标记是否省略原行。超长匹配自身不能完整显示时仍给完整匹配字节范围并标记截断。

context_lines 为 0..2，before/after 独立带行号和片段截断标记。查询结果无 total_count，只有扫描统计和本次返回的匹配行；避免把限额内数量冒充全集数量。

没有找到结果只有在 complete=true 时才表示规定范围内没有匹配；complete=false 时表示这次没有找到，但尚未查完。查询取消且已有可交付结果时，停止新增扫描，返回 complete=false、stop_reason=cancelled；没有可交付结果时返回顶层 CANCELLED。超时使用 timeout，不与主动取消混为一谈。

### 7.7 view_image：解码、方向、裁剪、预算

view_image 的输入 path 只指向本实例 root 内已有图片，不生成截图、不下载 URL、不接收附件。支持静态 PNG/JPEG/WebP，按文件头确认格式，不仅看扩展名。拒绝动画 PNG/WebP、GIF、SVG、多页文档及无法解码的内容；不把第一帧悄悄当成整份动画。

流程：

1. 根内打开普通文件，检查压缩源字节数，再做 DecodeConfig 检查宽、高、像素乘积和算术溢出。
2. 有界解析 JPEG APP1、PNG eXIf、WebP EXIF 的方向信息，只读取 TIFF IFD0 Orientation；处理全部八种方向。不依赖只识别 JPEG 的 AutoOrientation 就声称覆盖所有格式。
3. 完整解码后复核尺寸，先校正方向。crop 以校正后的图像左上角为原点，单位是源像素，范围为 [x,x+width) × [y,y+height)。校验整数边界与加法溢出，越界返回 CROP_OUT_OF_BOUNDS。即使只裁很小的区域，原图也须通过源字节数、边长、像素数等解码前检查。
4. 显式 crop 用于查看局部细节。从校正后的原图取指定矩形，按原像素尺寸编码无损 PNG，保留透明通道。成功时 preview_width/height 等于 crop.width/height、resized=false、reencoded=true。即使 crop 指定整幅图像也走该分支。输出硬限无法满足时返回 IMAGE_PREVIEW_TOO_LARGE，提示选择更小的 crop。
5. 省略 crop 时优先提供整图原分辨率。不需要方向或其他格式变换时，先试已通过验证的原 PNG/JPEG/WebP 编码，满足硬限就直接交付，reencoded=false。需要重新编码时优先尝试同尺寸无损 PNG，保留图形和文字细节。若仍超出硬限，仅对完全不透明的整图按 JPEG 质量 90、85、80、75 尝试同尺寸候选，从而允许照片等高复杂度区域保留分辨率。透明图在每个尺寸均保留透明通道；WebP 可以复用源编码或解码，首版无需 WebP 编码器。
6. 384 KiB 为编码软目标（Base64 大小为 512 KiB），不是拒绝条件。原编码或同尺寸无损候选超出软目标，只要满足提供方显式硬限且完整序列化帧可装下，仍优先返回。需要同尺寸重编码为 JPEG 时先选达到软目标的最高可用质量；没有质量档达到目标时，保留符合硬限的最高质量候选。检查预算时计入 Base64 的实际填充长度、文本、元数据、call_id 和 JSONL 换行，OIC/OIS 各跳再核对本跳完整封装。默认 worker 帧为 768 KiB，1 MiB 为既有链路上限。768 KiB 仅装 Base64 时的理论原始编码上界为 576 KiB；实际可用字节受完整封装和更小配置限制。编码与缓存候选应有界，一旦当前候选已不可能适配硬限即可放弃它。
7. 只有整图的原尺寸候选均无法满足硬限时，才从同一已解码图等比缩小并返回概览，resized=true。长边最低取 min(原图长边,min_preview_edge)，小图不放大；显式 max_preview_edge 按提供方硬限执行。最低可用尺寸仍超限返回 IMAGE_PREVIEW_TOO_LARGE，提示通过 crop 请求细节。完整回执仅含一张图，块的 MIME、编码字节数与 preview_width/height 均来自实际输出，Base64 只出现一次。

图片任务使用独立的一个并发槽；像素/源字节限制控制解码工作量。标准库解码不提供任意时刻可抢占的硬期限，取消在读取及各处理阶段间检查；不承诺向本地解码函数发 cancel 后立即终止。

## 8. 修改提交、属性与取消

一个实例采用全局写 mutex，首版所有 write/edit 串行，读和不同类别的有界查询可以并发。这比按路径字符串建锁更容易覆盖大小写别名和链接别名；小文件修改不需要先引入细粒度锁管理。

~~~text
检查参数/只读
  -> 获得可取消的写锁
  -> 检查路径并预留成功/失败回执（先于第一次mkdir或任何发布）
  -> 按需创建获准父目录，记录实际创建子集
  -> 根内打开目标并核对类型、属性、内容revision
  -> 在目标父目录创建唯一staging文件
  -> 写完整新字节、设置可支持的属性、Sync、Close
  -> 再次检查目标内容/身份和取消状态
  -> 发布到目标名字（提交点）
  -> 核对实际结果、生成有界回执、清理staging
~~~

- 回执预算先于首次目录创建或目标发布。按实际 JSON 转义及帧封装预检，保证成功与错误 details 中的完整 created_directories 都能放入；错误说明/warning 采用预留的有界文本。提交后可减少非必要诊断，不能删除状态、摘要、目标路径或已创建目录来伪造完整回执。
- create：必须 no-clobber。可用 Root.Link(staging,target) 发布完整文件，目标存在时失败，再删除临时名字。文件系统不支持该发布方式时返回 UNSUPPORTED_FILESYSTEM，不能退回“先判断不存在，再无条件覆盖”。
- replace/edit：目标同目录 staging，两个平台都使用 Root.Rename 发布。Go 的 Windows Root.Rename 已采用根内句柄相对操作，不另造一套未验证的路径式替换；平台文件主要封装属性及错误识别。不先删除旧文件，不退回原地截断后写入；处理 Windows 共享占用，不把非 Unix Rename 的语义夸大成普遍原子保证。
- 复制普通权限属性：Linux 保留普通 mode/执行位，源与暂存文件 owner/group 不一致时拒绝，不新增所有权迁移。Windows 的可判定属性范围见下表。ACL/xattr/审计/扩展元信息的完整保真不是通用承诺，不能以“保留属性”概括所有元数据。
- SHA-256 检查与发布处在 worker 的同一写临界区，防止本 worker 的合作调用覆盖旧版本；外部编辑器/Bash 可在最后检查后再次修改，因此不承诺 OS 级 compare-and-swap。
- 发布前收到取消，停止提交并清理临时文件；若已经创建父目录，在错误 details 中报告。发布后收到取消，尽量返回 applied，不能说“已取消所以未写入”。
- 发布调用报错后不能单凭 err!=nil 推断没改；核对目标文件身份是否为本次 staging 的身份，以及字节是否为新内容，两项均能确认才返回 applied 加 warning。不能仅凭哈希相同就把别人的同内容写入归为本次成功；能确认原目标身份和原内容未变则 not_applied，否则 mutation_state=unknown。
- 文件 Sync 与目录项发布不等于所有存储上的断电事务；平台允许时同步父目录，无法确认持久性时只描述已观察到的结果。

Windows 首版修改属性约定如下，检查及设置都基于打开句柄，任何检测/复制/读回校验失败均在发布前返回错误：

| 属性 | 行为 |
| --- | --- |
| 存储及目标 | 本地 NTFS、普通文件、非重解析点、观察到的链接数为1 |
| 常规属性 | 仅接受 NORMAL/ARCHIVE/HIDDEN/SYSTEM 的合法组合并保留；READONLY 明确拒绝；压缩/加密/稀疏等其他属性暂不支持 |
| owner/group | 与暂存文件不同则拒绝，避免静默所有权迁移 |
| DACL | 读取并复制 DACL 与保护控制状态，发布前读回核对，失败返回 UNSUPPORTED_FILE_ATTRIBUTES |
| 备用数据流 | 使用 FileStreamInfo 检查，发现命名流拒绝，不能只禁止输入冒号却丢失已有 ADS |
| 其他元信息 | SACL、EA、Linux ACL/xattr 等不承诺保真，也不声称未实现的检测能识别全部特殊文件；带这类要求的文件不在首版编辑支持范围 |

默认创建普通文件使用操作系统权限继承和受控的创建模式；不新增 chmod/chown Action。临时文件与目标同一文件系统，命名包含随机实例标识；正常退出清理当前实例拥有的 staging。崩溃后不按通配符盲删历史文件，残留文件的处理在部署说明中列出。

## 9. worker 生命周期和资源逻辑

~~~text
start(config, result_formats)
  -> 严格校验配置
  -> 打开root，初始化密钥/资源槽
  -> ready(handlers, result_formats)
invoke
  -> 校验call_id、可信context、handler
  -> 登记取消context并占在途槽
  -> 参数验证/默认值
  -> 资源槽及路径访问
  -> 执行Action（写操作先预留必要回执，再产生副作用）
  -> 编码/最终预算检查
  -> 一次result或error
cancel
  -> 标记该call的context，不阻塞输入循环
EOF
  -> 停止接收，取消未提交工作，结算已提交结果，清理并退出
~~~

默认最多16个在途调用；写临界区1个，图片解码1个，find/search重I/O槽1个。等槽的请求也计入在途上限并可取消，超限直接 CAPACITY_EXCEEDED，不建无界队列。stdio输入循环与业务协程独立，stdout 由一把锁输出完整帧，stderr 仅输出有界诊断。

每个调用内部默认15秒期限、配置最大45秒，给现有OIC的60秒截止留余量；预算不能使不可中断的OS操作变为硬实时操作。cancel消息只传递请求，不等于已经取消成功。

本 worker 对在途重复 call_id 核对 handler/input/context，重复相同调用不重复执行，冲突返回 CALL_ID_CONFLICT。已完成调用去重沿用 OIC 的有限缓存，不建设永久日志或承诺跨重启恰好一次。写入结果未知时，调用方先重新读取核对，不换新 call_id 自动重放。

诊断记录 action、call_id、耗时、字节/条目统计和错误码；不记录整份文件正文、Base64、令牌或完整请求。指标用于定位预算和失败，不成为新的公开 Action。

## 10. 通用图像结果的最小协议扩展

[MCP 2025-11-25 Tools 规范](https://modelcontextprotocol.io/specification/2025-11-25/server/tools#image-content)允许 tools/call 的 `result.content` 直接包含原生 ImageContent，例如 `{"type":"image","mimeType":"image/png","data":"<完整Base64>"}`。这是协议支持的图像内容，不是要求模型从文本中解码 Base64。

当前工程的 [internal/ois/mcp.go](../../../../internal/ois/mcp.go) 第296–299行仍将整个业务 payload 序列化成一个 text 块，并附 structuredContent；App 即使返回 type:image 对象，也会被包进文本。因此只安装 Filesystem App 不能获得原生视觉结果。必须同步实施下述显式 OIC/OIS 通用结果扩展；再由支持图像的消费端把 ImageContent 实际交给视觉模型，通过第12节的端到端验收后才宣称 view_image 可用。Browser 当前视口 screenshot 复用同一通用链路；两项共享结果格式，不互相扩展为文件传输或对方的输入能力。

推荐保留 protocol:1，新增可协商的 result_format，缺省 json：

1. Action 定义可写 result_format:content.v1；缺省 json。OIC校验已知值，按已绑定Action决定允许的格式，不检查App名。
2. 需要新格式的 App 才在 OIC start 增加 result_formats，worker ready 回报支持集合；JSON-only App 保持旧 start 外形，避免破坏严格解码的第三方 worker。字段缺失按仅支持 json；ready 集合必须是 start 提供集合的子集，且覆盖所有声明 Action 的格式，否则 load 失败。本 Filesystem 收到不含 content.v1 的 start 时明确失败。start 表示 OIC 本地能力，不受 OIS 在线状态影响；旧 OIC 会拒绝新 manifest 字段，使不兼容显式失败。
3. OIC bind 与 OIS bound 增加 result_formats，双方取交集；字段缺失仅表示 json。能力集合属于具体连接，重连重新协商。注册 ActionInfo 携带 result_format；OIC 只在该连接的登记副本中将上游不支持的 Action.ready 置 false，保留本地 worker/App Ready=true，不复用当前“任一Action未ready则整个App未ready”的汇总方式，否则六个文本Action也会被误禁用。在实际派发时再次检查连接能力，防止旧端忽略新字段后把图像变成文本。
4. worker result 帧包含 result_format:content.v1 和原有 result 字段；OIC核对它与Action声明一致、验证通用内容外形，并原样转发格式标记。普通JSON帧不需要新增标记。
5. OIS outcome、pending结果与持久于内存的调用缓存保留格式；MCP按显式格式适配，REST对新格式增加result_format。错误仍走error帧。

示例：

~~~json
{
  "type":"result",
  "call_id":"call_123",
  "result_format":"content.v1",
  "result":{
    "content":[
      {"type":"text","text":"原图1920×1080，按原尺寸返回。"},
      {"type":"image","mimeType":"image/png","data":"<完整Base64>"}
    ],
    "structuredContent":{
      "path":"images/sample.png",
      "source_mime":"image/png",
      "source_width":1920,
      "source_height":1080,
      "orientation_applied":false,
      "crop":null,
      "preview_width":1920,
      "preview_height":1080,
      "resized":false,
      "reencoded":false,
      "mime_type":"image/png",
      "encoded_bytes":420000
    }
  }
}
~~~

此为说明性例子，Base64与字节数不是实际图像。扩展后的 MCP 将图像对象直接放入工具结果的 `result.content`，元信息放入 `result.structuredContent`，并设置 isError=false；不能把整个对象 JSON 序列化后装入 text。REST 输出 `{call_id,result_format,result}`。

content.v1 本轮只规定 text/image 列表与可选 structuredContent。图像编码通过 Base64 内联在结果中，无需图片 URL、对象存储或额外媒体服务；这仍会把实际像素的编码字节沿 worker→OIC→OIS→消费端传送，并受原有授权与完整帧预算约束。图片内容检查 Base64 和 MIME，不把 Base64 复制到 structuredContent 或文本块，不引入任意嵌入资源。

| 组合 | 行为 |
| --- | --- |
| 新 OIS + 新 OIC + 旧 Bash/PowerShell | 缺省json，保持旧text/structuredContent及HTTP形状 |
| 新 OIS + 旧 OIC | 仅json；旧App继续用 |
| 旧 OIS + 新 OIC | bound缺能力表示json-only；图像Action不可用，文本Action可用 |
| 旧 OIC + 新Filesystem manifest | 未知result_format字段导致加载失败，明确提示升级 |
| 新链路 + 不支持图像的消费者 | 标准image结果不保证能进入模型视觉输入；该消费者不能算view_image验收通过，不以Base64文本代替 |

主工程预计修改点：internal/protocol/protocol.go及通用严格JSON解码辅助、internal/oic/config.go/worker.go/connection.go/host.go，以及internal/ois/connection.go/invoke.go/mcp.go/server.go和对应测试。兼容检查、缓存、登记和转发需一起覆盖，不只改最后一层MCP。

worker默认完整帧上限768KiB，为既有1MiB链路留封装余量。本轮需补齐OIC/OIS各发送入口的最终序列化大小检查：当前OIC→worker已有检查，WebSocket发送入口尚不完整。超限应在发送前返回有界错误，不能让接收端断开整个channel并影响其他调用。现有HTTP请求体也限1MiB，文本入参和图片返回分别受各自方向约束。REST响应不能被描述成已有同样的硬限额。

## 11. 错误与副作用约定

| 类别 | 代表错误 | 处理 |
| --- | --- | --- |
| 输入 | INVALID_ARGUMENT、UNSUPPORTED_ENCODING、INVALID_PATTERN | 修正输入，不执行写入 |
| 路径/权限 | FILE_NOT_FOUND、NOT_A_FILE、NOT_A_DIRECTORY、PATH_OUTSIDE_ROOT、READ_ONLY、PERMISSION_DENIED | 不切换目录或实例重试 |
| 修改冲突 | FILE_EXISTS、REVISION_CONFLICT、EDIT_NOT_FOUND、EDIT_AMBIGUOUS、EDIT_OVERLAP | 重新读取当前文件，形成新的编辑 |
| 读取/续取 | STALE_CURSOR、CURSOR_EXPIRED、READ_SEEK_LIMIT_EXCEEDED、FILE_CHANGED | 从清楚的起点重新读取 |
| 资源 | FILE_TOO_LARGE、DIRECTORY_TOO_LARGE、RESULT_TOO_LARGE、CAPACITY_EXCEEDED | 缩小范围或调整提供方限额 |
| 图片 | UNSUPPORTED_IMAGE、INVALID_IMAGE、CROP_OUT_OF_BOUNDS、IMAGE_PREVIEW_TOO_LARGE | 修正文件或请求裁剪 |
| 平台 | FILE_BUSY、UNSUPPORTED_FILESYSTEM、UNSUPPORTED_FILE_ATTRIBUTES | 按实际平台条件处理 |
| 生命周期 | CANCELLED、TIMEOUT、CALL_RESULT_UNKNOWN、RESULT_FORMAT_UNSUPPORTED | 区分确认取消、未确认结果与不兼容 |

worker 写入错误 details 必须包含 mutation_state（not_applied/applied/unknown）和已知 created_directories；not_applied 表示目标内容未提交，不代表此前没有创建目录。无能力证明状态时返回 unknown。成功与错误回执均使用首次 mkdir/发布之前保留的预算，不能因清单过大丢掉副作用说明；宿主传输错误仍按既有未知结果语义处理。

不要扩展一套与宿主冲突的重试规则。OIS现有业务错误通常映射为HTTP422、MCP isError=true；transport错误保持既有语义，不能全部归类为文件不存在。

## 12. 实现顺序、验证与交付条件

建议按依赖顺序实现：

1. 固定contract、生成manifest/示例，搭起独立worker和根目录访问。
2. 完成read/list/cursor、UTF-8和预算逻辑。
3. 完成revision、staging、平台发布及write/edit。
4. 完成共享walk、glob/ignore、find/search。
5. 完成content.v1兼容链路和view_image。
6. 独立打包、跨平台回归及真实Agent端到端使用。

必要验证按行为组织：

| 验证组 | 关键情境 |
| --- | --- |
| 协议 | 并发响应不交错、取消不堵输入、非法帧、嵌套/转义同名重复键与非法Unicode在首次解码前拒绝、近1MiB转义膨胀、超限返回小错误而非杀死worker |
| 路径 | ../、相邻前缀目录、Windows盘符/UNC/ADS、Unicode/空格、根内外链接、junction、硬链接、目标检查后被替换 |
| 文本 | 空文件、BOM、CRLF/LF混合、多字节UTF-8、非法编码、超长单行、精确边界分页、游标失效 |
| 资源 | 大日志首屏不全文扫描、目录超限、搜索总量/超长行/时间上限、图片单槽与取消 |
| 编辑 | 重复/重叠匹配、任意一段失败目标不写、版本冲突、同实例并发、new==old、结果超限在提交前拒绝 |
| 落盘 | 64层长路径/64KiB帧预算在首次mkdir前拒绝、部分父目录已建后的完整错误回执、磁盘满、无权限、Windows共享占用、提交成功但返回错误、取消跨越提交点、staging清理、属性保留 |
| 搜索 | 相对路径glob、隐藏、嵌套ignore及否定、父目录剪枝、二进制跳过、权限遗漏、每匹配行计数 |
| 图片和预算 | 静态 PNG/JPEG/WebP、8 种 EXIF 方向、动画拒绝、透明通道、像素炸弹、方向校正后的 crop 边界。原尺寸 1920×1080、编码高于 384 KiB 但可装入完整帧的 PNG 成功，resized=false；提供方更小的硬限或封装溢出仍生效。整图确需缩小时回报实际尺寸，显式 crop 超硬限提示更小区域。 |
| 兼容 | 五种新旧组合、普通业务JSON恰有content/image键、旧终端输出完全不变、HTTP/MCP等价图像 |
| 端到端 | 远端Agent列目录→查找→读→带revision编辑→再读；另一实例同名路径不能混用；图像实际进入视觉模型 |
| 独立交付 | App源码复制到仓库外构建/测试，部署机器无Go/rg/Python/Node仍可运行，Linux与Windows实际执行 |

根模块 go test ./... 不会进入独立App；CI必须给filesystem增加独立模块任务，Linux执行race，两个正式系统都执行文件提交和路径测试。arm64发布须在实际arm64执行或明确的执行环境中验证，不能只以交叉编译成功宣称正式支持。

图像验收使用能把 MCP 原生 ImageContent 交给视觉模型的真实消费端。准备 1280×720 和 1920×1080 的图文混排静态图，12/14 px 中文短句、数字与随机批号只出现在像素中。分别发起整图和局部调用，验证超软目标可保原尺寸、超硬限返回明确错误或已标明的整图概览；对旋转输入核对裁剪的四边和方向。解码局部 PNG，与原图校正后对应区域逐像素比对，并让模型逐字读回局部标记。回执的 MIME、尺寸和字节数与解码结果一致。Browser 的视口截图另按其契约独立验收。

本次文档不充当实测报告。实施前最值得做的小规模验证是Windows发布/属性保留，以及图像消费者端到端；发现差异应更新本设计的支持范围，不能用静默降级掩盖。

## 13. 与主设计的衔接

该文档记录已确认的七个 Action 及语言、配置、平台、预算、并发、错误和交付方式。平台名单、条件覆盖要求、2 MiB 可编辑文件范围及 128 KiB 调用正文范围已随方案获认可；依赖锁定和平台实现仍须按文中的验证计划执行。

本版保留这七个 Action；完整原文件传入/取回、Browser 网页上传下载及跨 App 联合文件流均暂缓，不再列作待补配套。view_image 的范围为根内已有静态图的有界预览，与 Browser 当前视口 screenshot 两项共享本文明确要求的 OIC/OIS 通用图像结果扩展；分别完成真实视觉消费端验收。当前共识文件记录设计范围，不代表功能已经实现。

## 14. 实现依据

- 现有工程边界：[独立 App](../../../../apps/README.md)、[App runtime](../../../spec/reference/app-runtime.md)、[OIC 配置](../../../spec/reference/configuration.md)。第10节明确标出的能力协商与结果格式是新增设计，不是这些文档已实现的能力。
- 路径与提交：[Go os.Root](https://go.dev/blog/osroot)、[Root/文件 API](https://pkg.go.dev/os)、[Windows 的句柄相对实现](https://go.dev/src/internal/syscall/windows/at_windows.go)。
- 搜索：[Go regexp](https://pkg.go.dev/regexp)、[doublestar](https://pkg.go.dev/github.com/bmatcuk/doublestar/v4)、[go-git ignore](https://pkg.go.dev/github.com/go-git/go-git/v5@v5.19.3/plumbing/format/gitignore)。
- 图像：[WebP decoder](https://pkg.go.dev/golang.org/x/image/webp)、[PNG eXIf](https://www.w3.org/TR/png-3/#eXIf)、[WebP container](https://developers.google.com/speed/webp/docs/riff_container)、[MCP image](https://modelcontextprotocol.io/specification/2025-11-25/server/tools#image-content)。
- 工具链：[x/image v0.48.0 go.mod](https://raw.githubusercontent.com/golang/image/v0.48.0/go.mod)、[go-git v5.19.3 go.mod](https://raw.githubusercontent.com/go-git/go-git/v5.19.3/go.mod)。

