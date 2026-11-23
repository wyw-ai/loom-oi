# loom-oi · beta 演进规范

Service 与官方 File Store 的首版设计于 2026-11-23 封板，以下文档供实现和验收使用。当前可运行的接口以 beta `e40e35b7` 为基线。[该版本的规范索引](https://github.com/wyw-ai/loom-oi/blob/e40e35b7da2c62855560566ca485a5c250f69dc1/docs/spec/README.md)

| 阅读目的 | 入口 |
| --- | --- |
| 确认交付范围、Host A 截图验收和后续扩展边界 | [Service 与 File Store 首版目标](goals/service.md) |
| 实现发布模型、管理 API、MCP/REST 调用及配置迁移 | [Service 发布与调用](reference/service.md) |
| 实现文件上传、列目录、按路径查找和取回 | [File Store OIApp](reference/oiapps/file-store.md) |

需要追溯设计时，可查阅[当前目标共识](../goal-shaping/current/service/design.md)、[封板设计快照](../goal-shaping/archive/service/2026-11-23-first-release/design.md)和[讨论记录](../goal-shaping/archive/service/2026-11-23-first-release/notes.md)。
