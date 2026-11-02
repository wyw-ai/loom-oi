# 架构结论索引

2026-11-02，已更新至ITER-002。C01–C06推导见[首轮综合结论](../docs/evidence/alpha-synthesis-001.md)，C08–C11见[最新反馈分析](../docs/evidence/alpha-feedback-002.md)；C07保留稳定ID供追溯，其旧方案已由v0.7修订。此处索引当前判断；人类采用单列于[决策台账](decisions.md)。Supported不是Accepted，Unknown不是必须暂停全部设计。

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

## C07：v0.6历史实现建议（已由C08–C11修订）

在H7允许持续收案例、按当前材料设计的前提下，将C04的共同边界收束为[推荐基线B01–B09](https://github.com/wyw-ai/loom-oi/blob/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture/alpha-v0.6.md#choices)：A/R/Go、独立观测，细化HTTPS/WSS/身份与通道、PostgreSQL元数据与分阶段主备。C07为Derived proposal，C=2；新增工程细节没有实测，不提升C01–C06的证据级别，也不写为人类Accepted。v0.5“不选当前A/B/C推荐”的状态被本版明确推荐替代，最终采用仍Pending。

## C08–C11：v0.7回复驱动收敛

| ID | 判断与类型 | 依据/限制 | 影响 |
| --- | --- | --- | --- |
| C08 | OI注册/通道与业务实例化分责 / User boundary + Derived contract | H9确认边界；computer-use实例链为纸面推演，未实测 | v0.7§2.1、#3；C04在当前范围收敛，不扩大到托管平台 |
| C09 | Ready/NotReady/Unknown按准入矩阵处理 / Derived proposal | C01不能决定默认；有效负证据两模式拒绝、最终复核是本轮细化，K2 Pending | v0.7§6.2、G2；未把HTTP夹具当实现通过 |
| C10 | PG/SQLite统一管理契约、部署保证分开 / User requirement + Derived design | H8要求双后端；官方机制限制与项目推演，C2；无数据库性能/HA实测 | v0.7§8、G4/G6；替代C07的PG单后端提案 |
| C11 | 实验结果绑定阶段出口 / Accepted workflow | H10；工具与独立只读行为验证只证明记录检查，不证明产品 | #11、gates.json；全部Blocked，闭环完成后才晋级 |

本轮见[反馈分析](../docs/evidence/alpha-feedback-002.md)。C01–C03旧窄机制判断保留，C06语言推荐保持Pending；没有新增样本或提升实验可信度。

## C12–C14：DISC-001讨论推论（未实测、待确认）

| ID | 当前判断 | 来源/限制 | 下游 |
| --- | --- | --- | --- |
| C12 / Derived proposal | 把目标与argv/script放入外部工具契约；A/R核心不新增exec | E42 D01/D02 + H9；可减少一层人工包裹是设计假设，不能保证Agent质量或消除shell语义 | 架构§5.4、client模块、#3，V01–V03待测 |
| C13 / Scope inference | 手机/PC/Pod是外部能力案例，OI不承担业务实例或UI互斥 | D04/D05 + H1/H9；截图不验证兼容，未来Loom意图不覆盖当前独立边界 | 概念/OIC、SC05/06/10、#3/#11 |
| C14 / Governance requirement + design | 每轮融合必须提交确认Issue；用需求ID和规格指纹限定确认范围 | E43直接授权工作纪律；具体SDD细则Review，记录检查不能替开发者确认 | DISC-001-R4、AGENTS/skill/SDD |

C01–C11证据范围不变，C06语言仍Pending。D06“大概Go”不新增Accepted，C12/C13不因文档/脚本验证而成为产品实测。
