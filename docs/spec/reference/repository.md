# 目录与工程约定

本页描述当前 beta 的代码、配置、文档和运行产物组织。接口与字段见同目录的 [CLI](cli.md)、[OIS API](ois-api.md)、[配置](configuration.md)、[App runtime](app-runtime.md)及[终端 Action](terminal-actions.md)。

## 源码与交付

| 路径 | 职责 |
| --- | --- |
| `cmd/ois/` | OIS CLI 程序入口 |
| `cmd/oic/` | OIC CLI 程序入口 |
| `cmd/oiapp-terminal/` | 托管终端 App 入口，由 OIC 启动 |
| `internal/ois/` | 中心配置、API、MCP、连接与调用交付 |
| `internal/oic/` | 本地配置、输入适配、worker 生命周期与 OIS 连接 |
| `internal/protocol/` | WS 和 JSONL 共用的业务无关消息结构 |
| `internal/terminal/` | Bash/PowerShell 的命令、终端、输入输出和执行状态 |
| `internal/procgroup/` | Linux/Windows 托管进程与后代回收 |
| `internal/servicecmd/` | CLI 后台进程及本地控制服务 |
| `examples/ois.yaml` | OIS 初始化配置示例 |
| `examples/oic.linux.yaml`、`examples/oic.windows.yaml` | 各平台 OIC 配置示例 |
| `examples/apps/{bash,powershell}/app.yaml` | 两种 App 的 Action、Schema 和 runtime 描述 |
| `scripts/` | 构建、部署、MCP 客户端、验收及辅助程序 |
| `.github/workflows/test.yml` | Linux/Windows 测试、vet、Linux race 与 Windows 构建配置 |

Go module 是 `github.com/wyw-ai/loom-oi`，`go.mod` 声明 Go 1.25.0；现有 CI 固定 Go 1.26.3。目标平台为 Linux 和 Windows；本版实际部署使用 amd64，构建脚本生成两平台的 `ois`、`oic`、`oiapp-terminal`。

App 需要一份 YAML 描述及可用的 runtime 入口，描述文件名不强制为 `app.yaml`，runtime 也可以引用已安装程序。当前示例使用 `app.yaml`，以相对路径指向包内的 `runtime/oiapp-terminal` 或 `.exe`。业务动作可以共同编译进一个 worker，不要求每个 Action 各建一个可执行文件或目录。配置路径解析和覆盖规则见 [App runtime](app-runtime.md)。

## 文档入口

| 位置 | 阅读用途 |
| --- | --- |
| [docs/spec](../README.md) | 后续开发查阅的当前项目规范，按职责维护正文与导航 |
| [运行指南](../../implementation.md) | 构建、部署、连接、调用和验收操作 |
| [访问配置示例](../../access-configuration.md) | HTTP/HTTPS/Token 的操作示例 |
| [首版报告](../../acceptance-report.md)、[访问模式验证](../../access-verification.md) | 已执行验证的配置、结果与证据来源 |
| `docs/goal-shaping/current/beta/` | beta 已确认设计共识与首版完成标准；设计本身不证明已实现 |
| `docs/goal-shaping/archive/beta/2026-11-10-first-flow/` | 已结束讨论的设计、笔记和图集快照；后续演进不继续改写该快照 |

后续开发先从[规范入口](../README.md)按改动范围查阅。修改能力时同步相关契约、使用步骤和导航；验收证据保留当时条件，不能把历史 HTTPS 地址或未提交状态当作当前部署、Git 状态。

## 本地产物与验证

`dist/` 保存构建产物，`.local/` 保存本次部署配置、凭据、证书、原始日志和验收记录，`.runtime/` 是忽略提交的运行目录之一。这些目录及可执行文件、测试产物不进入 Git；可提交的证据摘要使用脱敏内容和校验值。

OIS/OIC 的数据路径由配置决定；部署包、App、持久配置与运行状态的具体路径不是源码目录的固定要求。`state_dir` 的本地控制文件、OIS 的持久定义、OIC 的加载记录和 App 的内存执行状态互不等价，详见[配置参考](configuration.md)。

代码验证使用 `go test -count=1 -timeout=180s ./...` 与 `go vet ./...`；Linux 另有 race 检查。真实 PTY/ConPTY、长命令和进程回收按目标平台与验收规格验证。文档维护检查链接、字段覆盖和来源，不以此声称功能测试重新通过。运行命令见[运行指南](../../implementation.md)。

当前工作使用 `beta` 独立 worktree；保留 main、alpha 和历史实验 worktree，尤其不修改 alpha 的未提交内容。规范维护和讨论归档不自动触发 commit、push、发布或重新部署。
