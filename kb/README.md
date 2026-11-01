# 项目知识库

本库帮助架构与 Issue 工作保持连续，不替代源码、原始讨论、GitHub 当前状态或用户决策。

| 入口 | 内容与权威范围 |
| --- | --- |
| [证据台账](evidence.md) | 已核对的事实、团队意图、来源版本与局限 |
| [技术结论](conclusions.md) | Supported事实、Derived建议和未知的索引；不是采用决议 |
| [决策台账](decisions.md) | 人类决定及 Pending 索引；不把 AI 推荐登记为 Accepted |
| [工作经验](lessons.md) | 本项目已出现的协作缺陷、修正和验证结果 |
| [统一 prompt](../docs/prompts/architecture-partner.md) | 完整工作方法；skill 引用该文件 |
| [当前架构 v0.7](../docs/architecture/alpha-v0.7.md) / [图册](../docs/architecture/diagrams/README.md) | 已收束的推荐实现方案；采用与实现状态独立记录 |
| [alpha 方案](../docs/proposals/alpha-solution-design.md) | AI 具体提案及纸面推演，非已确认规范 |
| [实验室](../experiments/README.md) / [证据报告](../docs/evidence/alpha-lab-round1.md) | 原始数据、源版本、隔离等级与窄结论；不替代产品实现 |
| [产物联动](../docs/architecture-artifacts.md) | 每轮检查图、正文、Issue、契约和验收 |
| [当前规划](../docs/alpha-issues.md) | Issue 与方案/决策的映射，实时状态以 GitHub 为准 |

## 阅读与写入规则

- 开始工作先读相关条目，按引用回到原件，不要求每次加载全部历史。源码和外部规范使用固定版本；当前状态重新读取。
- 类型使用 `Fact`、`Intent`、`User decision`、`Proposal`、`Hypothesis`、`Superseded`。事实与产品选择是两条线：人类选择不能改变技术事实，技术事实也不能替人类选产品。
- 条目记录 ID、结论、范围、来源/版本、最后核对时间和局限。重要改正记录 `supersedes`；过期条目不继续作为当前结论。
- Proposal 的详细正文只存在提案文件；KB 只索引，不能因为被引用多次而升级为 Fact/User decision。工作流指令获准不等于其生成的架构方案获准。
- Issue 保存足够独立讨论的方案摘要、当前进展和决策点；详细机制以提案文件为准。两者冲突时回源核对更新者与证据，不能仅凭位置决定谁“永远正确”。
- 收到人类明确选择时，记录确认的准确范围和来源，更新受影响架构及 Issue。关闭/合并仍分别遵守授权。
- 每轮结束只沉淀可复用内容，检查引用、状态与影响链；没有新知识时不为填 KB 造条目。不建立自动定时任务。

## 本轮版本

v4（2026-11-02）：H3/H4/H5新增研究与质量要求，#10为产品前置；H6授权关闭#9。仓库实验室已运行探针/响应丢失机制fixture并保存纠错前后的原始记录；Go/Rust、真实客户端、生产HA仍缺验证。推荐Go统一核心是AI提案，未获人类采用。每轮按产物联动表同步图文，不以局部数据证明整套架构。

v5：已有证据经综合分析收敛为C01–C06。每轮实验室操作后自动更新结论及产物；没有新增样本时明确记录复核，保持技术判断与人类采用分离。

v0.7：[当前架构与实现方案](../docs/architecture/alpha-v0.7.md)及[图册](../docs/architecture/diagrams/README.md)为现行评审入口。H7持续案例规则替代等待齐全；C07记录明确基线及新细化的证据边界。

W7收尾：活跃Issue仅#3/#4/#8；其他已收束单按用户授权归档，持续案例迁入台账。六张当前架构图全部ImageGen重绘，提示词/哈希/逐图校对随仓库保存。

W8条件盘点：用户要求新建[实验条件汇总 #11](https://github.com/wyw-ai/loom-oi/issues/11)，承接LAB-003–006，当前3个决策入口加1个条件入口。[只读环境检查](../docs/evidence/lab-readiness-2026-11-02.json)未产生新实验结论。H8（[#4回复](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6564934039)）接受K3推荐、要求默认PG及SQLite后端与规模优化；已纳入LAB-006前置，完整数据库契约与架构图文修订仍待#4跟进，不视为HA承诺已确认。

v0.7当前：H8/H9/H10已进入[新架构](../docs/architecture/alpha-v0.7.md)、[六张矢量技术图](../docs/architecture/diagrams/v0.7/README.md)、#3/#4/#8/#11。K1业务实例化边界/R/双数据库要求已确认；K2、Go与具体存储/恢复细节待评审。阶段门槛均未通过。[本轮分析](../docs/evidence/alpha-feedback-002.md)包含旧证据只读复核、设计推导与技能验证，不新增产品样本。
