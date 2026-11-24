# 文档导航

oi 让 Agent 调用不同机器上的受控能力，目前支持 HTTP API 与 MCP 接入。本版通过 MCP 验证完整使用流程，未来会加入更多消费方式。

按阅读目的选择入口：

| 目录 | 用途 | 从这里开始 |
| --- | --- | --- |
| `guides/` | 构建、部署、调用及访问配置的操作步骤 | [Host A 截图与文件服务](guides/service-files.md)、[构建与运行](guides/build-and-run.md)、[HTTP/HTTPS/Token 配置](guides/access-configuration.md) |
| `spec/` | 后续开发遵循的当前概念、原则、架构、目标和技术契约 | [规范入口](spec/README.md) |
| `reports/` | 按版本保留已执行验证的结果和证据 | [Host A 桌面视觉验收](reports/service-files/acceptance.md)、[beta 首版验收](reports/beta/acceptance-report.md)、[访问模式验证](reports/beta/access-verification.md)、[独立 App 验收](reports/beta/app-independence.md)、[首版证据摘要](reports/beta/acceptance-evidence.json) |
| `goal-shaping/` | 已确认的目标、验收清单与讨论快照 | [beta 共识](goal-shaping/current/beta/design.md)、[oiapp 扩展共识](goal-shaping/current/oiapp-expansion/design.md)、[beta 归档](goal-shaping/archive/beta/2026-11-10-first-flow/notes.md)、[oiapp 扩展归档](goal-shaping/archive/oiapp-expansion/2026-11-20-initial-scope/notes.md) |

首次使用先读指南；修改实现先查对应规范。指南中的操作示例引用规范的完整规则，避免维护多套字段或生命周期说明。报告保留测试当时的配置、状态和结果，不代表实时部署状态；讨论归档保留历史候选与取舍，不扩大当前交付范围。

Filesystem 7 项、Browser 18 项的目标已于 2026-11-20 封板。开发从[扩展目标](spec/goals/oiapp-expansion.md)与[Action、Schema 和 YAML](spec/reference/oiapps/README.md)进入。Windows 和 Linux 实测见[开发验证](reports/oiapp-expansion/verification.md)。准备发行时使用[验收清单](goal-shaping/current/oiapp-expansion/acceptance.md)。图像预算的实测依据保存在[小字对照实验](goal-shaping/archive/oiapp-expansion/2026-11-20-initial-scope/image-preview-review.md)。

Service 与官方 File Store 首版已交付。通过 Host A 的交互桌面、服务端 AList v3.66.0 和 MCP 图像完成了 Agent 读图验收。[首版目标](spec/goals/service.md) · [Service 发布接口](spec/reference/service.md) · [设计讨论归档](goal-shaping/archive/service/2026-11-23-first-release/notes.md)

新增文档放入对应目录，并同步本入口和受影响的链接。根目录仅保留导航，运行数据、凭据及原始测试日志仍留在 Git 忽略的 `.local/`，不随报告提交。

创造者管理 Service 和读取现有 Skill 的 OI 侧目标已于 2026-11-24 封板。两款独立 OIApp 已通过本地集成测试。[管理配置](../apps/loom-oi/README.md) · [Skill 根与消费权限](../apps/skills/README.md)[当前目标](goal-shaping/current/creator-workspace/design.md) · [接口、配置与验收](spec/goals/creator-workspace.md) · [封板归档](goal-shaping/archive/creator-workspace/2026-11-24-service-skills/notes.md)
