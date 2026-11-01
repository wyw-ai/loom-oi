# alpha：产品前置、架构与证据的 Issue 规划

v4 · 2026-11-02。本轮新回复来自#2/#3/#8/#9，全部回源核对；#9明确授权关闭，已执行。#10按#3明确要求新建，未新建探索分支或分派负责人。

## 1. 回复与落实

| 来源 | 准确要求 | 本轮结果 |
| --- | --- | --- |
| [#3回复](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6562173600) | 调研“何时成立”，新建产品承诺/首批消费者前置单 | 建立[#10](https://github.com/wyw-ai/loom-oi/issues/10)，补A/B/C成立/反证与来源；A只作机制基线 |
| [#2回复](https://github.com/wyw-ai/loom-oi/issues/2#issuecomment-6562416689) | 更广更深业务场景，含探针、监控、不同能力节点 | 八类节点×生命周期/协议/探测/统计/资源/失败；补状态、监测责任与LAB-001 |
| [#8回复](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6562293923) | 给本项目推荐选型，考虑可用性、维护、灵活扩展 | 推荐Go统一核心，明确Rust翻转条件、HA机制与实验缺口；人类采用仍Pending |
| [#9回复](https://github.com/wyw-ai/loom-oi/issues/9#issuecomment-6562311062) | 允许关闭，边界已下结论 | 已closed/completed；未完成实验归活动主线，不勾假通过 |
| 当前会话 | 建隔离实验室skill，随进展更新架构最终产物 | 新skill、可运行夹具/证据校验、原始run、独立审阅；图文/KB联动 |

## 2. 当前结构

| 位置 / Issue | 独立目标与依赖 | 当前成熟度/状态 |
| --- | --- | --- |
| [#1](https://github.com/wyw-ai/loom-oi/issues/1) 总入口 | 整体证据/反馈/产物一致性 | Review |
| [#10](https://github.com/wyw-ai/loom-oi/issues/10) 产品前置 | 真实消费者、承诺层级、节点/可用性范围；前置#3，约束#2/#4/#8 | Designing；范围Pending |
| [#3](https://github.com/wyw-ai/loom-oi/issues/3) 消费主线 | 在#10条件下选择A/B/C及入口，承载独立闭环 | 方案/资料；产品形态Pending |
| [#2](https://github.com/wyw-ai/loom-oi/issues/2) 对象主线 | 多维节点、生命周期、观测与自治；吸收#7 | 方案+LAB-001窄机制；完整模型Pending |
| [#4](https://github.com/wyw-ai/loom-oi/issues/4) 连接主线 | 网络、信任、失效/恢复与资源约束；吸收#5/#6 | 方案+LAB-002窄机制；协议/HA Pending |
| [#5](https://github.com/wyw-ai/loom-oi/issues/5) 专项 | Integrated→#4；授权随路径与主体边界 | 无真实撤销实验 |
| [#6](https://github.com/wyw-ai/loom-oi/issues/6) 专项 | Integrated→#4；通道/业务结果与重试 | LAB-002反例，非完整OI验证 |
| [#7](https://github.com/wyw-ai/loom-oi/issues/7) 专项 | Integrated→#2；健康/资源/生命周期反例 | LAB-001局部观测；TTL/隔离未测 |
| [#8](https://github.com/wyw-ai/loom-oi/issues/8) 选型 | 质量目标下Go/Rust及实际部署成本 | Go为AI推荐；语言决策Pending；LAB-003 Blocked |
| [#9](https://github.com/wyw-ai/loom-oi/issues/9) 已完成边界 | 暂不接Loom、OI独立、未来Loom侧适配 | H1 Accepted；按授权关闭 |

依赖约束规格冻结，不阻止继续比较和实验。Integrated不是已完成；#5/#6/#7的成本/风险计入主线，不重复排期。#9关闭不影响其结论继续约束所有单。

## 3. 旧问题如何整合

保留v3的#3/#2/#4主线与专项关系；v4增加产品前置和证据，而不是再拆六个实验Issue。实验ID在[实验索引](../experiments/README.md)管理，需要独立产品/实施目标时再建单。此前#9所带的未完成试验已迁入#10/#3/#4/#8。

v2原拆分、v3调整均保留Git历史；无回复不是默许旧方案。D1/D5→#3，D2/D4/D6→#2，D3→#4；S2–S9锚点仅保持追溯。

## 4. 排序与可信度

#10为P1，I/U/N/E/C=5/5/4/3/2，参考分19；#3/#2/#4/#8维持P1，先确定真实约束再冻结规格。#5/#6/#7不重复计分；#9已完成，不列活跃资源分配。分数不是方案选择。

C不能整单升级：TCP可达/HTTP差异和无幂等盲重试反例有可复现机制观测；完整对象/协议/生产HA/语言性能仍没有实现数据。样本和范围见[证据报告](evidence/alpha-lab-round1.md)，Source/Model/Mechanism/Implementation/Deployment分层见实验室规约。

## 5. 当前评审入口

[产品与多维场景](proposals/consumer-scenarios.md) → [整体方案](proposals/alpha-solution-design.md) → [Go/Rust推荐](proposals/oic-language-tradeoffs.md)。[当前架构](distributed-architecture-conclusions.md)图文同步职责、分层状态、响应丢失时序和候选部署；[产物联动表](architecture-artifacts.md)负责以后每轮检查。
