# 开发任务入口

本目录记录具有独立交付目标的任务；长期契约仍以`docs/`为准。当前规则对齐后，已确认产品任务采用单任务feature分支、文档先行、95%有效单测和文档代码一致性检查。

| 任务 | 范围 | 状态 |
| --- | --- | --- |
| [TASK-003](TASK-003-autopilot-entry/README.md) | 对齐Autopilot开发规则、落实#18采用、核验G0并提供启动指令 | 规则与采用完成、G0已核验；不包含Go产品实现 |
| [TASK-004](TASK-004-p1-r-vertical/README.md) | P1 R单实例纵向闭环：B1–B4的M1–M5，申请G1 | 历史交付完成并合入alpha；ITER-004冻结保留，G1资源缺口及修正补证由TASK-005闭环承接 |
| [TASK-005](TASK-005-p2-g4-recovery/README.md) | P2双后端单实例恢复与严格G1资源补证 | 完成；G1/G4 Passed，六项CI成功，alpha已快进570b573，原始反例完整保留 |
| [TASK-006](TASK-006-p2-g3-path-faults/README.md) | P2真实传输/故障：34叶与62资源阶段，申请G3 | M1–M5及D19/D20已实现，bff7aa2的G1/G4复证闭环保留；D21后019139f新G3四项/34叶/62正式phase及ITER-013三单闭环完成，历史G3按记录范围Passed，d8bb599闭环源码与原件已纳入当前工作区；首轮失败不改 |
| [TASK-007](TASK-007-p2-g2-readiness/README.md) | P2/G2：观测、最终准入、独立发布与导出 | 已采用并完成核心/仪器/分析接线，纳入alpha的D20/D21及全部历史证据；当前首个本地G1已完整封口并精确复算失败，ITER-014反例及四单发布回读闭环已完成、G1保持Failed，G3/G4仍Inconclusive。本地执行与第二次DEV严格核验已完成，四份同源正式预注册已固定；最终G1/G3/G4/G2真实证据与闭环仍待完成 |

TASK-001与TASK-002位于各自历史feature；当前不直接合并其旧规则，也不复用其ID。
