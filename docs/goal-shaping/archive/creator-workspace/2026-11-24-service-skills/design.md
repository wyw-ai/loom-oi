# OI 的 Host 能力目录、Service 管理与 Skill 读取

2026-11-24 封板。基线为 Service beta `212b970`。本仓负责 `loom-oi` 和 `skills` 两款 OIApp 的开发与验收，harness 负责内部工具插件和 WebUI。两仓通过 OI Service 的既有发布和调用接口联验。

## 创造者怎样发布 Host 能力

部署方在持有管理凭据的 OIC 上加载 `loom-oi`，并为创造者发布管理 Service。创造者用 `list_hosts`、`list_apps`、`list_actions` 找到在线 Host、已加载 App 及 Action 的固定来源。用 `list_services`、`get_service` 查询发布状态，用 `apply_service` 创建或完整更新定义，用 `delete_service` 删除获授的 Service。`app_config.loom-oi` 限定管理地址、凭据和操作者能接触的 Host、来源与 Service。通道和 App 加载由部署方维护。

OI Service 的成员将发布名固定到 `channel, app, action`。在已有 Host A 示例中，`capture_a` 对应 `computer-use.screenshot`，`view_on_a` 对应 `filesystem.view_image`。创造者可在保留现有成员的基础上，把同一 Host 的两个 Skill Action 加入 `workbench`。

## Agent 怎样读取已有 Skill

`skills` 在文件所在 Host 运行。部署方通过 `app_config.skills` 管理可读根及产品标记。默认覆盖项目与用户主目录中的 `.agents/skills`、`.codex/skills`。`list_skills` 默认列出全部获准根的名称、简介、来源和引用，`product=codex` 只查两个 Codex 专属根。同名包各有引用。`read_skill(ref)` 读出所选包的 `SKILL.md` 及可读材料路径，`read_skill(ref, resource)` 读取指定材料。每次读取都在获授包范围内核验来源与文件位置。

## 两仓怎样验收

harness 通过预置目录 Service 发现可见 Service，用独立消费身份读取各 MCP 入口。每个 Service 对应一个 Toolset。获授权的另一 Agent 绑定业务集合后，用模型可见的原发布名调用截图和读图工具，并按需查阅目标 Host 的 Skill。验收以返回的实际内容及可关联的 harness ToolCall、OI `call_id` 核对身份、来源和结果。Host 离线、越权修改、同名 Skill 及包外读取均有对应检查。两款 App 的配置语义和操作范围已确定，具体嵌套 YAML 键留待实现。

[接口、配置和完整验收](../../../../spec/goals/creator-workspace.md) · [本轮记录](notes.md)
