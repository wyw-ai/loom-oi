# 实验阻挡墙与阶段放行

依据[#11人类回复](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708)，随实际实现到关键节点执行实验；未通过不能进入依赖它的下一阶段。唯一状态台账为[gates.json](gates.json)，设计对应[架构v0.7](../docs/architecture/alpha-v0.7.md#delivery)。

## 依赖与验收范围

| 门槛 | 何时验证 | 必需证据 / 实验归属 | 当前阻挡 |
| --- | --- | --- | --- |
| G0 设计入口 | P1前 | K1职责、K2准入、双后端契约/范围、K5语言及测试判定有可追溯采用范围 | K1/R5边界、R/双后端要求及OIS/OIC首版Go已确认；H15已确认C1默认/C2支持/C3研究边界；R6线格式/状态竞争、安全与单实例存储契约、首个任务/验收基线已收束为[P1规格包](../docs/architecture/p1/README.md)，已按[ADR-004](../docs/decisions/ADR-004-p1-adoption.md)采用，设计证据与发布回读已完成，G0已通过；不代表产品实验通过 |
| G1 纵向闭环 | P1出口→P2 | 原B1–B4完整实现条件 | **Inconclusive（当前G2源码）**：alpha 15deeab所含bff7aa2新G1及ITER-010闭环保留；观测/准入与依赖变化后须最终同源复证 |
| G2 观测/准入 | P2出口→P3/P4 | LAB-005：完整准入矩阵、TTL/乱序、独立发布、控制公平性 | 三项契约已采用，产品与普通/clock采集分析已实现；Linux竞争反例修正、真实跳钟早检与正式同源运行未闭环 |
| G3 传输/失败 | P2出口→P3/P4 | 半关闭/背压/撤销/响应丢失/不重放及62资源阶段 | alpha 15deeab的正式run41845796987已失败：4条预演记录误标正式行为，原始Go功能断言与资源复算通过不覆盖此失败；已整合34场景代码，新G2源码仍须最终同源复证 |
| G4 双后端恢复 | P2出口→P3/P4 | 原B3恢复、审计与资源条件 | **Inconclusive（当前G2源码）**：bff7aa2/ITER-011的正式通过与发布闭环保留，待最终同源复证 |
| G5 目标资源 | P3出口→单实例或HA交付 | LAB-003资源切片：OIC目标1C1G预算/平台/真实负载；Go实现按已采用范围验证；语言对照只按新增明确范围执行 | 用户已明确以现有主机完成验证，不再等待另一台整机1C1G；如实记录2逻辑CPU/约1.58GiB及可选应用配额边界，负载与正式预注册待完成 |
| G6 PG主备 | P3出口→HA承诺 | LAB-006部署切片：入口、任期、分区、长暂停、旧实例、数据库回退/恢复，预注册RTO/RPO与失效窗口 | 缺主备实现、隔离拓扑与故障环境；SQLite本版不提供此部署 |
| G7 真实兼容 | P4出口→对应支持矩阵交付 | LAB-004：版本固定的真实组合直连/经OI对照、身份/流/取消与所需bridge | SSH/Bash首组真实样本已选定且只读接入成功；具体直连/经OI、原生身份/流/取消/未知结果驱动与正式预注册尚未执行 |

G1依赖G0；G2/G3/G4依赖G1；G5/G6/G7依赖G2/G3/G4。P4样本采集和原生直连基线可先做，但不宣布P4已通过。P4不依赖G6，单实例兼容不被无关的PG主备试验阻塞。语言对比未获批准不能偷偷删除资源门槛；资源验证由#11承接，#8保持已关闭的Go决定；具体范围调整须留准确决议和台账变更。

## 运行与解释

    python experiments/check_gates.py
    python experiments/check_gates.py --stage P1
    python experiments/check_gates.py --stage P2
    python experiments/check_gates.py --stage P3
    python experiments/check_gates.py --stage release-single
    python experiments/check_gates.py --stage release-ha

不带stage只校验台账，退出0不表示实验或阶段通过。带stage：2=依赖未通过，1=台账/证据错误，0=台账中全部依赖记录为通过；仍需核对适用版本、环境和语义结论。脚本不运行产品、不编造结果、不发送消息、不提供后台事件监听，也不阻止人直接绕过命令；执行AI须将它作为阶段检查入口。

## 状态与不可绕过的记录要求

条件状态（#11）显示为受阻/部分就绪/就绪；结果显示为受阻/未运行/未通过/结论不足/已通过。JSON保留既有机器枚举。条件就绪、命令退出0、设计文档完成或案例数量都不是实验通过；未通过与结论不足同样阻止依赖阶段。

Passed须具备：范围、代码/契约与环境版本；预注册判定及反证；可复核原始run和摘要；原始数据核验；实验后综合分析/KB/架构联动；每项criteria的通过解释；评审记录及所有证据文件SHA-256。`validation`保存`criteria_passed`、`reviewer`、`subject_version`、`environment`、`artifacts[{path,sha256}]`（设计规格可用`文件#sdd-scope片段`，与SDD采用范围一致）；G0用人类采用来源及设计评审记录，其余门槛必须有实现或部署级证据。

通过记录还须`completion=complete`及匹配门槛的`evidence_level`。G0需`decision_sources`；其他门槛需`iteration_id`/`run_ids`，绑定完成迭代与综合分析指纹、原始run校验和、manifest的证据级别/subject_version及environment指纹。先完成闭环与远端回读，最后才晋级Passed；失效/失败则立即降级。

检查器核对字段、依赖、闭环绑定和文件指纹，**不证明测试断言正确或报告内容真实**。语义审阅必须确认实验层级与覆盖面，不能把机制夹具填入实现门槛。代码/协议/负载/环境变化影响结论时将相关门槛退回NotRun或Inconclusive，并递归使下游失效，保留旧证据历史。

每个未过门槛保留具体阻塞、下一步、受阻阶段与来源；失败不删除。独立设计和准备工作可继续，依赖该证据的实施升级/交付承诺必须暂停。若人类缩小范围或暂缓，明确修改支持范围/依赖及影响；不得把Deferred改名为Passed，也不得让实验AI自行豁免。

## 本轮状态

2026-11-04（TASK-005/ITER-005时点，历史）：唯一G4主run在`6c7d5ad`为observed/pass，C39–C42保留四项直接结论；后续语义审查发现统计错误变零、仅验上界和故障后重采基线等缺口，C43撤回当前G1资源完整性结论，G1=Inconclusive、G4=Blocked。`historical_validation`完整保留旧G1验证对象，冻结run、ITER-004、原分析及已发布回执不改。`check_gates.py --stage P2`应退出2；不得修改CI阶段、吞掉返回值或将旧发布回执当成当前通过。

该阶段计划为TASK-005先提交并回读D14补证spec，再修夹具并以新G1-P1-R2预注册重跑全部五项原判定；其首轮run现归属ITER-006，旧run不重复计入新样本。严格采样、故障前固定基线、双向±10、FD精确相等及T_close+1s标准不放宽；不是已证明产品泄漏，也不改变B1–B4采用。G1在新run核验、综合分析与Issue纠正/恢复回读完成后恢复；G4须按下述ITER-006当前限定新增修复版本正式run并完成自身闭环。依赖实施晋级暂停，独立设计/物料准备可以继续，不能将G1补证后置到G3。

以下保留历次状态轨迹，不覆盖当前台账。

2026-11-04（TASK-004/ITER-004）：P1纵向闭环已实现。预注册G1-P1首run（`9a42f4e`）未通过并保留为反例；修正后主run（`f844b29`）observed/pass：47项向量检查、G1五项、覆盖率97.4%、race/模糊/静态检查通过，零容忍0。综合分析见[ITER-004](../docs/evidence/p1-g1-evidence-005.md)；#18/#3/#4/#11阶段评论回读后（[回执](../docs/issue-history/2026-11-04/TASK-004-g1-published.json)）G1记录为通过，P2入口`check_gates.py --stage P2`。G2–G7阻挡已按实现现状更新，仍未通过。

2026-11-04（DISC-008）：W28已明确采用B1–B4与R6承接范围，B4恢复95%。G0已在TASK-003完成设计证据、实际回归与62份远端文件回读后记录为Passed；最新结果以[gates.json](gates.json)为唯一台账。无需再请求相同人类采用。G1–G7尚无产品实现证据，本轮没有新run/ITER；普通工具单测不是产品实验。

P1只检查G0；G1通过后继续P2，满足真实阶段证据后自主晋级，不额外索取逐阶段许可。单实例交付不因G6/HA环境缺失阻挡；完整首版仍须release-single及其所有前置。连续执行与实际实验适配见[开发流程](../docs/sdd/development-workflow.md)。

### ITER-006首轮补证失败后的当前限定

b7af582的G1-P1-R2首轮为observed/fail，见[ITER-006](../docs/evidence/p1-g1-resource-evidence-007.md)。D16使长OBS在Go test-json分片处丢失；仅内存重组后115次严格资源检查通过，但D17的race TestCompletedExchange仍以ChannelInterrupted失败，不能恢复G1。G1由Inconclusive降为Failed，因为正式run已有真实产品断言失败；D16内存诊断不能消除race失败。historical_validation与C43保留，不将实际失败继续仅标为未知。G2/G3/G4及后续依赖继续Blocked。

D16在已回读D14范围内修复，D17落实既有B1双FIN完成语义；修正/回归与新正式G1运行完成后才评恢复。D17涉及生产传输代码，G4必须在修复后的固定源码新跑正式证据并新建分析轮，不能将6c7d5ad的历史G4通过直接用于新产品。原run、ITER-004/005、采用契约与旧回执保持；本轮Issue尚未追加回读。

### ITER-006/007：同源G1通过与G4资源反例

5668b53的新G1-P1-R2完整运行通过五项判定，修正后的FIN回归在Linux race通过；这取代“修复版本尚无正式数据”的状态，不改b7失败run。相同源码的G4新run为observed/fail：11h/sqlite审计功能断言完成后，固定OIS PID的FD从16变17、39个样本未在两秒内归位。三项criterion通过、SQLite维护criterion失败，原失败独立归属ITER-007。当前G4记Failed，G1因最终源码/共同资源保证及发布尚未闭合记Inconclusive；不把新G1通过改写成失败，也不据单一通过覆盖G4反例。

D18按既定B3/B4修复资源行为，维持故障前固定基线、FD精确相等和T_close+1s；必要存储修正后固定新源码完整重跑G1/G4。原run、分析器、预注册与历史采用保持；Issue回读和最终CI完成后才恢复门槛与合入。前节是首轮失败时点，现行状态由本节及gates.json承接。

### 2ec3670正式复证后的当前出口

G1-P1-R2-20261104T080816Z-0667d6与G4-P2-20261104T080816Z-097edc均为observed/pass，分别绑定原五项/四项判定；D18默认单读池精确回归、显式旧四连接负对照主库FD多1及store.Close后清零在Linux unit/race实际通过。新G1严格资源序列与新G4的11h两后端通过，全部原始字节/摘要按被测源码保留。此前失败时点由新结论C48/C49承接，不覆盖原raw。

新证据综合分析/联动与四单结果发布回读已完成，当前G1/G4均记录Passed，验证对象绑定固定生产源码2ec3670及对应新run，历史失败与旧版本支持保持。CI与alpha集成进度见TASK-005检查点；最终CI全绿后再按授权快进alpha。

### TASK-006：CLI取消反例与当前适用性

真实活动连接取消后等待超出诊断窗口，主动FIN后才结束；最小Forward生命周期修复及竞态回归见[TASK-006](../spec/TASK-006-p2-g3-path-faults/README.md)。当前G1/G4退回Inconclusive，validation仍原样保存2ec3670通过对象；旧raw、ITER和Issue回执不改。先固定最终修复源码，再完整复验与闭环；未把本机定向PASS写为正式门槛。

用户对目标资源补充询问明确答复“没有，一样的1c1g的就用现在这个验证就行”。现有SSH主机由此成为G5实际验证目标，不再等待替代资源；这不将测得的2逻辑CPU/约1.58GiB改写为整机1C1G，也不豁免其他资源与性能检查。若加进程配额，单独说明系统及其他进程在配额之外。准确测试规格与预注册仍待独立编制，G5尚未运行。

2026-11-04（TASK-006 D20 / ITER-010/011）：bff7aa2新G1-P1-R4五项、G4-P2-R3四项均observed/pass，完整unit/race各392项、96.1%覆盖与原严格资源/负载通过。原始复算及139源码Git对象核验完成；新结论等待四单追加回读，当前G1/G4 Inconclusive。独立CI41835698583的PG注册持久化500ms超时→写fail-closed导致B1-02准备失败保留，不伪称协议断言失败或根因已知；最终六项CI仍须全绿后合alpha。见[综合分析](../docs/evidence/p2-d20-revalidation-011.md)。

D20发布闭环完成：[I3-P09](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6632408825)、[I4-P07](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6632409487)、[I11-P09](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6632410179)、[I18-P07](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6632411148)追加并精确回读，四单元数据与38条旧评论保持。ITER-010/011完成后G1/G4恢复Passed；当前六项CI与alpha快进仍须另验，G3正式run尚未执行。

### TASK-007整合15deeab后的当前适用性

2026-11-04，TASK-006闭环提交15deeab的六项CI41842647134全部通过，已按授权快进alpha并回读，main保持63c1db9。TASK-007合入其D20缓冲/CLI取消修正和完整历史证据；控制v2/观测/准入/导出及依赖变化仍使当前G1/G4为Inconclusive。15deeab独立首次正式G3未通过，原件与修正由TASK-006闭环；不替代新G2源码的复证。上述旧段保留当时记录；当前状态以页首和gates.json为准。用户现有主机授权保持，无新增资源请求。
