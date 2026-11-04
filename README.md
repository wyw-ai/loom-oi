# loom-oi

开发：[Autopilot启动](docs/prompts/autopilot.md) · [开发流程](docs/sdd/development-workflow.md) · [任务](spec/README.md) · [B1–B4采用](docs/decisions/ADR-004-p1-adoption.md)。以最新alpha和门槛台账为准，启动后持续交付已确认首版。

让脚本、应用和Agent发现并连接分散环境中的受控能力。OIC负责受控侧接入，OIS负责目录、策略与可信中继；业务实现由提供方拥有。当前工作在alpha：P1单实例R纵向闭环（`ois`/`oic`/`oi`）已实现，用法见[产品文档](docs/product/README.md)，历史G1证据见[ITER-004](docs/evidence/p1-g1-evidence-005.md)。TASK-005的2ec3670历史G1/G4通过、六项CI及alpha集成保留。TASK-006的73bb667负载失败及后续D19反例保留；D20有界缓冲修正后，bff7aa2新G1/G4正式复证均通过，四单发布回读已完成，G1/G4恢复Passed，见[ITER-010/011](docs/evidence/p2-d20-revalidation-011.md)。独立CI首次PG写超时准备失败保留，最终CI尚未全绿，正式G3尚未运行。各门槛状态以[台账](experiments/gates.json)为准。

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

D16/D17/D18的2ec3670历史验证保持。当前73bb667完整单测覆盖96.0%不抵消G1反例；后续修正须完整复证、Issue回读及六项CI后才能集成alpha。
