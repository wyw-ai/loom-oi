# alpha v0.7：精确架构技术图

对应[架构正文](../../alpha-v0.7.md)。使用Graphviz（@viz-js/viz 3.25.0）专业矢量布局，由DOT确定节点/边，输出可缩放SVG；图7–9使用中文说明；原图1–6保留原版，此后新建或更新的图统一中文，仅保留组件/产品/协议专名与必要标识。数据不是生成模型猜测，采用状态与实验状态分别说明。历史ImageGen图见[v0.6历史图册](https://github.com/wyw-ai/loom-oi/blob/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture/diagrams/README.md)。

| 图 | 本轮核对重点 | 可编辑源 |
| --- | --- | --- |
| [01 总体职责](01-overview.svg) | 原生消费与可选外部工具适配；OI注册/通道；外部业务实例；PG或SQLite | [DOT](01-overview.dot) |
| [02 建连](02-open-sequence.svg) | 两腿附着、最终复核、失败清理、连接副作用边界 | [DOT](02-open-sequence.dot) |
| [03 准入](03-observation-state.svg) | 共同硬条件；NotReady均拒绝、Unknown按模式；K2是提案 | [DOT](03-observation-state.dot) |
| [04 恢复](04-recovery-sequence.svg) | 双后端单实例重启或经验证PG切换；新通道、不重放 | [DOT](04-recovery-sequence.dot) |
| [05 部署](05-deployment.svg) | 显式后端选择；SQLite单实例；PG主备G6；无自动降级 | [DOT](05-deployment.dot) |
| [06 实施](06-delivery-dependencies.svg) | 与gates.json依赖相符；单实例不依赖G6；全门槛未过 | [DOT](06-delivery-dependencies.dot) |
| [07 应用路径](07-application-paths.svg) | ECS执行、PC/设备控制、选定Pod；OI与外部状态所有者；未来Loom虚线候选 | [DOT](07-application-paths.dot) |
| [09 目标绑定](09-target-binding.svg) | 两路独立端点的准确配对；不把通道隔离当资源隔离 | [DOT](09-target-binding.dot) |
| [08 资源仲裁](08-resource-arbitration.svg) | 外部单一权威、有限回合、静止后交接；结果未知与资源执行分开 | [DOT](08-resource-arbitration.dot) |

原六张SVG已渲染为预览并逐张人工语义检查：未缺字、遮挡节点或把建议画成已实现；箭头表示对应职责/依赖，不用布局暗示无依赖。图2/4是有分支的流程图，不冒充精确时序/延迟图。图源与SVG指纹在[manifest](manifest.json)，重绘脚本为[render.mjs](render.mjs)。

复现：安装指定@viz-js/viz版本或使用已有运行时，将NODE_PATH指向模块目录，执行`node docs/architecture/diagrams/v0.7/render.mjs`。v0.6旧图由Git历史保存；原始实验记录继续用于证据追溯。

DISC-001更新图1：新增可选外部工具适配与外部执行服务，虚线框标Review；原生消费者可直接走客户端。已逐项核对箭头、OI/业务边界、未知结果和待确认状态。图2–6语义与源/输出不变；本轮未运行产品实验。


DISC-001 r2新增图7，将同一内核上的三条应用链明确画出；未来Loom虚线表示上层候选，不是当前依赖。蓝色为OI职责，绿色为业务服务/资源，紫色为消费侧工具/流程。主流程实际渲染并核对目标、两向数据、部署框和Pending范围；修正初稿跨子图rank导致的组件错位及标题穿线。图1–6源/SVG字节不变，无新产品实验。

## 图7：中文应用优化提案

图7不是新增用户案例；它将原讨论中的ECS、应用控制和Pod场景转成使用loom-oi的拟议方案。改进点分别是：目标/工具参数与通道分责、共享输入资源由业务方互斥、实例替换后重新发现/授权而不迁移旧通道。收益仍需实现与对照实验，不因中文化改变Pending状态。

2026-11-03依W13将标题、分组、节点、连线及状态说明改为中文，组件/产品/协议名保留；使用Noto Sans SC字体并渲染校对。节点标识、连接方向、分组和可选范围保持。

## H11：通用接入与业务资源交接

图7的三份OIC均为同一通用组件部署，补连接运行状态和外部仲裁边界；图8展示同一资源的有限交互回合、跨期/断线与静止确认、不能确认时隔离。两图说明均为中文，产品/组件名称保留英文；只表达Review方案，未实现或实测。图1–6字节保持。

## r4：先辨资源，再讨论互斥

图9是B1/B2为独立端点时的设计解释，原文并未给出严格对象定义；两条线不声称物理P2P、独立桌面或独立进程。图7明确独立资源/业务句柄与共享冲突的不同路径，图8只适用于共享冲突资源。专业渠道选择Graphviz以确定连线/条件，保留中文与待评审状态；本轮渲染并核对图7–9，原图1–6字节保持。
