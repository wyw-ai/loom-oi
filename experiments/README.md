# Architecture Evidence Lab

> TASK-006当前状态：73bb667的G1负载复证失败、G4原四项直接通过；G1 Failed、G4 Inconclusive，正式G3尚未运行。旧2ec3670通过保留历史范围，见[ITER-008/009](../docs/evidence/p2-g1g4-revalidation-009.md)。

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
| LAB-006 | 真实传输、双后端单实例恢复、额外PG主备 | 单实例恢复保留[ITER-005](../docs/evidence/p2-g4-evidence-006.md)历史证据；2ec3670的ITER-006/007新G1/G4运行均通过，四单发布回读完成，两门槛恢复Passed；真实路径故障G3与PG主备G6仍缺对应证据，不能将整个LAB-006记为完成 |

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

执行实验的AI必须在同一任务内自动完成[综合分析闭环](../.agents/skills/architecture-evidence-lab/references/post-round.md)：核验raw → 结合KB与整体架构 → 收敛有条件结论 → 更新图文/Issue/KB → 校验并保存。无需用户再次要求分析。首个记录为[ITER-001](../docs/evidence/alpha-synthesis-001.md)，该轮只复核旧run，没有新增样本；物料准备及分析见[ITER-003](../docs/evidence/application-materials-003.md)，没有新产品run。首批产品run（G1-P1）及分析见[ITER-004](../docs/evidence/p1-g1-evidence-005.md)；本轮一个G4-P2主run、C39–C42直接结论与C43资源证据失效限定见[ITER-005](../docs/evidence/p2-g4-evidence-006.md)。当前TASK-005先补齐G1资源前置，再完成Issue阶段回读与最后门槛记录；补证预注册G1-P1-R2的首轮已失败，归属ITER-006，见下文当前状态；不能据原G4通过恢复门槛。

运行`python experiments/check_iterations.py`检查所有run是否已绑定分析输入指纹；返回0为记录覆盖完整，2为存在未分析run，1为证据/记录不一致。新run、失败run和纠错旧run都不能漏掉；检查器不评价推论，也不核实GitHub写入。`python experiments/test_iterations.py`验证这些约束。独立执行Python不会唤醒后台AI；这里的自动执行由当前架构AI工作流负责。

## v0.7：随实现节点验证与阻挡墙

[#11最新回复](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708)落实为[执行规则](GATES.md)及[台账](gates.json)。当前G0通过；2ec3670的新G1五项/G4四项正式运行均通过，见[ITER-006](../docs/evidence/p1-g1-resource-evidence-007.md)与[ITER-007](../docs/evidence/p2-g4-resource-evidence-008.md)。四单发布回读和门槛记录已完成，G1/G4均Passed，P2的G1前置已满足；CI与alpha集成进度见TASK-005检查点。旧失败/通过原始结果不改，记录检查不代替语义审查或发布。

历史门槛建设及回归验证、旧证据只读复核见[反馈综合分析](../docs/evidence/alpha-feedback-002.md)，未新增产品实验样本。LAB-001/002不替代G2/G3；LAB-006分实际传输、双后端单实例恢复和PG主备三组既有主题内切片。

## 产品实现证据（G1起）

G1及以后的门槛需要实现级证据，上面的机制夹具不能代替。[G1-P1预注册](manifests/G1-P1.json)与[G4-P2预注册](manifests/G4-P2.json)各自固定向量→测试映射、两种后端、判定项、覆盖率与质量信号。`experiments/product/run.py`只执行固定步骤（单元测试与覆盖率、race、两后端集成向量、两个模糊目标、vet、staticcheck、govulncheck；O3负载由manifest的load选择，G4为false），要求受测源码与HEAD一致并以HEAD为`subject_version`；未跟踪或被忽略的受测源码也不能混入。`experiments/product/analyze.py`只从raw重算summary，`verify.py`按source-manifest记录的分析器哈希复算。CI中推送`evidence/**`分支即由[evidence工作流](../.github/workflows/evidence.yml)在ubuntu-24.04与PostgreSQL 18.6上运行并上传run目录；审阅后提交到`runs/`，再按综合分析闭环记录门槛。

    python experiments/product/run.py experiments/manifests/G1-P1.json   # Linux；需OI_TEST_PG_DSN与cgo
    python experiments/product/run.py experiments/manifests/G4-P2.json   # 另需小容量文件系统与同版本PG工具
    python experiments/verify.py experiments/runs/<G1-P1-run>
    python experiments/test_product.py

退出码：0为observed且verdict为pass，1为其他结果，2为工作树与HEAD不一致而拒绝运行。缺失或超时的步骤只能得到inconclusive，不能记为通过。

| run | 被测提交 | 结果 | 用途 |
| --- | --- | --- | --- |
| [G1-P1-20261104T002624Z-35ec8b](runs/G1-P1-20261104T002624Z-35ec8b/report.md) | `9a42f4e` | observed / fail：TV-B2-11（postgres）到期后转发18字节、TV-B4-06两后端截断 | 反例，已被取代并保留，不计通过样本 |
| [G1-P1-20261104T014622Z-327df6](runs/G1-P1-20261104T014622Z-327df6/report.md) | `f844b29` | observed / pass：47项向量检查、G1五项与质量信号通过，零容忍0 | 历史G1主证据，分析见[ITER-004](../docs/evidence/p1-g1-evidence-005.md)；原run不改，当前资源完整性结论已由C43撤回 |
| [G4-P2-20261104T054837Z-00be90](runs/G4-P2-20261104T054837Z-00be90/report.md) | `6c7d5ad` | observed / pass：13个向量ID的75项必需父子检查、G4四项与质量信号通过，覆盖率95.9%，零容忍记录0 | 本轮唯一run，G4实现级主证据，分析见[ITER-005](../docs/evidence/p2-g4-evidence-006.md)；G4仍Blocked待G1资源前置补证、Issue回读与完整联动 |

G4主run的环境为GitHub托管ubuntu-24.04、2 vCPU、Go 1.27.3、SQLite 3.54.1和PG 18.6；SQLite写满由32 MiB tmpfs触发，PG备份/恢复使用服务容器内工具。5个集成SKIP分别为不适用的后端子场景（B1-17 postgres、B3-11f postgres、B3-05 sqlite、B3-08 postgres）及显式排除的B4-06负载；75项必需检查没有跳过。备份/恢复/迁移耗时、WAL和回收大小只作本次小库O2观察；不据此放行G2/G3/G5/G6/G7或承诺容量、RTO/RPO。

## ITER-006：补证首轮失败后的当前状态

[G1-P1-R2-20261104T064821Z-0830c3](runs/G1-P1-R2-20261104T064821Z-0830c3/report.md)在b7af582为observed/fail，详见[ITER-006](../docs/evidence/p1-g1-resource-evidence-007.md)。原分析器丢失Go Output分片；仅在内存重组后，115次严格资源检查通过，但race TestCompletedExchange仍失败（完整11/5字节却误报ChannelInterrupted），不是DATA RACE告警。原run/summary不改，诊断不增加样本。G1为Failed、G4仍Blocked；D16/D17修复并取得新G1正式证据后，生产变更还要求新的G4正式run，不能直接复用6c7d5ad历史结果放行。

最新续测：5668b53的新G1 run为observed/pass，D16/D17修正取得正式支持；同源G4 run为observed/fail，新增ITER-007/C46，原因是11h/sqlite末尾文件描述符未归位。D18修正和最终G1/G4复证未完成，状态由上方门槛入口承接，旧首轮失败叙述保留为历史轨迹。

2ec3670当前复证：新G1五项与新G4四项均observed/pass；Linux真实FD正负对照已执行，D18修正的正式支持由C48/C49承接。四单发布回读完成，G1/G4恢复Passed；CI与alpha集成进度见TASK-005检查点，不把前节5668失败时点继续当成修复后结果。
