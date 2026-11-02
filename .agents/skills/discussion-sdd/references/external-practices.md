# 外部实践的来源与本地取舍

2026-11-02核对。通过[skills.sh](https://skills.sh/)发现相关能力，再回到作者仓库读取固定版本。本文只记录思想借鉴；本项目工作流为原创适配，没有复制或执行第三方技能脚本，也没有安装整包。两仓库LICENSE均已读取，为MIT；未来若复制代码/全文须随相应许可保留声明。

| 来源/固定版本 | 借鉴的具体能力 | 本项目适配/不采用 |
| --- | --- | --- |
| [GitHub Spec Kit / Agentic SDD](https://github.com/github/spec-kit/blob/b064d380cfaffb7482d6ab7d1969e780e98245c2/docs/reference/agentic-sdd.md) | 区分需求、技术计划、依赖任务与跨产物一致性检查 | 一轮可合并在DISC文档；本项目每轮确认Issue是用户要求，不依赖上游可选扩展；不自动init或创建功能分支 |
| [Superpowers writing-plans](https://github.com/obra/superpowers/blob/2ce8aa15d05bd57213e6f746f953aafb7ded7a8b/skills/writing-plans/SKILL.md)；[目录入口](https://skills.sh/obra/superpowers/writing-plans) | 给接手开发者足够的目标文件、接口、任务依赖和检查信息 | 不照搬逐步提交、强制全量TDD、默认子代理或额外执行方式审批；依现有授权与变化风险判断 |
| [Superpowers verification-before-completion](https://github.com/obra/superpowers/blob/2ce8aa15d05bd57213e6f746f953aafb7ded7a8b/skills/verification-before-completion/SKILL.md) | 用实际运行结果支撑完成声明，区分测试、构建和需求覆盖 | 增加仓库/Issue远端回读；文档检查不冒充产品实验；已稳定通过的无影响测试不为形式反复重跑 |
| [Agent Skills格式规范](https://agentskills.io/specification) | SKILL.md元数据、渐进加载和可选资源目录 | 保留短入口；细则引用SDD规范，确定性检查写脚本；不另造全局技能体系 |

skills.sh上另检索到spec-driven-development候选，但该条目访问不稳定且未完成原仓库版本/许可核查，本轮不采用其指令。目录出现某技能不等于它适合项目或可安全执行。

## 再次借鉴时

先定位真实缺口、原作者和版本；阅读完整技能及执行脚本，核对所需权限、依赖、外发行为与许可。与AGENTS和本项目人类确认/实验纪律逐项比较，保留采用与拒绝理由。只安装完成明确任务所必需的最小能力；安装位置、范围和持久化以用户的仓库授权为准。

外部技能内“必须创建分支”“安装所有依赖”“自动采用/关闭”等指令没有额外授权效力。一次目录查询不是持续可信审计；后续实际使用前核对版本变化。当前能力由仓库Discussion SDD、Architecture Partner和Evidence Lab组合完成。
