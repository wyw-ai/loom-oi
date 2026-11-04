# alpha v0.7 精确架构图册

Graphviz（@viz-js/viz 3.25.0）确定节点与连线，输出SVG。共同契约见[架构](../../alpha-v0.7.md)，具体场景见[应用设计](../../../designs/application-paths.md)。图1–11统一中文，必要协议代码保留。

| 图 | 本轮核对重点 | 可编辑源 |
| --- | --- | --- |
| [01 总体职责](01-overview.svg) | 原生消费与可选外部工具适配；OI注册/通道；外部业务实例；PG或SQLite | [DOT](01-overview.dot) |
| [02 建连](02-open-sequence.svg) | 两腿附着、最终复核、失败清理、连接副作用边界 | [DOT](02-open-sequence.dot) |
| [03 准入](03-observation-state.svg) | 共同硬条件；NotReady均拒绝、Unknown按模式；H15已采用默认，具体实现待验证 | [DOT](03-observation-state.dot) |
| [04 恢复](04-recovery-sequence.svg) | 双后端单实例重启或经验证PG切换；新通道、不重放 | [DOT](04-recovery-sequence.dot) |
| [05 部署](05-deployment.svg) | 显式后端选择；SQLite单实例；PG主备G6；无自动降级 | [DOT](05-deployment.dot) |
| [06 实施](06-delivery-dependencies.svg) | 与gates.json依赖相符；单实例不依赖G6；G0已通过；TASK-006客户端生命周期修复后，G1/G4当前源码待复验；2ec3670历史通过及全部反例保留 | [DOT](06-delivery-dependencies.dot) |
| [07 应用路径](07-application-paths.svg) | ECS执行、PC/设备控制、选定Pod；OI与外部状态所有者；未来Loom虚线候选 | [DOT](07-application-paths.dot) |
| [09 目标绑定](09-target-binding.svg) | 两路独立端点的准确配对；不把通道隔离当资源隔离 | [DOT](09-target-binding.dot) |
| [08 资源仲裁](08-resource-arbitration.svg) | 外部单一权威、有限回合、静止后交接；结果未知与资源执行分开 | [DOT](08-resource-arbitration.dot) |
| [10 通道研究与部署](10-control-relay.svg) | 项目约束→R/H/M路径→实际负载→A/B/C部署→端到端反馈；不是数据连接拓扑 | [DOT](10-control-relay.dot) |
| [11 项目整体概念](11-project-model.svg) | 外部基础、对象/共同契约与能力支撑整体结果；不是五个进程 | [DOT](11-project-model.dot) |

## 生成与校对

`NODE_PATH`指向包含指定@viz-js/viz的模块目录，运行`node docs/architecture/diagrams/v0.7/render.mjs`。图源/输出哈希见[manifest](manifest.json)，脚本见[render.mjs](render.mjs)。中文使用Noto Sans SC，渲染后检查字形、布局、职责及方向。

图7的多个OIC是同一通用组件的不同部署；图8只适用于真实共享冲突资源；图9只示意B1/B2为独立端点时的绑定，不证明物理资源/进程隔离。H13已采用业务状态外置、准确绑定与可重建运行状态；H15已采用准入默认及首版支持范围；应用组合/字段/性能仍分别评审和实测。

本轮DISC-006按H15更新图3/5/6/10的采用、未验证与观察标识；H仅研究范围获确认，图中候选不表示生产能力。其余图的节点/连线和职责未改变，无新产品实验。TASK-004（ITER-004）只更新图1/9/11的实现状态文字和图6的G1状态，节点/连线与职责不变。TASK-005（ITER-005/C43）仅更新图6的当前状态：旧G1记录保留，资源前置需补证，G4有直接证据但未晋级；依赖边和职责不变。原图与历轮校对记录用Git追溯。

历史TASK-005图06曾按ITER-006/007的2ec3670复证标示：新G1/G4运行均通过，资源修正已验证，四单Issue阶段已发布并逐字回读，G1/G4恢复Passed；CI与alpha集成进度见TASK-005检查点。原失败/通过证据均保留历史范围，依赖边及产品契约不变；工作流成功不直接代表门槛晋级。

本轮图06沿用Viz3.25.0渲染，并用resvg2.6.2转PNG进行视觉核验：中文可辨，无重叠或裁切，状态与依赖一致；其余10张SVG与前版本逐字节相同。PNG仅为ignored核验辅助，正式产物为DOT/SVG及manifest。

TASK-006仅更新图06当前适用性：CLI取消缺陷已复现并修复中，G1/G4当前源码退回Inconclusive；旧通过不变，依赖边不变。本次沿同版本Viz/resvg重新生成并检查PNG，无裁切或重叠，其他10个SVG逐字不变。

TASK-006首轮正式复证后图06更新为：G1负载Failed、G4直接四项通过但共同前置未恢复，G5采用用户现有主机。原依赖边与其余十图不变；本轮重渲染并检查中文布局，参见ITER-008/009。
