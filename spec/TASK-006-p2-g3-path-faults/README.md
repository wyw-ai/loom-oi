# TASK-006：P2真实传输路径与故障验证（G3出口）

2026-11-04 · `source_base=570b573f8151466fd0ad42b4cdf94ac3c81d323f`（TASK-005最终集成基线，六项CI通过后已快进alpha）· 工作分支`feature/TASK-006-p2-g3-path-faults` · 集成目标`alpha` · 关联[#3](https://github.com/wyw-ai/loom-oi/issues/3)（传输）、[#11](https://github.com/wyw-ai/loom-oi/issues/11)（门槛）、[#15](https://github.com/wyw-ai/loom-oi/issues/15)（资源与运维）、[#18](https://github.com/wyw-ai/loom-oi/issues/18)（采用）。

## 基线、授权与当前状态

- 本会话用户原始Autopilot授权（2026-11-04 05:08，+08:00，记录于[TASK-004基线与授权](../TASK-004-p1-r-vertical/README.md#基线与授权)）：“基于 alpha 的最新文档、既有决议与仓库规范，启动 Autopilot，完整开发并交付 loom-oi 已确认首版。自主规划、实现、验证和迭代，持续保存进度；完成必要验证与评审后可合入 alpha 并继续。已确认事项无需重问，仅遇新的关键取舍或无法自行解除的阻塞时请我介入。”本任务沿既有授权实施、验证并在完整闭环后集成alpha，不触及main或生产部署。
- 原采用来源：[ADR-004](../../docs/decisions/ADR-004-p1-adoption.md)、[TASK-003准确采用记录](../TASK-003-autopilot-entry/approval.json)，覆盖DISC-007-R1/R2/R4及其依赖。长期契约以[B1§5–7](../../docs/architecture/p1/b1-protocol.md)、[B2§3–5](../../docs/architecture/p1/b2-security.md)、[B4§3–6](../../docs/architecture/p1/b4-baseline.md)及[共同架构§7](../../docs/architecture/alpha-v0.7.md)为准；本文件仅细化已采用G3的实现与取证，不新增approval.json或刷新旧Accepted指纹。
- 最新阶段来源：[I3-P07](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6626506980)、[I11-P07](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6626516421)、[I18-P05](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6626546930)。[TASK-005发布回执](../../docs/issue-history/2026-11-04/TASK-005-g4-published.json)记录逐字回读与旧内容保全；[门槛台账](../../experiments/gates.json)在任务启动时为G1/G4 Passed；本任务CLI生命周期修改后的73bb667首轮复证为G1负载Failed、G4直接通过但当前Inconclusive，G3仍Blocked（尚无正式G3实现证据）。G1是G3唯一gate前置，TASK-005先集成的分支安排不增加G4→G3门槛。
- [TASK-005最终CI 41802573081](https://github.com/wyw-ai/loom-oi/actions/runs/41802573081)六项已成功，alpha已快进至570b573并回读，main保持原63c1db9；事实见[TASK-005集成记录](../TASK-005-p2-g4-recovery/integration.json)。本任务先保存规格、推送并逐字回读，再实现G3。本任务不把ignored准备材料、静态枚举或历史G1/G4结果计为G3 run/ITER。
- 开始编码前核对`python tools/check_sdd.py docs/changes/DISC-007.json --phase implement --require DISC-007-R4`与`python experiments/check_gates.py --stage P2`，按[开发流程](../../docs/sdd/development-workflow.md)及[实验门槛](../../experiments/GATES.md)继续。G2三项采用保持，由TASK-007独立承接，不重复询问，也不将G2未实现扩大为本G3的新增前置。

## 目标与边界

验证真实 `ois`、`oic`、Go 客户端和 `oi connect` 的 R 单实例路径，在有方向的延迟、背压、断腿、控制失联和撤销下，仍守住半关闭、授权、失败不重放及有界回收。SQLite、PG 两后端各执行全部 17 个新场景；测试夹具负责故障，产品只修由可复现反例确认的既有契约偏差。

本任务不增加公共 API、默认配置、自动重放、业务取消/回滚、持久去重或 Exactly-once；不宣称 WAN/NAT、容量/1C1G、HA、G2 观测准入或 G7 应用兼容。G3 是受控回环上的真实产品路径故障证据，不是物理网络部署证据。原 B1/B2/B4、冻结 run/ITER 保持原字节。

## 固定输入与预算

下列均为预注册测试输入或执行预算，不是新 SLO。除列出的特例，使用现有 `testLimits()`：F=32768 B、prepare=2000 ms、close=1000 ms、retain=5000 ms、heartbeat=1000 ms、offline_misses=3、license_ttl=3000 ms、license_margin=500 ms、ping=1000 ms、idle=2000 ms、store_probe=500 ms。

| 项 | 固定设置与判定边界 |
| --- | --- |
| 短时延迟 | G3-01 在确定 FIN 门闩后延迟 250 ms，记录实际时间；不外推最大可承受 RTT |
| 背压 | G3-02 和 G3-05 两项背压撤销在环境创建前选择 B4 默认 `idle_ms=600000`；PING/H/L 不变。确认真实 writer 已进入、暂停生效且未写完后保持 250 ms。64 MiB 若未受阻即写完，判夹具失败 |
| 内容 | 64 MiB `fixture.Pattern`、块 F；半关闭剩余方向 1 MiB。增量逐字节/摘要校验，不让 fixture 保存整份载荷来代表产品缓冲 |
| 测试 socket | 可控提供方/CLI 本地端请求 send/receive buffer=32 KiB，记录实际值或平台说明；代理每方向最多一块 F 缓冲。内核、TLS、WS 缓冲与应用载荷分开 |
| 无重放 | 新 control generation 登记后观察 5 s；旧请求 ID 不再主动发送，显式新请求用新 ID。资源检查先完成，不等观察期结束才计时 |
| 限额/循环 | `n_ois=16`，其余 N_ACCESS/N_SUBJECT/MAX_LOCAL_CHANNELS=32/16/32；混合 8 Preparing+8 Open。反复回收固定 10 周期、每周期 8 条，周期号从 0 起按 mod 3 选择 RST/准备超时/撤销 |
| 门闩/完成 | 普通建立与故障门闩最多 5 s；准备门闩仍须在同一 2 s prepare 期限内成立；正常 64 MiB 完成最多 30 s。超时保留现场并失败，不算故障已生效 |
| G3 基线准备 | 最多 5 s、间隔 100 ms、连续 3 个有效空闲样本；FD 相同，goroutine 三次极差≤2，取第 3 次。此为 G3 夹具准备协议，不改 B4 通过线 |
| 资源窗口 | `vector_cleanup` 开始、任何 closeIdle/解除故障清理前记录唯一 D=start+2000 ms；全部采样和完成记录在 D 内，至少 3 个连续合格样本。G3 采样间隔 100 ms |
| 原独立时限 | 正常可观测撤销路径仍按 T_close=1 s；控制失联沿 B2-10 的 L±1 s，即 2–4 s。这些从各自事件计时，与资源窗口分开，不把网络阻挡后的远端读到错误时刻变成新 SLO |
| runner | unit 1800 s、race 2400 s；vectors 内层 40 min/外层 2700 s；两 fuzz 目标各 60 s。超时不放行 |

单通道每组件每方向至多一条在途应用消息，约 2F；多通道须按实际数量解释。2F 不表示 RSS、TLS/WS/内核或代理总内存。O3/O4 的 CPU/RSS/吞吐、重连耗时仅观察；准备期目标载荷、许可到期后转发、越权/串路、旧票据接受和隐式载荷重放保持零容忍。

## 实现与文件所有权

本spec与入口先提交、推送并逐字回读；再落实以下分工。同一共享文件只有一个编辑所有者，具体代理在实施时登记，不能多个向量各复制一套资源或代理实现。

| 工作包/独占文件 | 必要交付 |
| --- | --- |
| M1 故障仪器：`tests/integration/g3_proxy_test.go`、`tests/fixture/path_target.go` 及其测试 | 有方向代理、角色连接 ID、FIN/RST/暂停门闩、效果计数与所有 worker 退出；不改旧 proxy 的历史语义 |
| M2 G3 环境/资源：`tests/integration/g3_env_test.go`、`g3_resources_test.go`；必要的 `env_test.go`/`resource_helpers_test.go` 最小扩展由本包所有者统一修改 | 首次OIC启动前的路由/目标装配与最后冻结两个入口，online/control_fault 两类固定 profile、绝对期限、证据序列与反例。复用已有严格采集，不削弱 G1 online 判定 |
| M3 传输：`tests/integration/vectors_g3_transport_test.go`；`internal/client/backpressure_test.go`、`internal/tunnel/backpressure_test.go` | G3-01/02/03/04，客户端与桥接局部缓冲断言；依赖 M1/M2 稳定接口 |
| M4 控制/额度：`tests/integration/vectors_g3_control_test.go` | G3-05/06；与 M3 并行，使用同一资源/故障门闩；不写 M1/M2 文件 |
| M5 root 集成：任务 `consistency.md`、`experiments/manifests/G3-P2.json`、必要 analyzer/runner/CI 与对应 Python 测试、当前文档 | 精确预注册、schema 重算、旧证据兼容、完整验证、独立评审与阶段闭环 |

实施前审查的CLI活跃流取消风险已在实现阶段独立复现：目标确认20字节已到且无EOF，取消Listen后超过T_close=300ms加1s诊断窗口仍未返回；测试再主动FIN才返回Completed。原因是`Listen`取消只关闭监听器并等待worker，而`Forward`在附着后使用Background桥接。原失败日志及诊断overlay留在ignored coverage，不把它当正式产品run。在既有“停止CLI关闭转发与通道”契约内修复Forward的调用方取消传播；覆盖READY前、活动流、阻塞写、已Completed但等待远端关闭以及已有远端ABORT的竞态，保留首个终态，不把取消伪装为自然EOF。取消须释放本地连接与WS，不能仅换New(ctx)后留下阻塞本地读或无界drain。归M3客户端生命周期，不新增业务取消承诺或第18个正式场景。产品修改后旧G1/G4仅保留历史适用范围，须以最终源码重新预注册和验证后恢复当前门槛。

产品修复按包单独指定所有者，先保存最小反例及对应 B1/B2/B4 条款。UTF-8 LF；各所有者只格式化所持文件，root 最后统一格式化。未知产品取舍单列待确认，不靠改阈值或扩大范围消除失败。

## 故障仪器必须证明什么

`g3FaultProxy` 透明传递 TLS，每连接不可复用 connID，方向固定为 `client_to_server` / `server_to_client`。提供 WaitAccepted、BindRole、PauseRead/ResumeRead、HoldNew/ReleaseNew、Reset、WaitClosed、Snapshot 等测试操作；暂停确认必须说明当前 F 块是否已读入/转发，不把故障前在途字节算成故障后新读取。自然 EOF 只向另一端 CloseWrite，双向结束才全关；RST 用测试拥有的 socket 设置 SO_LINGER=0，不能用普通 Close 冒充。Reset须在目标socket设置SO_LINGER=0并执行Close的施加点，用测试控制器单调时钟保存reset_applied_ns；该时刻即对应phase的vector_cleanup起点。记录不能等WaitClosed、另一端关闭或join完成后再生成，后续动作全部占用D=reset_applied_ns+2s。WaitClosed与Close/join是独立操作；Close须停止accept、解除门闩并join worker。

每 OIC 独立 Access 代理。启动唯一连接经注册事实绑定 control；数据腿用独占识别区间、新 accept ID、真实 Channel/票据状态和目标门闩绑定，不用“最后一个连接”猜测。混合额度的并发 provider 集合只核对集合基数，不对未知映射的某一连接定点注入。消费者独立 API 代理，admin/stats 绕行；Open 后清理客户端空闲 HTTP，再附着并识别数据连接，保持客户端 base URL 与 attach host 校验。CLI 优先切已识别 provider 腿。

`PathTarget` 提供 Accepted、RequestFIN、ResponseFIN、EffectCommitted(id)、WriterEntered/Returned、ReceivedBytes、Closed 门闩。效果在完整请求及 ID 校验后增加，每次同 ID 请求都再次增加，禁止提供方去重掩盖重放；第一响应字节前有独立门闩。反向半关闭必须在 CloseWrite 后继续完整读入 1 MiB。所有门闩可取消，结束后目标连接/worker 可核验退出。

CLI 监听器及各代理在首次冻结前创建并完成正常请求预热；失败后保持其监听器拓扑到检查结束，不能杀 CLI 使 OIS 少一个 FD 来抵消泄漏。`oi connect` 是持续监听器，断言单条本地连接 RST/失败报告，不要求整个 CLI 必然非零退出。代理/目标自身资源与 OIS/OIC 分记。

## 6 组、17 个必需场景

层级固定 `TestTVG3_XX_Name/{sqlite|postgres}/<scenario>`，每叶子独立环境、无 t.Parallel、一次冻结。每项必须有行为断言与真实资源序列，OBS 不代替断言。

| 向量/根函数 | scenario | 触发与独立验收 |
| --- | --- | --- |
| TV-G3-01 / `TestTVG3_01_HalfCloseDelayed` | `consumer_fin_first` | READY→消费者 FIN→目标 RequestFIN→延迟 250 ms→完整 1 MiB 响应/FIN；消费者仅在响应 FIN 后正常 EOF，双 FIN 后 Completed |
| 同上 | `target_fin_first` | 目标先前缀/FIN，消费者确认后延迟 250 ms 再写完 1 MiB/FIN；目标仍收全，读 EOF 不提前关闭相反写方向；Completed |
| TV-G3-02 / `TestTVG3_02_Backpressure` | `upload_target_paused` | 暂停目标应用读，真实消费者写 64 MiB 受阻；保持后释放，完整数据/FIN/Completed，应用缓冲有界 |
| 同上 | `download_consumer_paused` | 目标写 64 MiB、消费者不 Read，证明 writer 受阻；释放后完整接收，不能因合法背压误触发关闭 |
| TV-G3-03 / `TestTVG3_03_DataLegInterrupted` | `consumer_open_rst` | READY 且目标收标记、未响应时 RST 精确消费者腿；存活侧 ChannelInterrupted，消费者传输失败而非 EOF，目标终止、旧票据不可用 |
| 同上 | `provider_open_rst` | 同阶段使用 CLI 桥接并 RST provider 腿；本地 TCP RST/CLI 失败报告、OIS close_reason=ChannelInterrupted、目标退出 |
| 同上 | `consumer_preparing_rst` | Hold provider，仅附着消费者并由票据/状态证明；RST 已附着腿，从不 READY、目标载荷 0、ChannelInterrupted，释放晚 provider 不能复活 |
| 同上 | `provider_preparing_rst` | 仅 provider 附着、余消费者票据后 RST provider；从不 READY、目标载荷 0、ChannelInterrupted；晚消费者 410、旧票据无效 |
| TV-G3-04 / `TestTVG3_04_ResponseLostNoReplay` | `consumer_leg_loss` | Go 消费者发唯一 ID/FIN；目标效果=1 且响应字节=0 后 RST 消费者腿；原调用失败。完成首轮资源检查后控制重连，新代后 5 s 旧 ID 仍 1；新 ID 显式新通道成功且效果 1，完成及最终资源检查后再次确认旧 ID=1、新 ID=1 |
| 同上 | `provider_leg_loss` | CLI 本地请求经过同一效果/零响应门闩，RST provider；本地 RST/失败；其余重连、计数、新请求断言相同 |
| TV-G3-05 / `TestTVG3_05_RevokeAndControlFailure` | `upload_backpressure_revoke` | 上传受阻门闩成立后撤销策略；管理返回后旧流不再 Preparing/Open、新 Open 403；释放目标读以观察 RST，独立核验关闭宽限及资源 |
| 同上 | `download_backpressure_revoke` | 下载受阻后吊销消费者令牌；返回后旧流非 Preparing/Open、新请求 401；恢复读只为观察失败，不要求读完 64 MiB 才清理 |
| 同上 | `preparing_revoke` | 已附着一腿、另一腿明确 Hold；管理撤销返回后才晚附着，从不 READY、目标载荷 0、Revoked、晚腿 410 |
| 同上 | `control_cut` | READY、持续小标记；禁止新 control 完成握手且 RST 原 control，两数据腿保持；2–4 s 内 LicenseExpired/AccessOffline、期间无新准备，到期后无新标记转发，新代不复活旧流 |
| 同上 | `control_blackhole` | 只暂停原control的OIS→OIC方向并确认，保持PID与数据腿，不以SIGSTOP替代；仍按2–4s核验原流的许可/关闭。暂停生效后新发Open不得造成OIC新增目标准备、READY或目标载荷；OIS尚未判离线时可短暂创建Preparing，不据此判产品失败。确认OIS离线后新Open须AccessOffline，释放积压control字节也不能恢复旧流或被阻挡的准备 |
| TV-G3-06 / `TestTVG3_06_QuotaAndCleanup` | `mixed_limit_reuse` | 8 Open+8 仅 provider 附着的 Preparing，核验混合状态后第 17 条恰为 429 ResourceExhausted；清理合格后重复同量占满，再清理；无额外状态/额度泄漏 |
| 同上 | `repeated_failures` | 同进程同 profile 10 周期：8 Open 逐腿 RST；8 仅 provider 附着超时；4 Open+4 Preparing 后撤销，按 mod 3 轮换。每周期独立原因、票据、额度、资源断言后才恢复策略进入下一周期 |

断腿的一侧可能收不到 ABORT，按存活腿、OIS 终态和本地 TCP 错误合并判断，禁止把自然 EOF 当中断成功。准备门闩未在 2 s 内成立不能以恰好 PrepareTimeout 通过。额度场景 Open 每 200 ms 传固定小标记，控制故障也用同周期标记避免 IDLE 先发生；停止时收齐 writer，不重放业务请求。

G3-03/04及G3-06的RST周期由代理在RST施加点保存vector_cleanup；调用方取得事件后沿用同一时间，不重新起算。随后才WaitClosed、释放目标响应门闩并回收worker，让写操作真实观察中断并退出；这些动作全部占用从RST施加开始的同一2s窗口。一个周期有多个RST时以第一个RST施加点为唯一清理起点，后续RST也在该窗口内。不能将 worker 人为锁住 5 s 再要求 2 s 回收。故障前效果与响应字节、原调用失败、原 ID 在完整窗口内不增加、新 ID 成功是四条独立事实；两个响应丢失场景都在显式新请求完成时、最终资源检查后再次确认旧 ID 仍为 1、新 ID 恰为 1，覆盖仅在新通道创建时发生的错误重放。从首次效果提交到最终断言不重置提供方或计数、不去重，保留每个 Accepted 请求 ID 与效果日志；“本次计数 1”不外推端到端 At-most-once。

G3-05正式业务控制故障注入前要求同代 LicenseRemainingMS≥2000。无业务的独立profile预演只建立相同故障拓扑，可在正常控制周期先选择相位；从实际故障施加点开始其5s采样/冻结窗口，不能先等离线或FD关闭再开始。不能仅用取整后的 0 ms 宣称到期：结合同代 Closed[LicenseExpired] 增加，再发送此前从未发送的 `after-expiry:<id>` 标记并确认目标未收；释放积压后再尝试旧通道仍不转发。若 AccessOffline 先结束，记录实际原因与旧流不能写，精准每次转发边界由既有 `TestLicenseExpiry`、`TestLateHeartbeatAfterLapse` 及 B2-11 补足，不伪称连续时间全测。撤销返回、OIS 状态结束、socket 本地关闭与释放后远端看见 RST 分开记录，既不把代理延迟误判为撤销不生效，也不以 Closed 代替 socket 清理。

## 资源夹具：复用已有严格核心，增加 G3 拓扑

当前 `resource_helpers_test.go` 已有 `statsRequest`/`validateStatsJSON`/`processAlive`、`resourceSnapshot`、`resourceProfile`、`processAtBaseline`、`resourceSampleOK`；HTTP200、完整字段、非负统计、活 PID、Linux FD/RSS 可用均严格校验。`newEnv` 预热 alice/carol 并首次 baseline；`baseline()` 禁止重采，当前实现为 10 s/50 ms/3 次；`settled()` 从 closeIdle 前的 vector_cleanup 开始，50 ms 采样、三连续合格、PID 在检查中固定、绝对 2 s。不是旧稿所称“错误变零/单向上界”的旧 helper。保留现有 7 项 `TestResource*` 回归，Linux FD=-1 仍为无效；Windows 不能替代正式 FD 证据。

G3 需新增以下测试基础，不改变产品契约或 G1 的 online 比较规则：

1. **装配与首次冻结分离。** M2负责两个仅测试用、默认行为不变的生命周期入口。第一入口在首次startOIC之前：OIS监听地址已就绪，G3创建PathTarget及每Access独立代理，按Access提供OIC端点和exposure/target配置；startOIC使用该预定路由，未提供时仍走原e.acc。不能等默认OIC已启动才靠重写配置、更换generation或停止进程接上代理。第二入口在所有进程/CLI/消费者已起且全部正常路径预热完成之后：选择本规格5s/100ms/三样本极差≤2的唯一冻结实现，替代该环境的默认baseline调用。所有代理/目标/CLI均在冻结前纳入拓扑；两个入口和必要envOpts/startOIC改动均由M2单一所有者完成，M1仅提供仪器。不得先执行默认 baseline 后再覆盖 `resourceBase`。预热须实际完成内容、双 FIN/Completed、目标关闭、双方活动通道/票据 0，并清理测试自有 idle HTTP；不停止产品进程、不强制 GC。使用原严格采集/数值比较核心，记录所有准备样本及最终 profile。
2. **两个独立控制故障 profile。** 仅 `control_cut/control_blackhole` 在真实业务故障前，以同一进程无业务流预演对应控制策略，分别冻结 `control_cut`、`control_blackhole` 拓扑参考，再恢复并验证原 online profile。明确连接保留/关闭、预期 OIS 在线数、各 OIC Connected/Registered、重连握手和 pending 连接数量；黑洞不一律等于 disconnected。重连策略采用受控的握手阻挡，不允许不确定数量的重试socket污染基线；切断场景的候选为接收下一条控制TCP但在连接OIS之前Hold，固定一个pending握手，记录其connID与10s现有拨号期限；黑洞场景须区分OIS已离线/关原控制socket与OIC仍因下行暂停保留旧session的状态，不套用切断场景的Connected=false。预演和实际使用同一规则，具体谓词须在M1/M2交界冻结后再实施向量；无法在固定准备预算内稳定则夹具失败。参考不能取自实际故障后的样本。
3. **拓扑判定器。** 现有 `resourceSampleOK` 要求每 OIC Connected/Registered，不能用于上述故障中间态。新增 G3 判定器，按故障前冻结的明确拓扑谓词比较，继续要求全部预期进程活着、通道/票据 0、goroutine±10、FD 精确相等。epoch/generation 用实际本轮绑定验证，不硬比较预演与实际故障的旧 generation 数字；同一 scenario 不允许意外进程替换。恢复后再次比较原 online profile，不重采。
4. **统一清理起点。** 行为失败/完成事件和其原时限独立记录；在该向量进入资源清理时立即保存本 phase 唯一 `vector_cleanup`，位于 closeIdle、解除阻挡、目标 worker 回收等动作之前。RST触发phase严格使用代理保存的施加时间，WaitClosed/join属于窗口内动作；首次故障清理不得等socket清完、重连成功或5s无重放观察完成才开始。控制故障phase的起点固定为向量行为检查完成：原流已在L±1s内终止，且OIS已判offline、随后新Open实际返回AccessOffline；两项均观察后立即取origin，不能先等控制socket/worker/FD归位。保持故障拓扑并独立通过 2 s 检查，随后才能解除控制阻挡；按既定 5 s 建立门闩预算等待新代登记并记录实际重连耗时，再开始独立的 online 清理 phase，仍比较原 online 基线且不重采。恢复 phase 不替首次故障检查延期，也不把重连塞入 2 s 资源窗口或引入 reconnect≤2 s 的新 SLO。每个 phase 的共享绝对 deadline 约束其所有 HTTP 请求、间隔和最终完成，超时/坏样本立即失败；合法但尚未归位的样本重置连续计数。
5. **连接证据。** FD 总数相等之外，代理/目标必须证实本轮数据连接已关闭，不能用少一个 control FD 抵消多一个 data FD；需要诊断时采 `/proc/<pid>/fd` 分类/端点，不用其后验结果改基线。产品进程与测试 worker/代理缓冲分别记录。

初始phase身份采集同样消耗上述原2s窗口。control_cut若首个被Hold的新尝试尚未接受，允许在原D内等待其真实pending身份；不能等待FD、goroutine或worker归位后另起期限。begin保存完整`binding_samples`（正常路径也至少一个），记录初始采集与100ms间隔，最终样本确定本phase的PID/代数/control/pending身份；这些样本不计入清理后三连续合格。正式采样不早于该最后采样结束，期间身份不得替换。组合行为结束事件由故障施加起并行观察，不能在WaitAccepted返回后才给已发生终态补一个更晚的origin。此为首次预注册前的仪器接线澄清，不增加准备或清理预算。

普通 15 场景只用 online profile；两个控制故障各检查故障态和恢复态。每后端最少资源检查为：G3-01=2、02=2、03=4、04=4（每叶子故障后/显式新请求后各一次）、05=7（三项撤销各一次，两项控制各两次）、06=12（混合两轮+10周期），合计 31、双后端 62。预注册须按完整叶子与 phase/cycle 精确列举，重复某一 phase 不能补足另一项；总数仅辅助核对。准备预演检查另记，不计入这 62 项。

### 62项资源阶段的精确展开规则

每个下列叶子均在sqlite、postgres各执行一次，完整路径为`TestTVG3_XX_Name/<backend>/<scenario>`；不得用一个后端、父PASS、额外phase或重复周期补另一项。键为`(Package, Test完整路径, backend, scenario, phase, cycle)`，Package固定为`github.com/wyw-ai/loom-oi/tests/integration`。单次phase的cycle为JSON null，混合轮为0/1，失败周期为0..9；每项恰有一个begin/complete和足够原始样本。

| 向量/叶子 | phase | cycle | 每后端检查数与起点 |
| --- | --- | --- | --- |
| G3-01的consumer_fin_first、target_fin_first | exchange_complete | null | 2；内容与双FIN完成后开始online清理 |
| G3-02的upload_target_paused、download_consumer_paused | transfer_complete | null | 2；受阻已证明且释放后64MiB/双FIN完成 |
| G3-03四个叶子 | interrupted_cleanup | null | 4；各指定腿RST施加点 |
| G3-04的consumer_leg_loss、provider_leg_loss | fault_cleanup、new_request_cleanup | null | 4；前者为效果已提交/响应零后的RST施加点；后者为新代5s观察后显式新请求完成 |
| G3-05的upload_backpressure_revoke、download_backpressure_revoke、preparing_revoke | revoked_cleanup | null | 3；撤销进入清理时、释放门闩/closeIdle前，独立T_close断言保留 |
| G3-05的control_cut、control_blackhole | control_fault_cleanup、online_restored_cleanup | null | 4；前者原流按B2结束且OIS离线后新Open拒绝，两项已观察即起点并保持故障，后者前一检查通过再恢复新代后另计，分别用固定故障/原online profile |
| G3-06的mixed_limit_reuse | quota_round_cleanup | 0、1 | 2；每轮8 Open+8 Preparing及第17条429核验后、主动清理前 |
| G3-06的repeated_failures | failure_cycle_cleanup | 0..9 | 10；0/3/6/9为8 Open逐腿RST，从第一个RST起算；1/4/7为8条provider已附着的prepare超时；2/5/8为4 Open+4 Preparing撤销。通过后才恢复策略、进入下一周期 |

每后端31、双后端62项；34个叶子各有唯一online profile，另有4个控制故障profile，合计38。所有profile均在真实业务故障前冻结、同叶子所有phase/cycle复用同一组PID；预演记录明确stage=preparation，不占上述62项。正式begin/sample/complete为stage=validation，绑定明确scenario/phase/cycle。RST相关20项的origin_event为first_reset_applied，begin的start沿用代理施加时刻；其余phase按表中事件起算。

## 预注册、原始记录与有效性

新增 `experiments/manifests/G3-P2.json`：id=G3-P2、gate=G3、Implementation、case.kind=product-p2、repeats=1、load=false；Linux、Go/SQLite/PG 精确版本及隔离事实沿 B4/最终 TASK-005 基线填入。复用现有 runner，不另建编排系统。每根函数 entry 的 tests 为上表根名，subtests 同时列 sqlite/postgres 父级和其全部 scenario；例如 `sqlite/consumer_fin_first`。6 根/12 后端父级/34 叶子均须实际 PASS，必要项 skip/missing/fail 均不通过。

| G3 criterion（与台账同名） | 必需映射 |
| --- | --- |
| 半关闭与背压 | TV-G3-01/02、故障仪器校验、三组件缓冲局部断言、TV-B1-06/07/09 |
| 撤销时限 | TV-G3-05、TV-B1-11/12、TV-B2-10/11/12/13 |
| 响应丢失不重放 | TV-G3-03/04、TV-B1-10/13 |
| 清理与限额 | TV-G3-03/05/06、G3 资源判定器校验及全部 62 项资源序列、TV-B1-14/16、TV-B4-03 |

仪器根测试预注册：`TestG3ProxyHalfClose`、`TestG3ProxyDirectionalPause`、`TestG3ProxyReset`、`TestG3ProxyCleanup`、`TestG3TargetEffectLatch`、`TestG3TargetHalfClose`、`TestG3TargetCleanup`、`TestG3ResourceTopologyProfiles`、`TestG3ResourceDeadlineDoesNotReset`、`TestG3BaselineCannotBeReplaced`。位于 integration，随 vectors 实际运行；内部子场景在首次正式运行前全部展开登记。复用七项 TestResource*；新增资源反例覆盖错误/缺字段、错误 topology、意外 PID 更替、故障后覆盖基线、样本/完成越时、错误期间不重启期限、缺某个 phase/cycle 和两个 profile 混用。

缓冲入口为 unit 的 `internal/ois/relay::TestBackPressureBoundsBuffering`、新增 `internal/client::TestStreamBackpressureBounded`、`internal/tunnel::TestBridgeBackpressureBounded`，以可阻塞读写与有效在途/分配断言验证每方向一消息；集成单通道检查 OIS/OIC 最大应用载荷≤2F。若客户端无法直接集成采样，明确局部证据，不能用 RSS 冒充完整缓冲证明。manifest 记录包/根函数唯一对应，按现有分析器实际能力避免跨包同名歧义。

沿用 `b4-resource-v2` 的字段语义与原始四段结构（baseline/begin/sample/完成）、同一测试控制器单调 ns、PID、完整双方 stats、profile_id/check_id/start/deadline/completion。G3 新增有版本的 `g3-resource-v1`、`g3_resources_baseline/begin/sample/complete`，携带 profile_kind、scenario、phase/cycle 和连接拓扑；单独 `case.g3_resources` 预注册并重算。不能在 b4-resource-v2 中把 online 健康条件改为可选，不能让 G3 offline 行被旧 G1 parser 当作有效 online 行。现有 analyzer 不认识新 schema，故 M5 的最小解析/反例测试是本任务必要适配，写 OBS 本身不等于已受 gate 检查。

其余 OBS 用 G3-fault/G3-effect/O3/O4，绑定向量、完整测试路径、后端、scenario、phase、单调时刻、连接/通道/请求 ID、方向、字节、原因和门闩事实。记录施加/释放/终止/清理、效果提交/零响应/重连代数/完整 5 s 计数快照/显式新 ID、每次统计。跨产品进程事件只能用控制器实际收到时刻，不能减不同进程墙钟造精确时限。分析器从 raw 独立核验叶子 PASS、阶段顺序、计数/界限/时限、资源 topology、零容忍、质量及来源。

全部原 B1–B4 随 integration 运行；B1-11 每后端 200 次原竞争不删减。显式引用的旧向量按现有根函数及 sqlite/postgres 登记，不能仅依赖父 PASS。TV-B4-06 因 load=false 不作负载通过线；恢复回归需要的 PG 工具和 32 MiB tmpfs 按现有 evidence workflow 保留。旧 source snapshots/run/summary/SHA256SUMS/ITER 不改；分析器变更前保留旧证据绑定源码，回归其逐字重算。

## 质量、流程与完成出口

完整受管产品分母固定为 `cmd/`、`internal/` 全部非测试 Go 源码，不混入集成覆盖率、不临时删文件：`go test -count=1 -covermode=atomic -coverprofile=coverage/unit.out ./cmd/... ./internal/...`，`go tool cover -func` 总体≥95%。Linux 双后端、race、两个 fuzz 各实际 active ≥60 s、vet/staticcheck/govulncheck、四平台构建与 Windows 既有 CI、仓库 Python/结构/采用/材料/迭代检查保持通过。fixture 包测试若不在现有 unit 范围，必须显式执行，不能当作默认被覆盖。

实施顺序：核对已恢复G1并读取TASK-005最终alpha集成基线→单任务spec提交回读→M1/M2 仪器校验→M3/M4 真实反例与必要修复→一致性表/独立评审→提交完整预注册→干净固定源码运行 evidence。每次正式 run 新目录，保留 manifest、源码清单/subject/prereg SHA、environment、execution、raw、stderr、summary、report、SHA256SUMS，失败与超时不删不改。按 verify→独立原始证据核验→综合分析/新 ITER/KB/图文/Issue 追加回读→最后更新 G3；run 与对应分析一同保存，未完成闭环不写 Passed。

本任务无必需的新产品取舍。若反例要求放宽撤销/许可/资源合同、扩大网络/业务支持或自动重放，明确差异和具体确认点，仅暂停依赖该新选择的部分。所有 34 叶子、62 项新资源阶段、仪器与缓冲断言、适用旧回归和完整质量实际通过，且固定版本原始证据/一致性/回读闭环完整后，才按既有授权集成 alpha。准备材料与 manifest 自检不代表 G3 已实现或通过。

## D19：首轮 G1 负载失败的最小诊断补全

固定源码 `73bb667e9d1535a4ccee5eb15aa440ff636964b9` 的正式 `G1-P1-R3-20261104T103502Z-186529` 为 observed/fail，原 run 精确复算一致。唯一失败为 `TestTVB4_06_Load/sqlite` 最后 64 MiB × 32 并发组；raw 第 6231 行保留 `received 0: channel aborted: IdleTimeout`。原始 OIS 日志中通道 `ch_s2sbfis7mpkkmik2e2hipptl6a` 于 10:44:28.552507701Z 记录 READY、10:44:30.792517324Z 首先记录 closing/IdleTimeout；OIC 终态 Up=0、Down=15。两进程墙钟只用于关联轨迹，不替代单进程单调时限证明。32 条均 READY，31 条分别完成 67108864 B 响应、1 条为零响应；该组 OIS 中继增量 2080375264 B 恰等于 31×67108864＋32×15。六次资源回收通过不能替代业务成功；原失败、原 summary、原始文件和新 ITER 独立保存，不因后来重跑通过而撤销。

当前证据只能证明请求 DATA 已写入目标 socket，不能证明目标已消费请求、收到 FIN 或开始响应。竞争解释包括：消费者 FIN 写失败/未完成、FIN 在转发路径受阻、目标尚未读到 FIN、目标首响应尚未写出、首响应已产生但后续转发受阻。原日志缺少上述边界，故不宣称已定位产品根因，也不把高 CPU 占用直接等同于调度停顿。负载所走 `Dial→Stream` 与 OIC/OIS/目标路径在 `2ec3670` 至 `73bb667` 间未改变，新增 CLI `Forward` 取消修复不是该调用路径。原 O3 `mib_per_s` 使用计划字节量，失败周期不能解释为成功有效吞吐。

本次只补可核验的诊断与计数，不改 B1/B4 契约、`idle_ms=2000`、原必需测试、并发/载荷、资源 profile、2 s 资源窗口或 gate 判线：

1. `tests/integration/vectors_b4_test.go` 的 Load worker 保存通道 ID、实际 Write 字节/错误、CloseWrite 错误、读入首字节与完成的单调时刻、实际接收字节及终态。Write/CloseWrite 的原首错立即保留；各 worker 结束后集中输出一次记录，不在每个 DATA/读写块打印。O3 保留计划载荷，同时用实际收到的字节计算有效吞吐并明确成功/失败条数，避免失败响应被计成完整载荷。
2. `tests/fixture/target.go` 增加每连接真实本地/远端 TCP tuple，以及首次请求字节、请求 EOF、首响应 Write 进入/返回、最终写出字节/错误、连接结束的一次性记录。时间在目标测试进程内保存，记录接口允许调用方与 consumer 事件使用同一单调基准；不对每块载荷增加共享锁/日志。映射使用已观测请求和真实 tuple，不按并发 accept 顺序猜通道对应，不记录凭据或任意业务正文。
3. `internal/ois/relay` 只在实际 IdleTimeout 判定时追加同进程 lastData/idle age、两方向 FIN 接收/成功转发状态及必要的 forwarding/队列状态。`internal/oic/relay` 的既有终态只补本通道 FIN、目标首读/首 leg 写边界及其结果的必要诊断；如边界位于共用 bridge，只增加有界内存快照和有断言的局部测试，不新增热路径逐帧日志、远程管理 API 或授权能力。日志关联使用 channel ID；TCP tuple 只用于该测试的本地目标归属，不输出令牌、票据、私钥或目标凭据。
4. 使用同步门闩验证记录能区分“请求 FIN 未完成”“目标已收 FIN 但首响应未写出”“响应已进入写路径但尚未完成”三阶段。保持原 Idle=2 s；门闩仅控制已存在的具体读写边界，超时仅防挂，不以 Sleep 猜发生顺序。前两类可合法产生同样的 Up=0/Down=15，仪器测试不冒称已重现正式 run 根因。若新实测证实 lastData/FIN 传递等具体实现错误，再把精确时序收窄为确定性产品反例后修复，不预先选择某个解释。

该计划已随失败证据/综合分析在52e69e2提交，439c609完成Pending指纹后推送并逐字回读，之后开始诊断实现。仅运行必要局部单元/仪器与可复现的 targeted Load 开发采样，所有失败日志保留；不将开发窄跑拼成正式 run，不以其放行 G1/G4/G3。实现、验证与当前根因仍分别记录；正式补证须重新绑定干净固定源码并保留本轮失败。完整质量分母及既有历史复算保持不变。

D19开发诊断的Linux补充入口为`.github/workflows/task006-diagnostics.yml`，只由固定`diagnostics/TASK-006-d19-load`分支触发。固定Go1.27.3、Ubuntu24、SQLite原6组Load（1/8/32并发，各1/64MiB），最多5轮，首个失败后停止并保存原stdout/stderr/退出码/进程日志、固定提交与主机信息。先显式执行新增边界仪器；不改2秒idle，不将这些窄开发采样写成formal run、ITER主证据或Passed，也不以若干通过轮抵消73bb原反例。它用于收集缺失阶段并决定下一项修复，独立于最终六项CI与完整正式G1/G4。

## 检查点

- 已完成：初始规格c6c0500推送并回读7文件；控制起点与已复现CLI取消回归的546598e再推送并逐字回读1文件。M1/M2仪器与六组17场景已实现，本机双后端34叶初轮通过，后续收紧清理起点及诊断的受影响窄回归通过。完整产品单测覆盖95.9%，静态/安全检查通过；不把Windows FD=-1当Linux验证。
- 已完成产品修复：Forward调用方取消的生命周期与七根/11子场景；本工作区G1/G4当前适用性退回Inconclusive，保留完整历史对象。M5已完成严格资源分析、Package/Test绑定、行为原始复算及独立反例；完整experiments 112项、tools 35项测试通过，结构/8份SDD/采用/材料/历史迭代检查通过。准确映射、失败保留、适用性和正式先后见[一致性核对](consistency.md)。
- 新检查点：73bb667已提交推送并回读34文件，三份manifest已固定；首轮Linux G1-P1-R3负载失败、G4-P2-R2四项通过，完整覆盖96.0%。[ITER-008/009](../../docs/evidence/p2-g1g4-revalidation-009.md)保留两run，当前G1 Failed/G4 Inconclusive；CI六项尚未全绿，alpha不变。D19诊断计划与证据先保存/回读，再实现诊断。正式G3、新源码复证、Issue闭环和alpha集成仍未完成，原B1–B4判线保持。

D19实现检查点：边界诊断、原负载实际字节/成功吞吐、真实TCP tuple映射与elapsed_ns已实现。独立复核发现的新日志前置阻塞与额外日志可能延迟janitor均已消除：只把有界字段加入原closing/closed的同一次日志调用，保持原位置/次数；不新增诊断队列或worker。门闩验证原ABORT与状态先于sink调用。完整Windows cmd/internal单测（包含PG）覆盖96.0%，局部仪器、vet/staticcheck/govulncheck通过；旧取消fixture已补完整LocalAddr/RemoteAddr，不改生产契约。一次Windows SQLite原6组开发Load完成82/82响应，独立tuple/字节对账一致，不代表原Linux失败已定位或G1通过。原失败保留，接着固定源码执行最多5轮Linux诊断，首失败停止分析。


### D19 Linux 首次边界诊断与运行时追踪

固定 `916293abb455199c9ac570984cd12a1ab994c6ab` 的开发诊断 [41823127149](https://github.com/wyw-ai/loom-oi/actions/runs/41823127149) 在原六组负载的首轮失败，按预定上限立即停止，未跑第二轮。最后 64 MiB×32 组为 15 条完整、17 条失败；逐通道→OIC真实TCP tuple→Target反向tuple唯一对应，17条均收到完整15 B请求与FIN、Target首64 KiB Write成功、OIC首32 KiB Read与首DATA Write成功。17个OIS首终因均为IdleTimeout（消费者其中4条最终见ChannelInterrupted不覆盖首因），判定时 consumer FIN已收且已成功转给provider，provider正在等待consumer写完成。可以排除本次样本的请求FIN丢失和目标首响应未产生；不能把这些新部分响应反例倒写成73bb原零响应反例的已知根因。

该组 elapsed=11654095136 ns，消费者实际 received=1113302139 B，完整成功=1006632960 B，对应91.103/82.374 MiB/s；所有已收到字节与完整成功量分开。OIS/OIC/test进程累计CPU分别9600/4710/7480 ms，runner实见2逻辑CPU/约7.75GiB内存；累计占用不能证明具体2秒调度停顿。消费者与Target共享单调原点，失败流Target首写→consumer首读为21.963–1761.723 ms；原raw缺单调原点对应墙钟，只能借test事件夹住约4.895 ms区间，不能伪称纳秒精确跨进程时线。六次原资源回收均完成；不能抵消响应失败。

下一项最有区分度的诊断只运行一次原六组Load，并通过 `.github/scripts/task006_trace_overlay.py` 生成临时Go overlay，同时采集consumer测试进程及真实OIS/OIC的runtime trace，区分network wait、同步锁等待和runnable调度延迟。固定入口为 `diagnostics/TASK-006-d19-trace`；原/overlay源码、映射与各自SHA、原始trace、stdout/stderr/退出码、进程日志、CPU/内存/cgroup条件一并保存。每条OIS腿reader/writer及consumer仅登记一次channel标签，不记录业务正文或凭据；不增加公开端点。trace从进程初始化开始，flush只发生原六组及全部资源断言之后；flush的5秒诊断上限不替代产品T_close或2秒资源窗口。追踪本身有开销，overlay不是发布产品，不作为正式run或通过证据，也不降低原阈值。若新trace不复现，只记录未复现，不把它当作缺陷消失。

916293a完整CI [41823082191](https://github.com/wyw-ai/loom-oi/actions/runs/41823082191) 的build-lint、unit/race、fuzz、integration通过，python/windows仍因当前G1失败、G4适用性未恢复而阻挡；未改CI门槛。#3/#4/#11/#18最新阶段仍为I3-P08/I4-P06/I11-P08/I18-P06，未出现新的采用变化。alpha/main仍分别为570b573/63c1db9。


### D20：从追踪反例收窄有界消息缓冲修复（文档先行）

一次固定 `1c6c1168a0ce340faba3f496b5829bc8ef14c739` 的D19 overlay诊断 [41827044352](https://github.com/wyw-ai/loom-oi/actions/runs/41827044352) 完成并保存原始trace及进程日志。末组31条完整，1条收到8486912 B后`ChannelInterrupted`；这与前轮17条`IdleTimeout`是两个不同观测，不能混写首因。3个服务和测试进程trace均完整可解析；cgroup `cpu.max=max 100000`、前后throttled_usec/nr_throttled均0，仅排除该次cgroup配额节流，不能排除物理竞争或追踪开销。

失败通道的consumer DATA writer G1124在TCP Write等待约1.277秒；其ping G1125先等待同一WebSocket writeFrameMu，取得锁后在setupWriteTimeout分配中经历GC assist，期限已到时才注册context.AfterFunc。trace明确记录G1125创建G1285，后者调用setupWriteTimeout.func1→Conn.close→TLS/TCP Close，再由reader G1020销毁FD并进入legGone/ChannelInterrupted。这证明本样本的实际关闭路径，不直接证明第一周期未收pong就被误断：此前已有一次Ping未在其预算内完成，仍须区分冻结“两次无pong”规则与当前代码的activity处理。不得据此直接跳过Ping、延长deadline、把DATA当永久pong或修改B1/B4。

同一consumer G1113的读取执行中多次出现约160–420ms间隔，相关栈含`io.ReadAll`、`runtime.growslice`、GC assist与抢占；现有coder/websocket.Read每消息调用io.ReadAll，OIS与consumer在每个32KiB DATA都反复扩容与复制。该事实支持先减少既有固定帧路径的临时分配，是有条件的修复候选；不能仅凭累计CPU或一次trace承诺它已解释73bb零响应和916293a全部Idle反例。

最小实施范围保持全部协议/错误/权限/时限/负载及2F应用载荷预算：

1. 为真实WebSocket消息读取增加按连接复用、严格有界的buffer，接入OIS wsConn与consumer Stream.next。初始按F+常量头/超限探测字节，按需增长最多到原有message readLimit+1；不把客户端既有兼容上限偷偷收紧。正常F字节DATA不再逐帧io.ReadAll扩容；返回切片仅在下次Read前有效，所有使用点必须证实同步消费/复制已完成。控制、FIN、ABORT、文本/畸形/超长与底层EOF仍按原分类处理，压缩仍禁用。
2. OIS onData现有有界队列与同步forward完成语义保持，暂保留其DataFrame复制，避免同时改变writer所有权。OIC Bridge uplink可将已存在F缓冲改为F+1并预置类型字节，Read到payload区域后同步Write整帧；只有实际Write完成后才复用，既有许可检查、FIN竞争、诊断首边界和buffer计数不变。
3. 确定性回归覆盖连续不同内容/短Read/分片消息、最大允许消息和超限、读错误及FIN/ABORT/EOF、缓冲在未消费时不被下一帧覆盖、真实阻塞Write期间内容不变。对真实正常最大帧记录旧/新分配对照，但不把一个人为allocation数字增加为生产SLO，也不以benchmark代替内容和边界断言。
4. 先做本地完整单测（含PG、同一完整分母≥95%）、静态/仪器与独立审查，再固定新源码运行原6组Linux开发Load（原2秒idle/PING/并发/载荷）。旧失败及trace原件保持；若仍失败继续以具体时线修复，不把通过重跑票数当因果证明。只有最终固定源码完整G1/G4正式复证、原始分析闭环与G3正式出口完成后才恢复门槛/集成。

客户端配置同时落实既有 F≤1 MiB、不得超过 OIS F 的约束：`client.New` 对正 `DataMax>1 MiB` 返回普通配置错误，覆盖 `MaxInt`、`1 MiB+1` 拒绝及 `1 MiB` 接受；非正值仍采用原默认值。该校验使配置输入在进入缓冲长度运算前有界，不将内部构造器的溢出保护变成用户可触发的 panic；它不能代替调用方按实际 OIS F 配置较小的发送上限，也不改变既有接收兼容上限。

这属于已采用范围的实现与分配修复，不新增公共能力、部署模式或新产品参数。D20初始文档已在2d6e6ea提交、推送并逐字回读，之后开始编码；当前G1 Failed/G4 Inconclusive与alpha570b573保持，局部回归不放行门槛。

D20实现检查点：完整Windows `cmd/... internal/...` 单测含本机PG通过，4612/4802条语句=96.04332%，388个根/子测试通过、0失败，唯一Linux专属FD跳过。全包vet与staticcheck v0.8.2通过；新增11根/34子项覆盖消息边界、读缓冲alias、真实fragment/1009关闭、同步uplink门闩和客户端无效F拒绝。真实32769 B WebSocket消息的原/新分配观测15→0 alloc/read；该数字不是生产SLO，也未证明旧Linux中断已消除。root独立检查读取/队列/Write生命周期，OIS队列仍复制DATA，client.pending耗尽前不再次Read，uplink只在同步Write返回后复用。接下来固定本提交，在原diagnostics/TASK-006-d19-load入口执行未改变的六组负载，最多5轮、遇首失败保留并停止；尚未运行新正式G1/G4/G3。

1c6c116完整CI 41826906142的build-lint、unit/race、fuzz、integration通过；python/windows日志明确因G1 Failed退出2/1。文档先行2d6e6ea的CI 41830142580同四项通过，python/windows仍失败；没有据此合入alpha。

D20提交前补核：govulncheck v1.9.0、全部35项tools与112项experiments回归、结构/SDD/迭代/物料检查通过；P2检查仍按当前G1 Failed退出2，未吞掉或改成通过。正式Linux竞态/fuzz/负载待新提交的CI及诊断。

### D20 Linux开发复验及新正式预注册

固定def5abf的原负载诊断41833914644成功完成上限5轮，每轮原六组（1MiB/64MiB×1/8/32）全部通过，410条流完整返回；原始Go输出按Package/Test重组，逐worker的success/收到字节与O3请求/成功量交叉一致。原b4-resource-v2独立复算30个check通过、均至少3连续样本，最慢115ms；64MiB×32五轮实测完整吞吐约158.0–177.5MiB/s，只作本环境观察，不当生产SLO或旧失败一般根因证明。所有原失败/trace保持，没有改PING/IDLE或重采故障后基线。

现在提交新的`G1-P1-R4`、`G4-P2-R3`，完整保留前版五/四项criteria、所有原向量和质量/负载/资源判线，仅附加D20新增11根/34子项在unit/race的精确包测试绑定。尚未运行的`G3-P2`同步附加这22条测试记录，其34场景/38profile/62phase不变。三者只按接下来固定的同一干净源码运行；先G1/G4原始闭环恢复前置，再运行独立G3，不用5轮开发结果替代正式run。

### D20固定源码正式复证检查点

bff7aa2两份新预注册的G1-P1-R4/G4-P2-R3均observed/pass；139源码逐Git核对、原件SHA和摘要复算通过，unit/race各392 PASS、完整覆盖96.1%。G1原负载与115次严格资源检查通过，G4原四项通过。新run各归ITER-010/011，详见[分析](../../docs/evidence/p2-d20-revalidation-011.md)；等待四单发布回读与最终六项CI，未宣称G3已正式验证。CI41835698583在B1-02/PG的newEnv注册持久化触发500ms写失败后正确fail-closed，导致token创建503；底层慢写原因未定、无baseline，不自动resume或重试未知token写，完整失败保留。
