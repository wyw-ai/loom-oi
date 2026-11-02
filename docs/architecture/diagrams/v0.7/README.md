# alpha v0.7：精确架构技术图

对应[架构正文](../../alpha-v0.7.md)。使用Graphviz（@viz-js/viz 3.25.0）专业矢量布局，由DOT确定节点/边，输出可缩放SVG；短英文标签对应中文契约。数据不是生成模型猜测，采用状态与实验状态分别说明。历史ImageGen图见[v0.6历史图册](https://github.com/wyw-ai/loom-oi/blob/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture/diagrams/README.md)。

| 图 | 本轮核对重点 | 可编辑源 |
| --- | --- | --- |
| [01 总体职责](01-overview.svg) | 原生消费与可选外部工具适配；OI注册/通道；外部业务实例；PG或SQLite | [DOT](01-overview.dot) |
| [02 建连](02-open-sequence.svg) | 两腿附着、最终复核、失败清理、连接副作用边界 | [DOT](02-open-sequence.dot) |
| [03 准入](03-observation-state.svg) | 共同硬条件；NotReady均拒绝、Unknown按模式；K2是提案 | [DOT](03-observation-state.dot) |
| [04 恢复](04-recovery-sequence.svg) | 双后端单实例重启或经验证PG切换；新通道、不重放 | [DOT](04-recovery-sequence.dot) |
| [05 部署](05-deployment.svg) | 显式后端选择；SQLite单实例；PG主备G6；无自动降级 | [DOT](05-deployment.dot) |
| [06 实施](06-delivery-dependencies.svg) | 与gates.json依赖相符；单实例不依赖G6；全门槛未过 | [DOT](06-delivery-dependencies.dot) |

六张SVG已渲染为预览并逐张人工语义检查：未缺字、遮挡节点或把建议画成已实现；箭头表示对应职责/依赖，不用布局暗示无依赖。图2/4是有分支的流程图，不冒充精确时序/延迟图。图源与SVG指纹在[manifest](manifest.json)，重绘脚本为[render.mjs](render.mjs)。

复现：安装指定@viz-js/viz版本或使用已有运行时，将NODE_PATH指向模块目录，执行`node docs/architecture/diagrams/v0.7/render.mjs`。v0.6旧图由Git历史保存；原始实验记录继续用于证据追溯。

DISC-001更新图1：新增可选外部工具适配与外部执行服务，虚线框标Review；原生消费者可直接走客户端。已逐项核对箭头、OI/业务边界、未知结果和待确认状态。图2–6语义与源/输出不变；本轮未运行产品实验。
