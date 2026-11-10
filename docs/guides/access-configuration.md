# HTTP、HTTPS 与可选 Token

本文提供访问方式的配置与部署示例。完整字段、默认值、路径和持久化规则以 [OIS/OIC 配置参考](../spec/reference/configuration.md)为准，接口行为见 [OIS API](../spec/reference/ois-api.md)。

默认传输使用 **HTTP + 无 Token**。oi 提供 HTTP API 和 MCP 两种消费接入方式，共用下述访问配置。本版 MCP 验收入口为 `http://121.199.175.151:25100/mcp/workbench`，选择 Streamable HTTP，无需认证头或 CA 文件；直接 HTTP 调用使用 [`POST /api/v1/exec`](../spec/reference/ois-api.md#3-rest-执行)。OIC 默认通过 HTTP 地址派生 WS 连接。

## 默认配置

```yaml
version: 1
server:
  listen: ':25100'
  public_url: http://121.199.175.151:25100
channels:
  linux-151:
    expected_host_id: linux-151
vmcps:
  workbench:
    tools: [linux_exec_command, linux_write_stdin]
```

以上省略了既有 Tool 定义；完整可启动样例见 [ois.yaml](../../examples/ois.yaml)。省略 `admin.token_env`、通道 `credential_env` 和 vMCP `access.credential_env`，分别关闭这三个范围的 Token 验证。关闭认证时，同一 vMCP 的消费者共享 `anonymous:<vmcp>` 身份；任意 Bearer 字符串不会创建额外身份。

## 可选 HTTPS 和公共 CA

HTTPS 证书配置在 OIS 监听层，同一个 HTTPS 入口可服务多个 vMCP。先在 OIS 启动配置中添加：

```yaml
server:
  listen: ':25100'
  public_url: http://ois.example.com:25100
  https:
    listen: ':25101'
    public_url: https://ois.example.com:25101
    cert_file: ./tls/fullchain.pem
    key_file: ./tls/privkey.pem
```

`cert_file` 放 CA 签发的证书及完整中间链，`key_file` 放对应私钥；路径相对 OIS 配置文件。使用客户端默认信任的公共 CA 证书时，Agent 无需导入额外 CA。签发与续期由部署方完成，此配置不自动申请公共证书。

新建或更新 vMCP 时，通过 `access.require_https` 选择：

```http
PUT /api/v1/vmcps/secure-workbench
Content-Type: application/json

{
  "tools": ["linux_exec_command", "linux_write_stdin"],
  "access": { "require_https": true }
}
```

返回的 `endpoint` 使用配置的 HTTPS 地址。该 vMCP 的 MCP 与 REST 执行请求在 HTTP 上返回 `403 HTTPS_REQUIRED`；HTTPS 上正常调用，不因启用 HTTPS 就自动要求 Token。必须先配置 HTTPS 入口，否则 `require_https: true` 会被拒绝。

省略 `require_https` 或设为 `false` 时，不强制 HTTPS；默认返回主入口地址，也可从已配置的 HTTPS 入口访问。判断依据是 OIS 实际收到的 TLS 连接，不相信 `X-Forwarded-Proto` 等自报头。

原来的 `server.tls_cert` / `server.tls_key` 仍兼容：两者同时填写时，主 `server.listen` 使用 HTTPS-only。它们不能与新的 `server.https` 块混用。不会因证书缺失或加载失败自动退回 HTTP。

## 可选 Token

Token 与 HTTPS 独立，可以只启用其中一项，也可以同时启用。

```yaml
admin:
  token_env: OIS_ADMIN_TOKEN
channels:
  linux-151:
    expected_host_id: linux-151
    credential_env: LINUX_CHANNEL_TOKEN
vmcps:
  workbench:
    tools: [linux_exec_command, linux_write_stdin]
    access:
      credential_env: WORKBENCH_TOKEN
      require_https: false
```

`token_env` / `credential_env` 指定 OIS 进程环境变量的名称；实际 Token 放在对应环境变量中。配置引用后，该范围必须携带正确 Token。引用存在但值缺失时，启动或认证报错，不退回匿名模式。

OIC 只需在通道启用认证时增加 `ois.credential_env`。Agent 只需在其 vMCP 启用认证时发送 `Authorization: Bearer <token>`。公共 CA HTTPS 使用系统信任；私有 CA HTTPS 可以显式设置 OIC `ois.ca_file` 或 MCP 客户端的 CA 文件。HTTP 配置 `ois.ca_file` 会报配置错误。

OIC/OIS CLI 内部的回环管理凭据仍由程序自动管理，不需要 Agent 配置。

## 加载与部署

`ois --config ois.yaml start` 每次读取监听、TLS、管理认证配置。channels/tools/vmcps 首次从 YAML 导入，此后通过管理 API 修改持久配置；不会在重启时用旧 YAML 覆盖 API 更新。

本项目部署脚本支持以下独立开关：

```powershell
./scripts/deploy.ps1                           # HTTP，无 Token，无证书依赖
./scripts/deploy.ps1 -RequireToken             # HTTP，要求 Token
./scripts/deploy.ps1 -EnableHttps              # HTTPS，无 Token
./scripts/deploy.ps1 -EnableHttps -RequireToken
./scripts/deploy.ps1 -EnableHttps -CertFile ./fullchain.pem -KeyFile ./privkey.pem
```

`-EnableHttps` 未提供证书时，显式使用本地验收 CA 生成或复用测试证书；这时客户端仍需信任该 CA。使用公共 CA 证书时无需提供 `-CAFile`，并确保其 SAN 匹配部署脚本使用的 `121.199.175.151`；使用域名证书则按上面的 OIS 配置直接部署对应域名入口。

脚本只修改部署所属的两通道和 workbench 的访问策略，保留 Tool 绑定及加载记录；降级 HTTP 前先通过旧入口解除 workbench 的 HTTPS 要求。其他 vMCP 仍要求 HTTPS 时不会移除该监听，其他资源仍引用凭据时不会清空其环境。旧证书和凭据文件保留，默认匿名部署不使用它们。

客户端示例：

```powershell
Import-Module ./scripts/McpClient.psm1
$mcp = Connect-LoomMcp -Url 'http://121.199.175.151:25100/mcp/workbench'
Invoke-LoomTool $mcp linux_exec_command @{cmd='ls -l'}
$mcp.Client.Dispose()
```

四个命令/生命周期验收脚本默认也不读取旧凭据；`-UseCredentials` 才显式读取 `.local/credentials.json`，或者通过 `-Token` / `-AdminToken` 提供。HTTPS 私有 CA 使用 `-CAFile`。原 management/channelprobe 验收是显式启用 Token 的回归工具。
