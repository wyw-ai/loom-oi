# ADR-005：G2三组具体契约的准确采用

2026-11-04 · 已采用设计，尚未实施或运行G2。三组选择属于同一条观测→准入→发布/导出链，本页只解释采用范围；详细字段、预算及验证仍以[G2规格](../architecture/p2/g2-observation.md)为唯一契约。

## 原始确认与固定版本

本会话用户对`request_user_input_async`调用`call_536f5c10d81142ac9e5a95e96565e3aa`的item 0/1/2分别回答：

- “采用 Commit-Snapshot（推荐）”
- “采用 Probe-HTTP（推荐）”
- “采用 Export-Pull（推荐）”

完整原问题、答案和questionItemId保存在[唯一确认凭据](../../spec/TASK-007-p2-g2-readiness/approval.json)。这是本会话用户的实际答复，没有可用请求UUID，不伪造chat-request来源，也不把AI的Issue评论当人类确认。问题明确指向[#18 I18-P03](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6624265781)发布的固定提案`deff207de157b1bf40130494d47a85c4e8bfacce`；采用绑定该提交DISC-009的六个规格片段及其原指纹，规范字节不改。

## 选择与后果

| 范围 | 已采用选择及代价 | 未一并采用 |
| --- | --- | --- |
| DISC-009-R1 | Commit-Snapshot；OIC在t_check固定本次健康决定，OIS保留授权/目标/代数/期限复核和READY提交权。检查后至READY的新NotReady影响后续Open；配置取消可能出现先READY后ABORT，本地不放行载荷。采用控制v2、错误/幂等/兼容与相应参数 | Commit-Strict及消除全部分布式竞态的保证 |
| DISC-009-R2 | Probe-HTTP；本地管理员允许的单源HTTP(S)健康契约、独立observation_revision及有限初始参数。变更清理旧样本/check/Preparing，已Open流保持，超时不伪造NotReady | Probe-Report、任意URL/shell、复杂业务鉴权 |
| DISC-009-R3 | Export-Pull；独立有界发布、默认关闭的回环metrics、有限标签与初始参数；中间事件可合并。采用G2-01–14及预注册候选预算，仍需真实链验证 | 主动导出、可靠历史、生产SLO或容量结论 |

C1、B1–B4、OIS权威、目标revision、授权/许可、透明传输、不重放和95%有效单测保持。R1依赖R2，R3依赖R1/R2；三组既已准确采用，后续不重复索取相同确认，只有实质改动才重评受影响范围。

## 实施前置与交付状态

设计采用不放行产品门槛。当前G1资源证据缺口仍由TASK-005补证，G2产品实施、预注册和run均未开始；不得把本分支继承的G1历史Passed当当前许可。先核验已修复并恢复的G1及适用回归条件，再实施TASK-007，最终以真实产品证据申请G2。

原提案发布回执逐字保留；本轮采用记录与导航完成后还须提交/远端回读，并在#18/#3/#11追加回应及保存回执。当前不虚构新的阶段评论或发布事实。图1–11继续表达现有实现/门槛，不把已采用但未实现的G2画成已完成；实施时同步准确交互图。
