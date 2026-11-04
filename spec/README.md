# 开发任务入口

本目录记录具有独立交付目标的任务；长期契约仍以`docs/`为准。当前规则对齐后，已确认产品任务采用单任务feature分支、文档先行、95%有效单测和文档代码一致性检查。

| 任务 | 范围 | 状态 |
| --- | --- | --- |
| [TASK-003](TASK-003-autopilot-entry/README.md) | 对齐Autopilot开发规则、落实#18采用、核验G0并提供启动指令 | 规则与采用完成、G0已核验；不包含Go产品实现 |
| [TASK-004](TASK-004-p1-r-vertical/README.md) | P1 R单实例纵向闭环：B1–B4的M1–M5，申请G1 | 历史交付完成并合入alpha；ITER-004冻结保留，当前G1资源前置失效及补证由TASK-005承接 |
| [TASK-005](TASK-005-p2-g4-recovery/README.md) | P2双后端单实例恢复：B3§8与TV-B3-11；当前同时修复统计夹具并补证G1前置，再申请G4 | 进行中（`feature/TASK-005-p2-g4-recovery`）；2ec3670新G1五项/G4四项正式判定均通过，D18正负对照与严格资源回归获验证；G1/G4为Inconclusive待Issue闭环和最终CI |

TASK-001与TASK-002位于各自历史feature；当前不直接合并其旧规则，也不复用其ID。
