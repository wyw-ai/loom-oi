# 决策台账

2026-11-02。每条只覆盖确认的内容；采用、实验、实现、关闭分别记录。

## 已明确的工作授权

| ID | 范围 | 来源与边界 |
| --- | --- | --- |
| W1 | 融合 Architecture Shaping 与 Issue Engineering；主动求解；仓库 skill/KB 持续建设；优化现有单 | 当前会话用户指令；不授权最终产品选型、关闭、分派或合并 main |
| W2 | 用户指定 alpha，探索结束与主线合并由用户决定 | [讨论§8](../docs/alpha-discussion.md#8-回归主线的条件) |
| W3 | 跟进已回复项，对未回复单按整体架构、证据与依赖优化整合 | 2026-11-02 当前会话；本轮 #1–#7 未回复，#8/#9 各一条用户回复 |

## 人类确认记录

| ID / 状态 | 选择及准确范围 | 原文来源/时间 | 影响与未选项 |
| --- | --- | --- | --- |
| H1 / Accepted（边界） | 暂不做 Loom 接入；Loom 不是 OI 必需依赖；未来由 Loom 端发起适配 | [plumeink 在 #9 的回复](https://github.com/wyw-ai/loom-oi/issues/9#issuecomment-6561327484)，2026-11-01 UTC，11-02 +08:00 核对 | 未来集成 Deferred；撤掉 #9 及架构中的 bridge 当前路线；#3/#2/#8 不得以 Loom API/对象/语言为强前置；独立验收实现仍待后续 |
| H2 / Accepted（研究要求） | 必须结合功能细化 Go/Rust 对比，Rust 不是已定答案 | [plumeink 在 #8 的回复](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6561546252)，2026-11-01 UTC，11-02 +08:00 核对 | #8 已补比较；**语言选择仍 Pending**，没有授权完整双实现或指定 Go |

### v4新增确认（均是要求/授权，不是采用全部方案）

| ID | 准确范围及来源 | 落实/未确认项 |
| --- | --- | --- |
| H3 | [#3回复](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6562173600)：调研成立条件并创建产品承诺/消费者前置单 | 已建#10；A/B/C仍Pending，首批消费者未确认 |
| H4 | [#2回复](https://github.com/wyw-ai/loom-oi/issues/2#issuecomment-6562416689)：扩深业务场景，含探针/监控/不同节点 | 已补八类场景及LAB-001；对象/探针规格未获批 |
| H5 | [#8回复](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6562293923)：本项目推荐选型，考虑可用性/维护/扩展灵活性 | AI推荐Go统一核心；语言采用仍Pending，Go/Rust实测缺失 |
| H6 | [#9回复](https://github.com/wyw-ai/loom-oi/issues/9#issuecomment-6562311062)：允许关闭，边界已下结论 | 已closed/completed；独立闭环和故障实验归#10/#3/#4/#8，未标测试通过 |
| W4 | 当前会话要求新建隔离实验室skill，每轮同步架构图等最终产物 | 仓库skill/runner/证据与产物联动已建设；只做受信本地实验，不自动扩大生产权限 |

H1 替代 v2 的“先脚本、再 Loom bridge”AI路线；后续H6明确授权关闭#9，已执行。H2 不代表用户拒绝 Rust。两条均未批准整体候选架构。

## 未决架构取舍

| ID/范围 | 归属 | 当前可评审建议 | 前提与决策状态 |
| --- | --- | --- | --- |
| D1/D5 消费边界 | #3 / [S3](../docs/proposals/alpha-solution-design.md#s3) | H7下按当前案例收束v0.6 A/R推荐；#10持续反馈，B/C按质变条件重评 | 首批消费者可用原协议是假设；Pending |
| D2 对象 | #2 / [S2](../docs/proposals/alpha-solution-design.md#s2) | 区分接入登记与暴露项，连接代数/配置版本分离；正式词典待定 | 同机多服务和重连反例驱动；Pending |
| D4/D6 生命周期/资源 | #2，#7 支撑 / [S7](../docs/proposals/alpha-solution-design.md#s7) | 已有服务管理者启停，真实资源所有者协调；OI 有界连接 | 托管或跨入口隔离要求会扩范围；Pending |
| D3 连接路径 | #4 / [S4](../docs/proposals/alpha-solution-design.md#s4) | 出站可信中继作为首个验证路径 | 可出站且接受中心成本/故障/信任是假设；Pending |
| 通道授权与撤销 | #5 并入 #4 / [S5](../docs/proposals/alpha-solution-design.md#s5) | R 在线审批；D/H 才细化离线访问依据 | 没有已批准 TTL 或即时撤销承诺；Pending |
| 故障与重试 | #6 并入 #4 / [S6](../docs/proposals/alpha-solution-design.md#s6) | 内核只报告有证据的通道状态，不隐式重放 | B/C 才扩适配结果语义；Pending |
| 语言与预算 | #8 / [比较](../docs/proposals/oic-language-tradeoffs.md) | AI推荐Go统一核心；Rust按资源/嵌入/维护证据保留翻转条件 | 已有选型建议，团队/预算/负载与语言实测仍缺；采用Pending |
| 独立闭环验收 | #10/#3/#4；#9仅历史边界 / [S9](../docs/proposals/alpha-solution-design.md#s9) | 普通脚本+只读示例服务+故障注入 | H1已定且#9关闭；LAB-001/002只是机制，独立OI闭环未运行 |

收到新评论后先核对确认范围，再沿 [影响链](../docs/proposals/alpha-solution-design.md#dependencies) 更新对应推荐与反例；尚未确认的方案不能因被引用而变成 Accepted。保持历史链接，不覆盖用户评论。

本轮局部机制结果及独立审阅见[实验报告](../docs/evidence/alpha-lab-round1.md)。实验支持窄技术判断，不构成人类设计决议。

## v5方法授权与技术结论

W5：2026-11-02当前会话用户要求结合实验可信数据与KB尝试得出结论，并在每轮实验室操作后自动完成。已落实为同任务内强制分析、方案收敛、图文/Issue/KB回写与覆盖检查；不等于批准无人值守生产任务或全部架构方案。

技术结论单列于[conclusions](conclusions.md)：C01/C03的反例成立，C02局部调度观测成立；C04/C05为设计推论，C06语言推荐未获新实证。H1–H6和最终采用状态保持原范围，避免把技术事实也写成等待人类判定真伪，或把证据成立写成人类采用。

## v0.6持续案例授权与收束范围

H7 / Accepted（工作方式）：[#10用户回复](https://github.com/wyw-ai/loom-oi/issues/10#issuecomment-6564017519)，2026-11-02 +08:00，要求保持待接受案例，持续纳入新案例，按当前已收纳案例分析建议，量变引起质变时启动评审。它替代“必须先完成#10才能提出当前方案”的读法，不代表接受A/R/Go或确认全部案例已真实接入。

W6：当前会话要求沿路径链条收束所有建议，交付新架构图和架构文档；已形成[alpha v0.6](../docs/architecture/alpha-v0.6.md)。B01–B09为本版AI推荐基线，包含A/R/Go、协议/存储/主备新细化；产品最终采用仍Pending，main不合并、Issue不擅自关闭。
