# TASK-001修订2验证

核验日期：2026-11-03；执行者：Codex。目标基线为alpha `d9d15631fa4b9deca8c4bd0c11b276deeb36bb76`，任务分支为`feature/TASK-001-sdd-workflow`。同步及文档先行提交为[9ea8abb](https://github.com/wyw-ai/loom-oi/commit/9ea8abbd03daec0f966bd01c9b01b7648c188b8a)，实现前已推送并回读。

本记录验证当前源码与受检文档；最终任务凭据见[task.json](task.json)。源码、测试及策略共同指纹为`e1230c94933e7d1142beb2b69defd3ebb5521f6f22289b9e44d2c5c6b675a9ad`。文档指纹绑定最终差异，排除本记录、task.json和原始覆盖率报告，避免自引用；这些凭据不能替代人工语义评审。

## 单元测试与覆盖率

环境：Linux、Python 3.12.14、coverage.py 7.17.0。以下命令实际执行成功，52项测试通过；运行的是工具单元测试，不含产品或集成/E2E覆盖率。

```bash
python -m coverage run --source=tools --omit='tools/test_*.py' -m unittest discover -s tools -p 'test_*.py'
python -m coverage report -m
python -m coverage json -o spec/TASK-001-sdd-workflow/coverage.json
```

统计范围由[coverage-policy.json](../../tools/coverage-policy.json)固定为tools目录全部非测试Python源码，包含未被执行文件；测试源码计入证据指纹。原始报告为[coverage.json](coverage.json)。

| 文件 | 已覆盖语句 | 总语句 |
| --- | ---: | ---: |
| check_sdd.py | 170 | 173 |
| check_structure.py | 162 | 164 |
| check_task.py | 153 | 154 |
| 总体 | 485 | 491 |

总体为485/491=98.778004%，高于95%；未额外要求每模块95%。正反例还验证了两模块90%与100%、等权总体95%可通过，以及94%总体、缺失未执行源码、缺报告、分母不匹配和证据过期会拒绝。测试结果不意味着已安装远端CI。

## 文档与实现对应

| 规范/任务条款 | 实现或实际产物 | 验证及结论 |
| --- | --- | --- |
| 确认来源与唯一凭据 | check_sdd.confirmation_errors、confirmation_ref；approval.json引用本次用户请求 | 原始用户指令、旧Issue批准兼容；缺确认、错误范围和改变后冒用凭据拒绝。Issue追踪映射仍必需，无需重复索取确认评论 |
| 明确片段和依赖范围 | check_sdd.fingerprint及所选需求依赖遍历 | 无关章节/无关需求变化不阻塞所选任务；相关片段、循环和缺失依赖被检查 |
| 一个共同交付目标 | task.json、spec目录、check_task.validate | 错误分支、重复ID、越界文件、其他任务目录变更拒绝；文件范围检查不替代目标语义判断 |
| 文档先行与确认前共享 | revision-2.md、9ea8abb、approval.json和contract.md | 核对本次明确授权、方案及实际推送顺序；检查器不声称能验证外部原文或历史推送时间 |
| 总体单测95% | check_task.coverage_errors、固定策略、上述真实单测 | 完整文件集合、分子/分母、单元测试命令和源码/测试/策略指纹检查通过 |
| Draft/正式评审区分 | check_task draft/review、MR模板 | Draft允许验证待运行；review要求覆盖率、一致性证据和最新基线。允许创建Draft不等于允许合并 |
| 文档代码一致性及新鲜度 | check_task.documentation_digest、source_digest及本矩阵 | 文档、代码或测试改变后旧核验失效；语义仍需本次人工逐项核对，不能靠非空记录证明 |
| spec引用和导航 | check_structure递归检查spec Markdown；AGENTS、模板和入口 | 结构及链接检查通过，模板明确是假例且不构成确认记录 |
| 同步保留现有规则与采用 | alpha合并、DISC-001/002、KB来源ID | R3/R5及DISC-002-R2三个已采用需求对象与alpha逐字段相同；仅待确认记录更新受改文件指纹。并行W16重名改为本任务W-SDD前缀 |

本任务只涉及流程工具、规范、技能与模板，没有更改产品API、权限/失败语义、部署默认值、数据迁移或架构图；相应产品规格与实验物料保留alpha版本。任务与长期规范分别承担执行记录和现行规则，没有把旧任务证据改写成新的产品结论。

## 行为核查与门禁

独立只读核查使用三个未预先给答案的任务场景，禁止读取本任务及本记录；该核查仅阅读规则/源码，没有执行全仓检查。结果如下：

- 聊天已确认且其他任务改共享文档：保留准确聊天来源及片段，完成文档先推后可实施，不索取重复GitHub确认。
- 已确认任务需要PR触发CI，整体96.2%但某模块90%，文档30秒而已确认实现60秒：允许Draft；整体门槛满足但文档仍须修为60秒并验证后才能正式评审；不新增模块阈值。
- 权限修复跨API、迁移、测试和文档，另有头像裁剪且alpha前进：权限修复同一任务，头像裁剪另立任务；先吸收最新规则、核对影响和基线再推进。

独立核查同时指出：聊天批准仍需真实Issue追踪；当前覆盖率适配只覆盖Python工具；语义一致性仍需人工核对。规范已明确这些边界。两份涉及技能的格式检查通过。

本轮本地门禁：结构检查、DISC-001/002/003完整记录检查、DISC-003-R1实施范围检查、TASK-001的draft和review检查、差异空白检查均通过。既有已采用范围单独检查仍通过，待确认产品范围仍阻挡实施。

## 交付边界

只更新本任务feature，未创建MR、未合并alpha/main、未部署CI或远端分支保护。产品实验未运行，阶段门槛未晋级。新增Go/JS等交付单元须先建立其完整统计策略和报告适配，不能套用工具目录覆盖率。任务ID检查仅覆盖当前树，跨分支分配仍需预占和远端核对；执行前必须同步远端引用。
