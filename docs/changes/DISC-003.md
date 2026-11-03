# DISC-003：单任务 SDD 开发交付

修订2，2026-11-03。任务为[TASK-001](../../spec/TASK-001-sdd-workflow/README.md)，同步基线alpha `d9d15631fa4b9deca8c4bd0c11b276deeb36bb76`。规范唯一正文为[开发流程](../sdd/development-workflow.md)，执行由[SDD Development](../../.agents/skills/sdd-development/SKILL.md)负责，治理入口沿用[#14](https://github.com/wyw-ai/loom-oi/issues/14)。

## 来源与确认范围

原请求`6eb9ade9-3e40-48d7-b9b1-c45f5de40245`规定spec、确认后单任务feature先推文档、95%单测与MR前一致性。用户在流程反思后通过`3cce496f-7eee-48d4-bc53-48c1425fa72e`要求同步并修正；准确修订范围见[已推方案](../../spec/TASK-001-sdd-workflow/revision-2.md)，[确认凭据](../../spec/TASK-001-sdd-workflow/approval.json)绑定[规格片段](../../spec/TASK-001-sdd-workflow/contract.md)。

R1现在依据实际用户指令记为已采用，凭据由JSON引用，不再用待确认掩盖旧工具的来源格式限制。已采用只覆盖本SDD任务，不批准任何产品方案。新工具兼容旧Issue凭据与既有批准范围。

## 实质修正与同步

统一确认来源；新增规格按片段校验、实施隔离无关需求；区分Draft/正式评审/合并；明确确认前的共享方案版本；撤回每模块独立95%附加约束；按共同交付目标界定任务；补足spec、任务关联和真实覆盖率报告的本地检查。

保留alpha的中文规范、Issue原评论回复纪律、H13/H14和实验物料。发现两个并行分支重复分配W16，本任务来源改为带请求前缀的W-SDD标识，保留原alpha W16。对旧待确认记录仅同步受改动文件指纹；不刷新或扩大已采用的产品凭据。

## 验证与交付

代码和验证均归同一个TASK-001；文档先行提交[9ea8abb](https://github.com/wyw-ai/loom-oi/commit/9ea8abbd03daec0f966bd01c9b01b7648c188b8a)已合入最新alpha历史并推送。真实单元测试、覆盖率口径、反例与文档核对统一记录在[验证结果](../../spec/TASK-001-sdd-workflow/verification.md)。仅本地门禁，未部署CI/分支保护、未运行产品实验、未晋级gate；不自动创建MR或合并集成分支。
