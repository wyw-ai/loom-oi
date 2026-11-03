# TASK-001：SDD开发流程与执行检查

一个交付目标：将用户四项开发纪律形成一致、可执行的规范、技能、模板和本地检查。目标alpha、唯一feature、基线及真实验证字段以[task.json](task.json)为准；需求采用状态在[DISC-003](../../docs/changes/DISC-003.json)，准确确认见[approval.json](approval.json)。

## 方案与演进

第一版保存了四项纪律，但将确认与工具来源混用、全文件哈希耦合任务、禁用Draft MR、附加每模块95%且spec缺少自动检查。用户要求反思后授权同步修正，范围见[修订2方案](revision-2.md)和[已确认规格片段](contract.md)。历史原貌通过Git提交e299201保留，不在当前正文复制旧规则。

修订2先合入alpha的中文规范、Issue回复纪律与实验物料，解决共享规范及来源ID冲突。方案与同步已在[9ea8abb](https://github.com/wyw-ai/loom-oi/commit/9ea8abbd03daec0f966bd01c9b01b7648c188b8a)推送，再实现配套检查器；全部改动服务同一目标，不混入产品实现、CI部署或实验门槛调整。

## 验收对应

| 需求 | 对应产物与核验 |
| --- | --- |
| 本任务spec与长期文档分工 | [开发流程](../../docs/sdd/development-workflow.md)、[模板](../_template/README.md)、结构检查覆盖spec链接 |
| 完整确认后单任务feature、先推文档 | 可追溯凭据和文档先行提交；任务检查核对分支、需求、基线与文件范围 |
| 单测总体≥95% | 固定工具交付单元，全部非测试源码，原始报告和源码/测试/策略指纹；不声称产品覆盖率 |
| 正式评审前文档代码一致 | [逐项核验](verification.md)，Draft、正式评审和合并分阶段执行 |
| 并行任务互不误阻塞 | 规格片段、所选需求/传递依赖校验、保留旧凭据；回归覆盖无关与相关变化 |

## 实际验证与交付

真实结果统一见[verification.md](verification.md)，机器证据见task.json和[原始覆盖率报告](coverage.json)。修订2含检查器代码，不能沿用第一版“覆盖率不适用”。本任务只推送feature；未请求MR、合并或部署，产品采用和实验结果不由此改变。
