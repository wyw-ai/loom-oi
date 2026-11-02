# alpha v0.7 精确架构图册

Graphviz（@viz-js/viz 3.25.0）确定节点与连线，输出SVG。共同契约见[架构](../../alpha-v0.7.md)，具体场景见[应用设计](../../../designs/application-paths.md)。图1、7–9为中文，旧图2–6在下次实际更新时依规则中文化。

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


## 生成与校对

`NODE_PATH`指向包含指定@viz-js/viz的模块目录，运行`node docs/architecture/diagrams/v0.7/render.mjs`。图源/输出哈希见[manifest](manifest.json)，脚本见[render.mjs](render.mjs)。中文使用Noto Sans SC，渲染后检查字形、布局、职责及方向。

图7的多个OIC是同一通用组件的不同部署；图8只适用于真实共享冲突资源；图9只示意B1/B2为独立端点时的绑定，不证明物理资源/进程隔离。应用方案均待相应评审/实测。

DISC-002因H12 Go采用修正图1并完整中文重绘；图2–9源/输出保持，无新产品实验。原图与历轮校对记录用Git追溯，不在本页重复追加每轮日志。
