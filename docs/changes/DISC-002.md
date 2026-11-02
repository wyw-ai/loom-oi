# DISC-002：Issue与文档结构维护

修订1 · alpha · 基线`36612d7f777c177d6bb09a7ae4b83d722b4bfae2`。治理/文档编辑已获用户授权，结构规则细则可评审；产品实施与实验门槛独立。确认入口为[#14](https://github.com/wyw-ai/loom-oi/issues/14)。

## 问题与推荐

请求74bf7bb3-76ba-4795-8024-3f9d9f084042要求寻找清晰结构规范并迭代Issue、文档与引用。实际问题是多轮整篇回应叠加、DISC兼作说明书、索引复制状态与缺少明确引用职责；另发现#8已有Go明确确认，当前正文却遗漏。

采用[六节Issue规范](../issue-management.md)、[内容归位与引用规范](../structure.md)、生成目录与轻量结构检查，并整合既有Discussion SDD技能。借鉴Diátaxis/arc42/MADR/GitHub官方方法，来源和不采用部分在结构规范，未安装新框架。替代“只加模板”不能解决旧文漂移；全量改目录破坏链接成本高，因此保留主要稳定路径，迁移真正重复的应用正文。

## 迁移与准确范围

| 旧形态 | 当前维护位置 | 保真检查 |
| --- | --- | --- |
| #13/#11等追加r2/r3/r4整篇 | 各Issue六节当前快照 | 原评论保留，具体正常/失败链仍在正文，完整历史基线可查 |
| DISC-001复制应用与状态方案 | [应用设计](../designs/application-paths.md)维护具体场景；架构维护共同契约；DISC保留来源/需求/验证 | D01–D08、R1–R5、V01–V10不改ID；旧锚点跳转 |
| 多份索引复述当前决定 | 根README→docs导航/域索引→权威正文；catalog生成完整目录 | 分类是用途，不冒充采用状态 |
| Go仍写Pending | [ADR-001](../decisions/ADR-001-core-language.md)、#8、架构/图1、KB和R3 | 只确认OIS/OIC首版Go；CLI/驱动/性能/HA未批准 |
| skill/prompt重复字段堆叠 | Discussion SDD调用结构/Issue规范与检查器 | 保留求解深度、人类确认、实验门槛与自动闭环 |

原应用的参数/脚本边界、未知结果、目标替换、Host/SNI/Origin、stdio、三类资源/仲裁、Pod身份和未来harness均保留；本轮无新增产品选择。图1出现旧Go Pending必须修正，依中文规则重绘；图2–9不变。

## 需求、任务与来源

| ID | 目标 | 来源 | 任务/验证 |
| --- | --- | --- | --- |
| DISC-002-R1 | Issue与文档当前结构、引用、模板/skill及可执行检查 | W15用户请求 | 文档迁移/Issue重写/目录工具；V01结构与负例、V02独立任务；远端交付另在#14记录 |

治理编辑授权与规则细则评审分开，R1保持Pending不阻挡用户已要求的文档优化。H12来源为[#8原回复](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6586292098)，其确认落实在DISC-001-R3，不重复创建语言需求。未运行/复核产品raw，不新增ITER。

## 验证与限制

本地检查：`python tools/check_structure.py --issues <本轮6单快照>`检查全量目录、引用/锚点、唯一用途、强依赖及六节正文；`python -m unittest discover -s tools -p 'test_*.py'`共15项通过（原SDD 9项、新结构6项）。两个项目skill的quick_validate通过。图1已渲染并目视检查中文/连线/边界，图2–9源和SVG逐字节未改变。实验目录、gates及raw均未修改。

SDD review需在刷新Pending指纹后通过；R3只绑定ADR-001及H12。对整份DISC-001执行implement预期被R1/R2/R4/R5阻挡；仅R3的记录检查可以通过，不代表G0/G5或整体实施获准。DISC-002-R1仍Pending，治理授权不冒充规则细则已获确认。

只读行为验证实际使用整理前#3正文/全部3条评论、alpha基线及修订技能，不提供主流程答案，任务为重新编写#3并指出应同步的文档。产出六节可独立评审稿，保留H9准确范围、K2矩阵、逐路目标、运行状态、业务失败与实质替代。主流程检查并修正其指出的旧章节号、stdio“由OI启动”措辞、K2最终复核歧义与确认范围；发布前补齐当时尚未完成的目录/检查器和指纹。它验证技能行为，不证明产品或人类采用。

可复核片段：最终复核时require_ready的Ready过期拒绝；best_effort按Unknown继续；两模式有效NotReady均拒绝。原笼统“按有效模式拒绝”被澄清，契约仍来自当前架构§6.2。独立任务输入SHA-256 `84aa00d0b4d914e8afa6f8d84afe5725ddb24c2585ddf983c20cda170267241e`；输出SHA-256 `af91b2dd7f8d33d8522b2a844cce25015237d11b034aa9bb9b2b99cf2e094af0`；这些仅标识本次临时任务产物，不替代已提交规范、测试或原始评论。

远端正文/提交一致性及成员字段回读结果在#14交付检查更新，不将本地通过提前当作远端成功。结构检查不判断原意/方案充分性；SDD仍校验源/确认/指纹，产品门槛保持独立，原Pending需求不得被自动接受。

## 历史追溯

[整理前DISC-001](https://github.com/wyw-ai/loom-oi/blob/36612d7f777c177d6bb09a7ae4b83d722b4bfae2/docs/changes/DISC-001.md)及同提交的架构/索引保留旧正文。此次Issue正文编辑不删除任何评论、负责人、标签、里程碑或开关状态；无main合并、无探索分支新增。
