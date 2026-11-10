# OIS 与 OIC 配置参考

本文列出当前 OIS/OIC 公开 YAML 字段、默认值、访问配置和持久化规则。App YAML 的 `runtime`、Action Schema 与映射见 [App runtime](app-runtime.md)；`app_config` 中终端 App 的具体字段见[终端 Actions](terminal-actions.md)。命令见 [CLI](cli.md)，资源管理接口见 [OIS API](ois-api.md)。

## 配置来源与路径

| 内容 | 当前权威来源 | 变更生效方式 |
|---|---|---|
| OIS `server`、`admin`、`state_dir` | OIS 启动 YAML | 修改后重启 OIS。 |
| OIS channels、tools、vmcps | 首次从 YAML 导入；之后由 `server.data_file` 中的持久状态管理 | 用管理 API 更新；正常重启不会用 YAML 覆盖。 |
| OIC Host、通道、OIS 接入及 `app_config` | OIC 启动 YAML | 修改后重启 OIC。 |
| OIC 已加载 App 集合 | `state_dir/loaded.json` | `oic load/unload` 修改；启动时按保存路径重新加载。 |
| App 定义与实现文件 | 本机 App 包 | 已加载定义有变化时先卸载再加载；重启也会读取保存路径上的当前定义。 |

CLI 的 `--config` 和 `load` 参数相对执行 CLI 时的当前目录解析。OIS 的状态目录、数据文件、证书/私钥，以及 OIC 的状态目录、CA 文件，相对各自配置文件目录解析。App 内 runtime 程序路径规则见 [App runtime](app-runtime.md)。`app_config` 内部值由 App 解释，OIC 不统一替换路径或插值环境变量。

这里的环境引用字段存放的是**变量名**，不是 Token 本身。当前没有通用 `${ENV}` YAML 插值。设置环境变量后应从该环境启动服务；改变调用者的环境不会修改已运行服务的环境。

## OIS 字段

### 顶层

| 字段 | 类型 / 默认 | 说明 |
|---|---|---|
| `version` | integer；省略或 `0` 按 `1` | 当前只支持版本 `1`。 |
| `state_dir` | string；`.ois` | CLI 控制、锁和后台日志目录。 |
| `server` | object | 监听、公开地址、数据路径和期限，见下表。 |
| `admin` | object；省略 | 管理接口认证。 |
| `channels` | map；空 | 首次导入的通道，以 channel ID 为键。 |
| `tools` | map；空 | 首次导入的消费工具，以工具名为键。 |
| `vmcps` | map；空 | 首次导入的 vMCP，以 vMCP ID 为键。 |

OIS 对已定义结构拒绝未知 YAML 字段；`input_schema` 是可扩展的 Schema 对象，不受固定配置结构的字段枚举限制。

### server 与 admin

| 字段 | 类型 / 默认 | 说明 |
|---|---|---|
| `server.listen` | string；`:25100` | 主 TCP 监听地址。未配置旧 TLS 字段时是 HTTP。 |
| `server.public_url` | string；根据主监听推导 | 主入口 origin。仅允许匹配监听协议的绝对 `http://` 或 `https://` 地址，不含用户信息、路径、query、fragment；末尾 `/` 被移除。用于返回 MCP endpoint 和检查 Origin，不改变实际监听。 |
| `server.tls_cert` | string；空 | 兼容字段：主监听使用的 TLS 证书链文件。 |
| `server.tls_key` | string；空 | 兼容字段：主监听使用的对应私钥。必须和 `tls_cert` 同时设置。 |
| `server.https` | object；省略 | 在 HTTP 主监听外增加一个 HTTPS 监听；不能和 `tls_cert/tls_key` 混用。 |
| `server.https.listen` | string；`:25101` | 附加 HTTPS 监听地址，必须与主监听地址不同。 |
| `server.https.public_url` | string；根据 HTTPS 监听推导 | HTTPS origin，规则同主公开地址，但协议必须为 `https`。 |
| `server.https.cert_file` | string；启用 `https` 时必填 | PEM 证书及中间证书链。 |
| `server.https.key_file` | string；启用 `https` 时必填 | 对应 PEM 私钥。 |
| `server.data_file` | string；`data/ois-state.json` | channels/tools/vmcps 的持久 JSON 文件；相对配置文件，而不是相对 `state_dir`。 |
| `server.allowed_origins` | string array；空 | 除主公开 origin 和已配置 HTTPS origin 之外，允许的 HTTP Origin 列表；按完整 origin 匹配，不支持通配规则。没有 Origin 的请求不因此被拒绝。 |
| `server.request_timeout_ms` | integer；`60000` | 单次服务端执行请求期限，必须大于 `0`；不是命令进程最长运行时间。 |
| `server.heartbeat_interval_ms` | integer；`10000` | OIS 通道心跳间隔，必须大于 `0`。 |
| `server.heartbeat_timeout_ms` | integer；`30000` | OIS 等待通道消息的读期限，必须大于心跳间隔。 |
| `admin.token_env` | string；空 | 管理 API Bearer Token 的环境变量名；省略或空字符串表示该范围不要求 Token。显式引用的变量为空时 OIS 启动失败。 |

三个期限字段的 `0` 也按默认值处理。监听 host 为空、`0.0.0.0` 或 `::` 时，自动推导的公开地址使用 `localhost`；远程部署应显式填写客户端可达的 `public_url`。OIS TLS 最低版本为 TLS 1.2。证书加载或任一监听绑定失败会导致启动失败，不自动降级为 HTTP。

OIC 当前固定每 10 秒发送心跳，30 秒未收到通道消息即重连；它没有对应可配字段。修改 OIS 心跳参数不会同步改写 OIC 的这两个值。

### channels、tools、vmcps

channel ID、Tool 名、vMCP ID、`expected_host_id`、source 中 App/Action 名使用 `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`。资源映射的键就是资源标识，无需再写 `id` 或 `name` 字段。

| 字段 | 类型 / 默认 | 说明 |
|---|---|---|
| `channels.<id>.description` | string；空 | 通道描述。 |
| `channels.<id>.expected_host_id` | string；必填 | 此通道允许声明的固定 Host ID，必须与接入 OIC 的 `host.id` 一致。 |
| `channels.<id>.credential_env` | string；空 | 此通道的 Bearer Token 环境变量名；空表示匿名接入，但仍检查 Host/Channel 绑定。 |
| `tools.<name>.description` | string；空 | 暴露给消费者的工具说明。 |
| `tools.<name>.input_schema` | object；必填 | 暴露给消费者的参数 Schema；可使用 JSON Schema 字段。OIS 不以它对比或强制统一 App Schema，也不据此执行完整参数验证。 |
| `tools.<name>.source.channel` | string；必填 | 必须引用现有 channel。 |
| `tools.<name>.source.app` | string；必填 | 目标 App code。 |
| `tools.<name>.source.action` | string；必填 | 目标 Action 名。 |
| `vmcps.<id>.tools` | string array；空 | 暴露的 Tool 名称集合；只能引用现有工具，不能重复。 |
| `vmcps.<id>.access.credential_env` | string；空 | 此 vMCP 消费者的 Bearer Token 环境变量名。 |
| `vmcps.<id>.access.require_https` | boolean；`false` | 是否要求 MCP 与 REST 执行请求实际通过 TLS 到达 OIS。设为 `true` 前必须已经配置 HTTPS 监听。 |

每个 Tool 固定绑定一个 `(channel, app, action)`，业务调用不再输入 Host 路由参数。同一通道同时只接受一个在线 OIC；默认匿名并不取消 `expected_host_id` 校验或重复连接检查。当前没有由用户配置的 Host 路由、负载均衡或自动换目标字段。

Token 引用属于三个独立范围：管理 API、OIC 通道、vMCP 消费。通道和 vMCP 引用的环境变量不在导入时强制解析，但认证时必须有非空值且与请求 Token 匹配；引用存在但值缺失不会回退匿名。管理 API 修改和引用约束见 [OIS API](ois-api.md)。

## OIC 字段

| 字段 | 类型 / 默认 | 说明 |
|---|---|---|
| `version` | integer；必填 `1` | OIC 与 OIS 不同，省略版本会失败。 |
| `host.id` | string；必填 | 当前机器声明的 Host ID；必须匹配 OIS 通道的 `expected_host_id`。 |
| `host.name` | string；空 | 本机展示名称；OS 和架构由运行进程自动登记，不是配置项。 |
| `channel.id` | string；必填 | OIS 中已创建的固定通道。 |
| `ois.url` | string；必填 | OIS HTTP/HTTPS origin 或完整 WS/WSS 通道地址，具体派生规则见下文。 |
| `ois.credential_env` | string；空 | 连接 OIS 使用的 Token 环境变量名；省略时不发送 Authorization。 |
| `ois.ca_file` | string；空 | HTTPS/WSS 额外信任的 PEM CA 证书集合；相对 OIC 配置文件。 |
| `state_dir` | string；`.oic` | 本地控制、App 加载记录与诊断日志目录。 |
| `app_config` | map；空 | 以 App code 为键的本机配置，每个值必须为对象。 |
| `app_config.<code>` | object | 在 worker 启动时作为 `start.config` 交付给该 App；内容由 App 定义，OIC 不识别 Bash/PowerShell 分支。 |

`ois.url` 支持 `http`、`https`、`ws`、`wss`。HTTP 转为 WS，HTTPS 转为 WSS；无路径或仅 `/` 时补上 `/api/v1/oic/connect`，非空路径保持原样。URL 必须包含 host，不能携带用户信息、query 或 fragment。

HTTPS/WSS 未设置 `ca_file` 时使用正常系统信任根。显式 CA 文件扩充信任根，不关闭证书名称、有效期或证书链检查；使用公共 CA 通常无需设置它。HTTP/WS 配置 `ca_file` 是配置错误。当前没有跳过 TLS 验证的字段。

显式配置 `ois.credential_env` 而变量缺失、为空或全为空白时，连接失败且不会匿名重试；错误可见于 OIC `status.last_error`。OIC 进程仍会重试连接，因此不能仅凭 `start` 成功判断认证成功。未配置引用时，即使进程环境中存在其他 Token 变量，也不会自动发送它们。App worker 使用受控环境，不直接继承 OIC 的通道凭据。

## 默认 HTTP，无 Token

默认启动无 TLS，三个认证范围均不要求 Token。以下是一个完整的单通道配置示例；四工具配置见仓库 [OIS 样例](../../../examples/ois.yaml)。

```yaml
version: 1
state_dir: ./state/ois
server:
  listen: ':25100'
  public_url: http://ois.example.test:25100
  data_file: ./state/ois/config.json
channels:
  linux-151:
    expected_host_id: linux-151
tools:
  linux_exec_command:
    description: Run a Bash command on the fixed Linux target.
    input_schema:
      type: object
      properties:
        cmd: {type: string}
      required: [cmd]
    source: {channel: linux-151, app: bash, action: exec_command}
vmcps:
  workbench:
    tools: [linux_exec_command]
```

对应 OIC 接入配置片段如下，运行终端 App 时还需设置相应 `app_config` 并执行 `oic load`：

```yaml
version: 1
host: {id: linux-151, name: Linux151}
channel: {id: linux-151}
ois:
  url: http://ois.example.test:25100
state_dir: ./state/oic
```

Agent 使用 `http://ois.example.test:25100/mcp/workbench`，不需要认证头或 CA。匿名模式下同一 vMCP 的调用共享 `anonymous:<vmcp-id>` 消费身份；随意附加 Bearer 字符串不会创建新身份。

## 显式启用 HTTPS 或 Token

HTTPS 与 Token 相互独立：可以各自启用，也可以同时启用。附加 HTTPS 监听的配置片段：

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

证书申请、更新和客户端信任由部署方管理，OIS 不自动申请证书。若使用兼容的 `tls_cert/tls_key`，则主监听本身改为 HTTPS-only，主 `public_url` 也必须用 HTTPS，不能再同时配置 `server.https`。

下面是三个范围的 Token 引用及 vMCP 强制 HTTPS 的字段片段，实际 Token 不写入 YAML：

```yaml
admin:
  token_env: OIS_ADMIN_TOKEN
channels:
  linux-151:
    expected_host_id: linux-151
    credential_env: LINUX_CHANNEL_TOKEN
vmcps:
  workbench:
    tools: [linux_exec_command]
    access:
      credential_env: WORKBENCH_TOKEN
      require_https: true
```

OIC 对应设置 `ois.url: https://ois.example.com:25101` 和 `ois.credential_env: LINUX_CHANNEL_TOKEN`；私有 CA 时再配置 `ois.ca_file`。消费者对该 vMCP 发送 `Authorization: Bearer <token>`，管理 API 使用管理范围的 Token。OIS/OIC 两端通道环境变量名可不同，但实际值必须匹配。

`require_https: true` 的 endpoint 使用配置的 HTTPS origin，HTTP 调用被拒绝为 `HTTPS_REQUIRED`。未要求 HTTPS 时，返回主入口的 endpoint，但同一 vMCP 也可经已配置的 HTTPS 入口访问。TLS 判断依据是 OIS 实际连接，不信任 `X-Forwarded-Proto`；当前配置不会把代理转发头视作 HTTPS 证明。

## 持久化与切换访问策略

首次启动且 `server.data_file` 不存在时，OIS 校验 YAML 中的 channels/tools/vmcps 后写入该文件。文件已存在时读取其中资源；解析或校验失败会阻止启动，不改用 YAML 重建。管理 API 更新通过临时文件写入并替换持久文件；保存的是凭据**引用名**，不解析写入实际 Token。

已导入资源的 Token 引用或 `require_https` 必须通过管理 API 修改，单改启动 YAML 的同名资源再重启没有覆盖效果。启用 HTTPS 时先配置监听；移除 HTTPS 监听前，先解除所有持久 vMCP 的 HTTPS 要求，否则 OIS 会拒绝启动。删除某凭据环境变量前应处理所有仍引用它的资源，避免这些资源认证失败。变更在线通道的 Host 或凭据引用前须先停相应 OIC。

OIC 的 `loaded.json` 只保留加载路径和 App code；重启重新加载 App，不恢复 worker 内存、命令执行或旧 `session_id`。加载/卸载及失败恢复规则详见 [CLI](cli.md)。

本地 CLI 的回环控制 Token 始终由程序自动生成和管理，与以上可选认证范围独立；默认 HTTP 无 Token 不会关闭本地控制认证。

## 实现与样例

- [OIS 配置解析及持久化](../../../internal/ois/config.go)、[访问与 Origin 检查](../../../internal/ois/server.go)。
- [OIC 配置解析](../../../internal/oic/config.go)、[URL、CA 与认证连接](../../../internal/oic/connection.go)。
- 完整样例：[OIS](../../../examples/ois.yaml)、[Linux OIC](../../../examples/oic.linux.yaml)、[Windows OIC](../../../examples/oic.windows.yaml)。样例里的机器地址、shell 路径和工作目录需按实际部署替换。
