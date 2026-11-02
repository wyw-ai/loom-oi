# ITER-002：最新回复、整体架构与实验门槛

2026-11-02 · alpha · 基线0063738add22ed1f1eea07cd27038dcbf6df066b。此轮为反馈驱动设计、既有证据只读复核及治理工具验证；**没有新增产品实验或样本**。

## 来源与证据边界

- H8：[#4回复](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6564934039)，R按推荐，默认PG/无PG时SQLite并关注规模优化。
- H9：[#3回复](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6564999241)，不耦合功能实例化，要求K1/K2详细设计。
- H10：[#11回复](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708)，按实现节点实验，记录阻挡墙，未过不得草率越级。
- 独立只读技能行为验证读取了LAB-001/002新版manifest、execution、raw及既有综合结论，并核对原始数据完整性/重算；两份新版记录可验证，4个历史run/ITER-001覆盖完整。未重跑fixture、未增加次数；纠错前run仍为superseded。
- SQLite WAL/PRAGMA/查询规划/备份、3.51.3修复记录与PG18锁的官方资料已核对，见[证据台账](../../kb/evidence.md)。一手机制依据不代表本项目容量、性能或恢复已经通过。

## 判断、建议与反证

| Claim | 本轮结论 | 推理与实际限制 | 方案影响 |
| --- | --- | --- | --- |
| C01/C02 | 旧局部观测结论保留 | TCP可达不等于业务Ready；完成时间不等于状态发布，不证明TTL/饱和公平性 | K2需完整准入矩阵与G2实现验证，默认仍待人类选择 |
| C03 | 丢响应反例保留 | 计数效果后丢响应可以重复执行；内存计数不证明真实OI或持久去重 | 恢复只建新通道、不隐式重放；G3仍Blocked |
| C08 | OI业务实例化边界已确认；接口为Derived | H9→实例由业务方拥有→OI只登记/连接；同桌面互斥仍由提供方处理 | v0.7§2.1、总体/建连图、#3；不恢复内置computer-use托管路线 |
| C09 | 准入细化为Derived proposal | 有效NotReady两模式拒绝；Unknown才区分best_effort/require_ready；最终复核无法保证未来业务成功 | v0.7§6.2、G2；K2没有被评论自动Accepted |
| C10 | 双后端要求已确认；实现/部署分责为Derived | PG/SQLite共用管理不变量；WAL/锁/备份机制不等于同等主备能力；文件大小需结合查询/事务诊断 | v0.7§8；显式选库、无故障自动降级、SQLite单实例、PG主备G6 |
| C11 | 阶段纪律已确认，机器检查只证记录完整性 | H10→条件/结果/放行分开；通过需闭环及适配证据级别 | G0–G7及#11持续跟进；当前全部Blocked |

保留Go工程推荐，驱动/双数据库构建与维护纳入比较；没有语言性能数据，#8采用仍Pending。若真实消费者必须核心实例创建、SQLite跨主机HA或无证据强行访问，需新明确范围及反证，不能偷偷改写当前边界。

## 正常场景与整体反例（纸面推演）

正常：业务方启动computer-use服务，OIC登记固定目标/描述，消费者经R建原生通道；双后端管理元数据按相同不变量更新；观测/准入独立于业务结果。

反例：服务未启动但OIC在线，不能宣告Ready；两个通道访问同桌面不能保证互斥；已连接PG失效不能新建SQLite空库；SQLite单进程重启不能证明PG主备/跨主机HA；一个机制报告不能让G2/G3实现门槛Passed。以上是设计约束与反例推演，不是新的实验读数。

## 门槛工具与技能验证

新增检查器覆盖依赖递归、缺证据/覆盖不足、文件漂移、无效状态、未知阶段及中断闭环。独立只读行为验证实际拒绝进入P3与单实例交付，并发现“先Passed、后补分析”的中断窗口。已修为失败立即降级、闭环和远端回读完成后最后晋级。

初版局部校验还可能接受缺raw或失败primary记录；已改为复用`check_iterations.check`的完整run/迭代核验，再校验证据级别、subject_version、run/analysis/environment指纹及完成状态。二次合成验证拒绝空校验清单/缺raw、失败run标primary、缺失联动产物和非法迭代schema。合成样本仅存在测试隔离内存/临时目录，没有写入产品run目录。

检查器仍不能证明报告语义、测试代表性或人类决议真实性；G0的人类来源、实现与部署结果须审阅。工具/skill验证不计入LAB产品通过率。当前缺实现、工具链/目标环境、真实样本、双数据库/主备夹具；无门槛Passed。

## 产物联动与收尾

现行[架构v0.7](../architecture/alpha-v0.7.md)、[六张Graphviz矢量图](../architecture/diagrams/v0.7/README.md)、案例/语言提案、Issue索引、KB和实验目录已按本轮设计一致更新。旧ImageGen图及v0.6见[Git历史快照](https://github.com/wyw-ai/loom-oi/tree/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture)，不覆盖新决定；原始run与ITER-001保持原样。

#3确认范围/矩阵、#4双后端/恢复、#8依赖变化、#11门槛在原单回写。没有关闭/重开/分派、没有合并main或启动产品实施。阶段是否通过按[gates.json](../../experiments/gates.json)和实际证据判断，不能从本文发布推断。
