# alpha：最新回复、设计与实验门槛

2026-11-02 · planning v8。现行[架构v0.7](architecture/alpha-v0.7.md)与[六图](architecture/diagrams/v0.7/README.md)。本轮原位处理#3/#4/#11并联动#8；未关闭、重开或分派Issue。

| 入口 | 已确认 | 本轮细化与剩余边界 |
| --- | --- | --- |
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
