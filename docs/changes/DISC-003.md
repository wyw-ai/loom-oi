# DISC-003：单任务 SDD 开发交付

2026-11-03，讨论基线alpha `aa9db84aa176aa688a4dcf68c11cbc6814b9905b`。用户请求 `6eb9ade9-3e40-48d7-b9b1-c45f5de40245` 明确四条规则：spec维护本次任务、完整确认后单任务feature并先推文档、单测覆盖率至少95%、MR前核对文档代码一致性。

## 增量与归属

唯一实施任务为[TASK-001](../../spec/TASK-001-sdd-workflow/README.md)，规范正文在[开发流程](../sdd/development-workflow.md)，执行为[SDD Development](../../.agents/skills/sdd-development/SKILL.md)。现有Discussion SDD继续承接讨论求解和确认，增加下游开发路由。治理关系复用[#14](https://github.com/wyw-ai/loom-oi/issues/14)，不新建产品问题、不处理该单所有其他反馈。

## 确认与依赖

W16用户原指令直接授权本次规则保存及其文档化范围；不要求再次确认相同四点。细则可以后续评审修订，机器DISC清单保持Pending：当前检查器的Accepted要求GitHub原始确认评论，本次用户指令不伪造成评论。这个机器记录限制不否定已存在的用户文档授权，也不放行任何产品实现。

规范落地任务和开发子步骤见TASK-001，不在DISC重复长方案。旧DISC-002中涉及编辑的Pending文档指纹按实际文件更新；原产品需求和已接受语言范围不改。此次没有产品实验，不产生ITER或gate晋级。

## 验证与交付

结构、SDD回归、spec链接、技能格式和独立行为任务的实际结果统一维护在TASK-001。覆盖率不适用仅针对本次纯文档diff，不是未来代码任务豁免。保存到独立feature，目标为alpha；未请求MR或合并。
