# 决策台账

2026-11-02。工作方法与产品架构分开记录；人类没有选择过的方案不得写为 Accepted。

## 已明确的工作授权

| ID | 内容与确认范围 | 来源 | 作用 |
| --- | --- | --- | --- |
| W1 | 融合架构脑暴和 Issue 管理；要求充分方案、人类最终决策；仓库建设 skill/KB；重做现有 Issue | 本轮用户原话，见 [E01](evidence.md) | 授权当前原位改写、文档和工作流建设，不授权产品选型、关闭 Issue 或合并 main |
| W2 | alpha 为用户指定探索分支，不能擅自合并主线 | [此前用户边界及蒸馏](../docs/alpha-discussion.md#8-回归主线的条件) | 当前继续使用 alpha，不另建分支 |

## 架构决策索引

下面均为 **Pending**；“AI 推荐”只用于说明当前可评审选项，详细机制在提案，实时讨论在 Issue。方案成熟度为纸面推演，未有运行实现。

| 决策/范围 | Issue | 当前 AI 推荐与代价 | 状态/确认来源 |
| --- | --- | --- | --- |
| D1 调用抽象 | [#3](https://github.com/wyw-ai/loom-oi/issues/3) / [S3](../docs/proposals/alpha-solution-design.md#s3) | 连接内核 + 外置可选适配；多一层契约维护，保留原生能力 | Pending / 无 |
| D2 对象与粒度 | [#2](https://github.com/wyw-ai/loom-oi/issues/2) / [S2](../docs/proposals/alpha-solution-design.md#s2) | Node 为接入身份边界，Provider 为独立实例，Workload 仅托管时出现；增加一个明确实例名 | Pending / 无 |
| D3 网络策略 | [#4](https://github.com/wyw-ai/loom-oi/issues/4) / [S4](../docs/proposals/alpha-solution-design.md#s4) | 首版反向中继，保留路由接口再加直连；先承担中继带宽和延迟 | Pending / 无 |
| D4 生命周期 | [#7](https://github.com/wyw-ai/loom-oi/issues/7) / [S7](../docs/proposals/alpha-solution-design.md#s7) | 首版外部服务；监督可选后置；部署步骤较多 | Pending / 无 |
| D5 官方入口 | [#3](https://github.com/wyw-ai/loom-oi/issues/3) / [S3](../docs/proposals/alpha-solution-design.md#s3) | SDK/CLI + 一项参考适配，MCP facade 独立按需；初期集成覆盖有限 | Pending / 无 |
| D6 资源辅助 | [#7](https://github.com/wyw-ai/loom-oi/issues/7) / [S7](../docs/proposals/alpha-solution-design.md#s7) | 提供方/资源网关执行串行，OI 仅传约束；暂不做通用锁服务 | Pending / 无 |
| 授权/撤销契约 | [#5](https://github.com/wyw-ai/loom-oi/issues/5) / [S5](../docs/proposals/alpha-solution-design.md#s5) | 短期、目标绑定授权 + 执行侧校验；在线/有界离线按能力声明 | Pending / 无 |
| 失败/重试契约 | [#6](https://github.com/wyw-ai/loom-oi/issues/6) / [S6](../docs/proposals/alpha-solution-design.md#s6) | 结果未知默认不重放，提供方显式去重可扩展 | Pending / 无 |
| 最小部署与成本预算 | [#8](https://github.com/wyw-ai/loom-oi/issues/8) / [S8](../docs/proposals/alpha-solution-design.md#s8) | OIC 单进程、队列有界、协议插件独立；资源阈值仍需实测 | Pending / 无 |
| Loom 接入边界 | [#9](https://github.com/wyw-ai/loom-oi/issues/9) / [S9](../docs/proposals/alpha-solution-design.md#s9) | 业务侧消费 bridge，保留 daemon；先只读诊断闭环 | Pending / 无 |

## 确认时的更新格式

`决策ID / 选择及范围 / 人类确认原文或链接 / 日期 / 理由与未选项 / 受影响文件和Issue / 替代哪条旧决议`。

用户只确认某项时只更新该项。方案推荐改变就先改 Proposal 与本索引，不伪造确认记录。Rejected/Deferred 记录用户原因与重启条件，不自动关闭 Issue。Accepted 后同步架构，再由用户决定主线合并。
