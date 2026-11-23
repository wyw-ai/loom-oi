# Service 与 File Store 首版目标

2026-11-23 设计封板。实现基线为 beta `e40e35b7`。以下内容约定下一版要交付的能力，研发与真实桌面验收待执行。[目标与验收](../../../spec/goals/service.md)

## 能力如何发布

OIC 在选定 Host 上加载普通 OIApp、运行 worker 并登记 Action。发布者把不同 App、不同 Host 的 Action 选入同一份 Service。每个成员有发布名和固定的 `(channel, app, action)` 来源，一个来源可被多个 Service 复用。OIS 负责发布、鉴权与路由。Agent 通过 MCP 或 REST 依次调用所需成员。

Service 接替 vMCP 的发布与访问职责。MCP Tool 呈现 Service 成员。配置迁移须保留既有入口、发布名、输入契约、来源及访问范围。[Service 与调用接口](../../../spec/reference/service.md)

## 文件如何交接

官方 File Store（`file-store`）和 Browser、Filesystem 一样，由部署方在所选 Host 的 OIC 加载。它提供 `put_file`、`list_directory`、`lookup_file`、`get_file`，负责完整文件的上传、浏览、定位和取回。首版接通一个真实后端。

同机的生产者和上传方在双方都可访问的目录内交接相对路径。上传方把原件直接写入存储，返回存储路径和可校验的文件引用。能读取同一存储范围的 Host 可凭引用取回，也可从完整存储路径或目录重新找到原件。凭据与文件元数据由 File Store 及其后端管理。[文件 Action 与跨 Host 传递](../../../spec/reference/oiapps/file-store.md)

## 完成判据

用户请 Agent 看一眼 Host A 的屏幕。Agent 通过 Service 截取本次请求的真实桌面，将同一原件上传、读回，再用 `filesystem.view_image` 获得 MCP 图像。**Agent 说出的窗口、文字或画面内容与 Host A 当时的屏幕相符，才算通过。** 截图用于检验整条链路，Service 只负责组合和发布 Action。

首验采用的实际存储后端和旧管理接口的兼容期限留在实施时确定。独立存储之间的限时直传属于后续扩展。封板时的方案与讨论过程见[归档设计](../../archive/service/2026-11-23-first-release/design.md)和[讨论笔记](../../archive/service/2026-11-23-first-release/notes.md)。
