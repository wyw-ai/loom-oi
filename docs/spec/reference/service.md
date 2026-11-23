# Service 发布与调用

本文规定 beta 后续版本的目标契约，状态为设计封板。运行现有部署时使用 [beta OIS API](https://github.com/wyw-ai/loom-oi/blob/e40e35b7da2c62855560566ca485a5c250f69dc1/docs/spec/reference/ois-api.md)。

## 模型与执行

| 模型 | 职责 |
| --- | --- |
| Host、Channel | Host 承载 OIC 和本机 App。Channel 是 OIS 路由到相应 OIC 的固定通道。 |
| App、Action | OIC 加载 App 清单并托管 worker，向 OIS 登记 Action 及状态。App 的业务逻辑在对应 Host 上执行。 |
| Service | 一份可持久化的发布配置，包含成员、访问规则和协议发布选项。同一来源可被不同 Service 复用。 |
| Service 成员 | 以 Service 内唯一的发布名引用固定的 `(channel, app, action)`。同名来源可分别命名。MCP 将成员呈现为 Tool。 |

发布者选定来源后，Agent 以成员的发布名发起调用。OIS 检查访问规则、成员归属及来源可用性，再把请求路由到对应 OIC。OIC 与 worker 沿用已有的调用、回执和取消链路。Agent 可以依次调用不同 App。Service 的职责止于组合和发布 Action。

## API

| 接口 | 目标行为 |
| --- | --- |
| `GET /api/v1/hosts`、`GET /api/v1/hosts/{id}/apps`、`GET /api/v1/actions` | 复用 beta 的来源目录，供管理者选择已登记的 Action。 |
| `GET /api/v1/services` | 列出 Service。 |
| `GET`、`PUT`、`DELETE /api/v1/services/{id}` | 读取、创建或完整替换、删除发布配置。一次提交成员、固定来源、访问规则和 MCP 发布选项。 |
| `/mcp/{service_id}` | `tools/list` 与 `tools/call` 使用这个 Service 的成员和权限。原生图像结果沿用 beta 的 MCP 映射。 |
| `POST /api/v1/exec` | 以 `service`、Service 内的 `action` 发布名和对象类型的 `arguments` 执行调用，与 MCP 共享来源和访问范围。 |

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
- 保留可信调用身份与原有作用域。beta worker 使用 `context.access_identity` 和 `context.vmcp_id` 关联会话和状态，迁移后的调用须维持原有隔离语义。[beta App 运行协议](https://github.com/wyw-ai/loom-oi/blob/e40e35b7da2c62855560566ca485a5c250f69dc1/docs/spec/reference/app-runtime.md)
- 旧管理写入路径为 `/api/v1/tools/{tool_name}` 与 `/api/v1/vmcps/{vmcp_id}`。这两组接口以及 `POST /api/v1/exec` 的旧 `vmcp/tool` 字段，切换方式和具体兼容期由实施阶段确定。
