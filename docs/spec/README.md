# loom-oi 项目规范

这里维护 beta 当前有效的产品与工程口径，供后续开发查阅和遵循。首次整理于 2026-11-10，以已确认的首版目标、实现提交 `79ea9ff` 和独立验收记录为依据；后续变更应同步更新相关正文与本入口。

oi 的目标是让 Agent 调用不同机器上的受控能力。目前提供 HTTP API 与 MCP 接入，本版通过 MCP 验证完整流程，未来会加入更多消费方式。具体范围见[目标与范围](goals/beta.md)。

## 从哪里读起

| 阅读目的 | 入口 | 内容 |
| --- | --- | --- |
| 了解本版完成了什么、哪些能力还没有 | [目标与范围](goals/beta.md) | 首版使用流程、两端交付、Done、证据及后续边界 |
| 统一术语与标识 | [概念模型](concepts/model.md) | OIS/OIC、Host、通道、App、Action、Tool、vMCP，以及调用与执行句柄 |
| 判断改动是否符合产品取舍 | [设计原则](principles/design.md) | 职责解耦、固定绑定、轻量适配、取消与访问默认 |
| 理解模块如何配合 | [系统架构](architecture/system.md) | 调用链、进程边界、加载发现、持续执行和故障生命周期 |
| 开发、配置或接入具体接口 | 下列技术参考 | 当前字段、命令、协议和行为边界 |

初次阅读建议先看目标、概念和架构；修改某个模块时再进入对应参考。部署和首次运行直接看[运行指南](../guides/build-and-run.md)，切换访问方式看[配置示例](../guides/access-configuration.md)。

## 技术参考

| 文档 | 覆盖范围 |
| --- | --- |
| [CLI](reference/cli.md) | OIS、OIC、终端 worker 的实际命令、参数、默认和生命周期操作 |
| [OIS API](reference/ois-api.md) | 管理 API、Host/App/Action 查询、REST 执行、MCP 与 OIC 接入协议 |
| [OIS/OIC 配置](reference/configuration.md) | 启动配置、持久定义、HTTP/HTTPS、各范围 Token 与路径规则 |
| [App YAML 与 runtime](reference/app-runtime.md) | Action Schema、参数适配、runtime 覆盖、worker 协议、加载及卸载 |
| [终端 Action](reference/terminal-actions.md) | Bash/PowerShell 的参数、结果、会话、TTY、输出预算、取消与回收 |
| [目录与工程约定](reference/repository.md) | 主工程与独立 App 模块、构建交付、文档、运行产物及验证入口 |

参考描述的是本项目实际支持的范围，不能据此推定实现了完整 JSON Schema、全部 MCP 可选能力或所有操作系统平台。多 runtime App 按已确认规则逐个初始化，每个 worker 的 ready 等待上限为 10 秒，不设整次 load 总共 10 秒的截止时间。

独立 App 的构建、加载与开发入口见 [apps](../../apps/README.md)；Bash 和 PowerShell 各自携带 YAML 与实现，不依赖主工程源码。CU 仍属后续范围。

## 规范与证据的关系

`docs/spec/` 是后续维护的规范入口；[goal-shaping 当前共识](../goal-shaping/current/beta/design.md)保留本轮已经确认的产品意图，[首版验收规格](../goal-shaping/current/beta/acceptance.md)保留具体完成判据。代码与规范若发生实质差异，需要标明事实和要求并处理差异，不能仅凭某一侧存在就宣称另一侧一致。

首轮显式 HTTPS/Token 部署的 A01–A13 结果见[验收报告](../reports/beta/acceptance-report.md)；默认匿名模式及四种访问配置的后续结果见[独立访问验证](../reports/beta/access-verification.md)。这些文件保留当时的条件和结果。后续独立 App 重构、仓库外构建、既有 OIC 兼容及相关功能回归见[独立 App 验收](../reports/beta/app-independence.md)；验证状态以各报告的范围和时间为准。

本轮过程见[归档设计](../goal-shaping/archive/beta/2026-11-10-first-flow/design.md)和[归档笔记](../goal-shaping/archive/beta/2026-11-10-first-flow/notes.md)。归档中的旧候选方案、图中旧工具名和历史 TLS 默认不扩大现行规范范围；新一轮讨论使用新的讨论标识，归档快照保留。
