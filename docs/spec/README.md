# loom-oi 项目规范

这里维护已交付的 beta 契约，以及 2026-11-20 封板的 Filesystem 与 Browser 扩展目标。运行现有系统时查 beta 参考。开发两个新 App 或图像结果时查扩展契约。实测范围见 [分平台开发验证](../reports/oiapp-expansion/verification.md)。

oi 的目标是让 Agent 调用不同机器上的受控能力。目前提供 HTTP API 与 MCP 接入，本版通过 MCP 验证完整流程，未来会加入更多消费方式。具体范围见[目标与范围](goals/beta.md)。

## 从哪里读起

| 阅读目的 | 入口 | 内容 |
| --- | --- | --- |
| 核对当前可部署的能力 | [beta 目标与范围](goals/beta.md) | 首版终端流程、两端交付、Done 和证据 |
| 开发 Filesystem 与 Browser | [扩展目标与完成判据](goals/oiapp-expansion.md) · [两个 App 的接口契约](reference/oiapps/README.md) | 25 个 Action、两份 Schema、App 清单、部署示例和待执行的验收 |
| 统一术语与标识 | [概念模型](concepts/model.md) | OIS/OIC、Host、通道、App、Action、Tool、vMCP，以及调用与执行句柄 |
| 判断改动是否符合产品取舍 | [设计原则](principles/design.md) | 职责解耦、固定绑定、轻量适配、取消与访问默认 |
| 理解模块如何配合 | [系统架构](architecture/system.md) | 调用链、进程边界、加载发现、持续执行和故障生命周期 |
| 开发、配置或接入具体接口 | 下列技术参考 | 当前字段、命令、协议和行为边界 |

初次阅读建议先看目标、概念和架构；修改某个模块时再进入对应参考。部署和首次运行直接看[运行指南](../guides/build-and-run.md)，切换访问方式看[配置示例](../guides/access-configuration.md)。

## 技术参考

| 文档 | 覆盖范围 |
| --- | --- |
| [CLI](reference/cli.md) | OIS、OIC、终端 worker 的实际命令、参数、默认和生命周期操作 |
| [OIS API](reference/ois-api.md) | 已有管理、REST、MCP 与 OIC 接入接口，以及扩展版图像结果的 HTTP/MCP 映射 |
| [OIS/OIC 配置](reference/configuration.md) | 启动配置、持久定义、HTTP/HTTPS、各范围 Token 与路径规则 |
| [App YAML 与 runtime](reference/app-runtime.md) | Action Schema、参数适配、runtime 和 worker 生命周期，以及扩展版 `content.v1` 的协商、预算与兼容规则 |
| [终端 Action](reference/terminal-actions.md) | Bash/PowerShell 的参数、结果、会话、TTY、输出预算、取消与回收 |
| [Filesystem 与 Browser](reference/oiapps/README.md) | 开发用输入输出、25 个 Action 的 YAML、主机配置、会话、失败回执和图像边界 |
| [目录与工程约定](reference/repository.md) | 主工程与独立 App 模块、构建交付、文档、运行产物及验证入口 |

技术参考中，beta 章节描述实际支持的范围，扩展版章节描述本分支的图像与 App 契约。beta 的多 runtime App 按已确认规则逐个初始化，每个 worker 的 ready 等待上限为 10 秒，不设整次 load 总共 10 秒的截止时间。

独立 App 的构建、加载与开发入口见 [apps](../../apps/README.md)；Bash 和 PowerShell 各自携带 YAML 与实现，不依赖主工程源码。CU 仍属后续范围。

## 规范与证据的关系

`docs/spec/` 是后续维护的规范入口。[beta 共识](../goal-shaping/current/beta/design.md)和[首版验收规格](../goal-shaping/current/beta/acceptance.md)保留终端首版判据。Filesystem 与 Browser 的[封板共识](../goal-shaping/current/oiapp-expansion/design.md)与[发布验收清单](../goal-shaping/current/oiapp-expansion/acceptance.md)保留本轮目标及各平台应观察的行为。

首轮显式 HTTPS/Token 部署的 A01–A13 结果见[验收报告](../reports/beta/acceptance-report.md)；默认匿名模式及四种访问配置的后续结果见[独立访问验证](../reports/beta/access-verification.md)。这些文件保留当时的条件和结果。后续独立 App 重构、仓库外构建、既有 OIC 兼容及相关功能回归见[独立 App 验收](../reports/beta/app-independence.md)；验证状态以各报告的范围和时间为准。

历史取舍可分别查阅 [beta 首轮归档](../goal-shaping/archive/beta/2026-11-10-first-flow/design.md)和 [oiapp 扩展归档](../goal-shaping/archive/oiapp-expansion/2026-11-20-initial-scope/design.md)。原件传输与网页上传下载留在扩展讨论记录中，已排在本次发布范围之外。
