# loom-oi 协作入口

<a id="writing-routes"></a>
## 编写任务入口

| 本次任务 | 必读入口 | 提交前检查 |
| --- | --- | --- |
| Issue回复、分类、状态与正文 | Discussion SDD → [Issue规范](docs/issue-management.md) → 对应模板 | 六节、中文显示、准确确认范围及原回复 |
| 架构/模块/应用方案 | Architecture Partner → [结构规范](docs/structure.md) → 对应当前规格 | 正常/失败链、上游契约、图文影响 |
| 文档/索引/引用维护 | Discussion SDD → [结构规范](docs/structure.md) → catalog | 目录、路径/锚点、唯一职责与强依赖 |
| 实验准备/运行/证据分析 | Evidence Lab → [实验规约](.agents/skills/architecture-evidence-lab/references/protocol.md) → [物料](experiments/materials/README.md) | 准备与实测分开、隔离/输入条件、闭环和门槛 |

按本次涉及的行加载，不为每个文件新建skill；以下公共约束始终适用。


处理架构塑形、讨论净化、脑暴、Issue 规划或知识沉淀时，读取 [.agents/skills/architecture-partner/SKILL.md](.agents/skills/architecture-partner/SKILL.md)，在同一问题上下文中完成方案与 Issue。

- 工作分支与主题由用户指定；本轮为已有 `alpha`，不自行创建探索分支、不合并 main。
- 先读 [KB 入口](kb/README.md)，再按需读取当前架构、Issue 及其评论；旧记忆不覆盖当前来源。
- 每轮先读[loom-oi整体理念](docs/concept/loom-oi.md)，按[项目收束规则](docs/sdd/README.md#project-convergence)从项目承诺推导能力/契约，再研究路径、机制和部署；结束时反向检查这些机制能否共同兑现承诺，并将局部结论收回当前整体方案。不能以Issue分工、文件组织或组件拼接代替产品设计。
- 先建立端到端架构和证据/依赖链，再整理专项；旧 Issue 结构和旧 AI 推荐不作需求证据。主动提出具体机制、实质不同的替代方案、推荐及反例。人类负责接受方案，不要求人类先给出方案。
- 讨论交付先展开应用方案：原始场景→具体部署/输入输出→正常与失败链→取舍。Issue正文必须能独立评审，不能用治理规则、来源表、链接数量或格式测试代替内容；参照[失效案例与内容评审](.agents/skills/discussion-sdd/references/content-review.md)。
- 每轮讨论蒸馏、分析和融合都必须创建或更新相关Issue供开发者确认，含保持原方案/无变化结论；优先复用活动单，保存DISC-ID、来源、具体方案与确认范围。不得只改文档就宣称完成确认提交。
- 每次处理用户Issue评论，更新正文和受影响产物后，还须按[评论回复规范](docs/issue-management.md#comment-replies)在原Issue发布回应并回读；逐条关联已处理的原评论，说明改动、验证/限制和剩余确认点。仅改正文或只在聊天汇报不算完成回复。
- Issue与文档维护遵循[结构规范](docs/structure.md)和[六节Issue规范](docs/issue-management.md)：当前正文原位整合、历史归Git、索引只导航、关系写类型与理由。运行`python tools/check_structure.py`；不以结构通过代替内容评审。
- 按[Discussion SDD](.agents/skills/discussion-sdd/SKILL.md)和[SDD规范](docs/sdd/README.md)维护概念→架构→模块→变更/任务/验证链；先运行`python tools/check_sdd.py docs/changes/<DISC-ID>.json --phase review`。产品实施还需核对最新开发者确认、适用需求范围与实验门槛，草案发布不等于采用。
- 不擅自关闭、分配负责人或修改成员任务；明确授权的治理/文档编辑可以直接完成，新产品取舍保持待确认。
- 区分事实、推演、实测、AI 建议和人类决议；检查文档链接与状态。技能大改进行只读行为验证。
- 仓库技能随当前分支提交，不自动安装为个人全局技能。只有用户或适用技能明确要求时才使用子代理。

- 需要实验/数据支撑时使用 [.agents/skills/architecture-evidence-lab/SKILL.md](.agents/skills/architecture-evidence-lab/SKILL.md)；按真实隔离能力执行，保留反证、失败、原始数据与版本。
- 每轮按 [产物联动表](docs/architecture-artifacts.md) 同步当前架构图、正文、接口、Issue、KB与验收；局部机制实验不能证明整个产品。
- 此后所有新建或更新图示的标题、节点、连线、图例和状态说明统一使用中文；仅保留OIS/OIC等组件、产品/协议名称与必要代码标识，通用动作和职责不得继续用英文。渲染后检查中文字形和布局。
- 后续绘图优先检查并使用可用且适合任务的 ImageGen 等专业渠道，提升视觉效果；按[绘图渠道与视觉标准](docs/architecture-artifacts.md#visual-policy)选择展示图和精确技术图的制作方式，保留语义校对，不把一次替代方案固化为长期默认。

- 每轮实验室运行/重跑/复核后，自动完成[综合分析闭环](.agents/skills/architecture-evidence-lab/references/post-round.md)，再交付；读取[kb/conclusions](kb/conclusions.md)，执行`python experiments/check_iterations.py`检查未分析run。事实、推荐与人类采用分别记录。

- 实现达到阶段节点时读取[实验阻挡墙](experiments/GATES.md)，运行`python experiments/check_gates.py --stage <阶段>`；未通过不得进入依赖阶段，不能用文档检查通过替代实验通过。
