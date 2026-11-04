# ITER-006：G1资源补证首轮失败与修正范围

2026-11-04 · 实现级实验 · 被测源码`b7af58249eb57e3b32e0fd0622764a439a530d54`。本轮只有一个新增run，原始结果为 **observed/fail**。补证没有恢复G1：既发现证据分析器丢失长OBS行（D16），也有真实产品交换在race步骤被误判中断（D17）。保留原run、原失败summary及旧ITER-004/005；内存诊断不改写原结果，不计作第二个run。当前G1记为Failed、G4保持Blocked；产品修复后须新跑G1和G4正式证据。

## 输入与核验

| 输入 | 事实与用途 |
| --- | --- |
| [G1-P1-R2-20261104T064821Z-0830c3](../../experiments/runs/G1-P1-R2-20261104T064821Z-0830c3/report.md) | [Actions 41787910693](https://github.com/wyw-ai/loom-oi/actions/runs/41787910693)，subject=b7af582；唯一新增失败run，作为反例/不完整通过证据保存，非primary |
| [G1-P1-R2预注册](../../experiments/manifests/G1-P1-R2.json) | committed_in=b7af582，2026-11-04 06:47:19 UTC；SHA-256 `7ec98bab2262427cdb9da3669570449780eee0e888ecb960019f18060d4400c6`。run于06:48:21 UTC开始，最早Go事件06:48:54.428811661 UTC，晚于预注册 |
| [原分析器归档](../../experiments/sources/product-g1-r2-chunks/analyze.py) | 保留本run绑定的原始分析器字节；缺陷修复不得使其原失败摘要无法复算 |

下载原件10个文件的SHA256SUMS一致；SHA256SUMS自身SHA-256为`898c887f7b0e63577f327a9d6e6f68044b7fa26bcde22cffa49fdbc06943aeb7`。114项source-manifest逐项与Git b7af582对象核对一致，`verify.py --current-source`通过。它证明输入完整、原分析结果可复算，不证明解析方法无缺陷或产品通过。原raw为2,339,485字节，未经编辑。

实际环境为GitHub托管ubuntu-24.04、镜像20261026.354.1，2 CPU（AMD EPYC 9V74）、MemTotal 8122984 kB，Go1.27.3 linux/amd64、Python3.12.15、SQLite3.54.1、PG18.6（Debian18.6-1.pgdg13+2）。environment记录PG工具来自容器、small_fs=true；正式范围仍为受控回环，不是G5目标1C1G、HA或真实应用兼容。

## 原始结果与失败边界

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

随后已实现确定性门闩反例并修复，本机完整单测与vet完成；Linux race和新正式实验证据仍待验证。门闩包装测试leg：FIN已交给对端但Write尚未返回；对端送回reply/FIN/EOF并确认EOF已被消费，再分别令Write返回nil/error。成功须Completed/Up11/Down5/零缓冲；写失败、缺对端FIN、ABORT或许可过期仍须失败。实现须区分FIN未开始/写入中/成功/失败；EOF遇“已收对端FIN但本地FIN写入中”只延后该完成判断，不能把仍可能失败的写预先当成功。真实WebSocket读到原始EOF未必主动关底层；该窄分支须在Bridge锁/once锁之外终止已失效leg，使阻塞Write退出，再依据实际返回值归约，不能引入无界等待。增加“写只能在CloseNow/原context取消后返回”的门闩反例；维持sync.Once首次终态，Completed提交时复核许可，已落定ABORT不被晚到写成功覆盖。读错误原许可检查保留，无新增接口或数值预算。

## 结论、竞争解释与阶段影响

| Claim | 当前判断 | 证据/限制与可行动结果 |
| --- | --- | --- |
| C44 | Supported：首轮补证整体失败，分片解析缺陷妨碍原资源摘要解释，不能恢复G1 | 原run及字节复核为事实；内存重组仅诊断出完整严格资源序列。修分析器、新完整运行，不将原fail改成pass，也不把缺OBS解释成实测泄漏 |
| C45 | Supported（限定）：b7af582被测源码存在FIN终态发布竞争，完整交换可被误判中断 | 原race断言与源码轨迹支持；确定性反例已在b7旧源码overlay上失败，修复后局部测试通过；尚无新正式run或Linux race结果。字节完整不能替代正确失败语义；不是Go DATA RACE报告，也不据单例推导发生概率 |

项目承诺要求可解释的连接与失败。D17影响客户端/目标对完整交换与未知结果的区分，属于已采用B1双FIN完成语义；修复不新增自动重放或业务成功保证。D16是观测到摘要的测量边界，不能用分析器结构通过代替产品结果。C43原资源证据缺口由新的完整运行承担，不能因诊断已找到丢行原因就撤销全部未完成条件。

当前将G1由Inconclusive降为Failed：正式run为observed/fail，D17是实际产品断言失败；即使D16内存诊断消除了资源缺失问题，race失败仍足以否定本轮通过。C43及historical_validation保留，不将真实失败继续仅标为测量不确定。G2/G3/G4及其下游均继续Blocked。D17要改生产传输源码，因此旧6c7d5ad的G4 run仅保留对其版本的历史四项直接支持；修复后必须以新固定源码完整重跑G4，不可将旧证据直接用于新产品gate。

## 传播、下一步与交付状态

当前保存本失败run、归档原分析器、D16/D17反例与修法、ITER-006和C44/C45。D16属于b0f0c7a已提交回读的D14测量/复算修正范围，输出拼接已实现，42项product回归通过；原run仍由归档分析器逐字复算为fail，当前解析器诊断资源pass而overall仍fail，包含仅剩OBS前缀/OBS换行等截断反例；D17属于已采用B1正常FIN契约的必要修复，可与本反例记录同提交，不额外引入一轮人为spec阻断。D17已以临时overlay精确映射b7af582旧tunnel.go，success场景确定性复现相同ChannelInterrupted/11/5及过早终态；修复后`go test -count=1 ./internal/tunnel`通过（2.332 s）。新增TestFINWriteOutcomeAfterLegEOF的success/failure/blockedUntilClose/missingRemoteFIN/expiredAtEOF/expiredBeforeReturn/AbortFirst/CompletedFirst及完成提交点许可失效共9场景，不用sleep；本机完整cmd/internal单测（含PG）通过，覆盖率95.9%（tunnel 97.8%），go vet ./...通过；42项product、84项experiments及35项tools回归通过。独立终态/锁序复审未发现阻断；Linux race和新正式run仍待完成。之后沿用G1-P1-R2预注册及原标准，以新固定源码和明确重跑记录运行G1，失败全部保留。G1验证、综合分析和Issue回读完成后才恢复；新G4正式运行另归属新的ITER，不能把本run重复计样本。

当前`ITER-006.completion=pending-publication`。#3/#4/#11/#18的新失败/修正规格阶段尚未发布回读，本轮不发Issue、不预造链接；保留全部旧正文、评论和回执。报告、KB、任务、门槛及索引已更新；架构拓扑、产品状态契约和B1/B2/B4采用契约不变，图06更新为G1未通过、G4需新正式运行；依赖边不变，无需新增图或修改冻结规格。没有G1/G4恢复、修复版本的正式实验证据、合入或部署事实。
