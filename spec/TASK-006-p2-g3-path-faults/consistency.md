# TASK-006：规格、实现与证据适用性核对

2026-11-04。当前固定15deeab首轮G3 observed/fail：Go与资源通过，4条辅助日志误标被严格行为分析拒绝；D21只修仪器分类，未产生新正式run。G1/G4保持bff7aa2范围Passed，所有历史反例保留。唯一行为规格见[任务README](README.md)，本轮原始事实见[ITER-012](../../docs/evidence/p2-g3-first-evidence-012.md)。

## 原范围与实现对应

| 已有要求/本任务验证 | 实现与实际检查入口 | 关键失败边界 |
| --- | --- | --- |
| B1双向FIN、透明传输、背压 | `tests/integration/vectors_g3_transport_test.go`，G3-01/02；Stream/Bridge新增局部缓冲测试及原OIS缓冲测试 | 反向保留1MiB、真实64MiB写阻塞至少250ms；内容/FIN完成后立即起资源窗口，目标及通道回收在窗口内 |
| B1准备隔离、数据腿中断、不重放 | `vectors_g3_interruption_test.go`，G3-03/04 | 精确连接ID上的RST；Preparing载荷与READY均零；效果先发生且响应零，旧ID不重放，显式新ID/最终清理后再对账 |
| B1撤销与B2有限许可 | `vectors_g3_control_test.go`，G3-05 | 政策/令牌撤销、准备态晚附着；控制故障只操作原control，保留进程与数据腿；2–4s关闭和同代许可事实分开记录 |
| B1混合额度与重复故障回收 | `vectors_g3_quota_test.go`，G3-06 | 两轮8Open+8Preparing，第17条429；10周期每轮8条，RST/原PrepareTimeout/撤销按既定顺序，各有独立资源phase |
| 可核验故障仪器 | `g3_proxy_test.go`、`g3_instruments_test.go`、`tests/fixture/path_target.go` | 非复用accept ID、双方向pause、真正SO_LINGER=0及实际施加时刻；目标逐次计效果，绝不去重/重置掩盖重放 |
| B4故障前基线、完整统计、原2s清理 | `g3_env_test.go`、`g3_resources_test.go`；旧resource helper原样复用 | 34正式叶、38正式profile、62正式phase；另有10仪器profile与8预演恢复phase，不混计。PID/代数/连接绑定、FD相等、goroutine双向±10、三连续样本与原绝对D |
| 停止CLI关闭转发与通道 | `internal/client/stream.go::forwardBridge`与`cancellation_test.go` | 真实长连接已复现旧实现取消后挂住；显式取消发送ClosedByClient，原T_close兜底关闭，完成drain和回调回收，保留首终态及无取消协议错误的原关闭宽限 |
| 原始证据独立复算 | `experiments/product/analyze.py`及`experiments/test_product_g3*.py` | Go Package/Test精确绑定；原始阶段/身份/效果/时限/拓扑独立核验；父PASS或另一包同名不能补缺叶，坏样本不当作零或静默丢弃 |

产品修改客户端生命周期，并在D19增加不参与决策的有界首I/O/Idle终态诊断；不增加公共API、配置、业务执行器、自动重放或持久去重。其余新Go文件属于测试仪器/向量。`envOpts.beforeOIC/finishSetup`为默认nil的测试入口；旧B1–B4装配/采样路径与原阈值保持。目标脚本/代理接缝不进入OIC运行时代码。

## 已有开发事实与限制

独立复核还修正了预注册中12个正常完成phase的origin_event为实际`exchange_fin_observed`，并补齐04的真实非EOF错误和06的Open请求/目标白名单。分析器拒绝跨通道/跨轮连接ID复用、Preparing伪非空请求头及历史已关闭目标再变化；原反例均保留并由另一审查者复验拒绝。product分析器70项测试通过，34个场景的最新实际开发日志分别作行为对账通过；这不是拼接正式run，也不验证Windows缺失的FD。

初始完整双后端G3开发执行为34叶全部通过，38正式profile、62正式phase、266正式采样；最慢清理1287ms。随后收窄正常FIN→资源起点，22个受影响叶通过（50正式phase，最慢1313ms），另针对原PrepareTimeout上界及新增诊断执行窄回归。原始文件在ignored coverage，均是Windows/本机PostgreSQL 18开发回归，FD为-1，不能当作Linux正式资源证明。不能把多个开发文件拼接命名为一次正式run。

完整`cmd/`与`internal/`单元测试使用本机PostgreSQL执行，覆盖率95.9%；vet、staticcheck v0.8.2、govulncheck v1.9.0通过。取消回归七根/11子场景通过，包含已取消上下文、READY前、活动读、两侧阻塞写、完成后等待远端关闭、原ABORT与FIN竞争、定时关闭和回调回收；不以最终主动FIN掩盖退出挂起。具体Linux race、active fuzz、跨平台与最终CI仍需固定源码执行。

开发期失败原件均保留：CLI旧实现挂起的确定性反例；仪器初采与首个样本共用Windows时钟刻度的过严顺序判断；测试先等Closed再读ABORT造成自留socket；正常结束后的重复Close被误当业务失败；以及root本机命令未引用coverage参数、使用相对OI_IT_RUNDIR导致的执行配置失败。后两项分别以引用完整参数、绝对测试目录纠正；原日志与失败夹具移入ignored coverage保存，不改产品规则。诊断overlay源码以`.go.txt`保存，避免被`go vet ./...`当作普通包；原字节与overlay映射保留。

旧证据分析器在修改前按2ec3670原字节存入`experiments/sources/product-g1g4-d18/analyze.py`，SHA-256为`15df949aa31f7c26624492fff52f936b9e8dafcb24b852414e0b364997d0e093`。旧run、ITER、source-manifest、summary、SHA256SUMS、发布回执和已采用scope不修改；历史通过与失败继续原样复算。

## 当前门槛与正式顺序

客户端产品代码变化后，进入本任务时G1/G4为Inconclusive；首轮正式复证后当前G1为Failed、G4仍Inconclusive，TASK-006入口c6c0500/546598e的完整validation对象原样保留，subject仍为2ec3670。入口提交曾补充TASK-005最终集成记录并更新其文档绑定指纹，不能误称与更早570b573的整个gate对象逐字一致；原run/ITER、源码与结果均未变。中间CI的P2门槛检查应如实阻挡；不改CI阶段、不吞返回值、不降低覆盖率或资源条件。当前alpha仍为已核验570b573，main未改。

首轮`G1-P1-R3`、`G4-P2-R2`已正式运行并闭环，G1失败。D20修复及Linux开发复验后使用新预注册`G1-P1-R4`、`G4-P2-R3`，以同一干净最终源码完整复验原五项/四项criteria。新run与新ITER、综合分析、图文/KB和Issue阶段回读共同保存，恢复当前G1/G4；再运行独立`G3-P2`正式证据。G3采用同一已审阅仪器、34叶/38profile/62phase和质量条件，不用前两次回归中的额外G3执行替代其预注册闭环。最后真实门槛完成、六项最终CI全绿后，按既有授权快进alpha。

用户最新明确指令将现有SSH主机作为G5验证目标，不再等待另一台整机1C1G；这只改变后续目标环境选择。本任务不宣称G5/G7通过，不把实测2逻辑CPU/约1.58GiB或应用配额改写成整机1C1G。后续资源规格另保留原问题/答案及实际预算边界，其他性能条件不自动豁免。

## D19诊断实现的独立复核

新边界使用同进程单调时刻、真实TCP tuple、有限错误分类；不依据并发accept顺序推断通道。Target后续响应块只有局部累加，Bridge只记首边界atomic，不逐帧日志。Idle字段最终并入原closing/closed调用；早期新增前置/额外日志的两个实现均已被审查否决并修正，不增加sink调用或阻塞点。门闩测试覆盖2秒idle、FIN各阶段、原状态/ABORT先于既有日志位置。实际O3分别记录requested/received/successful字节及elapsed_ns，失败部分不计完整成功量。

本机首全包因旧cancelConn未实现地址方法panic而失败的日志保留；补齐fake契约后完整双后端单测96.0%。Windows一次Load82条tuple/字节独立对账通过，只是开发定位样本。Linux固定源码诊断入口与上限见README；原73bb反例、已完成ITER-008/009与所有原阈值保持，不将窄开发通过当成正式复证。


D19后续Linux开发诊断41823127149首轮保留17条IdleTimeout：真实FIN和首响应边界均完成，只能定位到后续转发停顿，不能据forwarding=true认定持续有网络进度，也不能从93.5%累计CPU推断根因。README追加一次runtime trace overlay的范围和开销限制；产品源码、原负载、资源窗口及旧证据不改。临时overlay原/改后源码均归档，GOFLAGS绝对路径传递到实际子build，trace在初始基线前开启并在原资源判定结束后flush。当前G1失败及其依赖保持，正式复证尚未开始。

## D20有界消息缓冲复核

| 条款/保持项 | 实现与验证 | 当前结论 |
| --- | --- | --- |
| B1/B4 F与有界接收、原隧道错误 | `internal/wsio/reader.go::Reader.Read`；`reader_test.go::TestMessageReaderRealFragmentedAndLimit` | Reader仅一条消息，cap≤原readLimit+1；真实分片、原/新边界与超限1009一致。控制连接仍走原读取路线 |
| OIS消息所有权 | `internal/ois/server/ws.go::wsConn.Read/Server.accept`；`message_buffer_test.go` | 值复制的wsConn共享指针reader；onData仍先复制DATA到既有有界队列，下一Read不会覆盖writer载荷 |
| 消费端pending生命周期与兼容 | `internal/client/stream.go::Stream.next`；`message_buffer_test.go` | 持readMu，pending耗尽后才读取下一帧；1字节连续Read内容一致；保留原1MiB接收兼容上限/FIN/ABORT/EOF |
| OIC同步写/原FIN顺序 | `internal/tunnel/tunnel.go::Bridge.uplink`；`uplink_buffer_test.go` | F+1缓冲预置DATA类型；门闩证明Write未返回时不读取下一块；n>0+EOF仍先DATA后FIN，写失败不再复用 |
| 既有F输入约束 | `internal/client/client.go::New`；`client_test.go::TestNewDataMaxBounds` | 正DataMax>1MiB拒绝普通配置错误，包括MaxInt；≤0保持默认。防F+常量溢出，不新增wire码或降低合法范围 |

完整cmd/internal含PG开发单测4612/4802=96.04332%，388个测试终态PASS、0fail，Windows专属限制为Linux FD检查skip。全包vet/staticcheck通过；6受影响包97.8%只是局部分母。原/新真实WebSocket分配15→0仅支持减少分配，原Idle/ChannelInterrupted是否解决必须用固定源码的原Linux负载判定。无调大PING/IDLE/期限、无丢弃失败或调整2F载荷账本；旧run/ITER不动。

### D20固定源码正式复证检查点

bff7aa2两份新预注册的G1-P1-R4/G4-P2-R3均observed/pass；139源码逐Git核对、原件SHA和摘要复算通过，unit/race各392 PASS、完整覆盖96.1%。G1原负载与115次严格资源检查通过，G4原四项通过。新run各归ITER-010/011，详见[分析](../../docs/evidence/p2-d20-revalidation-011.md)；等待四单发布回读与最终六项CI，未宣称G3已正式验证。CI41835698583在B1-02/PG的newEnv注册持久化触发500ms写失败后正确fail-closed，导致token创建503；底层慢写原因未定、无baseline，不自动resume或重试未知token写，完整失败保留。

D20阶段发布闭环已完成：[I3-P09](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6632408825)、[I4-P07](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6632409487)、[I11-P09](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6632410179)、[I18-P07](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6632411148)，11个唯一固定链接可解析、38条旧评论与元数据保持，回执入库。证据提交4d17b2d的46文件远端逐字回读，ITER-010/011完成后G1/G4以bff7aa2恢复Passed；最终六项CI及alpha集成待完成，正式G3仍未运行。旧CI准备失败保留且未修改产品/夹具阈值。

### D21：首轮正式G3的辅助日志分类反例

固定15deeab的[G3-P2首轮](../../experiments/runs/G3-P2-20261104T141622Z-01edf5/report.md)为observed/fail；详见[C54/ITER-012](../../docs/evidence/p2-g3-first-evidence-012.md)。139源码、10原文件、预注册与原摘要精确复算。Go步骤全部退出0、96.1%覆盖，G3资源38+10profile/62+8phase通过；但4条blackhole_phase辅助预演误标G3-fault，缺正式schema/package/observed_ns，严格分析器正确拒绝，原四criteria均fail。G3现为Failed，G1/G4保留此前bff7aa2范围Passed，不外推到G2分支或后续门槛。

普通仪器实现修正D21仅改g3_env_test.go该发射点kind为G3-preparation，原stage/数据及正式分析器、schema、34叶、62phase、预注册与预算保持。新增4项路由反例与既有G3分析测试共32项PASS：旧误标仍拒绝、诊断保留、缺正式行为仍拒绝、诊断冒用正式schema仍拒绝。此为本机解析回归，不是新正式run。原被测分析器按Git原字节归档；失败run与新ITER012准备同commit保存，旧run/ITER/报告/回执不改。

当前检查点：f568bde保存首轮失败与D21，39个文件远端逐字回读。[I3-P10](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6634213228)、[I11-P10](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6634214130)、[I15-P07](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6634214809)发布并精确回读，31条旧评论/元数据保持、9个固定链接核验；ITER-012完成，G3仍Failed。图06中文/布局已检查。接下来固定新源码执行同一预注册的新正式run，单独核验/分析；产品承诺、接口、权限、时限与支持范围无新增选择。
