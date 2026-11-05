# loom-oi

开发：[Autopilot启动](docs/prompts/autopilot.md) · [开发流程](docs/sdd/development-workflow.md) · [任务](spec/README.md) · [B1–B4采用](docs/decisions/ADR-004-p1-adoption.md)。以最新alpha和门槛台账为准，启动后持续交付已确认首版。

让脚本、应用和Agent发现并连接分散环境中的受控能力。OIC负责受控侧接入，OIS负责目录、策略与可信中继；业务实现由提供方拥有。P1单实例R纵向闭环（`ois`/`oic`/`oi`）已实现，用法见[产品文档](docs/product/README.md)，历史G1证据见[ITER-004](docs/evidence/p1-g1-evidence-005.md)。当前TASK-007已集成alpha的D20/CLI修正、D21及完整证据：2ec3670与bff7aa2的历史G1/G4通过均保留，后者见已完成发布闭环的[ITER-010/011](docs/evidence/p2-d20-revalidation-011.md)；固定019139f的新G3四项、34叶/62正式资源phase及三单回读闭环见[ITER-013](docs/evidence/p2-g3-evidence-013.md)，其历史Passed仅适用于记录源码和环境。首轮[ITER-012](docs/evidence/p2-g3-first-evidence-012.md)、73bb667负载/D19与独立CI准备失败均保留，不外推G2/G5/G7。G2核心已实现、验证中；当前d9本地G1完整封口后仍为observed/fail，单测、控制切断、负载及附带G2输入反例见[ITER-014](docs/evidence/p2-local-g1-first-evidence-014.md)，G1为Failed；G3/G4仍Inconclusive，最终源码须完整复证。G3旧黑洞向量的v2准入适配及PG关闭锁交接问题由[TASK-007检查点](spec/TASK-007-p2-g2-readiness/README.md)承接；更早反例见[ITER-006](docs/evidence/p1-g1-resource-evidence-007.md)及[ITER-007](docs/evidence/p2-g4-resource-evidence-008.md)。各门槛当前状态以[台账](experiments/gates.json)为准。

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

D16输出分片、D17 FIN完成竞争与D18 SQLite读池资源修正已由2ec3670的新正式运行验证；该固定源完整覆盖95.9%，发布、CI及alpha闭环已完成。原失败与通过run都保留；当前G2尚无正式run，不复用旧覆盖率宣称新源通过。

TASK-007已合入alpha 15deeab的CLI取消与D20有界消息缓冲修正、G3的34场景/62资源阶段。独立15deeab首次正式G3未通过（4条预演记录误标为正式行为）；原件保存，D21修正及019139f复证已由ITER-013闭环；当前G2控制竞争反例与最终同源复证尚未闭环，不能用历史通过放行当前产品。
