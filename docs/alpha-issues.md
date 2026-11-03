# alpha Issue 导航

本页只维护问题分工和当前阅读入口；初始正文保留阶段快照，现状需结合明确阶段评论/采用范围读取，不能仅凭最新时间或旧正文推定。关键过程见[kb/lineage](../kb/lineage.md)。写作遵循[六节规范](issue-management.md)，变更/指纹见[DISC索引](changes/README.md)。

**当前采用与开发入口：[I18-P01](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6615315184)**——B1–B4已由本会话指令采用，95%更正见[ADR-004](decisions/ADR-004-p1-adoption.md)；已追加采用与G0核验回应，不重复征求同意。上一轮整理与回复入口：[#17](https://github.com/wyw-ai/loom-oi/issues/17)。H15已回复C1/C2/C3均按建议并持续观察；它承接本轮开发入口判断并映射回原问题，不另维护产品决议。已关闭的语言决策不因验证未完重新打开；发现疑似重蹈覆辙时执行[额外确认门控](issue-management.md#revisit-gate)。

| 入口 | 唯一负责的确认/跟进目标 | 规格 |
| --- | --- | --- |
| [#18 P1规格包确认](https://github.com/wyw-ai/loom-oi/issues/18) | B1–B4已采用，作为G0设计入口；承接采用/G0结果，产品实验另按阶段 | [P1规格包](architecture/p1/README.md)、[DISC-007](changes/DISC-007.md) |
| [#3 登记与准入](https://github.com/wyw-ai/loom-oi/issues/3) | 已采用R5边界下的详细登记/协议与提交契约；K2默认已采用；B1/B2按ADR-004已采用 | [I3-P03](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6614324363)、[架构§2–6](architecture/alpha-v0.7.md#decoupling) |
| [#4 存储与恢复](https://github.com/wyw-ai/loom-oi/issues/4) | C2首版支持/迁移边界已定；B3按ADR-004已采用，恢复与迁移在G4验证 | [I4-P03](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6614325932)、[架构§8](architecture/alpha-v0.7.md#deployment) |
| [#11 实验条件](https://github.com/wyw-ai/loom-oi/issues/11) | LAB-003–006解阻、R1/R2物料与真实运行条件；B4验收基线与F0 | [I11-P03](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6614327477)、[实验门槛](../experiments/GATES.md) |
| [#13 应用实验与边界](https://github.com/wyw-ai/loom-oi/issues/13) | R1/R2物料与验证、应用支持范围；R5边界已采用 | [I13-P01](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6610763602)、[应用方案](designs/application-paths.md)、[DISC-001](changes/DISC-001.md) |
| [#15 端到端通道](https://github.com/wyw-ai/loom-oi/issues/15) | C3研究范围已定；P1只走R、采用部署A；具体H/部署与实际中继负载继续验证 | [I15-P06](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6614328969)、[通道方案](designs/ois-control-relay.md)、[DISC-004](changes/DISC-004.md) |
| [#17 本轮汇总](https://github.com/wyw-ai/loom-oi/issues/17) | H15采用已回写；开发入口缺口已转入#18，无需重答C1/C2/C3 | [I17-P03承接](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6614330414)、[门控/收束规则](issue-management.md#revisit-gate)、[本轮记录](changes/DISC-005.md#normative-correction) |

本轮[开发准备度与观察清单](evidence/implementation-readiness-004.md)区分设计准备、P1正式入口和产品交付；当前具体采用见ADR-004；G0设计通过仍不代表产品实验通过。

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

#8按本轮W25清理授权及[H12明确确认](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6586292098)关闭；[I8-P01收束](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6610554083)与[#11承接](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6610553366)保持未完成验证。#14已在[I14-P05](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6610765080)收束关闭，本轮反馈由#17承接；历史I14-P02–P04不覆写，也不因此整体采用。PR #16已按用户纠偏撤回关闭，工具未合入alpha；[当前轻量方案](designs/issue-evolution.md)不依赖它。
