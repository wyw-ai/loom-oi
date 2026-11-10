# loom-oi beta

让 Agent 通过一个 MCP 入口调用不同机器上的受控能力。beta 使用固定配置直接连接 OIS、OIC 与独立 App。

当前阶段：**首版已实现、部署，A01–A13 验收全部通过。** [查看验收报告](docs/acceptance-report.md)

首版在 151 Linux 部署 OIS 与 Linux OIC，在本机运行 Windows OIC；Windows 上的 PowerShell 7 MCP 协议验收脚本连接一个 `workbench` vMCP，使用 Linux Bash 和 Windows PowerShell 7 两组固定命令工具。每组包含 `exec_command` 与 `write_stdin`，不做同一 Tool 的多目标路由。

## 使用

- [构建、目录、CLI、部署和调用说明](docs/implementation.md)
- [默认匿名接入与可选 HTTPS / Token](docs/access-configuration.md)
- [访问模式切换的实测记录](docs/access-verification.md)
- [OIS 配置](examples/ois.yaml)、[Linux OIC](examples/oic.linux.yaml)、[Windows OIC](examples/oic.windows.yaml)
- [Bash App](examples/apps/bash/app.yaml)、[PowerShell App](examples/apps/powershell/app.yaml)

```powershell
./scripts/build.ps1
./scripts/deploy.ps1 -SkipBuild
./scripts/acceptance-commands.ps1
```

构建需要 Go 1.25+，Windows 命令执行使用 PowerShell 7。默认部署使用 HTTP、无需 Token 或证书，运行状态保存在忽略提交的 `.local`。MCP 地址为 `http://121.199.175.151:25100/mcp/workbench`。需要时可分别启用 HTTPS 和 Token。

## 当前规格

- [当前已确认共识](goal-shaping/current/beta/design.md)
- [首版验收规格：151 Linux 与本机 Windows](goal-shaping/current/beta/acceptance.md)
- [从 App YAML 到 Agent 调用：双端固定工具完整流程](goal-shaping/drafts/beta/first-flow/app-to-agent-walkthrough.md)

## 设计材料与沿革

- [首个使用流程设计总稿](goal-shaping/drafts/beta/first-flow/design.md)
- [命令 App 的 Action、参数与执行机制详稿](goal-shaping/drafts/beta/first-flow/bash-action-design.md)
- [OIS API 设计](goal-shaping/drafts/beta/first-flow/ois-api-design.md)
- [固定调用链示意图](goal-shaping/drafts/beta/first-flow/diagrams/2026-11-09-v2/README.md)
- [历史：多 Host 模块交互与 Bash 生命周期图集](goal-shaping/drafts/beta/first-flow/diagrams/2026-11-09-v1/README.md)
- [沟通笔记与演进记录](goal-shaping/drafts/beta/first-flow/notes.md)

当前共识与验收规格是本版权威；早期设计材料和历史图不能扩大本版范围。CU、多目标路由、热更新和跨重启执行恢复不属于本版。
