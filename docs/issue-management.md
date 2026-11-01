# 架构与 Issue 统一管理规范

v2.0 · 2026-11-02。执行流程以[统一 prompt](prompts/architecture-partner.md)为准；本文件定义项目字段、状态和维护机制。替代 v1 的“研究单 + 独立候选决策”组织方式。

## 1. 一个上下文完成求解

同一问题的原意、方案、推演、证据和决策进入同一 Issue。先形成有机制、有取舍的可评审方案，再组织问题；如果是必须立即记录但暂时无解的风险，写明已尝试方案和阻碍，不用空候选填充。

已授权主题内，成熟问题与方案可以直接更新已有 Issue 或建单。最终选择 Pending 不阻止设计与建单；新主题、扩大产品方向、尚不成熟的构想才先列候选。当前 D1–D6 已归入 #2/#3/#4/#7，无需再单独建六张决策单。

先查 open/closed Issues、评论、模板和规范；同一目标优先原位更新，保留实质历史及来源。未经授权不关闭、重开、分配负责人、改成员任务或合并主线。GitHub Issues 是仓库级资源，正文写明 Topic=alpha。

## 2. 方案型 Issue 的最小结构

使用[问题与方案模板](../.github/ISSUE_TEMPLATE/problem.md)，探索价值假设用[探索/决策模板](../.github/ISSUE_TEMPLATE/exploration-decision.md)。

| 部分 | 必须使读者知道 |
| --- | --- |
| Context / Current Understanding | 原始意图、已有事实、版本、约束，哪些只是 AI 推演 |
| Problem / Goal / Scope | 具体缺口、要达到的行为及边界 |
| Proposed Solutions | 推荐机制的职责、交互、状态、失败处理；实质不同备选及代价 |
| Recommendation | 为什么推荐，牺牲什么，什么证据会推翻它 |
| Walkthrough / Validation | 本轮已完成的正常/反例推演；已实测与未知；最小验证及期望结果 |
| Decision Needed | 人类需要选择的具体取舍，不是“是否继续做调研” |
| Acceptance Criteria | 分别验收设计、实验、决策、实施，不能互相替代 |
| Assessment / Relations | 分值理由、依赖、关联架构/证据/决策及变更历史 |

Issue 内提供足够独立讨论的实质方案，不只是链接。详细机制以 `docs/proposals/` 的对应方案为准；重要摘要同步更新，不维护两份同等地位的完整规范。工作状态与讨论以 GitHub 当前正文/评论为准。

## 3. 分类与标签映射

| Type | 使用范围 | 标签映射 |
| --- | --- | --- |
| Product | 场景、体验、价值和范围 | `type:product` |
| Architecture | 对象、职责、边界和交互 | `type:architecture` |
| Technical | 协议、状态、算法与性能设计 | `type:technical` |
| Exploration | 有价值假设、反证及停止条件的新构想 | `type:exploration` |
| Decision | 充分方案已展开，需要明确人类取舍 | `type:decision` |
| Risk | 已发生缺陷或有触发/后果的潜在失效 | `type:risk` |
| Implementation | 依赖已确认、范围已授权的具体执行增量 | `type:implementation` |
| Governance | 工作方法、知识、模板与协作规范 | `type:governance` |

选一个主要类型，其他维度正文表达，不为分类齐全造单。辅助标签仅建议 topic、priority、status。

当前连接器无法列出/创建标签，前轮浏览器未登录该私有仓库；**标签未配置，不能断言仓库无既有标签**。正文 Type/Topic/Priority/Status 是当前生效分类。以后有管理入口时先查并复用，不覆盖已有体系。

## 4. 三种状态分别记录

| 维度 | 值与含义 |
| --- | --- |
| Work status | Triage=待界定；Designing=实际设计中；Review=方案可评审；Validating=实验中；Blocked=注明具体阻塞；Implementing=已获准实施；Resolved=用户确认并授权关闭；Deferred=用户暂缓 |
| Solution maturity | Sketch=初稿；Walkthrough=正常/反例纸面推演；Experiment=已有运行实验；Implemented=已有实现且注明验证范围 |
| Decision status | Pending / Accepted / Rejected / Deferred；Accepted 必须附人类确认范围、来源、日期 |

`Review + Walkthrough + Pending` 是有效组合：人类现在就能评审机制，但运行证据仍有缺口。没有“字段填齐即转 Resolved”的规则。新方向 Candidate 在文档中记录，获准进入主题后才建单；不是所有 Pending 的别名。

GitHub open/closed 与工作状态区分；方案被接受也不能自动关闭。D1–D6 保留为历史决策 ID，方案编号 S2–S9 与 GitHub #2–#9 一一对应；旧 ALPHA-01–08 仅作兼容追溯。

## 5. 轻量评分

I/U/N/E 使用1–5，C使用1–3；注明日期、评估者和理由，不确定写 TBD。本轮均为 AI 初评，非人员安排或最终选择。

| 指标 | 1 | 2 | 3 | 4 | 5 |
| --- | --- | --- | --- | --- | --- |
| Impact | 局部措辞 | 单个次要场景 | 单模块/主要场景 | 跨模块/多接入 | 核心边界、权限或副作用正确性 |
| Urgency | 以后可选 | 下一阶段 | 当前阶段后段 | 契约/验收前 | 当前核心范围/模型讨论前置 |
| Uncertainty | 已验证 | 少数边界未知 | 机制较明确但待实验 | 关键假设未测 | 多方案与机制均不清楚 |
| Effort | ≤0.5人日 | >0.5–1人日 | >1–3人日 | >3–7人日 | >7人日，需检查边界 |

C=1：主要推测；C=2：有可追溯原文/源码支持，具体方案未实测；C=3：有直接可重复的针对性运行证据。降低 N 不等于提升 C：机制更清楚了，仍可能未验证。

参考分 `S=2I+U+N`（4–20）帮助排列讨论，不自动决定优先级或选型。E/C 单列，不用复杂公式掩盖未知。E 明确估计后续设计验证或实现的哪一部分；本轮 S2–S9 的 E 仅指后续设计/实验验证，不含完整产品实现与采购。

P0=有证据的当前严重事故；P1=当前核心边界/正确性/契约前置；P2=有明确价值但可后续收敛；P3=认可但不紧迫。当前没有 P0，不因为存在潜在风险就标紧急。

## 6. 关系、维护与知识沉淀

Tracks 用父项清单；Depends on 只用于真正强前置；Related/Informs 用于提供证据。不要让研究与决策互相等待：方案可比较多候选，生产实现才依赖明确选择。

当前只用 Markdown 双向链接，未建立原生 Sub-issues/Projects。模板仍只在 alpha，默认分支 New Issue 页面尚未启用。工具能力限制不妨碍先用正文、关系和方案完整工作。

每轮按“读现状→求解→检查反例→改 Issue→改提案/架构→更新 KB”推进。知识条目的来源、状态、核对和替代规则见 [KB](../kb/README.md)。人类只确认一部分时只提升该部分；不覆盖反对意见或其他成员评论。

阶段评审检查重复、冲突、阻塞和来源失效；长期无进展提示人工判断，不自动关闭/转派。无用户请求不创建定时任务。技能大改检查结构与只读真实任务表现，将结果记录到 [经验](../kb/lessons.md)。
