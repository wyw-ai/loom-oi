# 可选 HTTPS / Token 验证记录

2026-11-10，Asia/Shanghai。按用户新要求，现有 151 与本机部署已切换为 **HTTP + 无 Token**；四个固定工具在线可用。当前入口：`http://121.199.175.151:25100/mcp/workbench`。

配置与使用方法见[接入配置](access-configuration.md)。首轮强制 HTTPS+Token 的历史证据仍保留在[首版报告](acceptance-report.md)，不覆盖旧记录。

## 实际部署验证

`scripts/acceptance-access.ps1` 对约定环境依次切换模式，每种都从本机经 MCP 执行 Linux Bash 与 Windows PowerShell 命令。

| 配置 | 无 Token 管理请求 | 无 Token MCP 初始化 | 合法调用 |
| --- | --- | --- | --- |
| HTTP，无 Token | 200 | 200 | 两端成功 |
| HTTP，有 Token | 401 | 401 | 携带 Token 后两端成功 |
| HTTPS，无 Token | 200 | 200 | TLS 验证后两端成功 |
| HTTPS，有 Token | 401 | 401 | TLS 验证且携带 Token 后两端成功 |

- 从 HTTPS+Token 降级回 HTTP 匿名成功，保留 Tool 绑定、vMCP 成员和 OIC 加载记录。
- HTTPS 测试使用原验收 CA；未显式信任它的客户端被拒绝，未绕过证书验证。公共 CA 证书加载使用相同服务端证书接口，但本轮没有申请新的公共 CA 证书。
- 实际新建 vMCP 时省略整个 `access`，不带认证头即可完成初始化与双端命令调用。
- 默认 HTTP-only 部署创建 `require_https: true` 的 vMCP 返回 400，不保存无效配置。
- 默认匿名模式下重跑四工具发现、短执行、TTY 输入、身份范围、参数与输出场景，全部通过；临时 vMCP 清理完成。
- 最终状态为 2 通道、2 Host、2 App、4 Action、4 Tool、1 workbench，全部在线。

## 代码与集成回归

- Windows 全量 `go test -count=1 -timeout=180s ./...`、`go vet ./...` 通过。
- Linux151 全量 `go test -race -count=1 -timeout=180s ./...` 通过。
- 双真实监听器验证：普通 vMCP 可走 HTTP/HTTPS；要求 HTTPS 的 vMCP 在 HTTP 返回 403、HTTPS 正常初始化。
- 明文请求伪造 Forwarded / X-Forwarded-Proto 不能绕过 HTTPS 要求。
- 显式凭据引用缺失时拒绝认证/连接；匿名模式忽略 Bearer 值，不根据任意字符串创建身份。
- 保留原 HTTPS-only 配置兼容；冲突配置、缺证书、部分监听失败均明确报错并回收资源；关闭超时强制关闭已接受连接。
- OIC 的 HTTP/WS、HTTPS/WSS 登记、调用与重连通过；HTTP 不接受 `ca_file`，HTTPS 不跳过验证。

## 原始记录

以下文件位于忽略提交的 `.local/reports`：

- `access-modes-20261110-023050.jsonl`：四种模式、两端命令及最终恢复。
- `commands-20261110-022827.jsonl`：匿名部署上的完整命令场景。
- `access-create-vmcp.json`：省略 access 的真实创建/调用及无效 HTTPS 配置拒绝。
- `access-final-state.json`：最终在线拓扑。
- `access-windows-tests.txt`、`access-linux-race-tests.txt`：自动化检查输出。

本轮未修改终端 App 业务逻辑，也未重复运行首轮已完成的 600 秒长测；此次验证聚焦访问策略、监听、协议兼容与部署迁移。代码与文档留在 beta 工作区，未自动提交或推送。
