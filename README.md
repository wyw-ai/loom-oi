# loom-oi

开发：[Autopilot启动](docs/prompts/autopilot.md) · [开发流程](docs/sdd/development-workflow.md) · [任务](spec/README.md) · [B1–B4采用](docs/decisions/ADR-004-p1-adoption.md)。以最新alpha和门槛台账为准，启动后持续交付已确认首版。

让脚本、应用和Agent发现并连接分散环境中的受控能力。OIC负责受控侧接入，OIS负责目录、策略与可信中继；业务实现由提供方拥有。当前工作在alpha：P1单实例R纵向闭环（`ois`/`oic`/`oi`）已实现，用法见[产品文档](docs/product/README.md)，历史G1证据见[ITER-004](docs/evidence/p1-g1-evidence-005.md)。当前统计夹具缺口及本轮FIN断言失败使G1为Failed、P2入口暂停；TASK-005已有G4四项直接证据，但须先补齐G1资源前置并完成Issue回读，见[ITER-005与C43](docs/evidence/p2-g4-evidence-006.md)。各门槛状态以[台账](experiments/gates.json)为准。

## 从这里开始

| 你要做什么 | 入口 |
| --- | --- |
| 理解项目主体、理念与共同承诺 | [概念](docs/concept/loom-oi.md) |
| 阅读当前设计 | [架构](docs/architecture/alpha-v0.7.md) · [图册](docs/architecture/diagrams/README.md) · [应用方案](docs/designs/application-paths.md) · [P1规格包](docs/architecture/p1/README.md) |
| 找待讨论问题和责任边界 | [Issue导航](docs/alpha-issues.md) |
| 查来源、已确认范围与实验 | [KB](kb/README.md) · [实验室](experiments/README.md) |
| 参与规格与文档维护 | [协作入口](AGENTS.md) · [SDD](docs/sdd/README.md) · [结构规范](docs/structure.md) |
| 查全部文档 | [文档导航](docs/README.md) · [完整目录](docs/catalog.md) |

当前正文维护现行方案，历史由Git保存；采用、实现和验证分别记录，不以文档完整代表产品完成。

当前补证首轮失败及修正规格见[ITER-006](docs/evidence/p1-g1-resource-evidence-007.md)：D16输出分片与D17 FIN完成竞争待闭环；G1为Failed/G4仍Blocked，修复生产后须新G1及新G4正式run。
