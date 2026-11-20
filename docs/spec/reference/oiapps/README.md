# Filesystem 与 Browser 接口契约

本目录是两个官方 oiapp 自 2026-11-20 封板后的开发入口。逐项语义见两个 App 的参考，精确字段以本目录的 Draft 2020-12 Schema 为准。YAML 给出两份发行清单以及把全部 Action 暴露到同一个 vMCP 的配置示例。beta 的终端 App 继续使用原协议。两个 App 和图像交付已在 Windows 跑通。Filesystem 另在 Linux amd64 和 arm64，Browser 在 Ubuntu 24.04 x64 完成了端到端验证。[开发验证](../../../reports/oiapp-expansion/verification.md) [扩展目标与发布判据](../../goals/oiapp-expansion.md) · [已发布终端契约](../terminal-actions.md)

| 开发或评审任务 | 技术参考 | 可机器校验的文件 |
| --- | --- | --- |
| 实现 Filesystem 或对照 7 个 Action | [根目录、读写、搜索与图像](filesystem.md) | [输入、输出与实例配置 Schema](filesystem.schemas.json) · [完整 app.yaml](examples/filesystem/app.yaml) |
| 实现 Browser 或对照 18 个 Action | [会话、页面、事件与操作回执](browser.md) | [输入、输出与实例配置 Schema](browser.schemas.json) · [完整 app.yaml](examples/browser/app.yaml) |
| 为调用端构造参数、核对回执 | [HTTP 与 MCP 调用](../ois-api.md) | [每个工具一组参数，共 25 组](examples/arguments.yaml) · [含弹窗与截图的请求和结果](examples/browser-flow.example.json) |
| 配置受控 App、对外提供工具 | [配置职责](../configuration.md) · [宿主及图像协商](../app-runtime.md) | [Linux OIC](examples/oic.linux.yaml) · [Windows OIC](examples/oic.windows.yaml) · [OIS 的 25 个固定绑定](examples/ois.yaml) |

## 怎样使用这些契约

1. **实现 Action。** 每份 Schema 的 `$defs.config` 描述该实例的 `app_config`。输入使用 `$defs.<action>_input`。Filesystem 的 `write_file` 和 `edit_file` 输出共用 `$defs.mutation_output`，其余 23 项输出使用 `$defs.<action>_output`。字节预算、根目录限制、会话归属和实时页面尺寸还需要 worker 在执行时检查。
2. **组装发行包。** 每个 App 共用本包的一个根 runtime，handler 与 Action 同名。清单的 `input_schema` 给出完整输入，普通结果沿用 JSON，只有 `view_image` 和 `screenshot` 声明 `result_format: content.v1`。示例清单给出 Linux 启动命令，文件头注明 Windows 的可执行文件名。旧 OIC 识别不了新字段，会拒绝加载两份完整新清单。
3. **绑定和调用。** 提供方在 OIC 配置本地路径和权限。OIS 示例将每项 Tool 固定到 `local-workstation` 上确定的 App 和 Action，放入 `workbench`。HTTP 的 `arguments` 与 MCP `tools/call` 的 `params.arguments` 使用相同字段。示例里的根目录、通道、凭据环境变量名、工具名前缀和 profile 别名可按部署配置。

例如调用 `filesystem_read_file` 时，可用 `{"path":"reports/status.txt","max_lines":20}` 先取正文和版本。调用 `browser_screenshot` 查看局部小字时，传会话与标签回执中的 `session_id`、`tab_id` 及 `clip`。文件选区用方向校正后的源像素，网页选区用当前视口的 CSS 像素。成功的局部图像按原像素无损交付。完整字段、条件必填和互斥关系分别见两份清单；验收逐项核对返回的实际字节和尺寸。[Filesystem 清单](examples/filesystem/app.yaml) · [Browser 清单](examples/browser/app.yaml) · [发布验收清单](../../../goal-shaping/current/oiapp-expansion/acceptance.md)
