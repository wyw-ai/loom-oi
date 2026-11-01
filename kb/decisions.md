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

H1 替代 v2 的“先脚本、再 Loom bridge”AI 路线，不是关闭 #9。H2 不代表用户拒绝 Rust。两条均未批准整体候选架构。

## 未决架构取舍

| ID/范围 | 归属 | 当前可评审建议 | 前提与决策状态 |
| --- | --- | --- | --- |
| D1/D5 消费边界 | #3 / [S3](../docs/proposals/alpha-solution-design.md#s3) | 先原生能力目录+通道；统一/可选调用为备选 | 首批消费者可用原协议是假设；Pending |
| D2 对象 | #2 / [S2](../docs/proposals/alpha-solution-design.md#s2) | 区分接入登记与暴露项，连接代数/配置版本分离；正式词典待定 | 同机多服务和重连反例驱动；Pending |
| D4/D6 生命周期/资源 | #2，#7 支撑 / [S7](../docs/proposals/alpha-solution-design.md#s7) | 已有服务管理者启停，真实资源所有者协调；OI 有界连接 | 托管或跨入口隔离要求会扩范围；Pending |
| D3 连接路径 | #4 / [S4](../docs/proposals/alpha-solution-design.md#s4) | 出站可信中继作为首个验证路径 | 可出站且接受中心成本/故障/信任是假设；Pending |
| 通道授权与撤销 | #5 并入 #4 / [S5](../docs/proposals/alpha-solution-design.md#s5) | R 在线审批；D/H 才细化离线访问依据 | 没有已批准 TTL 或即时撤销承诺；Pending |
| 故障与重试 | #6 并入 #4 / [S6](../docs/proposals/alpha-solution-design.md#s6) | 内核只报告有证据的通道状态，不隐式重放 | B/C 才扩适配结果语义；Pending |
| 语言与预算 | #8 / [比较](../docs/proposals/oic-language-tradeoffs.md) | 单语言目标，Go/Rust 同功能比较；有条件先 Go 骨架 | 非选型，团队/预算/负载未知；Pending |
| 独立闭环验收 | #9 / [S9](../docs/proposals/alpha-solution-design.md#s9) | 普通脚本+只读示例服务+故障注入 | H1 已定，具体试验规格 Pending/未运行 |

收到新评论后先核对确认范围，再沿 [影响链](../docs/proposals/alpha-solution-design.md#dependencies) 更新对应推荐与反例；尚未确认的方案不能因被引用而变成 Accepted。保持历史链接，不覆盖用户评论。
