# 历史架构入口（兼容导航）

本页仅保留旧锚点。当前共同契约在[架构v0.7](architecture/alpha-v0.7.md)，完整旧稿由Git保存；不要继续在此维护方案摘要。

## 1. 产品目标与范围

OI服务于受控能力接入；不管理Agent Session，不依赖Loom，未来Loom侧适配。[本版收束选择](architecture/alpha-v0.7.md#choices)。

## 2. 职责与总体结构

消费者/CLI → OIS目录授权与有界中继 → OIC本地约束 → 独立能力/真实资源。[职责与完整链条](architecture/alpha-v0.7.md#overview)。

## 3. 对象模型与命名

Access/Exposure、任期epoch、连接generation、配置revision、观测sample_seq分别变化。[对象表](architecture/alpha-v0.7.md#objects)。

## 4. 通信与适配边界

HTTPS JSON控制API、独立WSS控制连接及每通道双数据腿；原生协议/业务凭证保持明确所有者。[接入](architecture/alpha-v0.7.md#registration)与[授权建连](architecture/alpha-v0.7.md#open)。

## 5. 状态与隔离

执行与有效观测正交，Unknown不等于NotReady；best_effort缺省、require_ready可配置仍是提案；有效NotReady均拒绝，Unknown仅best_effort可尝试。[状态图和时效/资源规则](architecture/alpha-v0.7.md#observation)。

## 6. 候选契约

Discover/Open/Attach/Close及版本/权限/额度校验形成同一链。[API草图](architecture/alpha-v0.7.md#open)。并未发布正式稳定协议。

## 7. 调用与故障处理

不跨失败边界隐式重放；新通道重新鉴权，业务恢复归语义层。[错误、撤销与故障](architecture/alpha-v0.7.md#failure)。

## 8. 部署与场景映射

OIC不运行数据库；OIS默认推荐PG，无PG部署显式选择SQLite；共享契约，SQLite首版单实例，PG主备另受G6约束。[部署与恢复图](architecture/alpha-v0.7.md#deployment)、[八类案例覆盖](architecture/alpha-v0.7.md#coverage)。

## 9. 与 Loom 及已有服务的集成

H1/H6保持：暂不接Loom，未来Loom端适配，#9关闭范围不变。外部协议与服务经明确目标接入；不承诺MCP/stdio已兼容。[扩展条件](architecture/alpha-v0.7.md#coverage)。

## 10. 验证顺序与验收建议

P0/G0 → P1/G1 → P2/G2–G4；再分别进入P3资源/恢复与P4兼容，未过门槛阻止依赖阶段。[实施路径与出口](architecture/alpha-v0.7.md#delivery)。

## 11. 本轮变更与继续讨论

H7的持续案例机制继续有效；最新指令要求仅保留关键决策，#10已迁入[案例台账](proposals/consumer-scenarios.md#7-持续案例台账)，活跃入口为#3/#4/#8；#11持续跟进实验条件与阶段阻挡墙。[证据和采用边界](architecture/alpha-v0.7.md#review)。

### 11.1 方案、Issue 与知识联动

完整设计只维护一份现行文档；候选分析保留出处，Issue承载讨论和取舍。按[产物联动](architecture-artifacts.md)同步结构定义、图像及其源文件/生成记录、正文、Issue与KB，实验后自动分析流程保持有效。

[当前架构](architecture/alpha-v0.7.md)
