# loom-oi 规格驱动协作规范

2026-11-02 · v3 · alpha。用户已要求从本轮起每次讨论蒸馏/分析/融合都提交相关Issue给开发者确认，并建设仓库skill和SDD约束。该工作纪律立即适用。旧#12把治理置于应用设计之前，已被用户否定；当前[应用设计](../designs/application-paths.md)承载机制，[DISC-001](../changes/DISC-001.md)维护来源和确认范围，SDD为支撑方法。具体规格仍Review，不将建规范再次包装成主要产品决策。这里只定义项目内工作方式，不安装外部完整方法框架。

## 1. 权威入口与分层

| 层 | 位置 | 负责的问题 | 不应承担 |
| --- | --- | --- | --- |
| 协作约束 | [AGENTS.md](../../AGENTS.md) | 主题/分支、授权、技能路由和必查命令 | 复制完整设计、把草案当最终决定 |
| 执行技能 | [Discussion SDD](../../.agents/skills/discussion-sdd/SKILL.md)、Architecture Partner、Evidence Lab | 分别追踪规格链、求解架构、生成可信证据；共用原始上下文 | 三个角色互相只传摘要 |
| 概念 | [loom-oi](../concept/loom-oi.md)、[OIC](../concept/oic.md)、[OIS](../concept/ois.md) | 是什么、谁使用、价值和职责边界 | API参数、SQL、未经确认的营销承诺 |
| 顶层架构 | [当前架构](../architecture/alpha-v0.7.md)与[图册](../architecture/diagrams/README.md) | 跨模块关系、端到端契约、共同不变量与选择 | 各模块各写一份相互冲突的协议 |
| 模块规格 | [OIC](../modules/oic/README.md)、[OIS](../modules/ois/README.md)、[消费接入](../modules/client/README.md) | 模块输入/输出、状态所有权、失败、预算与验收 | 将外部适配器变成核心必装模块 |
| 讨论变更 | `docs/changes/DISC-NNN.md/.json` | 来源→增量需求→方案→任务→验证→确认的范围 | 把历史记录当当前正文 |
| 知识与实验 | [KB](../../kb/README.md)、[实验室](../../experiments/README.md) | 来源、事实/推论/决议；预注册、原始记录、复核和ITER | 用方案评审替代实验，用实验替人类选产品 |

现有v0.7正文保留稳定链接，避免一次目录调整破坏历史Issue和ITER引用。模块页引用其共同契约，只细化本模块责任；协议今后迁移须在同一改动修复回链与验证清单。完整旧版本交给Git，不在当前树保存archive副本。仍参与证据复现的run和源码快照保留。

## 2. 每轮必经流程

1. **Intake**：读最新alpha、Issue正文/评论和原件；标来源哈希、页/时间、作者原标签及图片局限；同内容重复上传不增加证据权重。主题外愿景、闲聊和未核验传言分别处理。
2. **Shape**：先把主要讨论线程展开成具体应用方案（部署、输入/结果、正常/失败交互、备选和取舍），未来场景推导当前接口接缝，再把新增信息放回完整访问链和已确认边界。产生可执行推荐、备选、反例、成本与推翻条件；区分事实与原因假设，不止摘录问题。
3. **Specify**：按影响范围更新概念/架构/模块的Review草案，分配需求ID，绑定验证和依赖。Spec说明行为/约束；plan说明做法；task说明最小可验收增量。小轮次可合并在一份DISC文档，不强制四份重复文件。
4. **Submit review**：Issue正文必须独立承载上述应用机制，链接只补充细节；提交前用[内容评审](../../.agents/skills/discussion-sdd/references/content-review.md)核对，不能以治理主单替代技术方案。按[六节规范](../issue-management.md)实际创建或更新相关GitHub Issue，原位整合当前答案；本轮ID、来源、差异和文件归DISC，Issue保留方案/确认摘要，避免重复长正文。优先已有活动单；仅独立确认目标新建。没有新技术变化也在已有单提交“保持原方案及理由”供确认，不为凑数新建单。已关闭历史单默认不重开。
5. **Confirm scope**：采用记录附可核验的开发者原回复/明确用户指令、作者、时间、需求ID、确切版本与范围。报告性语句、团队倾向、AI总结、Issue关闭、检查器退出0都不是批准。部分批准只解锁对应部分；身份/决议有歧义时让Issue承载确认，不猜作者映射。
6. **Implement and verify**：产品实施只依赖已明确确认的范围及适用G0–G7门槛；独立草案、工具规范、样本收集和测试准备可继续。发生接口/权限/失败语义变化时先更新上游规格并重新评审受影响范围。
7. **Reconcile**：同步正文、图、模块、案例、KB和Issue，逐项记录无影响理由。实验操作另触发ITER闭环；本轮无实验不制造run。提交后回读GitHub，写入失败就报告待同步，不假称开发者已收到。

## 3. 状态和确认边界

- 工作状态：Draft / Review / Implementing / Verified / Superseded；采用状态：Pending / Accepted / Rejected / Deferred；验证结果：NotRun / Passed / Failed / Blocked。三者分别记录。
- 当前用户已授权的治理建设可以完成并提交；新产品选项保持Review/Pending。每轮确认义务并不把用户已明确要求的可逆文档编辑变成反复询问。
- 提交Issue是确认入口，不是向某位成员分派任务。不自动@人、分配、关闭、合并main、创建新探索分支或部署。
- 变更清单为范围索引；被确认的正文/图源指纹变化时，该确认不能覆盖新字节，先记录差异再请求受影响部分确认。可以使用独立小DISC避免一个无关编辑锁住整个项目。

## 4. 可执行检查

在仓库根目录：

```bash
python tools/check_structure.py
python tools/check_sdd.py docs/changes/DISC-001.json --phase review
python tools/check_sdd.py docs/changes/DISC-001.json --phase implement --require DISC-001-R1
python -m unittest discover -s tools -p 'test_sdd.py'
```

review不检查讨论覆盖、推理说服力或Issue正文质量。先做内容评审；review只检查来源/需求/确认Issue/验证关联、路径和文件指纹完整；Pending仍可提交评审。implement检查所选需求及其前置都Accepted且具有确认来源和相同规格指纹；当前DISC-001全范围仍预期拒绝；R3单独检查只覆盖已确认语言，不等于放行阶段。**脚本无法鉴别某评论是否确由获授权开发者发出、是否覆盖该决定，也不访问GitHub。执行AI必须实际读取原文核对；伪造JSON不能取得授权。** 然后仍须按[实验门槛](../../experiments/GATES.md)检查具体阶段；本检查不修改gate、不豁免G0，也不证明产品已运行。

检查器失效或条件缺失时如实标Blocked，不删除字段或把Pending改为Accepted绕过。新增检查器的负例验证不能当产品实验。

文档角色、索引、引用与历史规则统一见[结构规范](../structure.md)。`check_structure.py`补充结构检查，不替代本页确认检查或实验门槛；当前ADR-001记录Go采用的窄范围。

## 5. 技能建设与外部借鉴

本项目规则来自本会话的连续要求：同一AI求解和管理Issue、只在alpha工作、保留依赖和证据链、实验后自动分析、图文联动、专业绘图、Git维护旧版本，以及本轮开发者确认。外部实践只补足具体缺口，见[来源与取舍](../../.agents/skills/discussion-sdd/references/external-practices.md)。

优先简短入口和按需加载资源；确定性重复检查才写脚本，实际运行正负例；重大技能改动做隔离只读任务验证。目录排名不是可信度，先读固定版本源码/许可/工具依赖，再决定采用。无必要不安装整包、不执行未知安装脚本、不引入全局技能或另一套分支约定。
