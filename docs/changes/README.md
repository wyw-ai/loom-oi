# 讨论变更索引

每份DISC记录来源、需求/确认范围、差异、任务与验证；详细设计在其规格链接，批准检查用同名JSON。

| 记录 | 本轮问题 | 当前规格/确认入口 |
| --- | --- | --- |
| [DISC-001](DISC-001.md) / [JSON](DISC-001.json) | 原始讨论融合、能力目标/状态与应用路径 | [应用方案](../designs/application-paths.md)、#13/#3/#4/#8/#11；R3确认见ADR-001 |
| [DISC-002](DISC-002.md) / [JSON](DISC-002.json) | Issue与文档结构维护 | [结构规范](../structure.md)、[Issue规范](../issue-management.md)；历史#14由[#17](https://github.com/wyw-ai/loom-oi/issues/17)承接本轮核对 |
| [DISC-003](DISC-003.md) / [JSON](DISC-003.json) | 保留原材料清洗；先进程后路径的旧收束被DISC-004替代 | [原版记录](https://github.com/wyw-ai/loom-oi/blob/3d21e2f375c6ee87d3abe69402984755a9786952/docs/changes/DISC-003.md) |
| [DISC-004](DISC-004.md) / [JSON](DISC-004.json) | loom-oi整体理念与上下收束；通道路径/负载/部署整合 | [项目理念](../concept/loom-oi.md)、[通道方案](../designs/ois-control-relay.md)、#14/#15 |
| [DISC-005](DISC-005.md) / [JSON](DISC-005.json) | Issue只追加与演进记录；r5按反馈收回门控/流程并清理已收束单 | [轻量门控](../issue-management.md#revisit-gate)、[演进说明](../designs/issue-evolution.md)、[本轮汇总#17](https://github.com/wyw-ai/loom-oi/issues/17)；#14保留历史，#15为案例 |
| [DISC-006](DISC-006.md) / [JSON](DISC-006.json) | C1/C2/C3采用及开发准备度评估 | [ADR-003](../decisions/ADR-003-first-release-boundaries.md)、[评估与观察](../evidence/implementation-readiness-004.md)、#17/#3/#4/#11/#15 |
| [DISC-007](DISC-007.md) / [JSON](DISC-007.json) | B1–B4实施规格包与Windows前置修复 | [P1规格包](../architecture/p1/README.md)、本轮决策Issue；#3/#4/#11/#15/#17追加去向 |

修订原位整合，历史不在本目录复制。返回[文档导航](../README.md)。

- [DISC-008](DISC-008.md)：连续开发规则、会话采用与G0核验；承接DISC-007，当前开发执行见[开发流程](../sdd/development-workflow.md)。

- [DISC-009](DISC-009.md) / [JSON](DISC-009.json)：G2观测、最终准入与导出三组契约已准确采用（[ADR-005](../decisions/ADR-005-g2-adoption.md)）；产品待实施，详细方案见[规格](../architecture/p2/g2-observation.md)。
