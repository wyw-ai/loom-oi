# DISC-008：连续开发规则对齐与#18采用

2026-11-04 · 基线`eacdb8054555723fa466dfd4b2cfaddb817672d6` · [TASK-003](../../spec/TASK-003-autopilot-entry/README.md) · 回应入口[#18](https://github.com/wyw-ai/loom-oi/issues/18)。原始来源及准确采用见[ADR-004](../decisions/ADR-004-p1-adoption.md)。

## 问题、方案与取舍

讨论规则逐轮积累后，开发入口仍缺位；旧工具只接受GitHub评论、整文件指纹，并在选择单项实施时核验无关文件，导致明确会话确认与正常维护可能反复受阻。旧开发feature尚未合入，不能直接覆盖alpha的Issue只追加规则。B4又将既有95%改成80%，本轮纠正。

采用[开发流程](../sdd/development-workflow.md)作为唯一执行规范：任务spec、单任务feature、文档先行；已确认范围内自主实现与调试，日常代码循环不进入讨论审批；阶段证据闭环后继续；只有实质新取舍或外部阻塞暂停相关工作。沿用Git、Markdown、Issue和现有检查器，不加入工作流数据库或新的PR审批层。

检查器兼容旧批准，增加原始会话确认、唯一confirmation_ref和显式规格片段；所选需求及传递依赖核验，基本ID/引用仍检查。片段外只放进度/导航；关键参数必须处于批准范围。反例测试覆盖无关变更放行、真实契约变化拒绝、缺确认/假来源拒绝、依赖Pending拒绝和G0证据失效。

## 本轮采用与授权

- 用户请求`9f0fe3b4-5d91-4f1e-b2ef-1eb3b48f9eb4`与`11dafb4a-fa26-420e-8931-43fb49723ac5`是直接原始依据，保存于任务的[确认记录](../../spec/TASK-003-autopilot-entry/approval.json)。
- 本轮落实DISC-007-R1～R4和DISC-001-R6；恢复B4为完整交付单元95%。治理执行范围为DISC-008-R1。
- 不自动采用DISC-001-R1/R2应用范围或DISC-004-R2的H具体方案；DISC-007-R5为已完成F0记录，仍不作为P1采用前置。
- 三份旧ADR及原Accepted记录不改；待确认记录因当前文件维护所需的指纹刷新只保持Pending，逐项保留本轮差异来源。

## 项目收束与产物联动

用户结果、OI职责和R可信中继不变；B1–B4从评审进入采用，G0仅放行设计入口。开发质量阈值回到用户要求，完整交付要求与真实产品验证保持。更新协作入口、开发skill、SDD、启动prompt、P1采用状态、Issue导航、KB、阶段规则与图6；其他拓扑/状态图无机制变化，保持原图。普通工具测试不记产品run，不生成ITER。

## 验证与发布

执行结果、源码与报告指纹、独立只读行为验证、G0检查及远端回读见[TASK-003验证](../../spec/TASK-003-autopilot-entry/validation.json)。先完成规则与采用记录并发布回读，再以设计证据核验G0；产品G1–G7保持受阻。本记录不把工具覆盖率或设计审查当作产品质量证据。

历史依据：[TASK-001修订2](https://github.com/wyw-ai/loom-oi/blob/118deda1db427e936e0bbd3ba0d52207bac5a0bb/docs/sdd/development-workflow.md)只作借鉴，不整体合并其旧规则；当前轻量重复探索规范保持，PR #16不重开。
