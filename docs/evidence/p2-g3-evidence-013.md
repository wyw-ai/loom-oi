# ITER-013：G3路径故障的固定源码正式复验

2026-11-04。固定源码 `019139f59b49cb035c9836befbfd7b4d3531f80f` 的新G3为 **observed/pass**，四项判定均得到实现级直接支持。证据提交、三单追加及逐字回读均已完成，G3按本固定源码/环境范围记录Passed；最终闭环提交CI与alpha集成尚待完成。G1/G4保持此前bff7aa2的验证对象与范围，G2及后续门槛不因此放行。

## 原件、预注册与唯一归属

- 新run：[G3-P2-20261104T151546Z-1040b5](../../experiments/runs/G3-P2-20261104T151546Z-1040b5/report.md)，[Actions 41855795598](https://github.com/wyw-ai/loom-oi/actions/runs/41855795598)，job126663938417，ref为`refs/heads/evidence/G3-P2`。本轮仅1个新样本，唯一归属ITER-013、C55、E77与EDGE-048，不合并ITER-012样本。
- GitHub evidence制品ID13337384683，ZIP851,073字节；SHA-256 `149c0f4a18399a69d2ee3e219f9f3cee225a5fc347e77149d41f199e21aeb722` 与远端digest一致。成功工作流按原`if: failure()`条件未上传evidence-work，并非缺失失败材料。解包10原文件逐字复制入库，未重写任何run内部内容。
- raw为10,580行、7,202,854字节；SHA256SUMS自身为`6b0bf72868830b15858fdc6fe5b34698016ce4b2ba3f0749c25cf0efe4631dfd`。`verify --current-source`通过；另外直接compile固定Git对象中的分析器字节，重组原始Go输出后重算summary，结果精确相等。
- [预注册](../../experiments/manifests/G3-P2.json)提交为`bff7aa278e997cf4777f62bc5e0f6a001d2c6090`，是subject祖先；SHA-256 `f9bc3e15a6b532ee41acb6051993da82f888fdc4bce18c10605c9808fe786e33`，case与固定对象完全一致。139项source-manifest均与subject Git对象逐字SHA相等，环境中的subject/GITHUB_SHA/run ID也一致。
- 被测分析器与现有[product-g3-first归档](../../experiments/sources/product-g3-first/analyze.py)逐字相同，沿用原归档，不复制第二份相同源码。

## D21修正与首轮反例的关系

[ITER-012/C54](p2-g3-first-evidence-012.md)的首轮原始observed/fail、报告、回执及四条误标行均冻结保留。对两份139项source清单逐项比较，无新增或删除，仅`tests/integration/g3_env_test.go`一处发射kind由`G3-fault`变为`G3-preparation`，其余138项相同。产品实现、正式schema、严格分析器、四项criteria、34叶/38profile/62phase与预算均不变。

本轮独立新采样支持D21修正后的正式证据，而非在内存中删除首轮问题行再给首轮改判。新的辅助blackhole_phase仍保留为preparation诊断；无业务预演的1600–1800ms许可相位不冒充正式业务故障≥2000ms的注入前置。

## C55：四项直接判定及其边界

| 固定判定 | 原始支持 | 不能外推的保证 |
| --- | --- | --- |
| 半关闭与背压 | 两后端FIN顺序、反向残余、上传/下载受阻路径及三个组件缓冲局部断言通过 | 2F是应用在途消息约束，不是所有TLS/WS/代理/内核内存上限 |
| 撤销时限 | 真实在途撤销、Preparing撤销、控制切断/黑洞与原许可边界通过 | 控制重连不续旧流，不提供业务取消或回滚保证 |
| 响应丢失不重放 | 两数据腿故障、已提交效果但响应丢失、原5秒无隐式重放窗口及显式新请求通过 | 无去重fixture的效果计数不是通用Exactly-once或持久业务去重 |
| 清理与限额 | 两后端混合额度与重复失败、全部62项正式资源阶段通过，故障前profile及原2秒不重置 | 单机回环有限场景，不是WAN、长期稳定性或G5目标主机预算证据 |

34个正式行为叶及28个向量ID全部pass，必要项无skip/missing/fail；原始问题和零容忍记录均为0。共同承诺仍是准确绑定、有限授权、有界原生传输与可解释回收；提供方拥有业务状态，通道结束不代表业务副作用撤回。

## 资源与质量的实际数量

G3资源独立重算pass：34正式叶对应38个固定profile，另10个仪器profile；62个正式清理phase、8个预演恢复phase，共70次完成、409个采样。正式62phase共385样本，最慢1,380.440022ms；70次范围为213.862257–1,380.440022ms。每项检查均维持故障前固定基线、同拓扑/真实PID，在原绝对2秒内最终连续三次样本满足FD精确相等与goroutine双向±10；更早尚未归位的样本也保留。没有故障后换基线或以缺控制FD抵消数据残留。

附带B4解析的79个必需leaf/至少85次清理要求满足：原始101个profile、103次完成、520样本，107.111461–1147.436165ms；额外执行不增加必需场景数。预演profile和正式样本不混计。

unit/race各392个根/子测试通过，完整cmd/internal覆盖96.1%；vectors384项PASS、5项非必需/不适用skip，无失败。skip分别为B1-17/postgres、B3-11f/postgres、B3-05/sqlite、B3-08/postgres及本manifest明确load=false的B4-06；race的3个cmd包无测试文件，不是遗漏必需测试。frame/control主动fuzz实际运行60s/61s，分别1,274,366和1,029,536次执行；vet/staticcheck/govulncheck通过。八步骤退出0且无超时，实耗依次87,851/113,729/532,430/100,827/61,416/12,978/42,313/14,529ms；这些是执行记录，不新增产品SLO。

环境为GitHub hosted Linux7.0.0-1017-azure、2逻辑CPU Intel Xeon Platinum8370C、MemTotal8121972kB、Go1.27.3、Python3.12.16、SQLite3.54.1与PG18.6，含32MiB小盘/容器PG工具。没有在用户指定G5/G7主机运行，不证明G2观测、H/M路径、WAN/NAT、HA或原生SSH/Bash兼容。用户已采用的G2三项选择不重复询问；其实现及证据由TASK-007独立承接。

## 发布闭环与后续

新run与ITER-013在证据提交70464617b4f014b7bd47216720c456b6be443014同次保存，36个文件远端逐字回读完成；KB、门槛及架构/模块入口、图06完成联动。独立复审再次核验原件、139源码、预注册、34行为叶与正式/预演资源，未见阻断。最终连续三次资源归位与全部早期样本分别保留，未重写原run。

[I3-P11](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6635311284)、[I11-P11](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6635314853)、[I15-P08](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6635318085)追加且正文逐字回读一致；三单元数据与34条旧评论保持，9个固定链接可解析，见[回执](../issue-history/2026-11-04/TASK-006-g3-published.json)。ITER-013完成后才记录G3 Passed；旧ITER-012及所有已完成报告/回执保持原字节。

本地tools35/experiments116、结构、8份SDD review、采用、物料、18run/13ITER检查通过；P2仅对已有记录范围放行，P3仍由G2阻挡。原019139f CI41855685963六项全绿；最终闭环提交仍须自身六项CI全绿后快进alpha。后续代码、契约或环境改变须重新评估适用性，不把本次通过套到独立G2分支，也不取消G5/G7真实前置。
