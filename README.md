# loom-oi beta

让 Agent 通过 oi 调用不同机器上的受控能力。当前支持 HTTP API 和 MCP 两种接入方式；本版本以 MCP 为入口验证完整使用流程，未来会加入更多消费方式。

beta 使用固定配置直接连接 OIS、OIC 与独立 App。

当前阶段：**首版已实现并完成部署验收，规范已整理，本轮设计讨论已归档。** [首版验收报告](docs/reports/beta/acceptance-report.md)与[访问模式验证](docs/reports/beta/access-verification.md)分别记录原配置和后续默认配置的结果。

首版在 151 Linux 部署 OIS 与 Linux OIC，在本机运行 Windows OIC；Windows 上的 PowerShell 7 MCP 协议验收脚本连接一个 `workbench` vMCP，使用 Linux Bash 和 Windows PowerShell 7 两组固定命令工具。每组包含 `exec_command` 与 `write_stdin`，不做同一 Tool 的多目标路由。

## 使用

完整阅读路径见[文档导航](docs/README.md)，其中区分操作指南、项目规范、验收报告和讨论记录。

- [构建、部署和调用步骤](docs/guides/build-and-run.md)
- [HTTP API 调用](docs/spec/reference/ois-api.md#3-rest-执行)与 [MCP 接入](docs/spec/reference/ois-api.md#4-mcp-入口)
- [默认匿名接入与可选 HTTPS / Token](docs/guides/access-configuration.md)
- [访问模式切换的实测记录](docs/reports/beta/access-verification.md)
- [OIS 配置](examples/ois.yaml)、[Linux OIC](examples/oic.linux.yaml)、[Windows OIC](examples/oic.windows.yaml)
- [独立 App 开发与交付](apps/README.md)：[Bash](apps/bash/README.md)、[PowerShell](apps/powershell/README.md)；[独立构建与兼容验收](docs/reports/beta/app-independence.md)

```powershell
./scripts/build.ps1
./scripts/deploy.ps1 -SkipBuild
./scripts/acceptance-commands.ps1
```

构建需要 Go 1.25+，Windows 命令执行使用 PowerShell 7。默认部署使用 HTTP、无需 Token 或证书，运行状态保存在忽略提交的 `.local`。MCP 地址为 `http://121.199.175.151:25100/mcp/workbench`。需要时可分别启用 HTTPS 和 Token。

## 项目规范

后续开发从 [docs/spec/README.md](docs/spec/README.md) 进入，按改动范围查阅并维护规范：

- [目标与范围](docs/spec/goals/beta.md)、[概念模型](docs/spec/concepts/model.md)、[设计原则](docs/spec/principles/design.md)、[系统架构](docs/spec/architecture/system.md)
- [CLI](docs/spec/reference/cli.md)、[OIS API](docs/spec/reference/ois-api.md)、[OIS/OIC 配置](docs/spec/reference/configuration.md)
- [App YAML 与 runtime](docs/spec/reference/app-runtime.md)、[终端 Action](docs/spec/reference/terminal-actions.md)、[目录与工程约定](docs/spec/reference/repository.md)

## 设计材料与沿革

- [beta 已确认设计共识](docs/goal-shaping/current/beta/design.md)与[首版验收规格](docs/goal-shaping/current/beta/acceptance.md)
- [已归档的首轮设计](docs/goal-shaping/archive/beta/2026-11-10-first-flow/design.md)与[沟通笔记](docs/goal-shaping/archive/beta/2026-11-10-first-flow/notes.md)
- [归档：从 App YAML 到 Agent 调用](docs/goal-shaping/archive/beta/2026-11-10-first-flow/app-to-agent-walkthrough.md)
- [归档：固定调用链图](docs/goal-shaping/archive/beta/2026-11-10-first-flow/diagrams/2026-11-09-v2/README.md)与[早期多 Host 图集](docs/goal-shaping/archive/beta/2026-11-10-first-flow/diagrams/2026-11-09-v1/README.md)

项目规范维护当前口径，讨论归档保留演进过程，验收报告保留实际证据。CU、多目标路由、热更新和跨重启执行恢复不属于本版。
