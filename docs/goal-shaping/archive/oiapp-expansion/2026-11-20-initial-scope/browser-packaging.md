# Browser oiapp：构建、分发与验收（候选）

本轮已确认 18 项 Browser Action 的范围及截图策略，含 17 项普通 JSON 和一项支持视口内 `clip` 的 `screenshot`。以下为 Windows/Linux 发行与验收设计。能力与参数见 [browser-actions.md](browser-actions.md)，运行规则见 [browser-lifecycle.md](browser-lifecycle.md)。网页上传下载和原文件传输暂缓，独立包仍待构建验收。

## 1. 现有仓库与本次新增边界

已核对当前仓库：根 [scripts/build.ps1](../../../../../scripts/build.ps1) 构建 Windows/Linux amd64 的 OIS、OIC，并分别构建 Bash、PowerShell 两个独立 Go App；它目前不会构建 Browser。[现有 CI](../../../../../.github/workflows/test.yml) 也只覆盖根 Go 模块及两个终端 App。不能把现有 `go test ./...` 或根构建成功作为 Browser 的验证结果。

Browser 源码放在 `apps/browser/`，使用独立的 `package.json`、`package-lock.json`、构建脚本和测试。发行含 Node、worker、两种纯 JS 图像编解码依赖与配套 Chromium，两平台共用截图代码。源码复制到仓库外后仍应能安装构建依赖、构建与测试。17 项复用普通 JSON，screenshot 使用共享 content.v1，完整发行依赖支持该格式协商的 OIC/OIS。

首版发布两个平台包。Node 的 `x64` 对应现有 Go 产物命名中的 `amd64`，不是新增 CPU 平台。

| 包 | 正式验收基线 | 明确边界 |
| --- | --- | --- |
| `browser-windows-x64.zip` | Windows 11 x64，普通用户，本地文件系统 | Windows Server、旧 Windows、ARM64 不因二进制可启动就自动列入支持。 |
| `browser-linux-x64.tar.gz` | Ubuntu 24.04 x86-64，非 root，系统依赖及 Chromium sandbox 可用 | 不宣称覆盖全部 Linux；Alpine/musl、其他发行版、容器、ARM64 分别需要后续验证。 |

这两个基线处于 Playwright 官方支持平台范围内，但项目支持仍以本文件的实际验收为准。macOS 不在本轮范围，现有 OIC 进程托管也没有 macOS 实现。[Playwright 系统要求](https://playwright.dev/docs/intro#system-requirements)

## 2. 版本锁与来源

Node/Playwright/Chromium 候选基线来自 2026-11-18 核验的固定版本与历史 smoke。图像处理锁定以下纯 JS 包，两个目标平台使用同一份源码和完整性校验。发行改用配套 Chromium，并在制成包上执行下列测试。版本升级应成组更新锁与回归证据。

| 层 | 候选锁 | 核验依据及状态 |
| --- | --- | --- |
| Node.js | `24.13.0`，按平台选择官方归档 | 本机 smoke 使用该版本；官方提供 win-x64 与 linux-x64 归档。两个发布包均未制作。 |
| Playwright | `1.62.1`；依赖 `playwright-core=1.62.1` | 已核对本机及官方同版本 package.json，Node 要求为 `>=20`。 |
| Chromium | `revision=1234`，`151.0.7922.34` | 来自该版本官方 browsers.json；本机没有下载此浏览器，尚未运行。 |
| PNG 编解码 | `pngjs=7.0.0` | 纯 JS，Node `>=14.19.0`，无 npm 运行依赖。使用 `PNG.sync.read/write` 进行有界解码和无损编码。 |
| JPEG 编码 | `jpeg-js=0.4.4` | 纯 JS，无 npm 运行依赖，BSD-3-Clause。worker 用 `jpeg.encode({width,height,data},80).data` 对同一帧 RGBA 编为 JPEG。将 npm integrity 写入 lockfile。 |
| TypeScript、类型与测试依赖 | package.json 使用精确版本，完整依赖图由 package-lock.json 锁定 | 实施时选定并提交；不借用 Codex 自带工具链充当项目锁。 |

来源：[Node 24.13.0 官方归档](https://nodejs.org/en/download/archive/v24.13.0)、[Playwright 1.62.1](https://github.com/microsoft/playwright/blob/v1.62.1/packages/playwright/package.json)、[浏览器 revision](https://github.com/microsoft/playwright/blob/v1.62.1/packages/playwright-core/browsers.json)、[pngjs 固定源码](https://github.com/pngjs/pngjs/blob/c565210c602527eb459f857eeb78183997482d5b/package.json)、[PNG 同步 API](https://github.com/pngjs/pngjs/blob/c565210c602527eb459f857eeb78183997482d5b/README.md#sync-api)、[jpeg-js 发布版本清单](https://github.com/jpeg-js/jpeg-js/blob/9ccd35fb5f55a6c4f1902ac5b0f270f675750c27/package.json)、[JPEG 编码器](https://github.com/jpeg-js/jpeg-js/blob/9ccd35fb5f55a6c4f1902ac5b0f270f675750c27/lib/encoder.js)。`pngjs` 与 `jpeg-js` 均为 Browser 自有直接生产依赖，锁定 npm 包及 integrity。Playwright 与配套 Chromium 按同一版本锁安装。[官方浏览器安装说明](https://playwright.dev/docs/browsers)

源码中的 `release-lock.json` 记录 Node 精确版本、各平台归档 URL/校验值、Playwright/pngjs/jpeg-js 版本、浏览器 revision、选定浏览器通道与构建工具版本。Node 归档以官方 SHASUMS 校验，npm 包校验 lockfile integrity。浏览器首次受控下载后记录实际下载来源、归档校验值和解包结果，后续构建核对。

每个生成包中的 `release-lock.json` 还记录源码 commit、实际构建命令、OS/架构、浏览器实际 `--version`、依赖清单。`checksums.txt` 覆盖分发文件，外层归档另提供 SHA-256。任何校验值都必须来自实际文件，不在正式锁中保留说明性占位值。依赖与浏览器许可证一并放入 `LICENSES/`。

## 3. 发布布局与入口

两个平台使用同一种布局，仅可执行文件后缀和浏览器平台内容不同：

```text
browser-windows-x64/                 # Linux 为 browser-linux-x64/
  app.yaml
  runtime/
    node.exe                        # Linux 为 node，保留执行位
  worker/
    main.cjs                        # 启动 bootstrap 与 JSONL 入口
    ...                             # 编译后的业务模块
    package.json
    package-lock.json
    node_modules/                   # 完整生产依赖及其必需资源
  browsers/                         # 固定 Playwright CLI 产生的匹配布局
    chromium-1234/...
    ...                             # 该平台安装器所需的辅助资源
  release-lock.json
  checksums.txt
  LICENSES/
  README.md
  examples/
    oic.yaml
    ois.tools.yaml
```

不把 Chromium 简化成一个 chrome.exe，不手工删掉它的 DLL、资源、locale 或安装器要求的辅助文件。生产包不需要 npm、TypeScript、Go 或源码；开发/构建机器需要的工具和部署机器需要的文件分别说明。

Windows manifest 的唯一根入口为：

```yaml
runtime:
  command: [./runtime/node.exe, ./worker/main.cjs]
```

Linux 为：

```yaml
runtime:
  command: [./runtime/node, ./worker/main.cjs]
```

18 项 Action 共用该根 runtime，不给单项 Action 配置另一个进程入口。OIC 当前将命令首项相对 App 目录解析，并把该目录设为进程 cwd，因而不需要 shell wrapper；命令参数作为数组交付，不拼接 shell 命令。[实际启动代码](../../../../../internal/oic/worker.go)

`main.cjs` 在加载 Playwright 前，依据自身文件位置解析包根并设置包内 `PLAYWRIGHT_BROWSERS_PATH`。不能依赖从 OIC 环境继承这个变量：当前 OIC 有环境变量白名单。运行时状态与 profile 使用提供方配置的专用可写位置，不写入程序安装目录，也不向业务调用暴露本地文件入口。

图像处理作为随 worker 编译的 `observations/image-worker` 线程入口，通过 Node worker_threads 在内存使用包内 `pngjs`、`jpeg-js`。主线程维持 JSONL 和控制调度，单实例最多一个图像处理任务。根据同一帧 CSS 视口截图返回完整 PNG、完整 JPEG Q80、缩小概览或指定矩形的原像素 PNG。构建保留线程入口、两种编解码器的生产包和许可证。Windows/Linux 制成包均执行一次真实 PNG 和 JPEG 编码并检查解码结果。

## 4. 构建脚本与命令

以下脚本名、npm scripts 和命令是实施后应提供的接口，当前仓库尚无这些 Browser 脚本；本轮没有执行它们。

- `apps/browser/build.ps1`：在 Windows x64 构建机上调用统一打包器。
- `apps/browser/build.sh`：在 Linux x64 构建机上调用同一打包器。
- `apps/browser/scripts/package.mjs`：核对版本锁、准备对应平台 Node、安装锁定依赖、编译 worker、生成 manifest/样例/清单、组装并验证包。
- Browser 的构建与发布需要两个平台分别执行；TypeScript 编译成功或取得另一平台的浏览器归档，不能代替该平台 Node、浏览器及系统库运行验证。

从仓库根执行的拟定入口：

```powershell
./apps/browser/build.ps1 -Target windows-x64
```

```bash
./apps/browser/build.sh --target linux-x64
```

每个平台打包器执行同一逻辑：

1. 检查构建机平台和锁定工具版本；取得并校验该平台 Node 官方归档。构建的网络访问发生在此阶段，不能延迟到 OIC load 或业务调用。
2. 在独立源码目录执行 `npm ci`、类型检查和单元/协议测试，编译 TypeScript；运行与构建依赖分别确定。
3. 在生成包的 `worker/` 目录用同一 package-lock 安装生产依赖，确认 `playwright`、`playwright-core`、`pngjs` 与 `jpeg-js` 都是锁定版本。
4. 设置生成包的绝对 `PLAYWRIGHT_BROWSERS_PATH`，用**包内 Node + 包内 Playwright CLI**执行 `install --no-shell chromium`。该方案只使用完整 Chromium 的新 headless 模式，worker 必须固定 `channel: 'chromium'`；不能省略 channel 后误找未分发的 headless-shell。[通道与 no-shell](https://playwright.dev/docs/browsers#chromium-new-headless-mode)
5. 从同一接口源生成两平台 manifest 与 OIS Tool 示例的 18 项 Action，其中仅 screenshot 使用 content.v1。保留运行所需 Playwright、pngjs、jpeg-js 的资源与许可证。
6. 在不含源码、全局 Node/npm、个人浏览器或浏览器缓存的环境中，使用生成包运行自检与 OI 端到端验收，再生成 checksums 与压缩包。

用于说明第 4 步的底层命令，在生成包根目录运行：

```powershell
$env:PLAYWRIGHT_BROWSERS_PATH = Join-Path $PWD 'browsers'
./runtime/node.exe ./worker/node_modules/playwright/cli.js install --no-shell chromium
```

```bash
PLAYWRIGHT_BROWSERS_PATH="$PWD/browsers" \
  ./runtime/node ./worker/node_modules/playwright/cli.js install --no-shell chromium
```

这两条命令用于构建/制包阶段，不是部署用户每次启动时执行的安装步骤。最终包解压即可获得匹配的 Node、worker 和 Chromium；Linux 的系统库仍按下一节准备。支持离线部署包时，需另外提供所支持发行版的系统依赖准备说明，不能把携带浏览器二进制描述为“任何裸机都零依赖”。

## 5. Chromium sandbox 与系统依赖

首版正式启动固定 `headless: true`、`channel: 'chromium'`、`chromiumSandbox: true`。Playwright 默认 `chromiumSandbox=false`，对应版本源码会添加 `--no-sandbox`；因此不能只是不手写这个 flag 就声称启用了 sandbox。[固定版本实现](https://github.com/microsoft/playwright/blob/v1.62.1/packages/playwright-core/src/server/chromium/chromium.ts#L388-L389)

启动失败时返回有界的浏览器启动错误及诊断，不自动改成 `chromiumSandbox:false`，不要求消费者在 Action 中提供任意 args。Browser profile 隔离、Chromium sandbox、OI 访问身份是不同边界；它们都不能代替提供方的网络策略。

Linux 使用非 root 账户运行 OIC/worker。Ubuntu 24.04 的共享库和字体依赖以**锁定版本** Playwright 的 nativeDeps 为来源，包括 NSS、GBM、X11/ATK/Pango 等库及字体；由部署管理员在安装阶段准备，不由 worker sudo 或 apt。可先查看安装计划，再显式安装：

```bash
./runtime/node ./worker/node_modules/playwright/cli.js install-deps --dry-run chromium
sudo ./runtime/node ./worker/node_modules/playwright/cli.js install-deps chromium
```

这会改变系统软件包，属于部署步骤而非 OIC load。无管理员权限时，由提供方预置等价依赖。验收还要记录实际 glibc/OS 版本、字体和 sandbox 可用性；启动缺库或系统策略禁止 sandbox 时应失败，不自行降级。headless 默认不需要 Xvfb，不能因此省略共享库和字体。[固定版本 Linux 依赖](https://github.com/microsoft/playwright/blob/v1.62.1/packages/playwright-core/src/server/registry/nativeDeps.ts)、[官方安装依赖说明](https://playwright.dev/docs/browsers#install-system-dependencies)

本轮不发布容器作为额外运行后端。若以后增加容器，需单独验证用户命名空间、seccomp、共享内存及非 root 配置；官方 Playwright Docker 镜像默认 root 会禁用 Chromium sandbox，不是可直接继承的安全运行承诺。[官方 Docker 说明](https://playwright.dev/docs/docker)

Windows 不依赖已安装 Chrome/Edge，不向现有个人 profile 写入内容；使用包内 Chromium及受控状态目录。需要实际验证普通用户权限、包含空格/中文的安装路径、杀毒软件/文件占用导致的错误，以及 Chromium 自身 sandbox 与 OIC Job 的进程关系。企业策略阻止子进程或二进制时报告启动失败，不绕过系统策略。

## 6. OI 集成与进程生命周期

OIC 加载与管理仍沿用现有命令，部署示例从已解压的部署根目录执行：

```text
oic --config oic.yaml start
oic --config oic.yaml load apps/browser/app.yaml
oic --config oic.yaml apps
oic --config oic.yaml unload browser
```

`oic.yaml` 的 `app_config.browser` 提供 Browser 运行配置；修改后按现有约定重启 OIC。`load` 不创建 OIS Tool；发行包中的 `ois.tools.yaml` 必须包含固定 `channel/app/action` 绑定及对应 vMCP 示例，并按 OIS 已有配置导入/管理规则启用。不同平台的实例可都使用 app code `browser`，由 channel 区分；同一 OIC 的多实例使用不同 app code 和配置键。

worker 在 `start.config` 到达后完成快速配置校验、程序/目录存在性检查和 handler 登记，及时发送 `ready`；当前 OIC 等待 ready 的期限为 10 秒。此阶段不下载依赖、不启动 Chromium、不递归校验整份大型浏览器包。完整安装包校验在安装/制包阶段完成，浏览器启动在 `create_session` 内完成。

当前线协议只支持请求及对应结果。浏览器事件保留在 worker 的有界状态中，经 Action 回执或查询交付。stdout 仅为完整 JSONL，日志与诊断写 stderr。17 项复用普通 JSON，screenshot 的 content.v1 含无 data: 前缀的 PNG/JPEG Base64 及元数据。MCP 产生原生 image，REST 保持内联内容。

部署兼容必须显式说明：旧 OIC 加载声明新格式的 manifest 失败；支持格式的新 OIC 与旧 OIS 连接时仅该连接的 screenshot 不可用，17 项 JSON Action 仍可调用，本地 worker 保持 Ready。正式 18 项验收使用支持 content.v1 的双端和实际图像消费者；不能将 Base64 字符串作为普通文本返回当作兼容降级。

已有 OIC 将 Node worker 纳入受管理进程树：Windows 使用 kill-on-close Job；Linux 使用 subreaper supervisor 回收子孙进程。Browser 必须保持该生命周期归属，不启动外部常驻服务。正常关闭由 worker 关闭 context/browser 并清理其临时资源；unload、OIC stop、worker 故障后的最终回收仍需要两平台实测，不能只凭已有终端测试认定 Chromium 全进程树已被覆盖。[Windows 托管](../../../../../internal/procgroup/group_windows.go)、[Linux 托管](../../../../../internal/procgroup/group_linux.go)

## 7. 测试矩阵与发布门槛

在现有 CI 外增加 Browser 独立任务。依赖安装与 TypeScript 检查在两个目标平台执行；浏览器行为测试使用**制成包中的** Chromium。CI runner 标签、OS 版本和实际执行架构留入报告；Windows Server runner 的通过不替代 Windows 11 正式平台验证。仓库外独立构建与不安装开发工具的部署验证是两种不同测试，都要覆盖。建议分组为 [6 → 11 → 1](browser-runtime.md)，下表是 18 项完成后的共同门槛。

| 验证组 | Windows 11 x64 / Ubuntu 24.04 x64 都需验证 | 可审查证据 |
| --- | --- | --- |
| 包与独立性 | 仓库外源码构建；包内版本/哈希一致；清洁部署环境无系统 Node、Chrome、Codex 仍可 load/create_session | 构建日志、release-lock、checksums、安装路径与 OS/架构记录 |
| sandbox/依赖 | 显式 sandbox 成功；缺库、缺浏览器、目录权限不足均明确失败；无 no-sandbox 回退 | 启动配置及受限诊断；实际 sandbox 状态检查记录，不能仅凭配置布尔值 |
| 协议与身份 | start/ready、恰好 18 handlers、17 项 JSON+1 项 content.v1、并发结果不交错、cancel、完整帧预算；跨 identity/vMCP/实例句柄拒绝 | 脱敏 JSONL 与 HTTP/MCP 请求回执；manifest 不注册 upload_files/downloads |
| 浏览器交互 | 导航、正文/控件 snapshot、role/label 唯一性、旧 ref detach、frame 导航、表单与条件等待、popup 找回；纯图像内容不虚报已理解 | 本地确定性 fixture、服务端结果及结构化观察断言 |
| 会话/profile | 从空专用目录完成普通登录表单，关闭重建后验证站点登录态；会话到期可重新登录；其他主体拒绝；临时会话隔离、profile 独占和失败会话找回 | 测试站点服务端会话记录及 session/profile 生命周期；不保存真实凭据 |
| 登录边界 | 需要 CAPTCHA、设备或人工可视操作的流程明确停下；不附着个人浏览器、不导入 cookie 或复制个人 profile | 不支持情境的观察与返回状态；可关闭本会话的证据 |
| 弹窗与控制 | confirm 在 click pending 时可处理；beforeunload、操作超时、迟到事件及最小容量下的控制路径不被 tab 锁阻塞 | 事件顺序、operation 状态与最终页面结果 |
| 文件交互拒绝 | acceptDownloads=false；直接文件控件、脚本 chooser、立即/延迟下载不选择本地文件、不发布成品；可归因的操作返回 UNSUPPORTED_INTERACTION，异步事件不改判旧操作；仍可普通操作及关闭 | 触发/处置事件、文件控件状态、专用临时目录清理及无交付路径；不以“零网络请求”验收 |
| 普通 JSON 消费 | 真实 HTTP 客户端和 MCP 消费者均经 OIS→OIC 操作同一测试站点；收到正文、控件、待处理状态及最终业务反馈 | 请求与 JSON 结果、服务端断言；该闭环不依赖 SDK、视觉模型或其他 oiapp |
| 整屏截图选择 | 构造 PNG 低于 384 KiB、超过 384 KiB 但完整帧恰好可装、原尺寸 PNG 超帧而同帧 JPEG Q80 可装、原尺寸两种都超帧四类输入；降低 `max_result_frame_bytes` 验证小图也可能超限。逐档验证概览的下限、错误与并发控制。 | 统计每次只抓一次页面；断言实际序列化长度、PNG/JPEG 签名与解码尺寸、`mime_type`、`encoded_bytes`、`source_rect`、`resized`，确认图像线程取消时其他控制调用仍可用。 |
| 局部原像素 | 对已滚动页面及高 DPI 会话指定视口内矩形，覆盖边缘像素、小矩形及显式全屏矩形。验证小数、负坐标、非正宽高、越界、捕获期间主文档替换以及低帧预算。 | 解码局部 PNG，与同一源帧指定矩形逐像素比较，`resized=false`。预算不足只接受 `IMAGE_PREVIEW_TOO_LARGE`，缩小 `clip` 后重试。未滚动页面，未截到视口外像素。 |
| 图像与小字真实交付 | 在 1280×720 及 1920×1080 图文页面以 canvas 绘制 12/14 CSS px 的随机中文短句、批号与数字。两平台的测试页使用同一份有授权、覆盖测试字符的本地字体，等待字体就绪后绘制并记录字体版本。用真实 MCP 视觉客户端读取整屏及原像素 `clip`，记录原尺寸 JPEG Q80 与局部 PNG 的逐字识读结果。DOM、可访问名称和 metadata 均不含答案。 | MCP image 类型、真值、识读文本及逐项比对；REST 解码内联 PNG/JPEG 并核对实际尺寸和编码字节。OCR 样例、元信息或 Base64 生成只作诊断。 |
| 图像兼容 | 新 OIC+旧 OIS 仅 screenshot 不可用，17 项 JSON 可用且本地 worker Ready；旧 OIC+新 manifest 加载失败 | 连接协商、Action 可用状态及实际调用记录 |
| 回收与故障 | close、unload、stop、Node 异常退出、浏览器崩溃、执行中断；无本实例残留 Chromium | 前后进程树与临时目录检查；持久 profile 按约定保留 |

完成一个本地页面点击或生成 Base64 不等于 18 项已交付。发布门槛是：两个正式平台均通过独立包、JSON/图像 OI 链路、页面交互、专用 profile 首次登录与复用、不支持文件交互的边界及回收验证；任一平台未通过则如实记录未完成，不能用另一平台结果代替双平台 Done。共享 content.v1 升级须与旧 Bash/PowerShell 普通 JSON 调用共存回归；Browser 本身不以安装 Filesystem App 作为前置条件。

验收报告记录日期、源码 commit、包 SHA-256、OS/架构、Node/Playwright/Chromium/pngjs/jpeg-js 版本、执行命令、测试项及失败/跳过原因。保存本地 fixture 的脱敏结果及图像交付断言。运行日志不写图像字节，公开验收材料不含敏感网站、真实用户 profile、Cookie、Token。尚未执行的条目标为未执行。

## 8. 历史验证与当前缺口

2026-11-18 在本机 Windows 执行过两组短 smoke，使用系统 Node 24.13.0、Codex bundled Playwright 1.62.1，显式启动系统 Chrome 157.0.8103.79 的临时无头实例，只访问本地临时 HTTP 页面；未安装依赖、未使用个人 profile，浏览器及 HTTP 服务均正常关闭。这是前期探索记录，不是当前 18 项发行包的验收报告。

- 基础 smoke：导航、标题、role locator 填写/点击、结果等待、ARIA snapshot 通过，整组约 2 秒；当时还生成过 8,070 字节的内存 PNG。该结果只证明当时组合可以捕获 PNG，不证明本轮 screenshot 契约、缩图预算或真实消费者图像交付已通过。
- 元素身份 smoke：旧 ElementHandle detach 后，同名新节点存在；旧 handle 点击返回 `Element is not attached to the DOM`，新旧节点点击计数均为 0。
- confirm smoke：先收到 dialog 事件，原 click 在之后 100 ms 内仍 pending；accept 后 click 完成，页面显示 Accepted。事件顺序为 `dialog_received → accept_requested → accept_completed → click_completed`。

本轮另在 Windows 从同一测试页的原始 PNG 做过编码烟测。随本机工具提供的 pngjs 7.0.0 解码后由 jpeg-js 0.4.4 固定质量 80 编码。1280×720 图文页的 PNG/JPEG 分别为 531099/215290 B；1920×1080 图文页分别为 1041293/380760 B。JPEG 均实际解码并核对了原尺寸。它们与[预算复核](image-preview-review.md)的 Chromium 直接 JPEG 是两种编码路径，同一 Q80 不保证相同字节数。

历史记录见[笔记](notes.md)。配套 Chromium revision 1234、目标 Linux、正式 OI 双端、正式包的双平台沙箱与进程回收、低预算分支和经 MCP 的真实小字识读仍须按矩阵执行。
