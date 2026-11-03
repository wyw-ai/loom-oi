# alpha Issue 导航

本页只维护问题分工和当前阅读入口；初始正文保留阶段快照，现状需结合明确阶段评论/采用范围读取，不能仅凭最新时间或旧正文推定。关键过程见[kb/lineage](../kb/lineage.md)。写作遵循[六节规范](issue-management.md)，变更/指纹见[DISC索引](changes/README.md)。

| 入口 | 唯一负责的确认/跟进目标 | 规格 |
| --- | --- | --- |
| [#3 登记与准入](https://github.com/wyw-ai/loom-oi/issues/3) | 已采用R5边界下的详细登记/协议契约、K2准入默认 | [架构§2–6](architecture/alpha-v0.7.md#decoupling) |
| [#4 存储与恢复](https://github.com/wyw-ai/loom-oi/issues/4) | 已确认R/双后端后的实现支持与恢复边界 | [架构§8](architecture/alpha-v0.7.md#deployment) |
| [#8 语言采用后验证](https://github.com/wyw-ai/loom-oi/issues/8) | Go采用的准确范围、平台/预算与后续比较范围 | [ADR-001](decisions/ADR-001-core-language.md)、[专题分析](proposals/oic-language-tradeoffs.md) |
| [#11 实验条件](https://github.com/wyw-ai/loom-oi/issues/11) | LAB-003–006解阻、R1/R2物料与真实运行条件 | [实验门槛](../experiments/GATES.md) |
| [#13 应用实验与边界](https://github.com/wyw-ai/loom-oi/issues/13) | R1/R2物料与验证、应用支持范围；R5边界已采用 | [应用方案](designs/application-paths.md)、[DISC-001](changes/DISC-001.md) |
| [#14 整体收束与结构维护](https://github.com/wyw-ai/loom-oi/issues/14) | 项目理念/整体收束、Issue与文档规范，不代作产品决策 | [DISC-005](changes/DISC-005.md)、[阶段血缘](../kb/lineage.md) |
| [#15 端到端通道](https://github.com/wyw-ai/loom-oi/issues/15) | 路径策略、实际中继负载及部署的完整提案；R基线保持 | [通道方案](designs/ois-control-relay.md)、[DISC-004](changes/DISC-004.md) |

## 关系与阻挡范围

| 关系 | 具体含义 | 不应产生的重复 |
| --- | --- | --- |
| #13 规格 / #3 | 应用引用共同登记/准入契约；接口改变时同步适用组合 | 两单不分别选择不同K2默认 |
| #3 依赖 / #4 | 产品Open/恢复实施需一致的任期、许可与恢复契约 | 不阻挡独立应用方案比较 |
| #8 依据 / #3、#4 | 功能/存储负担决定资源验证范围，Go采用已有原确认 | 不重复让用户选择Go，不宣称语言保证HA |
| #15 依据 / #3、#4、#8 | 路径共用准确目标/准入及单一权威/许可；真实负载反馈部署取舍 | 不把拆进程当HA，不把OIC的1C1G当OIS容量承诺 |
| #11 承接 / #3、#4、#8、#13、#15 | 准备并回报各自验证条件 | 不在条件单新增产品采用或假定实验已通过 |

<a id="remaining"></a>
## 验证归属

G0–G7的唯一阶段状态在[门槛台账](../experiments/gates.json)；条件Ready、实验已通过与设计已采用分别记录。P1真实闭环→P2观测/传输/双后端恢复→P3资源/可选PG主备→P4真实兼容的依赖见[门槛规则](../experiments/GATES.md)，应用V01–V10见[DISC-001验证](changes/DISC-001.md#validation)。本导航不复制一份门槛状态表。

<a id="3-旧问题如何整合"></a>
## 历史承接

#1/#2/#5/#6/#7/#10按W7归档，#9按H6关闭；#12按W12标失效，由#13承接应用求解。原评论和关闭原因保留，关闭不证明实现或实验完成。此轮不关闭、重开或分派成员。
