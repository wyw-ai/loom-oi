# 开发任务入口

本目录记录具有独立交付目标的任务；长期契约仍以`docs/`为准。当前规则对齐后，已确认产品任务采用单任务feature分支、文档先行、95%有效单测和文档代码一致性检查。

| 任务 | 范围 | 状态 |
| --- | --- | --- |
| [TASK-003](TASK-003-autopilot-entry/README.md) | 对齐Autopilot开发规则、落实#18采用、核验G0并提供启动指令 | 规则与采用完成、G0已核验；不包含Go产品实现 |
| [TASK-004](TASK-004-p1-r-vertical/README.md) | P1 R单实例纵向闭环：B1–B4的M1–M5，申请G1 | 完成：G1实现级证据已记录（ITER-004），合入alpha |
| [TASK-007](TASK-007-p2-g2-readiness/README.md) | P2/G2：观测、最终准入、独立发布与导出具体契约 | 规格待确认（DISC-009）；未实施，G3可独立先行 |

TASK-001与TASK-002位于各自历史feature；当前不直接合并其旧规则，也不复用其ID。
