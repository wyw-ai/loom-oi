# TASK-007 规格、实现与验证一致性核对

本表以 `fbda7ca4b550f6d070a0dea430d937c89e668ad9` 为当前已提交基线：此前 `5b51dfb` 从69708c9合入alpha `15deeab60e455a5e42032d5da5cf0cc543698118`，本次提交已包含query owner与clock归约仪器窄修；普通竞争仪器/分析器仍有未提交修改。D10 preflight截止修正与主collector已在此前提交中；当前仍无正式G2通过记录。路径/行号取本次可见源码，符号用于后续定位；并行修改须在最终提交重新绑定。下述分包、Windows开发、合成分析和旧Linux提交的通过不能组合成最终同源证据。

项目承诺仍是获准目标的发现、连接与明确失败边界：目录就绪不代替本次准入，READY 不证明业务成功，连接恢复不隐式重放业务。规格依据为[六个已采用片段](../../docs/architecture/p2/g2-observation.md)、[原始三项答复](approval.json)与[ADR-005](../../docs/decisions/ADR-005-g2-adoption.md)；本表不改片段、指纹、默认值或错误含义。初始G2实现曾合入TASK-005的 `570b573`，当前经5add63b继续纳入alpha d8bb599的D20有界消息缓冲、CLI活动流取消、D21及完整历史证据；TASK-005的FIN、单reader、恢复/维护与严格资源约束保持。当前G2树G1/G3/G4为Inconclusive、G2及G5–G7为Blocked；TASK-006首轮G3失败和019139f历史Passed均完整保留，不覆盖本树新源码的待复证状态。

## 1. 六个冻结范围与实现责任

| 冻结 scope／规格 | 实际符号与路径 | 已有验证与当前结论 |
| --- | --- | --- |
| `g2-probe`，§4：本地单源 HTTP(S)、地址许可、独立观测版本、attempt/sample/TTL | `internal/oic/config/probe.go:83` `Probe.compile`、`:132` `ContentDigest`；`internal/observation/plan.go:53` `NewPlan`、`http.go:18` `Execute`；`state.go:74/156/181/218` Set/Begin/Finish/Snapshot；`internal/oic/relay/reload.go:44` reload | Plan/HTTP/状态代数单测及 G2-01/02/03/04/08 开发链覆盖；CA 内容变化不依赖 JSON 改动，未采用来源不编译成 Ready。D3第三次真实OS跳钟仅完成297/400轮且失败，不能用observed_at或合成轨迹代替完整矩阵。 |
| `g2-wire`，§5：控制 v2、完整 binding、挑战查询、目录时效和授权过滤 | `internal/protocol/observation.go:57/406` ValidObservationBinding/DecodeControlForVersion；`internal/ois/server/ws.go:205` controlVersion；`internal/ois/relay/observation.go:85/173/234` pumpQueries/onObservationSnapshot/directoryView；`hub.go:292` Discover | v2 必填/null/枚举/大小写别名、v1 扩展兼容单测；G2-03/04/13 开发链。目录扣完整 RTT、同样本只收紧、历史字段不授予当前有效期；未通过正式统一来源复算。 |
| `g2-commit`，§6：OIC t_check 固定决定、OIS 最终权威、总 D/幂等/取消 | `internal/ois/relay/admission.go:57/251/403` OpenContext/createPrepared/onAdmissionResult；`channel.go:185/202/220` tryCommit/commitReady/commitCheck；`internal/oic/relay/observation.go:333/531` admissionReceived/allowReady | 双方门闩和 G2-05–08 开发链；最终决定不读新的目录健康值。D10已提交preflight创建前单CHECK期限防线；不是要求整个拨号期持续Ready。当前query owner交接另见D13，不能以健康冻结掩盖身份/期限错误。 |
| `g2-fairness`，§7：有界调度、独立发布、关键/遥测公平 | `internal/observation/scheduler.go:198/215/291` fillLocked/pumpLocked/execute；`internal/oic/relay/writer.go:110/249/284` offerUntil/expireQueuedQueryLocked/run；`internal/ois/relay/control_writer.go:45/73` nextControl/writeLoop | Scheduler 门闩与两端 writer 的 8:1/真实 seq/关键容量测试；G2-09/10/11 开发采样。病态负载仍可有限拒绝/断会话，未增加可用性或生产调度 SLO。当前D13补真实Write返回交接；D14因果仪器尚需完整六窗口重验。最终Linux饱和/资源验证未完成。 |
| `g2-export`，§8：默认关闭、回环、2 槽/64 KiB/200 ms、有限指标 | `internal/metrics/handler.go:29` ServeHTTP、`render.go:53` Render；`internal/oic/relay/reload.go:148` startDiag；`internal/ois/server/config.go:124` validateDiagnostics、`server.go:125` 诊断监听接线 | 独立 expfmt parser、真实慢 socket/截止/槽生命周期单测及 G2-12 开发链。发布可见延迟在生产端缺跨机单调观察者时不伪造值；正式证据由测试观察者测量。 |
| `g2-acceptance`，§9：G2-01–14、双后端、故障/饱和和真实时钟 | `tests/integration/vectors_g2_*_test.go`、`g2_env_test.go:45/137` newG2Env/settled；`g2_proxy_test.go` 真实 mTLS WS 代理；`g2_instruments_test.go` 仪器验证 | 普通22根/130叶、128叶归属106环境/profile；两个旧OIS拒绝新OIC叶不主张在线FD回收。D3另20叶×20轮；主collector/子分析器已接线并归档旧版本，正式manifest仍为ignored候选，当前仪器修正与完整Linux/正式闭环未完成。 |

三项已采用取舍在实现中保持：Commit-Snapshot 接受 t_check 后健康变化窗口；Probe-HTTP 不代替任意服务鉴权/业务正文检查；Export-Pull 合并最新状态，不保完整历史。没有选 Commit-Strict、主动上报源、可靠监控历史、任意命令执行器或新的服务托管职责。

## 2. 对外行为、权限与状态边界

| 契约／入口 | 实现与验证定位 | 结论及限制 |
| --- | --- | --- |
| 配置默认及覆盖 | `internal/limits/observation.go:27/33` Defaults/Validate；`internal/oic/config/probe.go:57/83` 严格解码/编译；`internal/ois/server/config.go:59` LoadConfig。`TestObservationLimits`、`TestObservationConfigBoundsAndExporterOptIn`、`TestObservationConfig` | 原 Probe 5000/1000/15000/500 ms、P4/Q32；query8/preflight32、CHECK/query1000 ms、遥测4096 B/100 ms/补取1000 ms。额度/单次期限按较小既有 B4 限制收紧；显式 null/非法值拒绝。实验值不是产品 SLO。 |
| Probe 网络权限与 TLS | `internal/oic/target/target.go:31` Dial；`internal/observation/http.go:18` Execute；`internal/observation/plan.go:53` NewPlan。HTTP 状态/失败、TLS 身份、读上界与 `TestProbeCompileRejectsUnsafeInput` | 本地允许网段、DNS 后逐地址核验、固定 HTTP/1.1、禁代理/重定向、8 KiB header/4 KiB 预读。关闭响应不额外 drain；probe 网络权限不由 welcome 扩张。 |
| 版本与 CA reload | `config/probe.go:132` ContentDigest、`config/config.go:280` Revision；`observation/state.go:74` Set；`oic/relay/reload.go:44` reload。`TestProbeCAContentAndFailedCandidate`、`TestReloadCancelsOldHTTPWorkBeforeNewAttempt` | 业务 target revision 与 observation revision 分离；CA 内容变更进入摘要；失败候选保留整份旧合法配置。A→B→A、删除重建不复用旧身份；新配置取消旧 work，不能恢复旧 sample。 |
| 目录 API 与 CLI | `ois/server/api.go:123` listExposures、`ois/relay/hub.go:292` Discover、`observation.go:234` directoryView；`client/cli.go:80` Main。G2-14、`TestDirectoryRTTExpiryAndHistory` | discover 过滤保留；原样展示 directory_snapshot/last_attempt/last_reported，不将 probe URL、地址、凭据加入公开视图或指标。目录 Ready 不保证 POST/业务成功。 |
| Open、相同 key 与 HTTP 取消 | `ois/server/api.go:136` openChannel 传真实请求 ctx；`ois/relay/admission.go:57/313` OpenContext/pruneWaiters。`TestAdmissionPendingMergeCancellationAndCredential`、LastWaiterCancellation、CancellationAlreadyVisibleUnderCoordinator；G2-08d | 同主体同 key/三参数合并，一次 preflight；16 waiter 上界，原创建凭证不被替换，返回前检查各自凭证。最后 waiter 取消在创建前释放；创建后取消不回滚 Channel。不保存失败幂等记录。 |
| 单 CHECK、总准备 D、模式收紧 | `admission.go:251/327/403`；`oic/relay/observation.go:280/333`。`TestAdmissionCompletionDeadline`、`TestFinalCannotExtendPrepareDeadline`、`TestObservationBudgetsIncludePriorStateWait` | 原 t_start 唯一确定 D，排队/合并/重发不续期。D10 在创建前以实际当前时刻复核单 CHECK，总 D 优先；模式只能收紧。未知结果不能降级成 best_effort。 |
| final、READY 与配置取消 | `ois/relay/channel.go:202` commitReady；`oic/relay/observation.go:531/559/567` allowReady/markCancelledLocked/closeCancelled；`agent.go:681` provide。`TestFinalSnapshotIgnoresLaterHealth`、`TestProbeReloadREADYOrdering`、`TestREADYNeedsFrozenDecisionAndLiveLicense`；G2-05–08 | t_check 后新健康样本不翻转固定决定；授权/目标/许可仍可终止。READY 本地先 Running 则 probe-only reload 不关旧流；先 Cancelled 则目标载荷为零，消费者可能先 READY 后 ABORT。socket/Bridge 操作在 Agent.mu 外。 |
| 失败码与首因 | `protocol/codes.go` HTTP/ABORT 映射；`ois/relay/admission.go:515` decisionReason；`ois/server/api.go:30` writeError；`client/client.go:91` apiError、`client/cli.go:113` stderr。`TestReadinessTerminalReasonIsPreciseAndFirstWins`、`TestReadinessErrorReasonHTTP`、G2-14 | ReadinessRequired=412/0x0107、ReadinessCheckFailed=503/0x0205、PrepareTimeout=504；有限 reason 保留，已落定终态不被迟到消息改写。传输损坏不能保证准确业务/新增码送达。 |
| 版本兼容与不降级 | `ois/server/ws.go:205` controlVersion；`oic/relay/agent.go:310` session、`handshake.go:14` controlHandshakeError；`protocol/observation.go:406` DecodeControlForVersion。G2-13、`TestControlHandshakeVersionUnsupported` | 新 OIS 接 v1/v2；新 OIC 仅 v2。旧 OIC 能力 Unknown，strict 412；旧 CLI 可能把新 ABORT 显示 ProtocolError，OIS GET 保留准确首因。旧二进制固定 alpha570b573；其他版本不自动进入支持矩阵。 |
| 管理、存储与恢复边界 | `ois/relay/admission.go:266` 新 Channel quota/store/maintenance 检查；`channel.go:220` commitCheck；`admin.go:229` enforce。`TestAdmissionFixedPreparationAndFinal` 在预检后设置 storeOK=false/maintenance=true 后仍验证既有 Preparing final | G2 不给已有 Preparing 增加维护/存储提交条件；已有 Channel 幂等回放保持。新 Open 在原边界拒绝；G2 状态仅内存，不新增 probe/sample 持久表。最终新源码仍须真实 G4，局部回归不能替代恢复证据。 |
| 有界回收与资源采集 | `ois/relay/control.go:219` endSession、`background.go:193` Shutdown；`oic/relay/writer.go:389` discardPending；`tests/integration/resource_helpers_test.go`（validateStatsJSON/resourceSampleOK/固定profile，settled现:366） 严格 JSON/边界/预冻结/绝对清理窗口 | 原双向 goroutine±10、FD精确相等、三连续样本和 T_close+1 s 保持。新 probe/writer 在预故障健康拓扑冻结，清理健康端点/idle 连接计入同一 2 s；Windows FD=-1 明确未测，不能作 Linux FD 通过。 |

## 3. G2-01–14 的真实测试与开发记录

下表的“开发通过”只表示相应日志中的实际 Go 断言/进程链结果，不表示同一最终提交完整运行；有些日志只覆盖场景子集。完整原始行为仍须由最终分析器逐条复算，不能仅用父 PASS 或把窄跑拼成正式样本。路径均为仓库 `coverage/` 下 ignored 开发文件，未登记为正式 evidence run。

| 向量／冻结判定 | 实际根函数与主要实现 | 已核开发事实／缺口 |
| --- | --- | --- |
| G2-01 / TV-B1-19：三状态×两模式，模式取交集 | `vectors_g2_readiness_test.go:18` TestTVG2_01_AdmissionMatrix；Table.Evaluate、双方 admission | `g2-matrix-03.log` 双后端通过；`g2-resource-normal-02.log` 补资源路径通过。包括真实目录/本地/请求收紧，不用目录缓存代替检查。最终 Linux 未完成。 |
| G2-02：Running/timeout 不替代旧有效正/负样本 | `vectors_g2_readiness_test.go:112` TestTVG2_02_AttemptDoesNotReplaceSample；Table.Begin/Finish/Snapshot | `g2-attempt-01.log`、resource-normal-02 双后端通过；状态 core 单测核一次 expiry Effect。D9 保留测试主动消费 Effect 的失败与修正。 |
| G2-03：低序/重复/冲突/旧绑定 | `vectors_g2_identity_test.go:357` TestTVG2_03_SequenceAndBinding；OIS onObservationSnapshot/acceptObservationIdentity | `g2-identity-03-01.log` SQLite 通过；`g2-identity-pg-01.log` 含 PG 两场景装配失败，`pg-retry-01` 仅重验该两场景通过。不能据此称同源整轮全绿；Injected/Dropped 必须按代理原始标记识别。 |
| G2-04：query/response 延迟、完整 RTT、缓存过期/迟到、真实墙钟 | `vectors_g2_identity_test.go:491/581/637` QueryTTL/ObservedAtIsDiagnostic/LateQueryCannotRevive；OIS query/deadline；新增 `vectors_g2_clock_test.go:18` RealClock | `g2-identity-04-01.log` SQLite 和 pg-01 对应根通过，final-01 两后端 late 通过。observed_at±一年只证展示字段；D3第三次实际REALTIME±30s早检完成297轮后失败，12个CHECK/D叶240轮完成，8个sample叶未通过；当前归约门闩和精确整数窗口修正尚未重跑真实钟。 |
| G2-05：preflight Ready 后变 NotReady，final 必须重查 | `vectors_g2_commit_test.go:228` TestTVG2_05_FinalRecheck；tryCommit/startAdmission/onAdmissionResult | `g2-commit-03.log` 双后端通过；原 commit-01 失败保留。不是从旧 preflight 直接发 READY；最终统一回归待完成。 |
| G2-06：t_check 后新健康不翻转，原冻结样本仍受期限约束 | `vectors_g2_commit_test.go:302/874` CommitSnapshotWindow/ExpiredFrozenReady；OIC admissionReceived/allowReady、OIS final | `g2-commit-08.log` 固定窗口双后端通过，resource-commit-03 原样本到期双后端通过；`g2-final-directory-counterexample.log` 的错误“目录 NotReady 翻转 final”overlay 确定失败。 |
| G2-07：授权/目标/代数/许可等安全条件独立重核 | `vectors_g2_commit_test.go:726` TestTVG2_07_FinalSecurityRecheck；commitCheck/allowReady/enforce | `g2-commit-07.log` 对应双后端窄复验通过；commit-04/05 原失败保留。不能把健康冻结解释为冻结授权。 |
| G2-08：消息取消、配置版本先取消、Running 后 reload、在途幂等 | `vectors_g2_commit_test.go:447/651/355` 08a/08b/08c；`vectors_g2_identity_test.go:145` 08d | 08a 在 commit-03/06/07、08b 在 commit-04/05、08c 在 resource-commit-03、08d 在 identity-08d-02 有真实通过；晚到/重复/先后为各场景门闩，必要竞态20轮。各版本不得拼成正式一轮。 |
| G2-09：另一慢目标不阻挡独立发布 | `vectors_g2_readiness_test.go:156` TestTVG2_09_IndependentPublication；reconcileProbes/markDirty/pumpQueries | `g2-publication-06-valid-paths.log` 双后端20轮用已采用500/200/50/1500 ms配置通过；resource-pressure-04 补回收通过。visible−fastHeaderStarted 是完整归约→可见延迟的保守上界，不冒称内部归约纳秒测点。早期非适用加大timeout/URL夹具记录保留。 |
| G2-10：P2/Q4/16源、14慢目标、公平等待≤2500 ms | `vectors_g2_pressure_test.go:13` TestTVG2_10_SaturatedFairness；Scheduler due/queue/实际start | `g2-saturation-01.log` 双后端3×60 s通过；resource-pressure-04 是资源窄回归。原定 due→实际执行的等待才有效，不以timer晚回调重置等待。实际max等待/oldest due age已接入有界诊断；最终同源Linux仍待。 |
| G2-11：遥测/导出饱和下关键 heartbeat/revoke | `vectors_g2_competition_test.go:358` TestTVG2_11_ControlExportCompetition；双方 writer/metrics | 旧03完整开发轮FAIL与04窄复验保持；新causal-05因双hold等Closed形成仪器互等而FAIL；06的PG/round2 118cycle行为通过但SQLite输入失败；07 SQLite round2通过、PG cycle20缺429失败；08仅round2双后端Go PASS，但PG cycle42原始因果FAIL（control释放早于其实际Received约0.57ms）。当前补真实gate命中见证，尚无最终六leaf同源通过；所有1s输入/500ms控制预算保持。 |
| G2-12：实体变化标签固定、慢抓取有界 | `vectors_g2_export_test.go:354/419` EntityCardinality/SlowReaders；metrics Handler/Render | `g2-export-02.log` 四叶通过；`g2-export-slow-04.log` 加强“主动 Close 前 timeout 已增2”后双后端通过。独立 expfmt 解析13 families/295 series，实测最大21245 B；32实体变化不增标签。外部1 s观测窗不能冒称测得服务端Write恰200 ms。 |
| G2-13：固定旧二进制真实组合、v2非法输入 | `vectors_g2_compatibility_test.go:48/205` Compatibility/V2WireRejection；version decoder/握手分类 | `g2-compat-03.log` 两根全通过、04是三个非法消息场景窄回归。旧alpha570b573源码/binary哈希记录；02中的v1 readiness_view=nil断言属夹具过强，Unknown兼容不能要求旧结构。newOIC丢 VersionUnsupported 属真实产品诊断缺陷 D7。 |
| G2-14：目录、准入、第一原生响应、业务结果分层 | `vectors_g2_readiness_test.go:241` TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate；API/CLI 原始字段 | `g2-diagnostics-01.log` 与 resource-normal-02 双后端通过；正常响应、就绪拒绝、连接失败和业务含义分开，不用 Channel Ready/中继字节宣称业务成功。 |

仪器另由 `TestG2ProxyRawBarrierAndStage`、ProviderReadyAndExplicitInjection、IdentityReconnectAndCloseHeld、CapacityAndCancellation、HandshakeFailureFidelity、TransportBoundBeforeTLS、DropIsExplicitAndOrdered 及真实 Health fixture 验证。代理保留 Raw、ConnID、序号、方向、received/written/held/drop/injected；原日志大行按 Package/Test 完整重组，不能静默丢分片或把注入值当真实 OIC 观测。资源反例根 `TestG2ResourceBaselineRejectsActiveProbe`、`TestG2ResourceDeadlineIncludesPreparation` 不替代真实 Linux profile 回收。

## 4. 缺陷、取舍与验证缺口台账

D0/D1 继续指 README 的准备/准确采用阶段；下表复用已有 D2/D3，产品问题从 D4 起，不改任何旧编号或来源。README D1及采用段的“G1/G4前置已恢复”描述进入实施时已集成570b573的历史状态，不覆盖本轮产品/依赖修改后的当前Inconclusive。

| 编号／状态 | 具体问题与修正／取舍 | 反例与验证边界 |
| --- | --- | --- |
| D2 已完成记录修复 | 原检查器不能表达真实 tool-call 用户逐项答复；增加 user_response/items 精确关联，不伪造 chat UUID、不用 AI 评论代替人类 | 4f12fea 的39项tools与完整工具分母98.70%，采用/回读见README；不是产品门槛证据。 |
| D3 开发原始复算已通过，正式G2仍Blocked | 04/fbda的专用VM实际400轮/恢复均完成；原分析失败保留，9f3c825修正FD诊断、跨轮health及整数失权界后对同raw诊断复算400轮/400资源窗口pass | 603源哈希与305原件exact，全部guard原件保持；15项clock及主collector反例通过。不同分析器的诊断不是原source正式pass；最终ordinary与clock必须同一新正式run，不在本机/用户主机改钟。 |
| D4 已修，有限HTTP读取 | 缺少 Request.Close 时 Transport 关闭response会额外读取；固定关闭策略保持4 KiB预读、不drain | `g2-http-close-counterexample.log` 去掉Close的overlay实得12288 B/2次read并FAIL；HTTP读取边界单测通过。该反例是负对照，不是生产业务根因推测。 |
| D5 已修，OIC原始期限/冻结决定/锁序 | 接收→等锁/排队不得重启预算；判断使用锁内当前时刻，final固定ID不被重复检查翻转，READY放行/取消共用归约序列 | `observation_boundaries_test.go:26/48/76/109/132/196` 与 rejection/lifecycle 门闩、ordering-20日志；属于已采用语义落实，没有扩大健康保证或新增预算。 |
| D6 已修，queued query主动回收 | 原队列中已超时/取消query长期占槽；按identity主动回收，旧callback/release不能删新owner，实际socket写入由writer完成释放（selected尚未写的回收见D13） | `g2-query-expiry-negative-overlay-02.log` 旧源确定等待回收超时；`writer_expiry_test.go:17/79/115`、positive-01及全B回归通过。未过期满槽/duplicate仍拒绝。 |
| D7 已修，兼容握手诊断 | 旧OIS实际400 VersionUnsupported被newOIC泛化为WebSocket失败；只解析完整<1024B合法公开code，固定错误提示且关闭body | compat-02 原真实失败保留；`handshake_test.go:29/38`与compat-03修后通过。不得打印任意peer body或回退v1；v1视图nil的旧夹具错误另行纠正，不冒称产品缺陷。 |
| D8 已修，有限最终原因与首因 | GET最终原因把已知timeout/observation_changed泛化unavailable；分离公开decisionReason与固定指标枚举，迟到通知不覆盖终态 | `TestReadinessTerminalReasonIsPreciseAndFirstWins`、严格TTL expired及真实08a复验。没有增加未收到事件的远端诊断保证。 |
| D9 已修，测试不消费被观察状态 | full-unit-04 中expiry测试主动Snapshot而丢Effect，先于timer消费一次expiry，导致真实Changed等待超时 | 原失败保留；改为先被动等Changed，再wire query核expired/metric=1，10次窄验及full-unit-05通过。只改测试观察方式，未放宽期限/改产品expiry。 |
| D10 已修并已提交，preflight消费前单CHECK截止 | 旧`admission.go:256`刷新now后只查总D：入口999ms、CHECK1000ms、创建读1001ms仍给Preparing/票据。新增`:263` guard置总D后/额度前；v1无check保持 | `coverage/g2-independent-review-01/`固定804f703旧admission字节与overlay：旧源码恰到/越界两反例FAIL，总D对照正确504。新 `TestAdmissionCompletionDeadline` 六子项+v1根及20次重复PASS；撤销401/取消优先、503 timeout/504、零资源泄露。只修本次检查消费边界，不要求整个拨号阶段持续Ready。 |
| D11 开发输入闭环已复算，Linux完整验收仍待 | causal09缺完整慢输入窗口，原件与旧分析保持；causal10明确最后Set+60s、原3s实际返回和prepared后全部cycle，716cycles六叶原始复算pass | 严格两slow socket/实际429与32Drop上限、原1s/500ms/2s保持；同一Windows完整运行不是LinuxFD或正式G2。全部早期失败与精确时序反例保留。 |
| D12 工具接线已实现，正式证据仍Blocked | 主collector按固定step/package重组D16完整OBS，普通130叶/clock20×20均依赖同raw的严格B4；clock同source/nonce/guard审计并要求ordinary companion。verify先静态读取固定依赖白名单，再核相邻原字节归档，不借当前子模块重算历史 | `analyze.py:23/258/1332`、`g2_analyze.py:1151`、`g2_clock_analyze.py:278`、`verify.py:16/62/91`。19项主接缝、400合成资源/时钟正例和破坏反例；合并后16个历史run精确复算。仅G2新增fixtures步真实执行三health根，不缩unit分母。尚无正式manifest/同源G2 run，当前窄修仍需最终统一测试。 |

| D13 已修，fbda7ca已提交，query实际写入交接与selected回收 | peer已完整读snapshot并补新唯一query，本地Write返回尚未调度导致旧owner未释放，原实现误判第9槽；selected但未写的取消项也可占槽。`writer.go:110/236/249/284`在原8槽内按owner身份交接，锁外等待min(新query原D,旧实际Write原D)，不续100ms、不预发seq、不无限等待 | 真实WS/Write返回门闩：`writer_handoff_test.go:90/177/248`；旧writer SHA7cef04b0cf455b73729118a53b9f4465d88227df6d63393b31f84ebc805959a5的overlay两个反例FAIL，`query-review-overlay/old-source-regression.log`保留。新writer roots及三个期限/取消分支、完整OIC包通过；原Linux离线日志缺完整query ID，不能据此宣称此机制唯一解释旧CI。fbda的Linuxunit/race已通过；最终同源整轮仍待。 |
| D14 撤销因果仪器开发通过，正式验证仍Blocked | 双hold先捕获control促成的真实OIC→OIS/provider ABORT，再释放下行完成drain；完整history核原controlReceived→OICClosed+1≤500ms，不clamp负间隔 | causal10六叶716cycles新严格复算pass，最大4.4916ms；46项ordinary分析回归包括prepared后cycle与精确反例。05–09原件保持，不借Go父PASS或WindowsFD未测冒充Linux正式矩阵。 |

本次独立代码审阅覆盖OIS admission/query/注册替换/control writer、server版本/API/诊断、client错误转交和limits/metrics/protocol；D10的只读发现→旧源overlay确定反例→窄修→回归链完整。此次文档更新另核当前符号/实际日志与D13/D14/D3交接；没有把所有并行未提交变更称作已全面二次审计。D3/D11/D12/D14的待验证项仍阻挡G2。D13的锁序由另一代理独立审阅未见高置信阻断，但这不替代最终Linuxrace或完整正式路径。

## 5. 最新原始证据接缝与失败范围

### query owner与绝对期限

D6的主动回收不能单独证明真实写入交接安全。D13的两个独立确定反例分别覆盖“peer完整收到snapshot后补新query、本地Write尚未返回”和“selected builder已取消但未写”。旧源码overlay两项FAIL保留；新实现只在已存在真实writing owner时等待其原截止内交接。等待不持writer.mu，queryReceived先释放Agent.mu，builder在writer锁外取Agent.mu；旧回调必须按owner指针匹配，正在socket写的owner仅由实际writer完成释放。真实未过期满槽且无writing、重复query仍拒绝，入队等待不消耗seq。`g2-query-handoff-03.log`的writer根/三个期限分支与`g2-query-handoff-oic.log`完整OIC包均PASS（relay9.494s），未运行新Linuxrace。原CI离线缺完整query ID轨迹，不能将该已确认机制倒写为其唯一根因。

### 普通G2-11撤销因果

provider下行ABORT和consumer ABORT可早于control CloseChannel，consumer只约束于`revoke_started_ns..backlog_after.completed_ns`，不把负间隔钳为零。两门闩在revoke开始前arm；真实下行Revoked ABORT与control CloseChannel都捕获后才放控制；下行仍hold时，真实OIC→OIS/provider ABORT0106证明本地首终态由控制促成，然后释放下行完成drain。`control.WriteStartedNS ≤ witness.ReceivedNS`且control实际Written>0；不强迫远端回应晚于本地Write返回。最后OIC Closed[Revoked]恰增1、同session，原control Received→completed≤500ms；完整链含回收/Cancel仍在最早slow socket connected起1s内。

`competition_revocation`复核完整原始history：同victim/ConnID/方向/码、非Injected/Dropped、最终快照相等；所有同channel下行ABORT均不得在release前开始Write，允许真实closed-before-write而不伪填Written。provider最终Closed/Workers0证明两个pump已join。40项ordinary单测包含这些正反例。causal08虽Go两叶PASS，PG cycle42的真实control Received晚于release约0.57ms仍被严格拒绝；当前Go已补controlGate.Wait。causal09六叶Go与旧分析通过后暴露慢输入实际窗口证据缺口；新causal10及其完整原始复算见末节，不把06–09窄跑或旧解释拼为正式通过。

慢导出输入使用连接前SO_RCVBUF请求与真实两socket写完成/429，不把设置值称作实际TCP窗口；两个准备阶段共用原3s，cycle输入/回收仍原1s。观察者让位20ms也消耗该窗口，产品200ms写超时未改。一次性matcher只回收不再活动项，保留barrier对象与完整wire历史，原128个同时活动门闩上界保持。新增输入/相位/门闩验收根须在最终manifest枚举时明确，不用fixture包或父PASS隐式覆盖。

### 真实clock03与两项仪器缺口

[第一次41840152659](https://github.com/wyw-ai/loom-oi/actions/runs/41840152659)和[第二次41841842228](https://github.com/wyw-ai/loom-oi/actions/runs/41841842228)均在NTP准备阶段失败、0个child_started/shift；masked状态与canonical alias分别窄修，原件保持，不能算产品跳钟通过。第三次[41845999913](https://github.com/wyw-ai/loom-oi/actions/runs/41845999913)固定2311c25：ZIP SHA-256 `5ef0ad3271da2c43a5dc9f40cc01783b30b5bd1583668f92551233f645909edf`，305个下载文件逐字核验。实际297次shift、298次restore（含最终恢复），completed=297、exit=1、restored=true，NTP原状态恢复。12个preflight/final/total-prepare叶完成240轮；8个sample叶未通过，整轮不是400轮通过，且不是正式G2 run。

已确认两项测量缺口，**不是已确认8叶同一产品根因**：

1. proxy完成WebSocket Write不能证明OIS已在协调者中归约本条snapshot；同样本后续query合法扣减RTT可能收紧旧firstview。`g2_clock_reduction_test.go:45`在原query观察截止内等待真实`oi_observation_query_rtt_seconds_count{result="success"}`恰增1；其更新位于OIS目录写入/dropQuery之后。完整before/polls exposition、HTTP起止与query身份保留；原始history仅允许该binding、期间无另一snapshot写入，不借全局计数误认他源。next guard只arm，不等待下一次sample命中。超时/错误/跳增/重置不重试续期。
2. `remaining_ms=r`向下取整且0即Unknown，原权威失效位置应在`[viewStart+(r−1)ms, viewEnd+r ms)`；`g2ClockAuthorityWindow`(:89)及clock分析器按该整数区间核验，不给TTL加1ms容差。每次sample失败立即记录完整阶段/firstview/轨迹，`real_clock_sample_failure`不能被忽略成通过。旧03缺完整firstview轨迹，所以不能事后补造其8叶具体时序。

fbda7ca的修正由`g2-clock-reduction-python-03.log`记录32项PASS（clock13+collector19，含全400合成轨迹/资源）；`negative-02.log`的17类针对反例PASS。`g2-clock-reduction-go-03.log`在OI_IT=1/OI_IT_CLOCK=0实际运行3根9子场景PASS，真实clock根为明确scope skip，integration包编译通过。Go01未开OI_IT导致包跳过、Python02错误cwd导入失败均保持。以上没有调用真实setter，不等于第四次早检或正式20叶通过；本次核对没有新成功实际clock矩阵。

主collector在同一raw绑定普通与clock：唯一40位source fact、三个严格封套、guard原JSONL哈希、同job/nonce/UID/boot及B4资源；clock仅证明两端原TTL失效的窗口，释放旧snapshot后不复活还必须同一正式run ordinary G2-04支撑。旧12run与合并后16run均按记录分析器字节精确复算；G3约1000行独立分析与G2路由同时保留。后续归档G2主版本必须同目录保存main、g2_analyze、g2_clock_analyze、clock_guard四模块并逐项匹配source-manifest。当前新G3失败原件由TASK006隔离工作树收尾，本树不凭缺失的新本地报告路径作引用。

## 6. 产品文档、模块与图

| 产物 | 与实际行为的核对 | 当前处置 |
| --- | --- | --- |
| [产品参考](../../docs/product/reference.md) §1–8 | OIS/OIC配置、HTTP v1、控制v2、原tunnel.v1、有限reason、目录/准入、显式metrics开关及预算均有实际入口 | 保持“已实现、尚待正式验收”；无复杂鉴权probe/任意远程执行/新生产SLO。 |
| [运维](../../docs/product/operations.md)、[产品入口](../../docs/product/README.md) | CA/reload、未知/过期、取消/中断及业务结果分开；G2不改变备份/迁移/维护责任 | TASK006的CLI取消/消息缓冲及历史证据已由5b51dfb合入；当前新增G2语义仍需最终统一质量与正式证据。 |
| [OIC](../../docs/modules/oic/README.md)、[OIS](../../docs/modules/ois/README.md)、[消费者](../../docs/modules/client/README.md) | OIC本地权限/probe/归约，OIS目录/授权/最终权威，客户端显示/原生流；无业务执行器或业务成功推断 | 当前页已写G2开发验证与G1/G4适用性Inconclusive，2ec3670/bff7aa2历史通过完整保留，不外推当前新源。 |
| [架构](../../docs/architecture/alpha-v0.7.md)及[图册](../../docs/architecture/diagrams/v0.7/README.md) | 图03为C1概念准入，详细preflight/final/取消固定点以G2§6为准；图06表达当前门槛依赖而非实现测试结果 | 审阅发现架构导航曾保留“尚未实施/前置已恢复”，主流程已同步为实施中、当前G1/G4 Inconclusive，并仅刷新相关Pending指纹；六冻结scope不改。最终图与渲染视觉QA由主流程收尾。 |

## 7. 质量事实与完成出口

804f703附近的完整Windows开发 `g2-full-unit-05.jsonl/.out` 为6746/7097条语句=95.05425%；包含显式PG，唯一Linux专属FD项skip，full-unit-04的失败保留。该完整分母不是新增D10及所有并行改动后的最终统计。D10当时的 `g2-preflight-deadline-affected-01.jsonl/.out` 仅完整OIS relay/server两包：112根/102子项、0skip/0fail、子集96.0%；vet、diff、UTF-8/LF通过，不能替代全部cmd/internal≥95%。本机Windows缺cgo导致该次race未运行，不记本机race通过。主流程另核固定804f703的 [CI 41833218575](https://github.com/wyw-ai/loom-oi/actions/runs/41833218575)：Linux unit/race job126585415912通过，完整6776/7124=95.115103874%；fuzz job126585416194通过。四平台构建完成，但build-lint曾因gofmt失败（工作区已修）；python/Windows按G1 Inconclusive正确阻挡，integration后来失败：B1-17/sqlite旧fake只协商v1被新OIC正确拒绝、未进入乱序断言；G2-11五叶另有失败。因此该CI并非全绿，失败原件保留；v2 fake已窄修并定向通过，不改原register前/错误generation零dial判线。该CI也不覆盖其后D10及当前并行改动；新state fuzz本机active 60s/1003376次是独立开发记录，仍须最终固定源复核。

后续完整Windows开发`g2-full-unit-06.jsonl/.out`包含显式PG，为6750/7099=95.08381%、1458个根/子场景PASS、0FAIL、1个Linux专属FD skip；它仍不覆盖fbda7ca及当前竞争改动的最终完整分母。05caa54的CI41841782565在unit通过后，race的metrics测试因客户端读完早于handler Flush返回而观察到success=0；用graceful Shutdown确认handler完成的窄回归100次通过，产品200ms和计数逻辑未改，旧race失败保持。69708c9工具接线检查点tools39/experiments182通过；合并G3与G2主路由的147项product测试、16历史run复算通过，随后ordinary新增到40项及clock修正32项均为局部后续验证，不能据此声称当前整树统一全绿。

当前合并已保留TASK006所有D20/FIN/CLI取消/存储修正与旧run/ITER/回执，未回退产品；最终质量须按本工作区固定源重新执行。主流程在本次文档更新期间并行收尾，后续运行/提交事实应追加具体版本，不把此刻“进行中”覆盖成历史失败不存在。

最终出口仍需：

1. 固定全部产品/依赖/测试/clock/analyzer源码，完成完整cmd/internal同分母≥95%、Linux race、规定fuzz active时长、vet/staticcheck/govulncheck、四平台构建与Windows CI；新增fuzz不能只以种子PASS代替active运行。
2. 独立核新分析器拒绝缺失/skip/失败/畸形或分片未完成OBS，绑定所有真实根/子场景、包名、资源与clock身份；manifest和脚本先固定、提交回读，才运行正式证据。
3. 当前源改变了控制/准入/回收与模块依赖，G1/G3/G4适用性为Inconclusive；TASK006最终D20/CLI/D21代码及历史原件已合入，最终G2源码的G1/G4/G3复验均未完成。独立15deeab首轮G3 [41845796987](https://github.com/wyw-ai/loom-oi/actions/runs/41845796987)为observed/fail（辅助预演日志误标）；019139f的新G3与[ITER-013](../../docs/evidence/p2-g3-evidence-013.md)闭环仅证明该固定源码，两轮报告均已合入。旧验证、run/ITER与回执完整保留，不改subject或摘要来覆盖新源码。
4. 完整G2双后端/全部20轮竞争/3×60s窗口/真实OS跳钟与Linux固定profile精确回收，不能把分支、窄跑或不同源码阶段的成功拼成一个run；失败保留并按原阈值解释。
5. 固定run的原始复算、独立审阅、报告/ITER/KB/图文/Issue回读和门槛闭环后才晋级；当前工作区合并alpha d8bb599不等于把G2交付回alpha；不以本文或结构检查放行P3/P4、G5/G7或HA。

### 本次整合后的补充复核

5b51dfb合并提交的121个远端文件共60,720,414字节逐字核对完成；原下载EOF保留，另一次只读Git blob核验成功。fbda7ca的8个新增修复文件也逐字回读。合并D20及query writer后，完整Windows PG单测为6907/7261=95.12464%，1543个根/子事件PASS、0FAIL、仅Linux专属FD项skip；tools39/experiments219与vet通过。固定fbda7ca的CI41855439373中Linux unit/race、build-lint、fuzz已经通过，python/Windows仍因G1 Inconclusive按原门禁受阻，integration未完成；这些不是六项CI全绿。

G2-11 causal09同一次六叶Go通过379.568s，当前分析器复算713cycles通过，最长control Received→Closed采样5.0713ms。随后独立审阅发现60s总体窗口包含healthy准备时间，不能证明14慢源持续完整60s；并发现通用eventually的条件回调可能跨原3s截止后才返回true。原09日志与旧复算结果保持，不能称完整饱和输入已闭环。已用slow_input_applied_ns和原prepare_deadline_ns明确两条已有时间边界并补反例。唯一causal10完整六叶Go及新分析全部通过，716 cycles，慢输入60.019376–60.4938919s、准备740.5638–807.8091ms、control Received→Closed+1最大4.4916ms；44项ordinary分析回归通过。Windows FD不适用，仍为开发验证；产品500ms/原1s/容量与清理预算不变。

clock04开发运行41855654890固定fbda7ca完成20叶400轮真实跳钟且恢复成功；603源哈希与305原件严格核验。原固定分析器因FD诊断路由、跨轮health响应和整数source失权边界而失败，旧结果不改。当前候选修正对同一原raw复算400轮及400次严格B4均pass，只是诊断复算，未改写旧源码结果或计为正式G2。G3独立019139f的新正式运行由TASK-006处理，仍不能代替最终G2产品源码复证。


新增clock分析回归覆盖诊断必须为严格true/无schema、原observations保留、terminal后响应可接受而当轮/异版本响应拒绝、伪借版本/未来时间/完成倒序拒绝，以及source整数失权界的−1ns反例。真实04仅三轮旧source界早262503/408811/485870ns；下界改为query.WriteStarted+(remaining−1)ms源于原remaining_ms向下取整及零即失权语义，非额外宽限。候选v3与原固定版及v2诊断结果均留在ignored coverage/g2-real-clock-04；正式运行须固定本次全部源码，不能借诊断结果越过ordinary同run要求。

runner新增严格可选vectors_timeout_minutes=90，仅为最终完整G1/G4复证显式声明执行预算；旧缺省行为、150m job上界及全部产品判线不改。四份候选及逐package/Test库存已准备，尚未发布为正式manifest。G4候选包含显式严格B4 79叶/至少85次原要求，不借Go父PASS或缺省资源摘要充数。


独立复审补齐竞争cycle的输入先后：每个backlog_before.started_ns必须不早于实际prepared_ns。真实causal10六叶原数据仍全部满足；将真实PG round_1的slow/full/prepared顺移至首个关键cycle之后的反例原先可误过，现在被明确拒绝。ordinary分析回归46项通过，新候选对同一716cycles诊断复算六叶pass；没有新Go样本，也未修改原causal10源快照/原件/旧摘要。


本次全部修正后的实验工具233项通过（g2-d20-experiments-03.log），clock15项及ordinary46项均包含在内；旧02的单个早到期反例仍按旧整数界构造而失败，已改为真实零权威下界前1ns，原日志保留。go fmt及vet通过。产品cmd/internal自fbda未再改，完整PG覆盖沿用本轮6907/7261=95.12464%记录；Linux固定fbda的unit/race/build-lint/fuzz已通过，integration旧夹具G2-11五叶失败证据保留，当前窄修须新Linux运行验证，不能称六项CI通过。


### G3黑洞与已采用v2准入的开发适配

5b51开发CI41854073582原始160,647,595B日志显示双后端control_blackhole在新Open得到503 ReadinessCheckFailed，失败点均为旧向量339行；此前故障profile预演已完成，不是该处FD/准备失败。G2冻结§6.1.2(5)/(7)要求v2 best_effort也取得可靠本地决定，黑洞无回应应默认1000ms单CHECK失败且不创建Channel/票据/target；不能把无回应当已知Unknown。旧G3规范允许但不强制短暂Preparing，Go/分析器只接受201或AccessOffline的假设须按已采用v2适配。

Go仪器与严格分析器已绑定实际HTTP/失败原因、无新资源及原始时间/模式等证据；新可选声明`blackhole_open_contract=control-v2-preflight`只容许503 ReadinessCheckFailed/timeout或503 AccessOffline/空reason，并核无Channel/Attach、同PID/epoch三快照、零票据/Preparing/check/open/waiter、相同旧READY fence后两次空open/ready日志及完整target历史。缺声明时保留旧解释，显式null或未知值拒绝。既有流的2–4s许可边界、故障前profile/原2s回收、零新target/READY和释放积压后不重放全部保持。

唯一Windows开发G3-05原运行10叶与两helper根22子项Go通过；新Open实际1002.0906/1001.7397ms，旧流关闭2495.3574/2491.6813ms，两段清理最慢213.9897ms。原raw SHA-256为f1f521fc4ef57d192dfb5e0fc5665963b667ff0bb826b3c5642bdef6dca17066，包含合并D21之前两条误标准备记录，完整行为路由继续如实失败；仅在内存隔离这两条后10叶诊断通过，Windows FD仍未测，不称正式证据通过。

独立复核保存两个候选分析器缺口：恢复后才发出的新Open可误通过；三张OIS快照可统一改为不同于资源phase的epoch而误通过。旧分析器原字节及实际6个子反例保留；现在要求blocked观测≤control terminated观测≤restore，并将三快照epoch锚定fault/online各OIC的原资源binding。25项G3行为测试通过，原raw的10叶诊断仍满足新约束。TASK006的d8bb599闭环/冻结材料不改，适配仅进入当前TASK007及最终同源预注册；不得把旧019139f G3 Passed外推当前G2。


### PostgreSQL关闭与锁交接的确定性修复准备

固定9f3c825的Linux CI41863406865 unit在TestRestoreExternal/postgres失败：已调用旧Store.Close后，立即Restore仍得到AnotherInstanceActive。此提交cmd/internal相对fbda无差异；覆盖门禁与race由unit失败而跳过，不能称它们失败或通过。原日志/摘要保留。当前pgLock.close仅将专用sql.Conn归池，再db.Close；pgx终止连接不等待服务端释放session锁，需核实同步解锁与关闭并发边界。

后续先用真实PG的测试连接门闩保留服务端旧session，构造Close返回后Restore仍被旧锁阻挡的确定反例；不以sleep或盲重试吞掉AnotherInstanceActive。修复须先拒绝新的写与Health重取锁，等待已进入写入/维护/导入/健康操作退出，再关闭业务连接并在专用锁连接用原有5s预算确认unlock，最后销毁该连接。必须保持单实例不重叠、关闭幂等、错误返回与失败时丢弃连接；确切覆盖范围和实现由原始反例及独立复核确定，不预宣称已修。产品改动统一纳入最终同源G1/G3/G4/G2复证及完整≥95%单测。


### PG 关闭修复完成与本地执行方向

真实 PG 负例及独立复核后，Store 关闭先阻止新操作并等待既有操作退出；取消事务通过专用 sql.Conn 等待本地后台析构，之后使用原锁连接的 5s 查询预算确认 advisory unlock，错误时丢弃连接且不声称已确认释放。关闭幂等，SQLite 保留原事务路径；5s 不是整体关闭期限，也不把连接析构解释为远端未知业务提交已确认。

公开 Export/Import/Restore、失败 Open 清理，以及 migrate/init/maintain CLI 现在保留主错误与关闭错误。主操作确实完成时保留结构化结果和已输出 token，并以非零退出报告清理失败；Export 只有实际 Sync/Close 也成功才保留完整文件与摘要。真实 PG 测试核实恢复 epoch、维护态和审计恰一次，未自动重放。新增 Store 11 根/17 子项和 CLI 3 根/20 子项，旧源码确定失败原件及修后日志均保存。

最终产品文件的本地 Windows 完整 cmd/internal 单测通过，7030/7387=95.1671855963%（Go 显示 95.2%），1594 项通过、0 失败、1 项 Linux 专属 SQLite FD 检查跳过。原始日志为 g2-pg-close-full-unit-01.jsonl，覆盖 profile SHA-256 为 8db5a6f43a3ca34b93d80f38c2f74252428b0df68b88df3740fc6553643b8f7e。产品 vet、staticcheck v0.8.2、govulncheck v1.9.0 通过；Go 1.27.3 对 Linux amd64/arm64、Windows amd64、Darwin arm64 各三二进制构建两次，各平台字节一致。以上不是 Linux race/FD 或最终正式门槛证据。

用户对 Actions 计费阻塞明确回复“转为本地，暂时无法处理，继续本地工作”（chat-request:call_e891f41bfe1b479182346b3170a0dfd3，2026-11-05，本会话用户）。据此继续本地实现与验证，不再等待账号恢复；保留原 Actions 六项未启动事实，不伪造 CI 成功。准备专用本地 Ubuntu 虚拟机承接 Linux、真实 PG、race 和 clock 检查，单独记录执行环境/镜像/来源/隔离身份；旧 Linux CI 证据和冻结批准不改，原 95%、500ms、2s、TTL、容量及失败判线保持。只在任务虚拟机内施加 clock，不能改 Windows 或用户提供远程主机的时钟。正式复证前先固定本地执行规格和源代码，当前仍不标记任何新门槛 Passed，不合入 alpha。

9f3c825 旧 Linux integration 完整日志回收后，G2-11 六叶中五叶通过，SQLite round_2 在关键处理后 exporter 再查询为 200 而失败。首个 429 后仅约 59ms 即解除拥塞，现有记录不能证明两连接持续占用；该输入失败保留。新输入准备将在原 connect+1s 内、同两个连接和原请求数上观察连续至少五个 429，首个完成至最后开始至少 40ms，中间 200 重置候选，不重启输入或延长产品期限。严格分析器必须核对全部原始查询，不能用固定 sleep 或单个 429 代替稳定输入；完整新开发运行仍需保存全部结果再诊断。


完整新 G2-11 开发运行六叶已结束，五叶 Go PASS、SQLite round_2 FAIL。失败发生在 cycle 2 构造 victim：真实 preflight Ready 只余 5ms，约 3.6ms 后 final 返回 Unknown/timeout，并出现同 sample 的 expired 更新；前两 cycle 的稳定 429 和关键操作已成功。该原始失败与全部 wire 保留，未重试，当前证据符合固定 sampleExpiry 的保守取消，不据此改产品。

后续仅调整 victim 的输入相位：newToken 后仍使用原 3s 绝对 context，在发 Open 前 Lookup 已存在的 Ready 及完整 readiness_view 绑定，剩余期扣除完整 Lookup 往返后至少原 CHECK_MS=500ms。保存选中 view、Lookup 起止、选择时间和原截止，再用同 context 只 Dial 一次；选择不到即输入失败，已发 Open 失败不重试。该相位不保证 Open 必成，最终 preflight/final 仍为权威；不刷新 probe、不延长样本/TTL/准备期限，不改变 native 路径或同两个慢读连接/原请求数、1s 输入与 500ms 关键操作预算。严格分析器将交叉核对选择与 victim 的实际先后及绑定；此说明先提交回读再实现。


### 稳定输入与 victim 相位的开发回合结果

稳定429分析现保留Windows同一HTTP开始/结束可能相等的真实采样分辨率，但不允许请求重叠、倒序、缺失或超出原1s输入期。01新raw SHA-256为de50ce636cec2a69051b97af4d08bdbbe97639c741fcde46abdcef6f00b6a500：五成功叶598cycle严格行为复算通过，SQLite round_2的临界样本失败保留，未称整轮通过。

victim相位已按前述原3s绝对期限与保守剩余期实现，完整view/Lookup起止/选择/截止均作为原始输入记录；strict analyzer交叉核对cycle、重复轮、绑定和实际Open前后。开发02完整结束349.305s，三叶Go通过、三叶输入失败。新victim原raw漏round字段使严格全组拒绝，该原件不补造；源码已补显式round并通过helper/schema检查，未自动第三次运行。最终纯Go三根26子项通过，ordinary Python 59项通过，未改变产品源码/判线。

02另外三个失败周期的两个147456B写入均完整，但在原1s内观察到88/86/88次HTTP200、零429；PG两例export timeout各增加2、rejected增加60/81。原wire/counter支持第三observer可能先占空槽，使另一个慢pipeline读到拒绝，而不是已证明两个成功scrape同时被慢读阻塞；不能归为控制500ms失败，也不以VM/Windows标签替代根因证据。

下一步只补慢读输入的因果前置：在同两个连接、同2048请求及原最早connect+1s绝对截止内，通过不消费socket字节的MSG_PEEK确认各自首个真实HTTP响应头是200，再允许第三observer进入现有连续429资格检查。必须保存两次peek的原始有限头字节/解析状态与起止；非200、头不完整或超时即输入失败，不重启连接、不重试已发业务、不延长预算、不改变窗口或添加固定sleep。peek本身不证明持续阻塞，原五次429/40ms、关键操作后429、关闭前两次真实失败和全部资源判线继续必需。此说明先提交回读再编码，原01/02与所有旧分析结果保持。

产品9b553f7另完成三项本地主动fuzz各60s：FuzzDecodeFrame、FuzzDecodeControl、FuzzObservationState均通过，原日志及SHA保存在g2-pg-close-fuzz-01；仍非Linux race或正式G2证据。本地clock规格bcc4e13已完成逐字远端回读，专用VM依赖准备与profile边界实现并行，尚未真实跳钟或正式复证。
