# Service 与 File Store 首版目标

**状态**　首版已实现并通过 Host A 桌面端到端验收。Agent 从 Filesystem 返回的 MCP 图像中读出了当次屏幕上的盲码，与独立记录相符。设计于 2026-11-23 在 beta `e40e35b7` 基线上封板。[本次验收记录](../../reports/service-files/acceptance.md) · [复现指南](../../guides/service-files.md)

## 交付范围

1. 发布者可从多个 Host 和 App 选取已有 Action，配置一份 Service 并发布 MCP 入口。来源在发布时确定。同一来源可进入多份 Service，各入口按自己的成员和访问规则提供调用。[发布模型、接口与迁移](../reference/service.md)
2. 随项目交付普通 OIApp `file-store`。它在部署方选择的 Host 上使用本地 OIC，接通服务端 AList 的 WebDAV 挂载，提供上传、存储目录浏览、完整路径查找和按引用取回。[File Store 契约](../reference/oiapps/file-store.md)
3. Windows Host A 上的 `computer-use.screenshot` 采集交互桌面，返回本机 PNG 相对路径和像素尺寸。由 Agent 选择、排序并调用截图、文件和看图 Action。Service 负责组合和发布，文件原件通过 App 与存储之间的连接传输。

## Host A 的验收

用户请 Agent 看一眼 Host A 的屏幕。发布者已将 A 的截图、所需 File Store 能力和 `filesystem.view_image` 发布到测试 Service。截图输出位于截图 App 可写、File Store 可读的位置。取回目录也在 Filesystem 的允许范围内。

| 步骤 | 可观察的结果 |
| --- | --- |
| 发现与截图 | Agent 通过 Service 的 MCP 入口发现工具，调用固定到 A 的截图成员。Host A 为此次请求生成实际桌面原件。 |
| 上传与定位 | A 的 File Store 上传原件并返回存储路径及 `file_ref`。持有完整路径可以找回同一引用，只知道目录时可以从可见条目找到对应文件。用此次回执或请求标识核对并发截图的归属。 |
| 取回与看图 | 用此次 `file_ref` 从后端读回到区别于源文件的位置，核对字节数和摘要，再通过 `filesystem.view_image` 将读回的图像交给 Agent。 |
| 用户结果 | **Agent 说出屏幕上可辨认的窗口、文字或画面内容，回答与 Host A 当时的桌面相符。** |

这条链路同时验证了跨 App 发布与文件保存。业务回执用于核对原件，最终验收依据是目标 Agent 对图像内容的回答。

## 跨 Host 使用与后续范围

文件引用应能由其他获准访问同一实际存储范围的实例独立解析。A 上传后，B 可以在文件保留期间稍后取回，完整原件依次经过 A、后端和 B。不同存储之间由接收端签发限时写入目标，属于后续扩展。

本版验收使用服务端 AList v3.66.0 的 Local 挂载。除 Host A 桌面视觉验收外，独立 Linux Host B 也已仅凭目录列举、按路径查回引用，再下载并校验与 A 一致的原件。其他存储驱动需另行验证。旧管理接口和旧 REST 字段在本版继续兼容。[Service 迁移规则](../reference/service.md)
