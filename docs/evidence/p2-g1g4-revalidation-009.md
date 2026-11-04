# ITER-008/009：TASK-006首轮G1失败与G4限定通过

2026-11-04 · 固定被测源码 `73bb667e9d1535a4ccee5eb15aa440ff636964b9`。本轮两个独立产品run均保留原件：G1 **observed/fail**，G4 **observed/pass**。当前G1为Failed，G4因G1前置未恢复而保持Inconclusive；尚未运行正式G3，也未合入alpha。旧ITER-004–007及其源码限定结论保持。

## 来源、原件与复核

| 范围 | 固定输入与原始结果 | 完整性 |
| --- | --- | --- |
| G1 / ITER-008 / E72 | [run](../../experiments/runs/G1-P1-R3-20261104T103502Z-186529/report.md)，[Actions 41815076141](https://github.com/wyw-ai/loom-oi/actions/runs/41815076141)，[G1-P1-R3预注册](../../experiments/manifests/G1-P1-R3.json) | manifest SHA-256 `867f6fed719a0cb2a7dc294340dd47e3f346b8a906d11196d5964ac3fa7dc91c`；SHA256SUMS自身 `6faff05e26f17fbc67ef0fc34b1246bbe9ae54e5bed0fe2bc7c5ca8e445c9f54`；raw 10,154行／7,058,654字节 |
| G4 / ITER-009 / E73 | [run](../../experiments/runs/G4-P2-R2-20261104T103505Z-af5f96/report.md)，[Actions 41815089605](https://github.com/wyw-ai/loom-oi/actions/runs/41815089605)，[G4-P2-R2预注册](../../experiments/manifests/G4-P2-R2.json) | manifest SHA-256 `7ace89f987de6e26f7537aed48cff68976e94529508ac5acbae96289e5e488f3`；SHA256SUMS自身 `fdc15fdadd37f90530dde5227863b89da6bc80a61c55b18dcafb15f65154d7b7`；raw 10,086行／7,045,778字节 |

两个manifest均已在73bb667预注册；运行只附加被测提交/预注册定位。每run的129项source-manifest逐项匹配Git对象，原始SHA全部通过，独立重新分析与原summary精确相等。入库副本逐文件保持下载原字节；失败原始判定不修改。ITER-008的输入用途为inconclusive，是检查器对失败产品输入的归档枚举，**不把实际observed/fail改成结果不确定**。

环境均为独立GitHub hosted Ubuntu24 Linux回环、2逻辑CPU、约7.75GiB、Go1.27.3、PG18.6容器和32MiB小盘夹具；G1 CPU为AMD EPYC 9V74，G4为AMD EPYC 7763。不是用户资源主机，也不证明G5容量或G7真实兼容。原environment中的网络说明含旧编码字形，保留原件，不影响已核对的数值/源码；不借机重写原始数据。

## C50：G1负载反例成立，资源回收不抵消失败

G1五项中四项通过，“超时与资源清理”因 `TestTVB4_06_Load/sqlite` 失败。父测试和leaf的两条FAIL是同一反例。完整单测与race各333项PASS、覆盖96.0%，两项fuzz各60秒、vet/staticcheck/govulncheck通过；48向量/165必需检查不构成失败豁免。

最后负载为32条并发、各请求64MiB。全部32条都进入READY，并各转发15字节请求；31条各返回67,108,864字节，一条返回0字节并收到IdleTimeout。该周期OIS relay_bytes=2,080,375,264，恰为31份完整响应加32份请求。SQLite全轮41份1MiB与40份64MiB成功，PG对应41与41份成功。

失败通道 `ch_s2sbfis7mpkkmik2e2hipptl6a` 的既有进程日志时间（UTC）：

| 事实边界 | 原始时间 |
| --- | --- |
| OIS open / READY | 10:44:28.272421531 / 10:44:28.552507701 |
| OIS首先记录closing，IdleTimeout | 10:44:30.792517324 |
| OIC ended，Up=0 / Down=15 / Remote=false | 10:44:30.844882590 |
| OIS closed / 消费者测试报错 | 10:44:30.864157689 / 10:44:30.879545691 |

READY至OIS closing墙钟约2.240秒。Remote=false可由OIS close_channel触发OIC Agent.abort先落终态解释，不能据此断言OIC独立触发idle。没有中途control断线、LicenseExpired或另一个close reason；结束清理的control lost不作失败起因。G1全部115次严格资源检查完成，556个sample，回稳105–1130ms；失败Load/sqlite六次资源回收也通过，但只证明回收，不能证明响应交付成功。

竞争解释尚不能区分：目标可能尚未收到请求FIN、可能在FIN之后尚未写出响应，也可能首响应已写而转发未完成。目标Program必须读到FIN才进入stream；Down15只证明请求DATA写入目标socket，Up0不证明目标未写。原件缺少consumer Write/CloseWrite结果、目标EOF/首写及OIS lastData/FIN实际转发时刻。周期2CPU总CPU约26960ms/墙钟14376ms显示较高调度压力，不证明发生了2秒调度停顿。原O3 mib_per_s按计划32份数据计算，失败周期142.459不得作为真实有效吞吐。

从2ec3670到73bb667，Load所经Dial→Stream路径未改变；CLI的Forward取消修复不是已证实起因。当前最佳行动是D19最小边界诊断和确定性门闩区分；保留idle=2000ms、并发32、载荷64MiB及资源判线。不用“环境抖动”豁免，也不以一次后续通过抹掉本轮反例。

## C51：G4原四项直接支持，前置未满足

G4原四项判定全部通过：SQLite规模维护拥塞/磁盘不足、备份重启恢复、并发版本冲突、旧任期拒绝。14向量/95必需检查、unit/race各333项和96.0%覆盖均通过，质量步骤通过，无失败测试。G4预注册load=false，因此它没有复现或反驳G1的负载反例。

另外对原raw执行只读补充资源分析：使用73bb667已预注册G3中的相同B4资源规则，103个资源完成、539个sample，回稳106–1141ms。该操作没有改变G4 manifest或summary，没有新增run/样本，不作为原manifest本来就有显式resources的虚构说法。原Go资源断言与补充核验共同限定支持该源码的所测恢复/回收，不覆盖负载。

保留双后端单实例恢复方案和D18固定单读池；G4当前不晋级Passed，原因是共同G1前置失败；本轮发布回读已完成但不抵消G1失败。D19若改变产品源码，需要重新评估并对最终源码复证。通过G4不允许越过G1运行正式G3或进入P3/P4。

## 项目影响、资源授权与后续

共同承诺仍是接对目标、守住授权、提供可解释且有界的连接；本轮反例说明一条已READY流未完成原生响应。它没有证明业务可安全重试，OI不新增自动重放或业务回滚。B1–B4、G2采用范围、API、错误语义、原资源判线和所有旧run保持；仅更新当前门槛、分析、任务进度、KB、图06和Issue阶段。

用户在会话 `call_f8848b5f1fc9465083fd35dba2f2bf2a` item0 回复：“没有，一样的1c1g的就用现在这个验证就行”。因此G5采用已提供主机，不再等待另一台整机1C1G；真实2逻辑CPU/约1.58GiB仍须记入环境，可选应用1CPU/1GiB配额不冒充含OS的整机等价。既有“随时可以用，能通过oic把bash的能力暴露出来就够了”承接G7 SSH→Bash样本。主机身份/接入配置留在本地ignored材料，不发布；未授权更改主机规格、重启或干预无关业务。

本轮新增样本精确为2，各归一个ITER。后续依次补诊断、区分反例、必要修复与固定源码复证，再执行正式G3；G2独立实现与真实样本准备继续，不宣称其门槛已通过。当前尚无新增产品取舍需用户确认。

## 发布与产物联动

已在[I3-P08](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6629179331)、[I4-P06](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6629190602)、[I11-P08](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6629199542)、[I18-P06](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6629209892)追加并逐字回读。四单旧正文/元数据及共34条旧评论保持，见[真实回执](../issue-history/2026-11-04/TASK-006-first-revalidation-published.json)。ITER-008/009标complete仅表示本轮分析/发布闭环完成；G1仍Failed、G4仍Inconclusive，未提升门槛。门槛与图同步Failed/Inconclusive；其余十张图及协议/权限/存储/概念的已采用契约无变化，理由记录在迭代。CI 41814866068的Python/Windows门槛检查因当前G1未通过而失败，六项全绿条件尚未满足，alpha仍为570b573，main保持原提交。

