# 创造者盘点 Host、管理 Service 与读取已有 Skill

**2026-11-24 目标、接口行为与验收范围已封板，两款 App 已实现并通过本地 OIS/OIC 集成验收。** 以 Service beta `212b970` 为基线。OI 新增两个普通 OIApp。`loom-oi` 供创造者盘点 Host 已启用的能力并管理 Service，`skills` 在目标 Host 读取现有 Skill。harness 的内部工具插件自动发现可访问的 Service，将集合和成员登记为与传输方式独立的工具身份。[目标说明](../../goal-shaping/current/creator-workspace/design.md) · [封板记录](../../goal-shaping/archive/creator-workspace/2026-11-24-service-skills/notes.md)

## `loom-oi` 管理 App

部署方可在 OIS 同机的 OIC 加载 `loom-oi`，用预设的管理 Service 将下表七个 Action 授予创造者。`app_config.loom-oi` 提供固定的 OIS 管理地址、管理凭据引用及操作者可查看的 Host、可发布的来源和可管理的 Service。OIS 管理凭据由部署方注入该 App 的 worker。对外消费每个 Service 时使用该 Service 的访问身份。OIC 使用自己的通道身份连接 OIS，通过本机 `oic load <app.yaml>` 加载已配置的 App。[管理 App 实际配置](../../../apps/loom-oi/README.md)和[Skill App 实际配置](../../../apps/skills/README.md)给出调用者匹配、授权范围、凭据文件与根目录的完整示例。[配置职责](../reference/configuration.md) · [OIC 本机 CLI](../reference/cli.md)

| Action | 入参 | 返回结果与 OIS 操作 |
| --- | --- | --- |
| `list_hosts` | `{}` | 从 `GET /api/v1/hosts` 返回可见的已登记 Host，含 `id, name, os, arch, online`。 |
| `list_apps` | `host_id` | 从 `GET /api/v1/hosts/{id}/apps` 返回该 Host 上 OIC 已加载并登记的 App，含 `app, ready, available, stale`。磁盘上仅下载的包不进清单。 |
| `list_actions` | `host_id?`、`app?` | 从 `GET /api/v1/actions` 按可选条件列出获授条目，含 Host、App、Action、说明、输入契约、可用状态及可直接交给 `apply_service` 的固定 `source`。 |
| `list_services` | `{}` | 从 `GET /api/v1/services` 返回可见 Service 的 `id`、成员摘要与发布选项。 |
| `get_service` | `id` | 从 `GET /api/v1/services/{id}` 返回完整 `actions, access, publish`，启用 MCP 时附 `endpoint`。成员含发布名与固定来源。 |
| `apply_service` | `id`、完整 `service`（`actions, access, publish`） | 调用 `PUT /api/v1/services/{id}` 创建或完整替换 Service，返回创建或更新结果及生效定义。先读回当前定义再修改，可保留需要的其他成员。 |
| `delete_service` | `id` | 调用 `DELETE /api/v1/services/{id}` 删除获准管理的 Service，成功后返回被删除的 ID；随后重新查询及通过原入口发起的新调用均找不到该 Service。 |

`ready` 是登记时 App 或 Action 自报的就绪状态。`available` 还要求对应 OIC 当前在线且已完成登记。Host 离线时，可返回 OIS 留存的上次清单，标记 `stale: true`、`available: false`。同一 Host 可能有多条登记来源，`list_actions` 返回的 `source` 沿用已有 `{channel, app, action}` 结构，创造者选中条目即可发布。通道设置由部署方维护。管理 App 按可信调用身份检查 Service 和各成员来源是否获授。[OIS 来源目录与管理 API](../reference/ois-api.md) · [Service 发布模型](../reference/service.md)

## `skills` 只读 App

在存有 Skill 的目标 Host 上加载 `skills`。给定该 Host 的工作区和目标用户主目录，App 按下表自动推导默认扫描根；缺失的目录跳过。部署方可在 `app_config.skills` 限定可读范围，或登记其他产品的约定目录。每个根带有稳定的 `root_id`、范围（`workspace` 或 `home`）、产品标记和路径。下表中的 `.` 指该目标工作区，`~` 指该目标用户主目录。

| 约定目录 | 范围 | 产品标记 |
| --- | --- | --- |
| `./.agents/skills` | 工作区 | `shared` |
| `./.codex/skills` | 工作区 | `codex` |
| `~/.agents/skills` | 用户主目录 | `shared` |
| `~/.codex/skills` | 用户主目录 | `codex` |

部署方可按相同规则登记其他产品的目录。一个 Skill 由根内的 `<包目录>/SKILL.md` 标识。工作区和主目录可以各有一份同名 Skill；目录保留两条记录，各自带可辨别的来源。调用时的 `product` 是产品标记。省略时查询全部获准的登记根，`product="codex"` 时只查询标为 `codex` 的工作区及主目录根，`product="shared"` 可查询共享根。

| Action | 入参 | 返回结果 |
| --- | --- | --- |
| `list_skills` | `product?`，可选分页 `limit?, cursor?` | `items[]` 含从 `SKILL.md` frontmatter 取得的 `name, description`、来源范围、产品标记、`root_id` 及 `ref`；有后页时返回 `next_cursor`。分页沿用相同来源、消费身份和产品条件。 |
| `read_skill` | `ref, resource?` | 省略 `resource` 时读取该包当前的 `SKILL.md` 正文，并列出可读配套材料的包内相对路径。指定 `resource` 时只返回清单中该材料的路径与文本。 |

`ref` 在同一 App 来源内稳定定位到登记根和包的相对位置。消费端跨多个 Host 使用时，应把工具的固定来源与 `ref` 一起保存。文件由其所在 Host 维护，重新读取反映磁盘上的当前内容；已移除的包返回找不到。每次读取复核消费权限及实际文件是否仍位于获准根和相应包内，拒绝绝对路径、越界路径和指向包外的链接。调用者不能通过参数增添扫描根。

## 交接给 harness

在已有的 Host A 示例中，`workbench` 用 `actions[].name` 将 `computer-use.screenshot` 发布为 `capture_a`，将 `filesystem.view_image` 发布为 `view_on_a`。创造者先通过 `get_service` 读回定义，再从 Host A 的 Action 目录选取 `skills.list_skills` 和 `skills.read_skill`，连同原成员和访问规则一起提交 `apply_service`。预置只读目录 Service（例如 `oi-catalog`）可以只发布 `list_services`，供 harness 发现包括 `workbench` 在内的可见 Service。目录和业务消费身份分别配置。业务 Service 用 `access.credential_env` 指定消费凭据环境变量名，用 `publish.mcp: true` 开放 `/mcp/workbench`。本轮联验走 MCP，OI 已有的 HTTP 执行入口为 `POST /api/v1/exec`。[现有 workbench YAML](../reference/oiapps/examples/service-files/ois.yaml) · [Service 管理与消费接口](../reference/service.md)

harness 自动同步其可发现的所有 Service，各 Service 对应一个 Toolset，每个已发布成员登记为一个 Tool。模型直接使用 `capture_a`、`view_on_a` 等发布名。上例的两个 Skill Action 也是普通成员，目录条目由模型选择后再用 `read_skill` 打开。Toolset 授权、Agent 绑定、刷新及 WebUI 产品交付归 harness 管理。

## 验收

| 场景 | 核对结果 |
| --- | --- |
| 盘点与状态 | 用创造者身份找到 Host 1、其已加载的 App 和对应 Action。断开 Host 后，已有目录能区分上次就绪与当前不可调用，调用不会转到其他 Host。 |
| 管理 Service | 以目录返回的固定来源创建一个 Service，依次列出、读回、修改成员并以消费身份实调。删除后从最新列表消失，原入口的新调用失败。 |
| 管理权限 | 用受限创造者身份查询并修改获授的 Service，再尝试发布越权来源、修改或删除未获授的 Service。管理 App 拒绝越权操作，读回原定义可核对成员和访问规则。 |
| 默认与产品过滤 | 在四个约定根各放一份带独立标记的 `SKILL.md`，其中两份同名。默认列出四个来源；过滤 `codex` 只返回两个专属根的条目。凭各自 `ref` 读回正确正文及指定材料，越界读取失败。 |
| 跨仓交接 | 创造者通过管理 Service 发布 Host A 的截图、查看图片和 Skill Action。harness 自动同步，在仅绑定相应业务 Toolset 的另一 Agent 上核对直接工具名，调用 `capture_a` 并将返回的路径交给 `view_on_a`，再调用 `list_skills`、`read_skill` 读到该 Host 的当次标记。用 harness ToolCall 与 OI `call_id` 核对来源和结果。 |
