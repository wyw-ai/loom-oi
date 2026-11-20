# 目录与工程约定

本页描述当前 beta 的代码、配置、文档和运行产物组织。接口与字段见同目录的 [CLI](cli.md)、[OIS API](ois-api.md)、[配置](configuration.md)、[App runtime](app-runtime.md)及[终端 Action](terminal-actions.md)。

## 源码与交付

| 路径 | 职责 |
| --- | --- |
| `cmd/ois/` | OIS CLI 程序入口 |
| `cmd/oic/` | OIC CLI 程序入口 |
| `internal/ois/` | 中心配置、API、MCP、连接与调用交付 |
| `internal/oic/` | 本地配置、输入适配、worker 生命周期与 OIS 连接 |
| `internal/protocol/` | WS 和 JSONL 共用的业务无关消息结构 |
| `apps/bash/` | 独立 Linux Bash App：YAML、Go 模块、worker、PTY/会话实现、依赖、构建与测试 |
| `apps/powershell/` | 独立 Windows PowerShell App：YAML、Go 模块、worker、ConPTY/会话实现、依赖、构建与测试 |
| [apps/filesystem](../../../apps/filesystem/README.md) | 扩展版独立 Go 模块，根内文件操作、图像预览、平台包生成和测试 |
| [apps/browser](../../../apps/browser/README.md) | 扩展版独立 Node.js / TypeScript 包，Playwright、Chromium、图像处理和原生平台打包 |
| `internal/procgroup/` | OIC 的 Linux/Windows worker 进程托管及回收兜底；App 自有子进程管理实现 |
| `internal/servicecmd/` | CLI 后台进程及本地控制服务 |
| `examples/ois.yaml` | OIS 初始化配置示例 |
| `examples/oic.linux.yaml`、`examples/oic.windows.yaml` | 各平台 OIC 配置示例 |
| `scripts/` | 构建、部署、MCP 客户端、验收及辅助程序 |
| `.github/workflows/test.yml` | Linux/Windows 测试、vet、Linux race 与 Windows 构建配置 |

主模块为 `github.com/wyw-ai/loom-oi`；两个 App 分别是 `github.com/wyw-ai/loom-oi/apps/bash` 与 `github.com/wyw-ai/loom-oi/apps/powershell`，各自拥有 `go.mod` / `go.sum`，声明 Go 1.25.0。现有 CI 固定 Go 1.26.3。主模块不导入 App，App 也不导入主模块或彼此，不使用 `go.work` 或本地 `replace`；目录可整体复制到独立仓库构建。

`scripts/build.ps1` 分别进入三个模块构建：`dist/{linux,windows}-amd64/` 保存对应平台的 OIS/OIC；`dist/apps/bash/` 与 `dist/apps/powershell/` 是包含 YAML 和 runtime 的独立运行包。当前产物为 amd64。构建与运行要求见各自 [Bash README](../../../apps/bash/README.md)、[PowerShell README](../../../apps/powershell/README.md)。

App 需要一份 YAML 描述及可用的 runtime 入口，描述文件名不强制为 `app.yaml`，runtime 也可以引用已安装程序。当前两个 App 使用 `app.yaml`，分别指向包内的 `runtime/oiapp-bash` 和 `runtime/oiapp-powershell.exe`。业务动作可以共同编译进一个 worker，不要求每个 Action 各建一个可执行文件或目录。配置路径解析和覆盖规则见 [App runtime](app-runtime.md)。

## 文档入口

| 位置 | 阅读用途 |
| --- | --- |
| [docs/README.md](../../README.md) | 文档总入口，按使用、开发、验证与历史回溯选择阅读路径 |
| [docs/spec](../README.md) | 后续开发查阅的当前项目规范，按职责维护正文与导航 |
| `docs/guides/`：[运行指南](../../guides/build-and-run.md)、[访问配置](../../guides/access-configuration.md) | 构建、部署、连接、调用和验证的操作步骤；完整规则引用 spec |
| `docs/reports/beta/`：[首版报告](../../reports/beta/acceptance-report.md)、[访问模式验证](../../reports/beta/access-verification.md) | 按版本保留已执行验证的配置、结果与证据摘要 |
| `docs/goal-shaping/current/beta/` | beta 已确认设计共识与首版完成标准；设计本身不证明已实现 |
| `docs/goal-shaping/archive/beta/2026-11-10-first-flow/` | 已结束讨论的设计、笔记和图集快照；后续演进不继续改写该快照 |
| [Filesystem 与 Browser 开发契约](oiapps/README.md) | 封板后的规范入口，含两个 App 的输入输出 Schema、发行清单、OIC/OIS 配置与调用样例 |
| [oiapp 扩展封板归档](../../goal-shaping/archive/oiapp-expansion/2026-11-20-initial-scope/design.md) | 截至 2026-11-20 的设计快照、取舍和实验，发行验收依据见[扩展目标](../goals/oiapp-expansion.md) |

后续开发先从[规范入口](../README.md)按改动范围查阅。修改能力时同步相关契约、使用步骤和导航；验收证据保留当时条件，不能把历史 HTTPS 地址或未提交状态当作当前部署、Git 状态。

## 本地产物与验证

`dist/` 保存构建产物，`.local/` 保存本次部署配置、凭据、证书、原始日志和验收记录，`.runtime/` 是忽略提交的运行目录之一。这些目录及可执行文件、测试产物不进入 Git；可提交的证据摘要使用脱敏内容和校验值。

OIS/OIC 的数据路径由配置决定；部署包、App、持久配置与运行状态的具体路径不是源码目录的固定要求。`state_dir` 的本地控制文件、OIS 的持久定义、OIC 的加载记录和 App 的内存执行状态互不等价，详见[配置参考](configuration.md)。

代码验证使用 `go test -count=1 -timeout=180s ./...` 与 `go vet ./...`。根模块测试不包含嵌套模块：Bash 须在 Linux 单独测试，PowerShell 须在 Windows 单独测试；CI 将各 App 源码复制到仓库外，在 `GOWORK=off` 下独立构建、测试和 vet。Linux 对根模块和 Bash 另有 race 检查。真实 PTY/ConPTY、长命令和进程回收按目标平台与验收规格验证。文档维护检查链接、字段覆盖和来源，不以此声称功能测试重新通过。运行命令见[运行指南](../../guides/build-and-run.md)。

当前工作使用 `beta` 独立 worktree；保留 main、alpha 和历史实验 worktree，尤其不修改 alpha 的未提交内容。规范维护和讨论归档不自动触发 commit、push、发布或重新部署。
