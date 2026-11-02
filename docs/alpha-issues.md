# alpha Issue 导航

本页只维护问题分工和影响关系；正文、评论及实时工作状态以GitHub为准。写作遵循[六节规范](issue-management.md)，变更/指纹见[DISC索引](changes/README.md)。

| 入口 | 唯一负责的确认/跟进目标 | 规格 |
| --- | --- | --- |
| [#3 登记与准入](https://github.com/wyw-ai/loom-oi/issues/3) | 通用登记/状态契约、K2准入默认 | [架构§2–6](architecture/alpha-v0.7.md#decoupling) |
| [#4 存储与恢复](https://github.com/wyw-ai/loom-oi/issues/4) | 已确认R/双后端后的实现支持与恢复边界 | [架构§8](architecture/alpha-v0.7.md#deployment) |
| [#8 语言采用后验证](https://github.com/wyw-ai/loom-oi/issues/8) | Go采用的准确范围、平台/预算与后续比较范围 | [ADR-001](decisions/ADR-001-core-language.md)、[专题分析](proposals/oic-language-tradeoffs.md) |
| [#11 实验条件](https://github.com/wyw-ai/loom-oi/issues/11) | LAB-003–006解阻输入与阶段跟进 | [实验门槛](../experiments/GATES.md) |
| [#13 应用方案](https://github.com/wyw-ai/loom-oi/issues/13) | 外部参考例、应用组合和三类资源路线 | [应用方案](designs/application-paths.md)、[DISC-001](changes/DISC-001.md) |
| [#14 结构维护](https://github.com/wyw-ai/loom-oi/issues/14) | Issue/文档结构与检查机制，不重做产品决策 | [DISC-002](changes/DISC-002.md) |

## 关系与阻挡范围

| 关系 | 具体含义 | 不应产生的重复 |
| --- | --- | --- |
| #13 specifies / #3 | 应用引用共同登记/准入契约；接口改变时同步适用组合 | 两单不分别选择不同K2默认 |
| #3 depends-on / #4 | 产品Open/恢复实施需一致的任期、许可与恢复契约 | 不阻挡独立应用方案比较 |
| #8 based-on / #3、#4 | 功能/存储负担决定资源验证范围，Go采用已有原确认 | 不重复让用户选择Go，不宣称语言保证HA |
| #11 tracks / #3、#4、#8、#13 | 准备并回报各自验证条件 | 不在条件单新增产品采用或假定实验Passed |

<a id="remaining"></a>
## 验证归属

G0–G7的唯一阶段状态在[门槛台账](../experiments/gates.json)；条件Ready、实验Passed与设计Accepted分别记录。P1真实闭环→P2观测/传输/双后端恢复→P3资源/可选PG主备→P4真实兼容的依赖见[门槛规则](../experiments/GATES.md)，应用V01–V10见[DISC-001验证](changes/DISC-001.md#validation)。本导航不复制一份门槛状态表。

<a id="3-旧问题如何整合"></a>
## 历史承接

#1/#2/#5/#6/#7/#10按W7归档，#9按H6关闭；#12按W12标失效，由#13承接应用求解。原评论和关闭原因保留，关闭不证明实现或实验完成。此轮不关闭、重开或分派成员。
