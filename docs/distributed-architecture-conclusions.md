# loom-oi 当前架构入口 · alpha v0.6

2026-11-02。现行完整文档为[架构与实现方案 v0.6](architecture/alpha-v0.6.md)，配套[六视图图册](architecture/diagrams/README.md)。本文保留历史锚点作为导航，避免多份正文各自演化；v0.5的完整内容保留于[c5e2907](https://github.com/wyw-ai/loom-oi/blob/c5e29076840f6a6cc02fb5fa71ee313529b9fc77/docs/distributed-architecture-conclusions.md)。

本版按当前案例推荐A/R/Go：目录与原生通道、出站可信中继、统一Go核心、独立观测；进一步细化HTTPS/WSS、身份/票据、持久元数据和分阶段主备。**这是一份已收束的建议方案，尚无OIS/OIC产品实现，不代表人类最终采用或生产HA验证。**

![总体架构 v0.6](architecture/diagrams/01-overview.png)

## 1. 产品目标与范围

OI服务于受控能力接入；不管理Agent Session，不依赖Loom，未来Loom侧适配。[本版收束选择](architecture/alpha-v0.6.md#choices)。

## 2. 职责与总体结构

消费者/CLI → OIS目录授权与有界中继 → OIC本地约束 → 独立能力/真实资源。[职责与完整链条](architecture/alpha-v0.6.md#overview)。

## 3. 对象模型与命名

Access/Exposure、任期epoch、连接generation、配置revision、观测sample_seq分别变化。[对象表](architecture/alpha-v0.6.md#objects)。

## 4. 通信与适配边界

HTTPS JSON控制API、独立WSS控制连接及每通道双数据腿；原生协议/业务凭证保持明确所有者。[接入](architecture/alpha-v0.6.md#registration)与[授权建连](architecture/alpha-v0.6.md#open)。

## 5. 状态与隔离

执行与有效观测正交，Unknown不等于NotReady；best_effort缺省、require_ready可配置是本版建议。[状态图和时效/资源规则](architecture/alpha-v0.6.md#observation)。

## 6. 候选契约

Discover/Open/Attach/Close及版本/权限/额度校验形成同一链。[API草图](architecture/alpha-v0.6.md#open)。并未发布正式稳定协议。

## 7. 调用与故障处理

不跨失败边界隐式重放；新通道重新鉴权，业务恢复归语义层。[错误、撤销与故障](architecture/alpha-v0.6.md#failure)。

## 8. 部署与场景映射

OIC不运行数据库；OIS建议PostgreSQL元数据，先单实例闭环再验证单活主备。[部署与恢复图](architecture/alpha-v0.6.md#deployment)、[八类案例覆盖](architecture/alpha-v0.6.md#coverage)。

## 9. 与 Loom 及已有服务的集成

H1/H6保持：暂不接Loom，未来Loom端适配，#9关闭范围不变。外部协议与服务经明确目标接入；不承诺MCP/stdio已兼容。[扩展条件](architecture/alpha-v0.6.md#coverage)。

## 10. 验证顺序与验收建议

P0契约 → P1独立纵向闭环 → P2观测/失败 → P3恢复与目标部署 → P4真实兼容及条件扩展。[实施路径与出口](architecture/alpha-v0.6.md#delivery)。

## 11. 本轮变更与继续讨论

#10依据[用户新回复](https://github.com/wyw-ai/loom-oi/issues/10#issuecomment-6564017519)持续收纳案例，设计按当前材料推进，边界质变触发评审。[证据和采用边界](architecture/alpha-v0.6.md#review)。

### 11.1 方案、Issue 与知识联动

完整设计只维护一份现行文档；候选分析保留出处，Issue承载讨论和取舍。按[产物联动](architecture-artifacts.md)同步图源、PNG/SVG、正文、Issue与KB，实验后自动分析流程保持有效。
