# ITER-007：G4严格资源断言失败与证据边界

2026-11-04 · 被测提交 `5668b5335f364df7ee182128d0154f9cb64d5643` · 实现级产品实验。**本轮只有1个新增G4 run，observed/fail。** SQLite审计维护向量在功能断言完成后，OIS的FD在资源清理采样中持续为17，固定基线为16；本轮未满足严格资源条件，不能晋级G4。原始记录没有FD目标或连接池状态，原因待查，尚不能将其定性为产品泄漏或夹具假失败。

当前门槛应记录 **G4 Failed、G1 Inconclusive**。同提交的新G1通过run由[ITER-006](p1-g1-resource-evidence-007.md)独立承接，支持D16/D17修正，不计为本轮样本；同源G4资源反例、后续可能的存储修改及未完成Issue闭环仍阻止最终恢复。不能继续把当前FIN写成未修复的Failed，也不能以新G1通过抹掉本轮G4失败。

## 输入与原始核验

| 输入 | 版本与结果 | 本轮使用方式 |
| --- | --- | --- |
| [G4-P2-20261104T072937Z-bebf95](../../experiments/runs/G4-P2-20261104T072937Z-bebf95/report.md) | `5668b53`；[工作流41792534101](https://github.com/wyw-ai/loom-oi/actions/runs/41792534101)；observed/fail | 唯一新增样本，作为未通过及定位依据；不是primary通过证据 |
| [G4-P2预注册](../../experiments/manifests/G4-P2.json) | `67a164ab0f4f393ae90426d1e87aab0823ea523e`，2026-11-04 05:29:34 UTC | SHA-256 `74aaedae340299213b23739e074a0d054f6076d6d6894660f07f74a1866ea9db`；没有按结果改变原四项判定、向量或load=false |
| [原G4分析](p2-g4-evidence-006.md)与[ITER-005](../../experiments/iterations/ITER-005.json) | `6c7d5ad`的历史observed/pass | 保留原run、原四项直接结论与时点；不重复计样本，不把旧subject改成5668b53 |

独立执行 `verify.py --current-source` 返回integrity=verified、recomputation=observed，原summary精确复算一致。另逐项读取被测Git提交，核对source-manifest的114个路径和原始字节，全部SHA-256匹配；去除运行附加的subject_version/preregistration后，manifest与已提交预注册对象完全一致。被测提交时间07:27:23 UTC，run标识时间07:29:37 UTC，首个Go事件07:29:38.707371841 UTC，预注册先于被测执行。

[raw.jsonl](../../experiments/runs/G4-P2-20261104T072937Z-bebf95/raw.jsonl)有5,419行、2,366,351字节；SHA256SUMS自身指纹为`842a4d92521c52c8b7e51fec38eb6b4a48a712de045bdf62c495b5af9b4e94b9`。独立按step/package/test拼接Go Output，631个未换行分片被重组成完整逻辑行，结束后无尾残；所有OBS分类计数与原summary一致。D16的分片测量问题没有在本轮造成遗漏。校验与复算不证明断言正确、外部真实性或根因已确定。

环境为GitHub托管ubuntu-24.04，镜像20261026.354.1、Linux amd64、2个可见CPU、MemTotal=8,121,972 kB，Go1.27.3、SQLite3.54.1、PostgreSQL18.6（Debian 18.6-1.pgdg13+2），单机回环。environment记录small_fs=true、PG工具在服务容器执行；11e两后端及11a/b/c工具路径实际运行通过，没有把必需故障分支跳过。32MiB是预注册tmpfs配置，不是独立磁盘容量测量；本轮没有1C1G、WAN、HA或真实应用兼容结论。

## 失败轨迹：功能完成后固定基线未归位

唯一失败leaf为 `TestTVB3_11h_AuditRetention/sqlite`，父测试随之失败；不是两个独立故障。对应源码为固定提交的 `tests/integration/vectors_b3_recovery_test.go:904–973`。该测试先验证审计导出、篡改与错误摘要拒绝、合法修剪及SQLite增量vacuum，再记录O2并调用严格资源清理。

| raw行 | 记录 | 可支持的事实 |
| --- | --- | --- |
| 4869 | resources_baseline，profile-161，OIS PID8519，无OIC；G14、FD16 | 故障/业务操作前固定基线，channels/tickets/accesses均0；sampled_ns=124740480558 |
| 4870 | O2：审计304行，库1,413,120→73,728字节，345→18页 | 该位置之前的摘要保护、修剪及页回收断言已执行完成；不代表整个向量通过 |
| 4871 | resources_begin，check-162 | started_ns=125310032766，deadline_ns=127310032766，固定预算2,000,000,000ns |
| 4872–4910 | 39条resources_sample | 每条OIS PID8519、G14、FD17，channels/tickets/accesses均0；没有FD回到16的样本 |
| 4911–4913 | 未归位错误、leaf FAIL | 最后sample ended_ns=127304314146；未产生resources成功终记录；leaf耗时2.71s包含前面的功能操作 |
| 4916、4922、4924 | PostgreSQL对照：基线G13/FD12，3条合格样本，106ms完成，leaf PASS | 另一后端在其原固定基线归位，不排除SQLite特有连接/文件行为 |

最后一个SQLite资源样本距begin约1.994s，距固定deadline约5.7ms；39次采样均是同一+1 FD差值，而非把一次瞬态读数当作整个失败。它同时不是持续增长曲线，不能据此推出每轮都增加、描述符永不释放或泄漏速度。goroutine值没有超线也不能抵消FD精确相等条件。

本轮全部资源原始记录为101个baseline profile、103个begin、619个sample和102个成功final。另以现有严格资源规则只读核对所有已发记录，唯一未完成的check为11h/sqlite；其余102次完成耗时106–1130ms，记录的字段、角色、基线冻结、采样顺序/期限、PID及最终连续样本未发现其他问题。TV-B3-11a–i范围内有18次check、17次成功final，失败的第18次就是该leaf。此核对是原始数据诊断，不给原G4 manifest事后新增G1最低check列表，不生成新run或改写summary。

## G4判定与其他质量信号

原13个G4向量ID为12 PASS、1 FAIL；75项必需父子测试检查为73 PASS、2 FAIL。原四项criteria有三项通过、一项失败，整体observed/fail正确。

| 判定项 | 本轮结果 | 限定解释 |
| --- | --- | --- |
| 并发版本冲突 | pass | 两后端配置冲突及11g拥塞通过；queue8、burst40、各1成功/7冲突/32 StoreBusy |
| 备份与重启恢复 | pass | 04、11a/b/d/i、store回归通过；维护期间新Open为0，恢复权限需管理员重新施加并resume |
| 旧任期拒绝 | pass | 10、11c通过；已见epoch2后恢复为3，旧通道结束、旧票据拒绝 |
| SQLite规模维护拥塞及磁盘不足 | fail | 11e/f/g通过；11h/sqlite因严格FD资源条件失败，不能只取此前功能断言来宣布该项通过 |

八步均执行完成，无timeout；仅vectors退出1。unit/race各309个PASS测试事件，覆盖率95.9%；vectors为262 PASS、5 SKIP、2 FAIL（测试事件含父子项，不能当独立样本数）。两个指定fuzz目标均有2 workers的active fuzz记录，目标Elapsed各60.09s；vet、staticcheck v0.8.2、govulncheck v1.9.0退出0，raw第5418行明确No vulnerabilities found。零容忍标记行0不抵消资源断言FAIL；本轮race无失败不等于未来代码或所有时序均已验证。八步总耗时384,633ms。

五个SKIP均为原预注册支持边界：B1-17/postgres不涉及存储、B3-11f/postgres与B3-08/postgres为SQLite专属、B3-05/sqlite为PG专属，B4-06_Load由本轮load=false明确排除。四条O3观察来自两后端的大流慢读与1000次无串流检查，不是G1的1/8/32通道×1/64MiB完整负载矩阵；不能把本G4 run当成新容量或完整O3通过证据。

O2/O4的耗时和库大小保留为观察，不增加性能判线：11a在线备份为SQLite9ms/73,728字节、PG169ms/12,179字节，分别23/88次并发写且写错误0；11b恢复13/159ms、resume至Open均4ms；11e磁盘满/只读检测1/8ms、恢复323/329ms，均不重启；11g最慢快速拒绝1/2ms、撤销1068/1020ms。PG审计303行，库8,459,967→8,380,943字节，不冒称SQLite式页回收。它们不构成恢复SLO或资源平台外推。

## C46：严格资源条件有正式反例，根因仍未确定

**C46 / 实现级观察与限制：在5668b53的G4正式run中，SQLite审计维护向量完成其功能断言后，OIS FD在固定2s资源检查窗的39个样本里保持17，固定基线为16；该run未满足严格资源条件，原因待查。** 来源登记为E68，演进关系为EDGE-039；本轮不重复计入ITER-005。

原始证据排除了“只有summary丢行”“goroutine超限”“缺少统计请求成功记录”“PG工具或必需SQLite故障向量被跳过”等解释；它没有识别第17个FD。连接池按需打开/底层SQLite延迟close、健康基线未覆盖既定长期资源、真正未释放的文件或连接，都仍需有区分度的证据。源码中的某种可能机制也不能反过来证明原run必然由它触发。

最有区分度的下一步是在不修改故障后基线、不放宽FD==baseline和T_close+1s的前提下，固定相同源码/配置复现11h：在预热、bulk写、导出/修剪、checkpoint/vacuum及清理各点记录OIS实际FD链接/类型和DB连接状态，重复维护轮次并观察是否有界、何时释放。若是既定长期资源，应把其真实健康生命周期在初始化/预热中明确，仍不能在失败后重采；若是延迟close或泄漏，应修实际资源生命周期并加可稳定区分旧/新实现的回归。修复后重跑受影响完整G1/G4正式范围，保留本次失败。

不能仅调整预热让数字恰好匹配，也不能凭+1 FD就宣布产品泄漏已证实。若后续发现采样或基线错误，应新增解释与证据而不是回改本run；若新产品修复导致原source不再适用，应在新版本上产生新run。无论根因最终归于产品或夹具，本次失败都不支持晋级。

## 联动与恢复条件

本轮新增本报告和[ITER-007](../../experiments/iterations/ITER-007.json)，completion保持pending-publication。KB的C46/E68/EDGE-039、任务一致性/偏差、全局门槛及产品/图文入口由本轮主线联动；完成前不宣称传播或远端交付已结束。Issue #3/#4/#11/#18保留原正文与旧评论，待追加本次失败、定位与恢复阶段并逐字节回读；没有预造评论ID或人类决议。

G4改为Failed，不能只写前置Blocked掩盖直接反例。G1新通过证据可支持D16/D17已修正，但同源资源反例、待确定修法及闭环限制使当前G1为Inconclusive；不提前Passed。最终恢复依赖问题定位、适用修复与回归、清洁固定源码的新正式验证、原始复算、独立评审、ITER/Issue回读及门槛一致性。B1/B2/B3/B4采用契约不改，旧run、旧ITER-005和历史结论时点均保留。

G2三组契约已在独立分支准确采用，内部设计可准备；G3也可做独立规格/夹具准备。它们不因本轮质量多数通过而跨过尚未恢复的产品入口。G5真实1C1G、G7真实兼容资源仍须各自条件，G6仅属HA；本轮不能代替这些证据。

## D18修正检查点（尚待Linux机制与正式复证）

源码审查定位一个与持续+1吻合的候选机制：固定modernc SQLite Unix VFS在仍有文件锁时将已关闭连接的主库FD放入待关闭链，数据库/sql只保留一个idle reader并不等于底层文件已回收。这个源码机制尚不能识别旧raw中的具体第17个FD。当前修正固定一个读连接、保留独立WAL写连接，满足已采用B3读连接≤4。读请求改为串行等待，沿原context取消，不增加吞吐或容量承诺。

新回归包括实际默认池的精确DB/WAL/SHM/lock描述符回归、独立store显式恢复旧max4策略的负对照、最终store.Close后所有数据库FD消失，以及读池等待取消/活读事务下独立WAL写入/释放后读取新版本。Linux负对照若未呈现预期结果，必须重新定位而不能据本段宣布根因实证。新增observer侧fd_diagnostic只在初始基线和失败退出记录链接，不进入资源判定、不重采基线或延长窗口。

本机Windows等待取消与读写分离回归通过；11h SQLite/PG两叶通过（7.672s），完整cmd/internal（含PG18.6）单测通过、覆盖95.9%，vet通过。LinuxFD回归在本机明确SKIP，不作为Linux资源证据。修正后清洁固定源码须按原完整G1/G4预注册新跑；现有run仍属于5668b53，不能重绑定到新代码。当前报告/KB/任务/门槛/图06和索引已联动，Issue回读仍待完成。

D18独立代码复审未发现阻断：读事务复用同一tx，无嵌套取第二reader；Linux回归检查实际文件链接与池状态，诊断只读子进程FD链接、不读取内容/环境或凭据，不改变清理预算。Linux机制实测仍待CI。
