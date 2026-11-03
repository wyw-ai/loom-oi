# alpha Issue 导航

本页只维护问题分工和当前阅读入口；初始正文保留阶段快照，现状需结合明确阶段评论/采用范围读取，不能仅凭最新时间或旧正文推定。关键过程见[kb/lineage](../kb/lineage.md)。写作遵循[六节规范](issue-management.md)，变更/指纹见[DISC索引](changes/README.md)。

本轮整理与一次回复入口：[#17](https://github.com/wyw-ai/loom-oi/issues/17)。它汇总待确认项并映射回原问题，不另维护产品决议。已关闭的语言决策不因验证未完重新打开；发现疑似重蹈覆辙时执行[额外确认门控](issue-management.md#revisit-gate)。

| 入口 | 唯一负责的确认/跟进目标 | 规格 |
| --- | --- | --- |
| [#3 登记与准入](https://github.com/wyw-ai/loom-oi/issues/3) | 已采用R5边界下的详细登记/协议契约、K2准入默认 | [架构§2–6](architecture/alpha-v0.7.md#decoupling) |
| [#4 存储与恢复](https://github.com/wyw-ai/loom-oi/issues/4) | 已确认R/双后端后的实现支持与恢复边界 | [架构§8](architecture/alpha-v0.7.md#deployment) |
| [#11 实验条件](https://github.com/wyw-ai/loom-oi/issues/11) | LAB-003–006解阻、R1/R2物料与真实运行条件 | [实验门槛](../experiments/GATES.md) |
| [#13 应用实验与边界](https://github.com/wyw-ai/loom-oi/issues/13) | R1/R2物料与验证、应用支持范围；R5边界已采用 | [应用方案](designs/application-paths.md)、[DISC-001](changes/DISC-001.md) |
| [#15 端到端通道](https://github.com/wyw-ai/loom-oi/issues/15) | 路径策略、实际中继负载及部署的完整提案；R基线保持 | [I15-P03](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6607873200)、[通道方案](designs/ois-control-relay.md)、[DISC-004](changes/DISC-004.md) |
| [#17 本轮汇总](https://github.com/wyw-ai/loom-oi/issues/17) | 核对整理及C1准入/C2存储支持/C3路径范围；将回复按范围回写主单 | [门控/收束规则](issue-management.md#revisit-gate)、[本轮记录](changes/DISC-005.md#normative-correction) |

## 关系与阻挡范围

| 关系 | 具体含义 | 不应产生的重复 |
| --- | --- | --- |
| #13 规格 / #3 | 应用引用共同登记/准入契约；接口改变时同步适用组合 | 两单不分别选择不同K2默认 |
| #3 依赖 / #4 | 产品Open/恢复实施需一致的任期、许可与恢复契约 | 不阻挡独立应用方案比较 |
| ADR-001 依据 / #3、#4、#15 | 首版Go已采用；功能、存储与路径决定验证负担 | 不重复让用户选择Go，不宣称语言保证HA |
| #15 依据 / #3、#4及ADR-001 | 路径共用准确目标/准入及单一权威/许可；真实负载反馈部署取舍 | 不把拆进程当HA，不把OIC的1C1G当OIS容量承诺 |
| #11 承接 / #3、#4、#13、#15及已关闭#8 | 准备并回报各自验证条件；#8原平台/预算、Go版本/驱动/构建缺口在此继续 | 不在条件单新增产品采用或假定实验已通过；Rust对照须有新依据和准确授权 |

<a id="remaining"></a>
## 验证归属

G0–G7的唯一阶段状态在[门槛台账](../experiments/gates.json)；条件Ready、实验已通过与设计已采用分别记录。P1真实闭环→P2观测/传输/双后端恢复→P3资源/可选PG主备→P4真实兼容的依赖见[门槛规则](../experiments/GATES.md)，应用V01–V10见[DISC-001验证](changes/DISC-001.md#validation)。本导航不复制一份门槛状态表。

<a id="3-旧问题如何整合"></a>
## 历史承接

#1/#2/#5/#6/#7/#10按W7归档，#9按H6关闭；#12按W12标失效，由#13承接应用求解。原评论和关闭原因保留，关闭不证明实现或实验完成。

#8按本轮W25清理授权及[H12明确确认](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6586292098)关闭；[I8-P01收束](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6610554083)与[#11承接](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6610553366)保持未完成验证。#14本轮规范维护由#17承接，完成发布核验后关闭；历史I14-P02–P04不覆写。PR #16已按用户纠偏撤回关闭，工具未合入alpha；[当前轻量方案](designs/issue-evolution.md)不依赖它。
