# ITER-006：G1资源补证失败、修正与限定通过

2026-11-04 · 实现级实验。本未发布迭代现承接两个G1 run：`b7af58249eb57e3b32e0fd0622764a439a530d54`首轮 **observed/fail**，修正后`5668b5335f364df7ee182128d0154f9cb64d5643`重跑 **observed/pass**。旧失败、原summary与旧ITER-004/005全部保留；首轮内存诊断仍不计新run。新正式结果支持D16分片修正、D17 FIN回归及本次严格资源判定，但同提交G4出现新的SQLite资源反例，另归[ITER-007](p2-g4-resource-evidence-008.md)。当前 **G1 Inconclusive、G4 Failed**：不是把新G1 run改判失败，而是最终适用性和闭环尚不足，后续存储修复需要新固定源码再次验证，不能现在晋级。

## 输入与核验

| 输入 | 事实与用途 |
| --- | --- |
| [G1-P1-R2-20261104T064821Z-0830c3](../../experiments/runs/G1-P1-R2-20261104T064821Z-0830c3/report.md) | [Actions 41787910693](https://github.com/wyw-ai/loom-oi/actions/runs/41787910693)，subject=b7af582；本迭代首轮失败run，作为反例/不完整通过证据保存，非primary |
| [G1-P1-R2预注册](../../experiments/manifests/G1-P1-R2.json) | committed_in=b7af582，2026-11-04 06:47:19 UTC；SHA-256 `7ec98bab2262427cdb9da3669570449780eee0e888ecb960019f18060d4400c6`。run于06:48:21 UTC开始，最早Go事件06:48:54.428811661 UTC，晚于预注册 |
| [原分析器归档](../../experiments/sources/product-g1-r2-chunks/analyze.py) | 保留本run绑定的原始分析器字节；缺陷修复不得使其原失败摘要无法复算 |

下载原件10个文件的SHA256SUMS一致；SHA256SUMS自身SHA-256为`898c887f7b0e63577f327a9d6e6f68044b7fa26bcde22cffa49fdbc06943aeb7`。114项source-manifest逐项与Git b7af582对象核对一致，`verify.py --current-source`通过。它证明输入完整、原分析结果可复算，不证明解析方法无缺陷或产品通过。原raw为2,339,485字节，未经编辑。

实际环境为GitHub托管ubuntu-24.04、镜像20261026.354.1，2 CPU（AMD EPYC 9V74）、MemTotal 8122984 kB，Go1.27.3 linux/amd64、Python3.12.15、SQLite3.54.1、PG18.6（Debian18.6-1.pgdg13+2）。environment记录PG工具来自容器、small_fs=true；正式范围仍为受控回环，不是G5目标1C1G、HA或真实应用兼容。

## 首轮原始结果与失败边界

| 步骤 | 退出/耗时 | 原始结果 |
| --- | --- | --- |
| unit | 0 / 90.476 s | 299个PASS事件，完整cmd/internal覆盖率95.9% |
| race | 1 / 115.148 s | 298个PASS、1个FAIL：`TestCompletedExchange`；未出现`WARNING: DATA RACE` |
| vectors | 0 / 475.439 s | 267个PASS、4个SKIP；47个预注册向量ID通过。父子事件不是独立实验样本 |
| fuzz-frame / fuzz-control | 0 / 101.708 s、60.520 s | 两个目标各PASS，active分别60.10/60.05 s、均2 workers；步骤总耗时含构建，不能全记为active fuzz时间 |
| vet / staticcheck / govulncheck | 0 / 13.209 s、41.559 s、13.492 s | 步骤完成，无超时 |

runner自身exit0只是成功保存了实验产物，八步没有超时；它不覆盖race exit1或summary verdict=fail。原summary中四个G1 criterion为pass，“超时与资源清理”为fail，共280条problem（race exit1及资源记录缺失诊断），零容忍记录行数为0。没有DATA RACE告警不等于race步骤通过：这里是该调度下暴露的功能断言失败。

### D16：输出事件不等于完整行

Go test-json可把一条Output拆成多个事件。本run有631个无换行分片且均为1024字节，最长完整逻辑行1286字节；例如raw第3042–3043行将`TestTVB1_01_ReadOnlyHTTP/sqlite`的baseline拆为1024+164字节。旧分析器直接对单event执行OBS正则，漏掉跨分片baseline/sample。原summary仅识别14条baseline、39条sample，却识别115条begin和115条完成，因此报告缺profile/检查不足。此结果不能直接解释为产品资源泄漏。

独立诊断仅在内存按`(step, Package, Test)`拼接Output直到换行，再交给原分析器的既有严格资源规则：识别103条baseline、115条begin、581条sample、115条完成，无尾残片；98个test的115次资源检查通过，包含预注册81个必需leaf及至少97次检查。逐样本另核固定profile/角色/PID/2秒窗口及三连续规则：检查用时107–1157 ms，最小截止余量842,009,488 ns，最终三个样本goroutine相对基线为0..1、FD差均0；18次检查先有未归位样本后才满足，不声称581个样本一开始全部合格。47个向量ID、144个唯一必需父子Go检查均PASS，五项criteria在该诊断中通过，但overall仍为observed/fail，剩余唯一problem为race exit1、唯一失败测试仍为TestCompletedExchange。该诊断用于区分测量缺陷与产品失败，不覆盖原summary、不登记通过样本或恢复gate。

修正规格见[TASK-005 D16](../../spec/TASK-005-p2-g4-recovery/README.md#d16d17首轮补证失败后的修正规格)：按来源隔离重组，处理一事件多行/多事件一行、并行测试交错和末尾残片；解析OBS、fuzz active与零容忍标记均基于完整逻辑行。不得把残片静默丢弃、把不同测试合并，或仅修OBS而遗漏被切开的失败标记。保留真实raw样本及确定性分片反例，旧run继续由归档源码复算。

### D17：FIN完成状态发布存在竞争

raw.jsonl第2188行记录`tunnel_test.go:216: {Code:ChannelInterrupted Remote:false Up:11 Down:5}`；2189–2190为该测试失败事件，2317–2319为包失败。完整上行11、下行5与预期相同，最终原因却不是Completed。

源码审查定位`internal/tunnel/tunnel.go`（b7af582）：第286行Write(FIN)返回后，第290行才发布finSent；对端可先收到该FIN，随后回复DATA/FIN并结束WebSocket。downlink第241行发布finRecv，第243行仍可能看不到finSent，随后第209–212行读到EOF并错误选择ChannelInterrupted。真实WebSocket写在Flush后才返回，远端先响应并不要求本地写协程已完成状态发布。这是终态时序缺陷，不能通过sleep、重跑直到绿或仅放宽测试原因解决。

随后已实现确定性门闩反例并修复；本机完整单测与vet完成，5668b53新正式run的Linux unit/race结果见下节。门闩包装测试leg：FIN已交给对端但Write尚未返回；对端送回reply/FIN/EOF并确认EOF已被消费，再分别令Write返回nil/error。成功须Completed/Up11/Down5/零缓冲；写失败、缺对端FIN、ABORT或许可过期仍须失败。实现须区分FIN未开始/写入中/成功/失败；EOF遇“已收对端FIN但本地FIN写入中”只延后该完成判断，不能把仍可能失败的写预先当成功。真实WebSocket读到原始EOF未必主动关底层；该窄分支须在Bridge锁/once锁之外终止已失效leg，使阻塞Write退出，再依据实际返回值归约，不能引入无界等待。增加“写只能在CloseNow/原context取消后返回”的门闩反例；维持sync.Once首次终态，Completed提交时复核许可，已落定ABORT不被晚到写成功覆盖。读错误原许可检查保留，无新增接口或数值预算。

## 修正后正式重跑：5668b53的限定通过

新增输入为[G1-P1-R2-20261104T072920Z-9b6d51](../../experiments/runs/G1-P1-R2-20261104T072920Z-9b6d51/report.md)，[Actions 41792529139](https://github.com/wyw-ai/loom-oi/actions/runs/41792529139)，subject=`5668b5335f364df7ee182128d0154f9cb64d5643`。本run以primary用途支持该固定源码的G1判定，不能替代同源G4或后续修改的证据。E69/EDGE-040承接此新增来源，旧E67/首轮失败用途保留。

独立核验仓库10个run文件与TEMP下载逐字相同，全部SHA256SUMS通过，自身指纹为`96ba2c79dba1ad8991f0779e692cf58e81ebf97166cf75669936b3c7c6f430d5`；114项source-manifest逐项与5668b53 Git对象一致。`verify.py`精确复算原summary为observed/pass。当前工作树已有D18修改，故本次源码核验使用固定Git对象，不将当前工作树字节冒充该run源码。

预注册仍是b7af582于06:47:19 UTC提交的G1-P1-R2（SHA-256 `7ec98bab2262427cdb9da3669570449780eee0e888ecb960019f18060d4400c6`），未改变原vectors/criteria/steps及load。run标识07:29:20 UTC、首个Go事件07:29:48.041163435 UTC均晚于预注册。环境为GitHub托管ubuntu-24.04镜像20261026.354.1、2 CPU、AMD EPYC 9V74、MemTotal 8122984 kB、Go1.27.3 linux/amd64、Python3.12.15、SQLite3.54.1及PG18.6（Debian18.6-1.pgdg13+2）；small_fs=true、PG工具容器路径实际启用。没有1C1G、WAN、HA或真实应用兼容结果。

| 步骤 | 退出/耗时 | 原始事实 |
| --- | --- | --- |
| unit | 0 / 80.654 s | 309个PASS事件；完整cmd/internal覆盖率95.9% |
| race | 0 / 108.381 s | 309个PASS，无FAIL/SKIP、无DATA RACE告警 |
| vectors | 0 / 418.937 s | 267个PASS、4个已解释SKIP；47个唯一向量ID、144个唯一必需父子检查通过；负载两后端实际运行 |
| fuzz-frame / fuzz-control | 0 / 93.373 s、60.512 s | active分别60.09/60.08 s、各2 workers；步骤耗时不等于active时间 |
| vet / staticcheck / govulncheck | 0 / 10.556 s、32.886 s、10.881 s | 全部完成，无超时 |

四个SKIP分别是B1-17/postgres（该向量不依赖存储）、11f/postgres（SQLite长读）、B3-05/sqlite（PG不可达）、B3-08/postgres（SQLite设置）。它们不包含本轮必需的81个资源leaf，也没有跳过load或新增FIN测试。原summary五项criterion全部pass，overall=pass、problems/failed_tests/zero_tolerance_lines均空；runner exit0与这些判定分别核对。

独立从raw按(step, Package, Test)重组Output：597个无换行分片、最长完整行1285字节，末尾无残片，raw为2,297,207字节。资源OBS为103条baseline、115条begin、547条sample、115条完成；98个测试共115次检查，覆盖预注册81个必需leaf及至少97次检查。逐次复核profile固定、实际backend/OIC角色完整、PID有效且单次检查内稳定、采样先后与2,000,000,000ns绝对窗口；采样中的1566个进程快照没有负FD或零值进程伪成功。

每次末尾三个样本均满足OIS Preparing/Open/Closing/tickets与OIC channels归零、在线拓扑与固定基线一致、OIC Connected/Registered、FD精确相等和goroutine双向±10。实际末尾goroutine差0..2、FD差全0；完成106–1143ms，最小截止余量856,082,928ns。18次检查先出现未归位样本后才连续三次合格，不声称547个样本从开始都已归位，也不把115次检查当115个独立run。

[raw](../../experiments/runs/G1-P1-R2-20261104T072920Z-9b6d51/raw.jsonl)第853/2149行分别为unit/race的`TestCompletedExchange` PASS。`TestFINWriteOutcomeAfterLegEOF`在两个步骤都实际运行9个门闩子场景并通过：successful_write_not_yet_published、failed_write、blocked_write_released_by_close、missing_remote_FIN、license_expired_at_EOF、license_expired_before_write_returns、license_expired_at_completion_decision、Abort_wins_before_write_returns、Completed_wins_before_later_Abort；父测试PASS分别见957/2201行。该证据填补先前Linux race待验证项，支持实际写结果、许可及首次终因边界保持；不能据一次正式运行推导竞态发生概率或所有调度均已穷尽。

同源[G4 run](../../experiments/runs/G4-P2-20261104T072937Z-bebf95/report.md)在11h/sqlite资源清理出现FD17对基线16，正式结果仍fail；本G1 run内相同测试通过不能抵消该反例。G4原始核验、C46/E68/EDGE-039和D18定位归ITER-007，本迭代只交叉引用，不重复登记其输入或计样本。生产读池变更后，5668b53的通过只能继续作为历史固定范围支持，必须以修复后的固定源码重跑G1/G4。

## 结论、竞争解释与阶段影响

| Claim | 当前判断 | 证据/限制与可行动结果 |
| --- | --- | --- |
| C44 | Supported：首轮补证整体失败，分片解析缺陷妨碍原资源摘要解释，不能恢复G1 | 原run及字节复核为事实；内存重组仅诊断出完整严格资源序列。修分析器、新完整运行，不将原fail改成pass，也不把缺OBS解释成实测泄漏 |
| C45 | Supported（限定）：b7af582被测源码存在FIN终态发布竞争，完整交换可被误判中断 | 原race断言与源码轨迹支持；确定性反例已在b7旧源码overlay上失败，修复后局部测试及5668b53新正式unit/race通过，见C47。字节完整不能替代正确失败语义；不是Go DATA RACE报告，也不据单例推导发生概率 |
| C47 | Supported（限定）：5668b53的G1严格资源补证、D16重组及D17 Linux FIN回归通过 | 原raw、固定源码与预注册核验支持；只覆盖该提交和受控回环，不能覆盖同源G4资源反例、后续存储变更或尚未完成的Issue闭环，不据此晋级G1 |

项目承诺要求可解释的连接与失败。D17影响客户端/目标对完整交换与未知结果的区分，属于已采用B1双FIN完成语义；修复不新增自动重放或业务成功保证。D16是观测到摘要的测量边界，不能用分析器结构通过代替产品结果。C43原资源证据缺口由新的完整运行承担，不能因诊断已找到丢行原因就撤销全部未完成条件。

首轮正式失败时将G1由Inconclusive降为Failed是当时正确记录，b7失败与C44/C45历史不改。修正后的5668b53 G1 run现在通过，不能继续称FIN当前未修复或将新run说成失败。综合同源G4资源反例、D18受影响实现变更和未闭环状态，当前G1为Inconclusive、G4为Failed，下游仍不得放行；此限制与两次G1 run各自的fail/pass结果并存。旧6c7d5ad的G4四项直接支持只保留原固定版本，不用于新产品gate。

## 传播、下一步与交付状态

当前ITER-006保留首轮失败与诊断，加入修正后通过run为第二个且唯一新增G1样本；`new_samples=2`是本未发布迭代累计输入，不重复计G4或内存诊断。D16已实现输出重组并保留原分析器逐字复算失败；D17已在b7旧源码overlay确定性失败、修复后本机完整单测/vet及新正式Linux unit/race通过。C44/C45保留历史缺陷，C47记录有限修正支持，不删除旧反例或放宽任一判线。

当前`ITER-006.completion=pending-publication`。#3/#4/#11/#18本轮失败→修正→限定通过阶段尚未发布回读，尚未追加Issue阶段，不预造链接；保留全部旧正文、评论和回执。KB、门槛、任务、架构/图06及导航已联动；ITER-007负责同源G4失败。D18修复后须在新的固定提交完整重跑G1/G4，原始核验、综合分析、Issue回读及适用依赖全部满足后才恢复门槛。本报告没有宣布G1/G4恢复、修复后新源码已获正式通过、合入或部署。
