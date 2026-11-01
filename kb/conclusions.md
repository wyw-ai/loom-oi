# 架构结论索引

2026-11-02，ITER-001。详细推导只维护于[本轮综合结论](../docs/evidence/alpha-synthesis-001.md)，此处索引当前判断；人类采用单列于[决策台账](decisions.md)。Supported不是Accepted，Unknown不是必须暂停全部设计。

| ID | 当前结论 / 类型 | 证据 / 范围 | 推荐与采用 |
| --- | --- | --- | --- |
| C01 | 单一接入/TCP在线不能保证业务就绪 / Supported fact | E26，五次明确503、五次超时未知；窄claim C3 | 推荐分离观测维度；具体字段/准入Pending |
| C02 | 局部串行等待可由独立调度避免 / Supported observation；扩展设计Derived | E26；只测请求完成，不测状态发布/公平 | 有界并发、独立发布建议C2；实现未验证 |
| C03 | 丢响应不等于未执行，盲重放可能重复效果 / Supported counterexample | E27/E09；内存效果，非持久去重；窄claim C3 | 内核不跨失败边界隐式重放，语义层决定业务恢复；采用Pending |
| C04 | 有界连接内核、可选观测、消费语义分责 / Derived proposal | H1/E03/E04与C01/C03共同支持推理；C2 | A/B/C共同候选基础；产品入口Pending |
| C05 | 对象和观测按独立变化维度描述 / Derived proposal | 场景推演+C01；C2，不证明两层充分 | access/exposure草图保留，执行状态与就绪观测分开；Pending |
| C06 | Go统一核心推荐保持，语言/HA实证未增加 / Proposal + Unknown | E16/H5，LAB-003仍Blocked；C2 | Go/Rust最终采用Pending，不给整单提高C |

本轮明确排除：把online直接当ready、把结果未知当未执行、把不重放当Exactly-once、把Python性能当语言比较。v4将Probing放入就绪状态轴的图被v5正交表示替代；这是设计修正，未冒充新增实测。

更新时保留稳定ID，标明哪项事实、推论或推荐被何轮替代；反证出现时撤回相应claim及全部下游引用，不能靠历史“已写入KB”维持旧答案。

## C07：v0.6当前实现建议

在H7允许持续收案例、按当前材料设计的前提下，将C04的共同边界收束为[推荐基线B01–B09](../docs/architecture/alpha-v0.6.md#choices)：A/R/Go、独立观测，细化HTTPS/WSS/身份与通道、PostgreSQL元数据与分阶段主备。C07为Derived proposal，C=2；新增工程细节没有实测，不提升C01–C06的证据级别，也不写为人类Accepted。v0.5“不选当前A/B/C推荐”的状态被本版明确推荐替代，最终采用仍Pending。
