# alpha：方案与 Issue 规划 v2

2026-11-02。依据用户要求，把同一主题的脑暴、方案和 Issue 合并。已将 #1–#9 原位改写；不增加另一套问题编号，不关闭/分派，不合并 main。旧版保留在 [8abc35f](https://github.com/wyw-ai/loom-oi/tree/8abc35f25866f4193543d6d0c2390df5ac3267a2)。

入口：[总索引 #1](https://github.com/wyw-ai/loom-oi/issues/1) · [完整方案](proposals/alpha-solution-design.md) · [统一 prompt](prompts/architecture-partner.md) · [项目 skill](../.agents/skills/architecture-partner/SKILL.md) · [KB](../kb/README.md)。

## 1. 本次规划的实际变化

旧版主要要求未来“比较/验证/梳理”，候选 D1–D6 与 Issue 各存半份上下文。v2 已为每项展开推荐机制、三个实质候选、正常/反例推演、失败处理、待验证项与人类取舍。验证成为解决方案的一部分，问题不再只等待下一次研究。

按三组组织：核心消费与接入模型（#2/#3/#4）；跨层正确性契约（#5/#6/#7）；最小交付与消费场景（#8/#9）。现有8个主题都有独立设计目标，保留编号，但目标、方案与验收已重写。没有为每个实验再建子Issue。

## 2. 当前方案图谱

以下是重规划快照：Work=Review，Maturity=Walkthrough，Decision=Pending，全部仍 open。细节与进展以 GitHub 为准；AI 推荐不是人类决定。

| Issue / 类型 | 当前 AI 推荐 | 主要取舍 | 方案 | Priority / I-U-N-E-C / S |
| --- | --- | --- | --- | --- |
| [#2](https://github.com/wyw-ai/loom-oi/issues/2) · Architecture | 接入 Node + 独立 Provider + 可选 Workload | 保留层级但增加明确实例术语 | [S2](proposals/alpha-solution-design.md#s2) | P1 / 5-5-4-2-2 / 19 |
| [#3](https://github.com/wyw-ai/loom-oi/issues/3) · Decision | 连接内核 + 可选适配，SDK/CLI先行 | 便利性与协议维护面 | [S3](proposals/alpha-solution-design.md#s3) | P1 / 5-5-4-3-2 / 19 |
| [#4](https://github.com/wyw-ai/loom-oi/issues/4) · Architecture | 首版反向中继，路由接口保留直连 | 交付面与中继带宽/延迟 | [S4](proposals/alpha-solution-design.md#s4) | P1 / 4-4-4-3-2 / 16 |
| [#5](https://github.com/wyw-ai/loom-oi/issues/5) · Technical | 目标绑定短期授权，在执行侧落实 | 撤销窗口与断联可用性 | [S5](proposals/alpha-solution-design.md#s5) | P1 / 5-4-4-3-2 / 18 |
| [#6](https://github.com/wyw-ai/loom-oi/issues/6) · Technical | 结果未知不自动重放，提供方显式去重 | 正确性与自动恢复便利 | [S6](proposals/alpha-solution-design.md#s6) | P1 / 5-4-3-3-2 / 17 |
| [#7](https://github.com/wyw-ai/loom-oi/issues/7) · Architecture | 外部服务 + 资源侧串行，监督可选 | 接入方工作量与核心范围 | [S7](proposals/alpha-solution-design.md#s7) | P1 / 5-4-4-3-2 / 18 |
| [#8](https://github.com/wyw-ai/loom-oi/issues/8) · Technical | 独立可裁剪 OIC，全局队列/缓存有界 | 部署便利与资源可控 | [S8](proposals/alpha-solution-design.md#s8) | P2 / 4-3-4-3-2 / 15 |
| [#9](https://github.com/wyw-ai/loom-oi/issues/9) · Architecture | 脚本独立闭环→Loom消费桥接 | 先验证价值再决定复用 | [S9](proposals/alpha-solution-design.md#s9) | P2 / 4-3-3-3-2 / 14 |

## 3. 决策就在对应 Issue 内

| 原编号 | 当前承载位置 | 本轮状态 |
| --- | --- | --- |
| D1 调用抽象、D5 入口 | [#3](https://github.com/wyw-ai/loom-oi/issues/3) | 已展开方案，Pending |
| D2 对象/能力粒度 | [#2](https://github.com/wyw-ai/loom-oi/issues/2) | 已展开方案，Pending |
| D3 网络策略 | [#4](https://github.com/wyw-ai/loom-oi/issues/4) | 已展开方案，Pending |
| D4 生命周期、D6 资源辅助 | [#7](https://github.com/wyw-ai/loom-oi/issues/7) | 分开取舍，Pending |

#5/#6/#8/#9 也各自有具体契约/交付取舍，见 [决策台账](../kb/decisions.md)。不再单独维护六份完整候选卡片，也不把它们建成重复 Issue。新的主题或扩大范围构想仍按规范先列候选。

## 4. 推进依赖与工程拆分

1. 先评审 #3/#2 的推荐组合，明确首版能提供什么、消费对象是什么。可分别接受/修改，不要求一次批准整套架构。
2. #4 的网络选择与调用抽象独立；#5/#6/#7 为任一路径提供授权、失败和并发约束，不能把这几项延后成上线后修补。
3. #8 给出可控成本设计，#9 给出独立消费闭环；它们用于反驳过重或侵入业务的推荐。31个场景已完成纸面推演，网络、资源及故障实验均未运行。
4. 对应决定确认后才拆明确 Implementation 增量；当前不承诺生产实现、负责人或排期。研究/草图可继续，只有依赖具体选择的实施等待。

## 5. 旧内容如何处理

- #2–#9 原位替换研究待办式目标，保留问题来源与有效约束，补充推荐、机制和反例；旧 ALPHA-01–08 只作历史兼容。
- #1 改为统一架构方案评审总索引；之前“建了Issue/补了候选卡片”不再作为设计完成的验收。
- 原研究步骤保留在每项 Validation 内，按能否推翻当前推荐来判断价值，不丢弃有用证据工作。
- 原 D 候选选项进入方案比较与 Issue Decision Needed；决策仍 Pending。
- 八种分类继续支持，但不为凑分类建立空的 Exploration/Implementation。
- 标签和默认分支模板限制仍如实记录；分类、成熟度和状态已通过正文管理。

## 6. 如何继续讨论

指定一个 Issue 后，AI 读取其现状、关联机制与证据，直接从现有方案推进；补反例、修机制、更新推荐，不从零重复摘录。人类选择时记录准确范围并同步架构/KB；只说“继续脑暴”不意味着接受方案。主线合并与关闭仍按用户授权执行。
