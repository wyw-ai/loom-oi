# alpha：最新回复、设计与实验门槛

2026-11-03 · planning v11。现行[架构v0.7](architecture/alpha-v0.7.md)与[基线图及应用图](architecture/diagrams/v0.7/README.md)。本轮DISC-001 r4回溯指定原文，联动#13/#3/#4/#11；早前失效#12保持关闭，本轮不关闭或分派。#4既有中继/双后端决定保留，#8无新输入。

| 入口 | 已确认 | 本轮细化与剩余边界 |
| --- | --- | --- |
| [#13](https://github.com/wyw-ai/loom-oi/issues/13) | H11解耦/状态/互斥诉求；尚未采用新增细节 | r4补目标配对、三类资源、会话/连接及today/SDK取舍；R1–R5 Pending |
| [#3](https://github.com/wyw-ai/loom-oi/issues/3) | H9：OI不实例化业务功能 | computer-use完整责任链、登记契约、K2状态/策略矩阵；具体契约与默认仍Review |
| [#4](https://github.com/wyw-ai/loom-oi/issues/4) | H8：R中继；默认PG/无PG时SQLite | 双后端Store、SQLite规模维护、显式迁移；单实例/PG主备与恢复承诺待评审 |
| [#8](https://github.com/wyw-ai/loom-oi/issues/8) | 要求按功能/质量给语言推荐 | Go推荐未改变，双数据库驱动/构建纳入对比；采用Pending |
| [#11](https://github.com/wyw-ai/loom-oi/issues/11) | H10：按实现节点实验，未过不越级 | G0–G7、条件/结果分离、闭环后最终晋级；当前全部Blocked |

评分不因文档变多而提升：#3 P1/S19、#4 P1/S18、#8 P1/S16、#11 P1/S15；C均为2，尚无新的实现/部署实证。

<a id="3-旧问题如何整合"></a>
## 历史归档与承接

#1/#2/#5/#6/#7/#10按W7归档，#9此前由H6确认关闭；状态未改变。对象/生命周期/授权/失败方案进入架构；案例持续进入[台账](proposals/consumer-scenarios.md#7-持续案例台账)，问题由#3/#4/#8承接。归档不表示实验通过或产品实现完成。

<a id="remaining"></a>
## 未完成验证与阻挡墙

| 阶段/实验 | 缺口 | 放行门槛 |
| --- | --- | --- |
| P1入口 | K2/K5及具体存储契约待评审 | G0 |
| P1实际纵向闭环 | OIS/OIC/CLI、双后端CRUD、拒绝/超时/目标替换 | G1，阻止P2 |
| P2观测 | TTL/乱序/准入/独立发布/饱和公平性，LAB-005 | G2，阻止P3/P4 |
| P2失败与恢复 | 实际通道不重放/撤销与PG/SQLite恢复，LAB-006 | G3/G4，阻止P3/P4 |
| P3目标资源 | LAB-003目标1C1G/平台/负载与批准的语言比较 | G5，阻止交付承诺 |
| P3 PG主备 | LAB-006入口/任期/分区/旧实例/DB回退 | G6，仅HA承诺额外要求 |
| P4真实兼容 | LAB-004实际版本组合及身份/流/取消 | G7，阻止对应支持矩阵交付 |

[条件汇总 #11](https://github.com/wyw-ai/loom-oi/issues/11)跟进4类未就绪实验；[规则](../experiments/GATES.md)与[唯一状态台账](../experiments/gates.json)记录依赖、阻塞和证据。当前G0–G7未通过，机制fixture不满足实现门槛。独立样本/设计准备可以继续，不宣布下一阶段完成。

## DISC-001 r2：应用方案确认，旧#12失效

[#13应用方案确认](https://github.com/wyw-ai/loom-oi/issues/13) 直接承载Mac/ECS工具链、应用控制、宿主/Pod及未来harness接缝的来源、机制、示例和取舍。它负责本批讨论的应用组合/参考例范围，不再是治理壳；#3保存共用登记/准入契约，#8语言，#11真实实验条件，#4存储/恢复不受本次应用细化改变。

旧[#12](https://github.com/wyw-ai/loom-oi/issues/12)按W12明确授权关闭为not_planned，原文保留为反面案例；失效原因是主要交付变成治理确认、应用设计展开不足，非全部产品问题已解决。重做复用DISC-001并升为r2，新方案Pending；其他Issue不关闭/分派。

每轮实际提交确认Issue的纪律保留，正文必须独立可评审。详见[应用方案](changes/DISC-001.md)、[技能内容评审](../.agents/skills/discussion-sdd/references/content-review.md)。

## DISC-001 r3：#13诉求回应预处理

H11来自[#13回复](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6586567010)：能力端解耦、协议/OIC状态、应用互斥。本轮以[同一DISC的回应](changes/DISC-001.md#feedback13)细化通用接入、业务状态外置/运行状态重建、外部单一资源仲裁与静止后交接。R1/R2补范围，R5新增公共协议/状态待确认，R3/R4保持；#3承接共用契约，#11承接V04扩展及V09/V10，#4/#8无新取舍。

这是回应预处理与方案Review，没有产品实现/实验结果，未关闭或分配Issue；图7更新边界、图8补应用仲裁，现有G0–G7保持Blocked。
