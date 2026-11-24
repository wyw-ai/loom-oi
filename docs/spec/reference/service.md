# Service 发布与调用

Service 是本版 beta 的发布和访问单位。OIS 用同一份配置提供 REST 与可选 MCP 入口。可运行的 Host A 示例把三个 App 的六个 Action 发布到一个 workbench。[部署指南](../../guides/service-files.md) · [完整 OIS 接口](ois-api.md)

## 模型与执行

| 模型 | 职责 |
| --- | --- |
| Host、Channel | Host 承载 OIC 和本机 App。Channel 是 OIS 路由到相应 OIC 的固定通道。 |
| App、Action | OIC 加载 App 清单并托管 worker，向 OIS 登记 Action 及状态。App 的业务逻辑在对应 Host 上执行。 |
| Service | 一份可持久化的发布配置，包含成员、访问规则和协议发布选项。同一来源可被不同 Service 复用。 |
| Service 成员 | 以 Service 内唯一的发布名引用固定的 `(channel, app, action)`。同名来源可分别命名。MCP 将成员呈现为 Tool。 |

发布者选定来源后，Agent 以成员的发布名发起调用。OIS 检查访问规则、成员归属及来源可用性，再把请求路由到对应 OIC。OIC 与 worker 沿用已有的调用、回执和取消链路。Agent 可以依次调用不同 App。Service 的职责止于组合和发布 Action。

## API

| 接口 | 行为 |
| --- | --- |
| `GET /api/v1/hosts`、`GET /api/v1/hosts/{id}/apps`、`GET /api/v1/actions` | 复用 beta 的来源目录，供管理者选择已登记的 Action。 |
| `GET /api/v1/services` | 列出 Service。 |
| `GET`、`PUT`、`DELETE /api/v1/services/{id}` | 读取、创建或完整替换、删除发布配置。一次提交成员、固定来源、访问规则和 MCP 发布选项。 |
| `/mcp/{service_id}` | `tools/list` 与 `tools/call` 使用这个 Service 的成员和权限。原生图像结果沿用 beta 的 MCP 映射。 |
| `POST /api/v1/exec` | 以 `service`、Service 内的 `action` 发布名和对象类型的 `arguments` 执行调用，与 MCP 共享来源和访问范围。 |

`actions` 按发布顺序排列。省略 `name` 时使用来源的 Action 名，成员名必须在本 Service 内唯一。`source.channel` 必须事先配置。成员可以只指定来源，让 OIS 从 OIC 的登记信息取得描述和输入 Schema。来源尚未登记时仍可预配置成员，MCP 发现会用对象 Schema 占位，执行须等待来源可用。`publish.mcp` 默认为 `false`，REST 执行仍可用。仅发布 MCP 时管理视图才包含 `endpoint`。

消费端若需固定本次调用的来源，可在 `tools/call` 的 `params._meta["loom.expected_source"]` 携带已发现的 `{channel,app,action}`。OIS 在派发时将它与当前成员的固定来源一起核对。变更返回 `BINDING_CHANGED`，此时尚未向 OIC 发送调用。未指定该字段时按原有规则使用当前来源。

下面的 `PUT /api/v1/services/workbench` 示例把 A 的上传和 B 的取回发布到同一个入口。此处的 `host-a`、`host-b` 是部署方已配置的通道 ID。

```json
{
  "actions": [
    {"name": "put_from_a", "source": {"channel": "host-a", "app": "file-store", "action": "put_file"}},
    {"name": "get_on_b", "source": {"channel": "host-b", "app": "file-store", "action": "get_file"}}
  ],
  "access": {"credential_env": "WORKBENCH_TOKEN"},
  "publish": {"mcp": true}
}
```

这个 REST 请求将 Host A 的截图上传到指定存储路径。

```json
{"service": "workbench", "action": "put_from_a", "arguments": {"path": "captures/83.png", "store_path": "shots/host-a/83.png"}}
```

## beta 配置迁移

Service 接替 vMCP 的发布及访问范围。新管理流程直接保存成员和来源，迁移同时覆盖首次导入的 YAML 和已有实例的持久 JSON。

- 以原 vMCP ID 建立 Service，维持对应 MCP 入口及其访问规则。
- 按原成员关系展开全局 Tool，保留发布名、描述、消费侧输入 Schema 和固定来源。不同 Service 中复用同一来源的成员可以有各自的发布名。
- 保留可信调用身份与原有作用域。OIS 仍用 worker 协议中的 `context.vmcp_id` 传递 Service ID。`context.access_identity` 在匿名模式下为 `anonymous:<service_id>`，启用凭据后为 `consumer:<service_id>:<credential_env>`。[App 运行协议](app-runtime.md)
- 首次导入 YAML 或读取 v1 持久 JSON 时，OIS 将展开后的成员保存为 v2 状态。首次配置不能同时声明同 ID 的 `services` 和 `vmcps`。后续重启沿用已保存的定义。
- 本版保留 `/api/v1/tools`、`/api/v1/vmcps` 和对应详情、写入路由。仍由旧 vMCP 管理的 Service 随旧配置同步。对同 ID 执行原生 Service PUT 后，其成员由 `/api/v1/services/{id}` 独立管理，再通过旧接口 PUT 同名 vMCP 会返回 `409 RESOURCE_IN_USE`。
- `POST /api/v1/exec` 接受新 `service/action` 或旧 `vmcp/tool` 字段对，同一请求混用两对非空字段返回 `400 INVALID_REQUEST`。这组兼容入口在本版有效，后续移除日期尚未确定。
