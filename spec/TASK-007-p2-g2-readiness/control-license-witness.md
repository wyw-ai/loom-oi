# B2/G3 控制故障许可输入见证修正

状态：编码前固定的测试仪器规格，已完成独立源码与边界复核；本文对应实现尚未开始。提交并远端逐字回读后实施，不改已采用 B2/B4/G3 契约。所属任务与进度见 [TASK-007](README.md)。

## 原始事实与限制

固定源 `d9b884929aec6c8a14460876f13c14958239f652` 的首轮本地 G1 仍在执行。已保留 `coverage/g1-local-formal-01/vectors-control-cut-01/raw.jsonl`（SHA256 `5fbb4f92a8678a74d6527b12455f5cb58a8a614b4aafa4c3637df49a10562804`）、D16 拼接输出及两后端进程日志。

- TV-B2-10 SQLite 实测 LicenseExpired after 1.9532166s；PostgreSQL 为 1.9129249s，均不满足原 2–4s 判线。
- lab-a 的 control connected→license expired 日志差分别为 2.5019482s / 2.5022997s；基线采样 remaining 为 2029ms / 1982ms。它们与初始 `3000−500=2500ms` 许可在 Dial/装配中已消耗的解释相容，不能从墙钟日志倒推出故障时单调 deadline、最后 heartbeat 或唯一根因。
- `vectors_b2_test.go:428–453` 一次 Dial 后未选许可相位，且计时起点在同步 cut 返回后。原失败不改称通过。
- `resource_helpers_test.go:167–188` 的 SampledNS 在 statsRequest 完成后取值；`vectors_g3_control_test.go:348–376` / `analyze.py:1172–1181` 当前只扣 `applied−SampledNS`，漏扣 HTTP 采样耗时。这是独立的输入证明缺口，不据此断言每条历史 G3 实测均不合格。

## 不变的产品与验收边界

TTL=3000ms、margin=500ms、heartbeat=1000ms、keepAlive=200ms 均保持；LicenseExpired/AccessOffline 两个合法终因、原 L±1s 即 2–4s、拒绝新 Open/无新目标连接、原 B4 精确 FD/goroutine/零 channel/ticket 与独立 2s 回收窗口保持。不得续许可、伪造 heartbeat、重试业务 Dial、重置原故障/资源期限或把准备观察上限当产品 SLO。B2 不借用 G3 正式行为 schema；G3 原 applied/terminal/offline/cleanup 顺序不改。

## 最小共用采样 helper

新增测试专用 `control_license_test.go`，仅封装既有 `env.resourceOIC`，不修改共享 `resourceProcess.SampledNS`：

```text
controlLicenseObservation {
  request_started_ns: int64,
  request_completed_ns: int64,
  process: {pid: int, sampled_ns: int64, stats: 完整 protocol.OICStats}
}
```

开始时刻紧贴调用前，完成时刻在该调用返回后取；均用现有 resourceNS 单调原点。有效采样满足 `0 < started <= sampled <= completed`；低精度时钟允许相等，不能伪造采样耗时。HTTP/JSON/进程存活错误直接失败，不使用旧快照或全零值。完整 stats 先沿现有 validOICStats 校验；AccessID=lab-a、PID 与已装配 OIC 相等、Connected/Registered=true、epoch/generation 与业务流及控制连接绑定一致，且本流确已 Running。样本 remaining 是服务端对实际许可的毫秒向下取整；本许可的终止按真实 deadline 判断，故保守下界用 `r*1e6`，不套用 readiness 的 floor==0 提前失权公式。

纯函数 `controlLicenseRemainingNS(observation, atNS)` 返回 `max(0, r*1e6−(atNS−request_started_ns))`；先检查 at>=completed、字段类型/范围、减法不溢出。保留原始纳秒，不用四舍五入后的 ms 作判定。

## B2 的一次性装配、故障与结果

1. 在原一次真实 `consumer.Dial` 前保存 target 身份快照，返回后取得 `Stream.ChannelID()`，维持原 keepAlive。本轮 target Conns 增量必须恰一项，以 index+Accepted 单调时刻+Local/Remote tuple 固定身份，并实际观察该同一连接收到原 keepAlive 的 `k` 字节，才证明 provider 已 Running；Dial 返回、OIS ready 或 OIC Channels=1 单独均不充分。保留真实 OIS ready 日志作 channel fence。控制索引沿 env.startOIC 启动前保存的 `e.oics[lab-a].ctrl`，并用该进程原 control connected 日志的 epoch/gen 绑定最终 stats；如故障前已重连而无法证明索引重绑定，输入失败，不能取 count−1。PID/access/epoch/gen 必须与完整 stats 一致。
2. 一次 Dial 返回后立即固定绝对 **5s** 准备 ctx；原 keepAlive、唯一新 target 实收 k 的装配与后续 stats 选择共用此 D。在此窗口内观察真实 stats，选择 `remaining_ms>=2250` 的相位。2250 是输入余量选择条件，5s 是新增有界装配 watchdog，均不是产品期限；从第一次选择开始不重置。允许等自然 heartbeat，不发送新的测试续期指令。记录准备 start/deadline、尝试次数及最终真实 observation；不存在合格样本即输入失败。G3 不套此新增 5s，仍沿原场景 ctx。
3. 保留 refuse 新控制重连的原动作。新增仅该向量调用的 `proxy.cutObserved(index, now)`：在原 proxy 锁下取得实际 pconn 并复制 index 和两侧 Local/Remote tuple，随后解锁，所有 Close 均在锁外；紧贴**第一次 Close 调用前**记录 cut_started_ns，按原顺序关闭两侧，第二个 Close 返回后记录 cut_completed_ns。两个 Close 的错误原文各自记录（nil 为 JSON null）；net.ErrClosed 可能是自然并发关闭，不能新增“双 nil 才通过”的产品门槛。是否施加与终态仍由原连接身份、时间和结果证明；不把 socket/worker join 或重新连接等待放进区间。原其他 `cut()` 调用语义不动。该区间是夹具施加边界，不虚称某个内核字节到达时刻。
4. 施加后马上核 `r*1e6−(cut_completed_ns−request_started_ns)>=2e9`。未满足必须输入失败，不续 TTL、不再次 Dial/cut。保存已取得的失败原始事实后终止。
5. 独立读取协程在 cut 前就开始排空同一 stream，读取错误返回后立即记录真实终态 `terminal_ns` 和终因，再向主协程传结果；不能等 offline 轮询完成才为终态打时刻。有界清理必须 Close stream、停止并 join keepAlive/reader，失败路径也如此，禁止 goroutine Fatal/未 join 遗留。
6. 以原切断区间严格断言 `terminal−cut_completed>=2e9` 且 `terminal−cut_started<=4e9`。这保证区间内任意实际施加点均满足原 2–4s，不引新容差，不把 Close 的耗时从上界抹掉。终因仍只允许 LicenseExpired/AccessOffline，正常 EOF/成功完成拒绝。
7. 原 5s offline 观察以 cut_started_ns 固定截止；使用既有 GET `/v1/admin/accesses/lab-a` 轮询，必须实际 AccessID=lab-a、Online=false、Generation=原 generation，不能用聚合 AccessesOnline==1 替代。取到此精确离线结果后，在同一原截止内只做一次实际拒绝 Open（status=503、code=AccessOffline、返回 channel_id 为空/无票据）。不为等离线重试业务 Open。保存 GET 与 Open 各自请求区间/HTTP 状态/实际响应，access_view 不包含 epoch，epoch 由既有 OIS/OIC 完整快照及控制日志绑定，不补造字段。故障前后、终态且离线后与恢复清理后的 target Conns 记录集合保持同一身份前缀且数量不增；允许原连接数据与终态字段自然更新，不能以总数量相抵掩盖替换。记录原流 ready 的 OIS 日志 fence（位于实收 k 后），fence 后不得新增 open/ready 事件。
8. 原连接 Close、控制恢复、waitRegistered 和 `e.settled()` 保持。恢复前保存 acc 的连接 mark；开放新控制后，只接受该 mark 之后唯一实际新增 forwarded 连接，结合同 PID/access 的真实新 control connected 日志、相同 epoch/递增 generation 与 Registered stats 更新控制索引；lab-b 的原控制身份也须未变，以排除将别的连接当 lab-a。不能猜 count−1。若额外重连令唯一性无法证明，夹具失败而非择最后一项。不得重采基线、推迟已开始的回收窗口或把新 helper worker 放到清理成功之后释放。

B2 复用 `kind=O4, vector=TV-B2-10`，新增 `event=control_cut`，不新造 G3 schema。准确 JSON 键及分组以下节为准；前述 cut_started/cut_completed/terminal_ns 分别对应 cut.started_ns/cut.completed_ns/terminal.at_ns。保留原 O4 reason=terminal.reason、ms=(terminal.at_ns−cut.completed_ns)下取整作为观察字段，不作判定权威；两者必须与纳秒原值一致。成功恰一条完整结果；失败尽力先输出同事件的实际已知字段与 input_error，缺失字段仍导致独立分析失败，不能用补零/补造数据使其合格。

## 最小字段与有界原始记录

B2 成功事件只有一个原 O4 记录（旧版没有 event，新版固定 `event=control_cut`）；不再追加另一条旧 O4 摘要。下列为明确字段分组，名称固定后实现/分析器共用，所有时间均为测试进程单调 resourceNS：

- 根字段：kind/vector/event/test/backend、reason/ms、control_binding、channel_id、access_id、epoch、generation、preparation_started_ns/deadline_ns（差恰 5s）、selection_attempts、license_observation、cut、terminal、offline、targets、channel_log、restored、recorded_ns。kind=O4/vector=TV-B2-10/event=control_cut；不新增 scenario 字段或第二套业务模式。
- `cut={control_index,client_local,client_remote,server_local,server_remote,started_ns,completed_ns,client_close_error,server_close_error,remaining_ns}`；索引与 tuple 来自实际 pconn，不是事后猜测。两错误允许 null 或原始字符串；remaining_ns 独立按原 request start 重算，两个时间不得落在准备截止之后。`terminal={at_ns,reason,read_bytes}` 来自一次独立 ReadAll 的返回；bytes 仅记录，本 B2 不新增零总响应字节标准（原 fixture 可返回有限诊断响应）。
- `offline={deadline_ns,access_request_started_ns,access_request_completed_ns,access_status,access_view,open_request_started_ns,open_request_completed_ns,open_status,open_code,channel_id,attach_present}`；deadline=cut.started+5s，前一 GET 完成先于唯一 Open，二者完成均不晚于原截止。不输出 token/票据值。
- `targets` 恰四个固定键 `before_dial/running/fault_complete/restored`。每值有采集 start/completed 和连接数组；每连接只白名单 `index,accepted_ns,local_address,remote_address,data_base64,eof,reset,done`，不复制无关 fixture 状态。数组完整、按原 index 排列、最多 16 项，每项 Data 解码后最多 1024B（原标准 base64，不接受损坏编码）；这是该短测试仪器容量，不是产品限制。超过容量立即夹具失败并保留原诊断，不能截断后冒充完整白名单。running 相对 before_dial 恰一个新身份且其实际 Data 解码含本轮 k；该身份的接收见证必须先于选定 stats 请求。随后两个快照保持相同完整身份集合，不允许删旧补新抵消数量。
- `channel_log={started_ns,completed_ns,after_index,ready_index,ready_event,new_open_events,new_ready_events}`：ready_index 为实际原日志索引，ready_event 是该流真实 ready 原行；after_index 在实收 k 后取得，且大于其真实日志索引；两新增数组预期均为空，若出现则原样记录（仪器上限各 16，溢出失败）。统计到 fault_complete，restored 再核一次且保存其最终数组，不能在控制恢复前就结束“无自动重放”观察。
- `restored={connection_mark,control_index,control_tuple,control_connected_event,license_observation,channel_log}`：新索引>=mark且唯一，真实 connected_event 的 access/epoch/gen 与新完整 process 逐值一致，PID/access/epoch 保持、generation 递增；不伪造初始/恢复日志字段。根 `control_binding={pid,control_index,control_tuple,log_index,control_connected_event}` 保存初始归属；log_index 为该进程实际原日志索引，control_tuple 的四个键与 cut 中 client_local/client_remote/server_local/server_remote 相同。它与 cut 索引及许可样本绑定。若日志本身未带 PID，PID 来自该 log 所属已启动进程，单独记录而不改写原行。

共同 license_observation 只保存被选中的真实完整样本，最多一次/正式事件；不无限保存轮询历史，selection_attempts 是有界准备内的计数，不能用该计数证明未记录的性能。G3 只向既有 control_applied 加共同样本和纳秒下界，原事件数量、目标记录容量及原 34/62 图不新增。所有数组满时必须明确失败，不以限长静默吞掉违例。

## G3 的最小同步修正

`g3ControlFailure` 保留现一次真实业务、2250ms 选相位、原 30s 场景 ctx、真实 Reset/PauseRead 返回的 applied_ns 与既有控制/恢复/资源事件。仅把选定样本改为上述 request-window observation；使用 `r*1e6−(applied_ns−request_started_ns)>=2e9`。不改 `SampledNS`、不等待资源归位后重打 applied、也不新增 heartbeat。

原 `G3-fault/control_applied` 增加 `license_observation` 与 `license_remaining_at_apply_ns`。旧 `license_sampled_ns` 必须等于其中 process.sampled_ns，旧 `license_remaining_ms` 必须等于 stats 值，旧 `license_remaining_at_apply_ms` 必须等于新 ns 下取整，防新旧字段互相矛盾。PID/epoch/gen/access 必须和故障 phase 真实 binding、原 channel/控制 ID 一致；新 observation 必须完成于原 applied 前。后续原 t−applied 2–4s、blackhole 拒绝、许可失效后零载荷、两阶段独立资源回收全部沿现有分析器，不改 G3 预演相位记录为正式判线。

## 独立复算与兼容接线

下一四个 manifest（G1-P1-R6、G4-P2-R5、G3-P2-R3、G2-P2-R2）显式声明单一能力 `case.control_license_witness="request-window-v1"`。缺键才保持 legacy；显式 null、bool、其他字符串均拒绝。不建立第二个平行 G3 schema。该能力只绑定共享许可输入/期限回归，不增 gate criteria，不把 G1/G4/G2 附带的四个 G3 control 叶解释为完整 G3 门槛证明。

当前四 case 都实际运行完整 integration 包；声明精确要求 package=`github.com/wyw-ai/loom-oi/tests/integration`、step=vectors 下六个叶：`TestTVB2_10_ControlCut/{sqlite,postgres}`，以及 `TestTVG3_05_RevokeAndControlFailure/{sqlite,postgres}/{control_cut,control_blackhole}`。每叶只允许一次正式对应结果/事件，缺叶、重复、错误 package/test/backend/实际 Go FAIL/SKIP 都拒绝。B2 只消费 O4/control_cut；G3 消费既有 control_applied/terminated 与相关身份记录；不得凭 GoPASS、字符串摘要或另一个叶的样本补证。只有本来声明 g3_resources/g3_behavior 的 G3 case 才继续核完整 34 叶/62 phase；另外三 case 的本能力只核这四个控制叶的相位、进程/控制/流绑定与原 2–4s，不要求隐含完整 G3 资源图。四叶绑定使用原 control/ready 事件与对应局部 g3_resource phase 的完整 process/binding 原记录交叉核对；只对这些被引用记录检查结构、身份和采样时序，不要求全 34/62 图。不能因无 case.g3_resources 就跳过本能力的身份核对，亦不能据此输出整套 G3 resources 通过。

在主 analyze.py 增加小的共用 control-license 验证器，复用现有整数/完整 stats/OBS 逐流拼接规则；新能力路径必须独立核上述字段、单调顺序、完整 RTT 公式、身份、原 2–4s 边界。G3 原行为分支在此能力下用 request_started 而非 SampledNS 重算，不能先按旧式通过再忽略新式失败。B2 的实际拒绝/无目标增量/原严格资源关联必须同时成立。输出有界 `control_license_witness` 摘要和 problems；任何问题影响整体 verdict。未声明旧 case 的解析/摘要语义不变。

修改前逐字归档本次正式 d9 原分析器及依赖；旧 manifest/run/ITER、原失败 raw、G3 已冻结来源均不改。新 helper 根/子项作为 vectors 仪器库存纳新四 manifest，不能只给代码而漏 required tests。新版本 source 必须固定并完整闭包回读，不能把新 parser 诊断 PASS 倒写到旧正式 run。

## 必要回归与实现分工

Go 聚焦纯 helper/可控门闩，不借产品时间变更：
- request-window 正常下界及精确 2s 通过 / 1ns 不足失败；2250ms 选择与最终资格分开；HTTP 300ms 延迟使旧 after 式 2249ms 假通过而新式 1949ms 拒绝。
- 缺/错误身份、相反请求区间、完成晚于 applied/cut、r 为零/异常、准备超限拒绝；实际 statsRequest 错误不复用旧值。
- cut 门闩证明 first Close 前起点、两 Close 返回后终点，延迟第二 Close 被完整计入；非选中 control/数据连接不被切断。
- reader 的 terminal 时刻先于主线程稍后取结果，不能后打戳；失败 cleanup 释放并 join 所有 worker。不能依靠 sleep 增加被测许可。

Python 必须包含原样 mutation：删整个/任一字段、null/bool/float 伪整数、错 PID/access/epoch/gen/channel/control ID；只改 completed/SampledNS 让旧式过但完整 RTT 不足；使用未来或别轮 observation；新旧 G3 字段不一致；重复或缺一个六叶；GoPASS 但 raw 坏；终态早 1ns/晚 1ns、cut 区间倒置/漏计 Close；503 错码/带 channel 或票据、目标身份替换/新增、缺 offline 或资源关联均拒绝。精确边界、合法较早采样和真实终态早于主线程观察应能通过；不误把后续正常恢复的新 generation 当故障期身份。

最小改动建议：新增 `tests/integration/control_license_test.go` 及专用 helper 测试；`proxy_test.go` 仅加 cutObserved 入口；`vectors_b2_test.go` 只改 TV-B2-10（可在测试 helper 中增加精确 AccessView GET 与连接快照，不改变产品接口；keepAlive 如需 join，新增有返回 done 的 helper，旧调用不改）；`vectors_g3_control_test.go` 只接采样与字段；`experiments/product/analyze.py` 与独立 `experiments/test_product_control_license.py` / 现 G3 behavior 测试；最后更新四新 manifest 及相关规格/库存。无需修改产品 Agent.setLicense、Stats、协议、共享 SampledNS、其他向量时限或旧归档。

以上装配 5s、单能力固定六叶与 helper 为本次固定的仪器选择。复核仅使用源码与既有原件；本规格对应的新测试尚未执行，正式通过仍需后续固定源码的完整证据。
