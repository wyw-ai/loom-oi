# TASK-007 规格、实现与验证一致性核对

本表核对已推送 `804f703` 的 G2 实现，以及本轮尚未提交的 preflight 截止窄修；不是正式 G2 通过记录。行号取本次工作区，符号名用于后续定位。clock 仪器、原始分析器、调度诊断和状态 fuzz 正在并行完善，须在最终提交重新绑定源码和验证，不将本表的开发记录写成最终源码证明。

项目承诺仍是获准目标的发现、连接与明确失败边界：目录就绪不代替本次准入，READY 不证明业务成功，连接恢复不隐式重放业务。规格依据为[六个已采用片段](../../docs/architecture/p2/g2-observation.md)、[原始三项答复](approval.json)与[ADR-005](../../docs/decisions/ADR-005-g2-adoption.md)；本表不改片段、指纹、默认值或错误含义。初始实现基线已合入 alpha `570b573`；TASK-005 的 FIN、单 reader、恢复/维护与严格资源约束保持。

## 1. 六个冻结范围与实现责任

| 冻结 scope／规格 | 实际符号与路径 | 已有验证与当前结论 |
| --- | --- | --- |
| `g2-probe`，§4：本地单源 HTTP(S)、地址许可、独立观测版本、attempt/sample/TTL | `internal/oic/config/probe.go:83` `Probe.compile`、`:132` `ContentDigest`；`internal/observation/plan.go:53` `NewPlan`、`http.go:18` `Execute`；`state.go:74/156/181/218` Set/Begin/Finish/Snapshot；`internal/oic/relay/reload.go:44` reload | Plan/HTTP/状态代数单测及 G2-01/02/03/04/08 开发链覆盖；CA 内容变化不依赖 JSON 改动，未采用来源不编译成 Ready。真实 OS 跳钟仍待 D3，不能用 observed_at 元数据替代。 |
| `g2-wire`，§5：控制 v2、完整 binding、挑战查询、目录时效和授权过滤 | `internal/protocol/observation.go:57/406` ValidObservationBinding/DecodeControlForVersion；`internal/ois/server/ws.go:190` controlVersion；`internal/ois/relay/observation.go:85/173/234` pumpQueries/onObservationSnapshot/directoryView；`hub.go:292` Discover | v2 必填/null/枚举/大小写别名、v1 扩展兼容单测；G2-03/04/13 开发链。目录扣完整 RTT、同样本只收紧、历史字段不授予当前有效期；未通过正式统一来源复算。 |
| `g2-commit`，§6：OIC t_check 固定决定、OIS 最终权威、总 D/幂等/取消 | `internal/ois/relay/admission.go:57/251/403` OpenContext/createPrepared/onAdmissionResult；`channel.go:181/198/216` tryCommit/commitReady/commitCheck；`internal/oic/relay/observation.go:333/531` admissionReceived/allowReady | 双方门闩和 G2-05–08 开发链；最终决定不读新的目录健康值。新增 D10 补 preflight 创建前已知单 CHECK 到期的防线，尚未提交，不把预检成功扩张为全拨号期持续 Ready。 |
| `g2-fairness`，§7：有界调度、独立发布、关键/遥测公平 | `internal/observation/scheduler.go:198/215/291` fillLocked/pumpLocked/execute；`internal/oic/relay/writer.go:101/180/213` offerUntil/expireQueuedQueryLocked/run；`internal/ois/relay/control_writer.go:45/73` nextControl/writeLoop | Scheduler 门闩与两端 writer 的 8:1/真实 seq/关键容量测试；G2-09/10/11 开发采样。病态负载仍可有限拒绝/断会话，未增加可用性或生产调度 SLO。D3 与最终 Linux 饱和/资源验证未完成。 |
| `g2-export`，§8：默认关闭、回环、2 槽/64 KiB/200 ms、有限指标 | `internal/metrics/handler.go:29` ServeHTTP、`render.go:53` Render；`internal/oic/relay/reload.go:148` startDiag；`internal/ois/server/config.go:124` validateDiagnostics、`server.go:125` 诊断监听接线 | 独立 expfmt parser、真实慢 socket/截止/槽生命周期单测及 G2-12 开发链。发布可见延迟在生产端缺跨机单调观察者时不伪造值；正式证据由测试观察者测量。 |
| `g2-acceptance`，§9：G2-01–14、双后端、故障/饱和和真实时钟 | `tests/integration/vectors_g2_*_test.go`、`g2_env_test.go:45/137` newG2Env/settled；`g2_proxy_test.go` 真实 mTLS WS 代理；`g2_instruments_test.go` 仪器验证 | 804f703 既有 22 根/130 叶的源码清单已有；128 叶归属 106 环境/profile，两个旧 OIS 拒绝新 OIC 叶明确不主张在线 FD 回收。新增 D3 五组双方向双后端共 20 个 clock 叶另行固定。manifest/analyzer/最终 Linux/闭环均待完成。 |

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
| final、READY 与配置取消 | `ois/relay/channel.go:198` commitReady；`oic/relay/observation.go:531/559/567` allowReady/markCancelledLocked/closeCancelled；`agent.go:681` provide。`TestFinalSnapshotIgnoresLaterHealth`、`TestProbeReloadREADYOrdering`、`TestREADYNeedsFrozenDecisionAndLiveLicense`；G2-05–08 | t_check 后新健康样本不翻转固定决定；授权/目标/许可仍可终止。READY 本地先 Running 则 probe-only reload 不关旧流；先 Cancelled 则目标载荷为零，消费者可能先 READY 后 ABORT。socket/Bridge 操作在 Agent.mu 外。 |
| 失败码与首因 | `protocol/codes.go` HTTP/ABORT 映射；`ois/relay/admission.go:515` decisionReason；`ois/server/api.go:30` writeError；`client/client.go:88` apiError、`client/cli.go:113` stderr。`TestReadinessTerminalReasonIsPreciseAndFirstWins`、`TestReadinessErrorReasonHTTP`、G2-14 | ReadinessRequired=412/0x0107、ReadinessCheckFailed=503/0x0205、PrepareTimeout=504；有限 reason 保留，已落定终态不被迟到消息改写。传输损坏不能保证准确业务/新增码送达。 |
| 版本兼容与不降级 | `ois/server/ws.go:190` controlVersion；`oic/relay/agent.go:310` session、`handshake.go:14` controlHandshakeError；`protocol/observation.go:406` DecodeControlForVersion。G2-13、`TestControlHandshakeVersionUnsupported` | 新 OIS 接 v1/v2；新 OIC 仅 v2。旧 OIC 能力 Unknown，strict 412；旧 CLI 可能把新 ABORT 显示 ProtocolError，OIS GET 保留准确首因。旧二进制固定 alpha570b573；其他版本不自动进入支持矩阵。 |
| 管理、存储与恢复边界 | `ois/relay/admission.go:266` 新 Channel quota/store/maintenance 检查；`channel.go:216` commitCheck；`admin.go:229` enforce。`TestAdmissionFixedPreparationAndFinal` 在预检后设置 storeOK=false/maintenance=true 后仍验证既有 Preparing final | G2 不给已有 Preparing 增加维护/存储提交条件；已有 Channel 幂等回放保持。新 Open 在原边界拒绝；G2 状态仅内存，不新增 probe/sample 持久表。最终新源码仍须真实 G4，局部回归不能替代恢复证据。 |
| 有界回收与资源采集 | `ois/relay/control.go:219` endSession、`background.go:193` Shutdown；`oic/relay/writer.go:302` discardPending；`tests/integration/resource_helpers_test.go:94/232/272/372` 严格 JSON/边界/预冻结/绝对清理窗口 | 原双向 goroutine±10、FD精确相等、三连续样本和 T_close+1 s 保持。新 probe/writer 在预故障健康拓扑冻结，清理健康端点/idle 连接计入同一 2 s；Windows FD=-1 明确未测，不能作 Linux FD 通过。 |

## 3. G2-01–14 的真实测试与开发记录

下表的“开发通过”只表示相应日志中的实际 Go 断言/进程链结果，不表示同一最终提交完整运行；有些日志只覆盖场景子集。完整原始行为仍须由最终分析器逐条复算，不能仅用父 PASS 或把窄跑拼成正式样本。路径均为仓库 `coverage/` 下 ignored 开发文件，未登记为正式 evidence run。

| 向量／冻结判定 | 实际根函数与主要实现 | 已核开发事实／缺口 |
| --- | --- | --- |
| G2-01 / TV-B1-19：三状态×两模式，模式取交集 | `vectors_g2_readiness_test.go:18` TestTVG2_01_AdmissionMatrix；Table.Evaluate、双方 admission | `g2-matrix-03.log` 双后端通过；`g2-resource-normal-02.log` 补资源路径通过。包括真实目录/本地/请求收紧，不用目录缓存代替检查。最终 Linux 未完成。 |
| G2-02：Running/timeout 不替代旧有效正/负样本 | `vectors_g2_readiness_test.go:112` TestTVG2_02_AttemptDoesNotReplaceSample；Table.Begin/Finish/Snapshot | `g2-attempt-01.log`、resource-normal-02 双后端通过；状态 core 单测核一次 expiry Effect。D9 保留测试主动消费 Effect 的失败与修正。 |
| G2-03：低序/重复/冲突/旧绑定 | `vectors_g2_identity_test.go:357` TestTVG2_03_SequenceAndBinding；OIS onObservationSnapshot/acceptObservationIdentity | `g2-identity-03-01.log` SQLite 通过；`g2-identity-pg-01.log` 含 PG 两场景装配失败，`pg-retry-01` 仅重验该两场景通过。不能据此称同源整轮全绿；Injected/Dropped 必须按代理原始标记识别。 |
| G2-04：query/response 延迟、完整 RTT、缓存过期/迟到、真实墙钟 | `vectors_g2_identity_test.go:491/581/637` QueryTTL/ObservedAtIsDiagnostic/LateQueryCannotRevive；OIS query/deadline；新增 `vectors_g2_clock_test.go:17` RealClock | `g2-identity-04-01.log` SQLite 和 pg-01 对应根通过，final-01 两后端 late 通过。observed_at±一年只证展示字段；D3 新增真实 REALTIME±30 s 仪器/20叶正在开发，未实际运行。 |
| G2-05：preflight Ready 后变 NotReady，final 必须重查 | `vectors_g2_commit_test.go:228` TestTVG2_05_FinalRecheck；tryCommit/startAdmission/onAdmissionResult | `g2-commit-03.log` 双后端通过；原 commit-01 失败保留。不是从旧 preflight 直接发 READY；最终统一回归待完成。 |
| G2-06：t_check 后新健康不翻转，原冻结样本仍受期限约束 | `vectors_g2_commit_test.go:302/874` CommitSnapshotWindow/ExpiredFrozenReady；OIC admissionReceived/allowReady、OIS final | `g2-commit-08.log` 固定窗口双后端通过，resource-commit-03 原样本到期双后端通过；`g2-final-directory-counterexample.log` 的错误“目录 NotReady 翻转 final”overlay 确定失败。 |
| G2-07：授权/目标/代数/许可等安全条件独立重核 | `vectors_g2_commit_test.go:726` TestTVG2_07_FinalSecurityRecheck；commitCheck/allowReady/enforce | `g2-commit-07.log` 对应双后端窄复验通过；commit-04/05 原失败保留。不能把健康冻结解释为冻结授权。 |
| G2-08：消息取消、配置版本先取消、Running 后 reload、在途幂等 | `vectors_g2_commit_test.go:447/651/355` 08a/08b/08c；`vectors_g2_identity_test.go:145` 08d | 08a 在 commit-03/06/07、08b 在 commit-04/05、08c 在 resource-commit-03、08d 在 identity-08d-02 有真实通过；晚到/重复/先后为各场景门闩，必要竞态20轮。各版本不得拼成正式一轮。 |
| G2-09：另一慢目标不阻挡独立发布 | `vectors_g2_readiness_test.go:156` TestTVG2_09_IndependentPublication；reconcileProbes/markDirty/pumpQueries | `g2-publication-06-valid-paths.log` 双后端20轮用已采用500/200/50/1500 ms配置通过；resource-pressure-04 补回收通过。visible−fastHeaderStarted 是完整归约→可见延迟的保守上界，不冒称内部归约纳秒测点。早期非适用加大timeout/URL夹具记录保留。 |
| G2-10：P2/Q4/16源、14慢目标、公平等待≤2500 ms | `vectors_g2_pressure_test.go:13` TestTVG2_10_SaturatedFairness；Scheduler due/queue/实际start | `g2-saturation-01.log` 双后端3×60 s通过；resource-pressure-04 是资源窄回归。原定 due→实际执行的等待才有效，不以timer晚回调重置等待。当前调度补充在并行修改，最终同源Linux仍待。 |
| G2-11：遥测/导出饱和下关键 heartbeat/revoke | `vectors_g2_competition_test.go:257` TestTVG2_11_ControlExportCompetition；双方 writer/metrics | `g2-competition-03.log` 完整开发轮仍 FAIL：PG round_1/2 未构成两个实际阻塞 scrape；04 只重验这两叶通过。原输入上限不放宽，等待实际socket写完成与10ms轮询修正夹具。必须最终整轮3×60 s/后端重验，不合并旧4成功叶与新2叶为正式通过。 |
| G2-12：实体变化标签固定、慢抓取有界 | `vectors_g2_export_test.go:350/415` EntityCardinality/SlowReaders；metrics Handler/Render | `g2-export-02.log` 四叶通过；`g2-export-slow-04.log` 加强“主动 Close 前 timeout 已增2”后双后端通过。独立 expfmt 解析13 families/295 series，实测最大21245 B；32实体变化不增标签。外部1 s观测窗不能冒称测得服务端Write恰200 ms。 |
| G2-13：固定旧二进制真实组合、v2非法输入 | `vectors_g2_compatibility_test.go:48/205` Compatibility/V2WireRejection；version decoder/握手分类 | `g2-compat-03.log` 两根全通过、04是三个非法消息场景窄回归。旧alpha570b573源码/binary哈希记录；02中的v1 readiness_view=nil断言属夹具过强，Unknown兼容不能要求旧结构。newOIC丢 VersionUnsupported 属真实产品诊断缺陷 D7。 |
| G2-14：目录、准入、第一原生响应、业务结果分层 | `vectors_g2_readiness_test.go:241` TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate；API/CLI 原始字段 | `g2-diagnostics-01.log` 与 resource-normal-02 双后端通过；正常响应、就绪拒绝、连接失败和业务含义分开，不用 Channel Ready/中继字节宣称业务成功。 |

仪器另由 `TestG2ProxyRawBarrierAndStage`、ProviderReadyAndExplicitInjection、IdentityReconnectAndCloseHeld、CapacityAndCancellation、HandshakeFailureFidelity、TransportBoundBeforeTLS、DropIsExplicitAndOrdered 及真实 Health fixture 验证。代理保留 Raw、ConnID、序号、方向、received/written/held/drop/injected；原日志大行按 Package/Test 完整重组，不能静默丢分片或把注入值当真实 OIC 观测。资源反例根 `TestG2ResourceBaselineRejectsActiveProbe`、`TestG2ResourceDeadlineIncludesPreparation` 不替代真实 Linux profile 回收。

## 4. 缺陷、取舍与验证缺口台账

D0/D1 继续指 README 的准备/准确采用阶段；下表复用已有 D2/D3，产品问题从 D4 起，不改任何旧编号或来源。README D1及采用段的“G1/G4前置已恢复”描述进入实施时已集成570b573的历史状态，不覆盖本轮产品/依赖修改后的当前Inconclusive。

| 编号／状态 | 具体问题与修正／取舍 | 反例与验证边界 |
| --- | --- | --- |
| D2 已完成记录修复 | 原检查器不能表达真实 tool-call 用户逐项答复；增加 user_response/items 精确关联，不伪造 chat UUID、不用 AI 评论代替人类 | 4f12fea 的39项tools与完整工具分母98.70%，采用/回读见README；不是产品门槛证据。 |
| D3 进行中，阻挡完整G2-04 | 元数据时钟测试不足以覆盖实际 OS 跳钟；在一次性专用 GitHub hosted Linux VM 中受控±30 s并恢复 | README D3规格已先行；guard/workflow/20叶/原始分析器仍需完成、独立核验并实际运行。不得在本机/用户SSH主机改钟。 |
| D4 已修，有限HTTP读取 | 缺少 Request.Close 时 Transport 关闭response会额外读取；固定关闭策略保持4 KiB预读、不drain | `g2-http-close-counterexample.log` 去掉Close的overlay实得12288 B/2次read并FAIL；HTTP读取边界单测通过。该反例是负对照，不是生产业务根因推测。 |
| D5 已修，OIC原始期限/冻结决定/锁序 | 接收→等锁/排队不得重启预算；判断使用锁内当前时刻，final固定ID不被重复检查翻转，READY放行/取消共用归约序列 | `observation_boundaries_test.go:26/48/76/109/132/196` 与 rejection/lifecycle 门闩、ordering-20日志；属于已采用语义落实，没有扩大健康保证或新增预算。 |
| D6 已修，queued query主动回收 | 原队列中已超时/取消query长期占槽；按identity主动回收，旧callback/release不能删新owner，inflight由writer释放 | `g2-query-expiry-negative-overlay-02.log` 旧源确定等待回收超时；`writer_expiry_test.go:17/79/115`、positive-01及全B回归通过。未过期满槽/duplicate仍拒绝。 |
| D7 已修，兼容握手诊断 | 旧OIS实际400 VersionUnsupported被newOIC泛化为WebSocket失败；只解析完整<1024B合法公开code，固定错误提示且关闭body | compat-02 原真实失败保留；`handshake_test.go:29/38`与compat-03修后通过。不得打印任意peer body或回退v1；v1视图nil的旧夹具错误另行纠正，不冒称产品缺陷。 |
| D8 已修，有限最终原因与首因 | GET最终原因把已知timeout/observation_changed泛化unavailable；分离公开decisionReason与固定指标枚举，迟到通知不覆盖终态 | `TestReadinessTerminalReasonIsPreciseAndFirstWins`、严格TTL expired及真实08a复验。没有增加未收到事件的远端诊断保证。 |
| D9 已修，测试不消费被观察状态 | full-unit-04 中expiry测试主动Snapshot而丢Effect，先于timer消费一次expiry，导致真实Changed等待超时 | 原失败保留；改为先被动等Changed，再wire query核expired/metric=1，10次窄验及full-unit-05通过。只改测试观察方式，未放宽期限/改产品expiry。 |
| D10 已修未提交，preflight消费前单CHECK截止 | `admission.go:256`刷新now后只查总D：入口999ms、CHECK1000ms、创建读1001ms仍给Preparing/票据。新增`:263` guard置总D后/额度前；v1无check保持 | `coverage/g2-independent-review-01/`固定804f703旧admission字节与overlay：旧源码恰到/越界两反例FAIL，总D对照正确504。新 `TestAdmissionCompletionDeadline` 六子项+v1根及20次重复PASS；撤销401/取消优先、503 timeout/504、零资源泄露。只修本次检查消费边界，不要求整个拨号阶段持续Ready。 |
| D11 资源/输入夹具已补，Linux仍待 | 首次profile须健康正常暖机、ProbeActive/Queued=0且FD稳定；清理helper保留原起点，恢复健康/关idle也在同一2s。G2-11两slow socket须实际构成输入，不能以计划请求数代替 | 资源normal/commit/pressure三个窄回归及专门反例；competition-03失败与04重验均保留。Windows=-1非Linux FD证明；最终统一源码不能合并跨轮结果。 |
| D12 分析与正式证据未完成，阻挡门槛 | 新g2-behavior、wire历史、资源profile及clock原始数据须独立重算；缺项/skip/乱序/错binding/TTL续期/无实际饱和不得靠Go PASS通过 | `experiments/product/g2_analyze.py`及反例正在并行开发，尚未作为最终来源归档/预注册。现有84项旧实验回归不等于新分析器验收。 |

独立审阅本次覆盖 OIS admission/query/注册替换/control writer、server版本/API/诊断、client错误转交和limits/metrics/protocol。D10从只读发现→ignored旧源确定反例→授权三行修复→正负回归完整保留；其他已审部分没有另一个已确认且未修的高置信实现缺陷。此结论不覆盖尚在并行变更的clock/analyzer，也不把“未发现”视作形式化安全证明。

## 5. 产品文档、模块与图

| 产物 | 与实际行为的核对 | 当前处置 |
| --- | --- | --- |
| [产品参考](../../docs/product/reference.md) §1–8 | OIS/OIC配置、HTTP v1、控制v2、原tunnel.v1、有限reason、目录/准入、显式metrics开关及预算均有实际入口 | 保持“已实现、尚待正式验收”；无复杂鉴权probe/任意远程执行/新生产SLO。 |
| [运维](../../docs/product/operations.md)、[产品入口](../../docs/product/README.md) | CA/reload、未知/过期、取消/中断及业务结果分开；G2不改变备份/迁移/维护责任 | 尚需最终统一质量与正式证据；TASK006并行的CLI取消/消息缓冲等最终合入后重新核适用性。 |
| [OIC](../../docs/modules/oic/README.md)、[OIS](../../docs/modules/ois/README.md)、[消费者](../../docs/modules/client/README.md) | OIC本地权限/probe/归约，OIS目录/授权/最终权威，客户端显示/原生流；无业务执行器或业务成功推断 | 当前页已写G2开发验证与G1/G4 Inconclusive，历史2ec3670通过只作历史来源。 |
| [架构](../../docs/architecture/alpha-v0.7.md)及[图册](../../docs/architecture/diagrams/v0.7/README.md) | 图03为C1概念准入，详细preflight/final/取消固定点以G2§6为准；图06表达当前门槛依赖而非实现测试结果 | 审阅发现架构导航曾保留“尚未实施/前置已恢复”，主流程已同步为实施中、当前G1/G4 Inconclusive，并仅刷新相关Pending指纹；六冻结scope不改。最终图与渲染视觉QA由主流程收尾。 |

## 6. 质量事实与完成出口

804f703附近的完整Windows开发 `g2-full-unit-05.jsonl/.out` 为6746/7097条语句=95.05425%；包含显式PG，唯一Linux专属FD项skip，full-unit-04的失败保留。该完整分母不是新增D10及所有并行改动后的最终统计。本轮D10的 `g2-preflight-deadline-affected-01.jsonl/.out` 仅完整OIS relay/server两包：112根/102子项、0skip/0fail、子集96.0%；vet、diff、UTF-8/LF通过，不能替代全部cmd/internal≥95%。本机Windows缺cgo导致该次race未运行，不记本机race通过。主流程另核固定804f703的 [CI 41833218575](https://github.com/wyw-ai/loom-oi/actions/runs/41833218575)：Linux unit/race job126585415912通过，完整6776/7124=95.115103874%；fuzz job126585416194通过。四平台构建完成，但build-lint曾因gofmt失败（工作区已修）；python/Windows按G1 Inconclusive正确阻挡，integration在本次核对时仍运行，因此不称整轮CI全绿。该CI也不覆盖本轮D10及并行改动；新state fuzz本机active 60s/1003376次是独立开发记录，仍须最终固定源复核。

最终出口仍需：

1. 固定全部产品/依赖/测试/clock/analyzer源码，完成完整cmd/internal同分母≥95%、Linux race、规定fuzz active时长、vet/staticcheck/govulncheck、四平台构建与Windows CI；新增fuzz不能只以种子PASS代替active运行。
2. 独立核新分析器拒绝缺失/skip/失败/畸形或分片未完成OBS，绑定所有真实根/子场景、包名、资源与clock身份；manifest和脚本先固定、提交回读，才运行正式证据。
3. 当前源改变了控制/准入/回收与模块依赖，G1/G4适用性为Inconclusive；最终源码的G1/G4正式复证、TASK006最终G3代码集成及G3适用性复验均未完成。旧2ec3670验证、run/ITER与回执完整保留，不改subject或摘要来覆盖新源码。
4. 完整G2双后端/全部20轮竞争/3×60s窗口/真实OS跳钟与Linux固定profile精确回收，不能把分支、窄跑或不同源码阶段的成功拼成一个run；失败保留并按原阈值解释。
5. 固定run的原始复算、独立审阅、报告/ITER/KB/图文/Issue回读和门槛闭环后才晋级；当前alpha/main不变，不以本文或结构检查放行P3/P4、G5/G7或HA。
