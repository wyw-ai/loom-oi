# Architecture Evidence Lab

本实验室为 alpha 架构提供可复现证据，入口是 [仓库 skill](../.agents/skills/architecture-evidence-lab/SKILL.md)。2026-11-02（+08:00）首轮运行，run 名使用 UTC。实验代码与证据一同版本化；它们不是 OIS/OIC 产品实现。

## 已执行与待执行

2026-11-03按H13准备[R1/R2应用物料](materials/README.md)，物料自检与真实实验分开。[#11](https://github.com/wyw-ai/loom-oi/issues/11)维护剩余条件；[本次只读盘点](../docs/evidence/materials-readiness-2026-11-03.json)只检查PATH和产品源路径，未验证容器/设备/集群可用性。

| 实验 | 当前范围 | 当前结果/条件 |
| --- | --- | --- |
| [LAB-001](runs/LAB-001-20261101T191556Z-7863f9/report.md) | TCP可达/就绪和探针机制 | 历史机制夹具；不证明OI实现 |
| [LAB-002](runs/LAB-002-20261101T191558Z-270709/report.md) | 效果后响应丢失 | 历史机制反例；不证明OI/持久去重 |
| LAB-003 | 已采用Go的目标资源验证；Rust按批准范围作最小对照 | 受阻：工具链、实现、目标环境/预算与预注册未齐 |
| LAB-004 | 真实消费者/提供方兼容；R1/R2为其中的应用切片 | 测试输入/用例/模板已准备，真实OI、组合版本、专用UI/Pod环境与故障驱动仍缺 |
| LAB-005 | 探针归并/发布/导出与控制公平 | 受阻：实际实现、测量点与故障条件 |
| LAB-006 | 真实传输、双后端单实例恢复、额外PG主备 | 受阻：实现、数据库/主备隔离拓扑；R5边界采用不等于HA通过 |

详细结果和可外推范围见 [首轮证据报告](../docs/evidence/alpha-lab-round1.md)。不是所有未知都适合立即用模拟器作答；消费者意图要有真实来源，语言比较要有对应实现。

## 复现与核验

在仓库根目录：

    python experiments/run.py experiments/manifests/LAB-001.json
    python experiments/run.py experiments/manifests/LAB-002.json
    python experiments/verify.py experiments/runs/LAB-001-20261101T191556Z-7863f9 --current-source
    python experiments/verify.py experiments/runs/LAB-002-20261101T191558Z-270709 --current-source
    python experiments/test_evidence.py

每次生成新目录，不覆盖原始数据。runner 只支持固定受信 fixture，标准库运行，无需 pip 下载。verify 检查 hash、执行结果和 raw 重算；test_evidence 验证原始数据损坏、伪造摘要、重复试次、attempts不一致和失败运行冒充成功会被拒绝。

## 实际隔离与局限

清洁环境、独立进程组、临时工作目录；固定 127.0.0.1；CPU 10秒、地址空间512MiB、128 FD、单文件8MiB、wall30秒。结束清理进程组和临时目录。namespace 实测失败，原因记录在 environment.json；bwrap 的可用性检查也失败。本 runner 不提供 OS 网络/文件系统沙箱，不能执行不可信代码或真实系统故障注入。

执行环境 cgroup 为8GiB、8 CPU额度，不能称为1C1G资源测试。局部地址空间限制也不等于完整机器规格。没有继承用户密钥，没有连接生产服务；数据为合成fixture。证据文件包括预注册问题、源代码哈希、环境、原始观测、stderr、退出结果、重算摘要和校验和。源码当前版本与运行前哈希一致；后续修改要保留旧run对应版本。

## 更新纪律

按照 [产物联动表](../docs/architecture-artifacts.md) 更新 Issue、KB、正文、图与验收；只对有直接证据的窄 claim 增强可信度。产品决定仍由人类确认。

## 每轮操作后的自动分析

执行实验的AI必须在同一任务内自动完成[综合分析闭环](../.agents/skills/architecture-evidence-lab/references/post-round.md)：核验raw → 结合KB与整体架构 → 收敛有条件结论 → 更新图文/Issue/KB → 校验并保存。无需用户再次要求分析。首个记录为[ITER-001](../docs/evidence/alpha-synthesis-001.md)，该轮只复核旧run，没有新增样本；本次物料准备及分析见[ITER-003](../docs/evidence/application-materials-003.md)，没有新产品run。

运行`python experiments/check_iterations.py`检查所有run是否已绑定分析输入指纹；返回0为记录覆盖完整，2为存在未分析run，1为证据/记录不一致。新run、失败run和纠错旧run都不能漏掉；检查器不评价推论，也不核实GitHub写入。`python experiments/test_iterations.py`验证这些约束。独立执行Python不会唤醒后台AI；这里的自动执行由当前架构AI工作流负责。

## v0.7：随实现节点验证与阻挡墙

[#11最新回复](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708)已落实为[执行规则](GATES.md)、[gates.json](gates.json)和`check_gates.py --stage <阶段>`。G0–G7均未通过；4类未就绪实验保留。无阶段参数时退出0仅表示台账格式有效。真实实验后先完成综合分析/迭代/图文回写，再最后晋级；失败立即降级。

历史门槛建设及回归验证、旧证据只读复核见[反馈综合分析](../docs/evidence/alpha-feedback-002.md)，未新增产品实验样本。LAB-001/002不替代G2/G3；LAB-006分实际传输、双后端单实例恢复和PG主备三组既有主题内切片。
