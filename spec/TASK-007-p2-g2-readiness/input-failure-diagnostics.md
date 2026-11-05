# G2-11/12 失败即时诊断（编码前固定）

2026-11-05；Preparation。依据固定 `d9b884929aec6c8a14460876f13c14958239f652` 的原测试、本轮已保留失败原件和独立只读诊断；遵循[首终态诊断](terminal-diagnostics.md)的记录原则。所属任务见[TASK-007](README.md)。这是已授权修复范围内的取证实现计划，不改变任何已采用产品取舍；规格提交和远端回读后编码，原正式运行完成并封口前不运行新 Go/guest。本稿不改变 2 个 exporter 槽、200ms 写期限、500ms 控制期限、原 1s/3s/40ms/60s/2s 输入及验收要求，不增加请求、连接、轮次或重试。原 d9 失败原件保持。

## 共同记录规则

复用现有 `g2-behavior-v1`、`kind=G2` 与真实 test/vector/backend/round/cycle；不另造能绕过正式校验的通用 diagnostic 豁免。失败事件继续令该叶失败，不能替代任何成功事件或资源 complete。已有 analyzer 会拒绝未注册/失败事件，先保持此行为；若增加独立诊断读取器，只解析精确失败事件白名单，不能把它们纳入通过计数。

“即时”指在真实错误返回或原条件拒绝时，先把当前事实复制到固定容量对象，记 `failure_captured_ns`，再执行原有关闭/取消/join；序列化与打印可在原清理后进行。不能在关闭慢连接之后补采并标为关闭前，也不能先阻塞写大段日志再开始原清理预算。所有未执行的动作明确 null；不要用0或成功默认值冒充完整返回。新 error_class/context_state 仅有限枚举，不新增任意 err.Error、请求体、token、ticket、DSN 或私钥。

建议共用薄的观察包装，返回原值/原错误完全不变：`call_started_ns` 在调用前取，`call_returned_ns` 在返回后立即取，`context_state` 在现有 cancel 前取。特别是 g2ReadExposition 在 client.Do 失败时原 CompletedNS=0；外层应另存真实 returned_ns，不能回填内部字段伪称收到完整响应。

## 一、G2-11 满态组合与双槽年龄

### 满态准备失败

嵌入 `vectors_g2_competition_test.go` 原两次 eventually 回调及 :474 外围。每次实际回调只覆盖一个 `last_preparation` 固定对象；不另做一次 stats 请求或事后查询。保留 `attempts` 计数、原 prepare_started_ns/deadline_ns、slow_input_applied_ns、phase=`healthy_backlog` 或 `slow_full`。对象包含：

- 原 `g2CompetitionStats` 完整 OIS/OIC 样本及实际调用起止；若中间读取失败，明确 `completed_roles` 和失败 role/有限错误类别，未取得角色为 null。
- `evaluated_ns`：实际检查组合谓词的时刻；`full` 各原子布尔值：capacity16、backlog16、pending8、probe_active2、probe_queued4。由同一份 sample 计算，不能再读一次 Stats。
- 原 young 判断真正使用的至多8条 query 投影：原 wire EventID、QueryID、完整 Binding、ReceivedNS/WriteStartedNS/WrittenNS、DeadlineMS、Injected/Dropped。保留实际检查顺序，不补选“更年轻”的8条；`young` 使用同一次 now。
- `failure_captured_ns`、`failure_class`（stats_error/input_not_qualified/capacity/panic）及 original deadline 是否已过。计数/布尔值只是便于读诊断，独立读取器必须能从原 sample/query 投影重算。

失败仅发一次 `competition_failed_preparation`。为取得部分样本，可把 g2CompetitionSample 的内部 Fatal 改为私有 try 函数返回原部分对象+error，原外层仍在同一分支 Fatal；OIS/OIC 请求顺序与各自既有 context 不变，不能借该重构偷偷重起/延长原3s。若准备期间发生 panic，defer 只能捕获已有局部事实、原样重新 panic；不吞掉失败。

### Active 资格失败

`g2_competition_active_test.go:125` 已保留最多128次完整 stats（含失败末项），`competition_failed_cycle` 已保留写前 Active0、双写、双首200和阶段。因此**不新增产品 Activity 字段或提高采样频率**。只补：

- 每次 read 的外层 returned_ns、error_class、context_state；原 Process.Stats=null/HTTPStatus=0 保持真实。
- `selected_index`（未合格为null）、`failure_captured_ns`、`last_sample_index`、`activity_qualified=false`。可附 observed_max_age_ns/active2_samples 摘要，但只从已有数组计算。
- 失败阶段保持 `await_export_active`，原 `actual_rejection` 与后续阶段若未发生应为null；不伪造一个429。当前数组已经足以证明“采到的年龄都不足40ms”，而不足以证明采样间隙没有资格窗口。

此批补充不解决“为何所有 owner 都较年轻”的唯一根因。若仍需要区分服务端连续快速完成、旧200之后的其它请求、真正阻塞的 Write/Flush，应另用已存在、默认跳过的透明 `TestG2CompetitionSlowInputTrace`；那是单独来源绑定的诊断，不把它偷偷放进正式竞争场景，也不自动重复至通过。本阶段不新增产品热路径 trace。

## 二、G2-12 慢读：明确关闭前200资格

只在 `vectors_g2_export_test.go:437` 每轮开始建立固定本地 recorder，原两段循环/原请求/原 stop 不变。插入点：before取得后、slow连接返回后、每次 g2ReadExposition 返回后、native前后、:479进入第二段、:504关闭前、Close返回后、after返回后、每个原 Fatal 前。

单个失败轮最终发一次 `export_failed_slow_round`，字段如下：

| 字段 | 来源与有限容量 |
|---|---|
| role、round、stage | role仅ois/oic，round原0..19；stage为baseline/connect/rejection/native/failure_before_close/close/recovery/condition |
| before | 原完整 g2Exposition；未取得为null |
| slow_writes | 原 Snapshot，恰0..2条；不新读socket |
| input_stop_ns、stop_set_ns | 原 `time.Now().Add(time.Second)` 的同一绝对截止及赋值时刻，不能替换成另一个起点 |
| observations | 原读取共用至多384项（原1s输入的两段合计至多128项、原2s恢复至多256项）；每项有ordinal、phase=rejection/failure_before_close/recovery、call_started_ns/call_returned_ns、原status/内部started/completed、body_bytes/body_sha256、parse_complete、timeout/write_error计数（未完整解析为null）、error_class/context_state。先保存原返回，随后照旧判断；满容量记overflow，不覆盖旧项、不增加循环 |
| first_rejection_index | 首次真实429所属项，未出现null |
| native | 原请求started/returned、实际channel_id/有限code；未执行null，不为诊断重试或读取新target |
| bounded_failure_index | 原 :495 满足status200且counter差>=2时所选项，未成立null；不能从关闭后after反填 |
| failure_captured_ns、predicate | 原拒绝时刻；三项分别为rejected、bounded_failure_200、after_delta_at_least_2，并记录各项是否已求值，不能全填false冒充未执行 |
| close_started_ns/close_completed_ns、close_error_class | 紧贴原唯一显式Close；重复幂等t.Cleanup不冒充本次Close |
| after、recovery_deadline_ns | 原after与原恢复截止；若未执行为null。after永远标属于关闭后 |

最小方案直接沿原OBS保存紧凑数组，仅保留 before、last_complete_200、boundedFailure、after 至多四份原完整 g2Exposition（各沿现有65537B读取上限）。数组中的计数明确是本仪器独立Prom解析器所得的诊断值，未保存完整body的条目只能帮助定位，不能作为新增正式正证据；body摘要也不能替代原body。所有会被用来证明关闭前资格的 boundedFailure 必须同时保留完整body，使现有独立分析仍可复算。此方案无需新增artifacts路径、collector或封口规则；在原清理结束后一次发射，不能边采边打印。384项是记录存储上限，不是新增循环次数；overflow只表示本次诊断不完整，不能覆盖旧项或改原判断/期限。

从记录可以精确区分：没有429；原stop在native完成前后已耗尽；第二段只有429；有200但counter不足2；读取错误；关闭前资格已取得而after/恢复失败。它仍不能从某个observer耗时直接断言服务端Write超200ms。原成功 `export_slow_round` 的字段/判定保持，不把新失败事件计作成功轮。

## 三、PG 写超时：扩展既有错误日志，不新增轮询

最小产品改动是扩展 `relay/maintenance.go:108` 原 `store write failed` 的一个有限 `operation` 对象；保持原 was/prev 日志抑制、state latch、锁顺序和sink位置/次数，不增加逐SQL成功日志。先在原调用边界取事实，再原样传入现有写失败处理。

operation 名称只取真实入口：`apply_config`（admin.go:87）、`raise_epoch`（background.go:142）、`upsert_registry`（:167）、`maintain`、`append_audit`、`prune_audit`（maintenance.go）。每个对象固定：`name`、`duration_ns`、`context_state_at_return`、`error_class`、`deadline_remaining_at_call_ns`/`deadline_remaining_at_return_ns`（无deadline为null，有则允许负数）。这两个remaining必须来自原ctx.Deadline与紧贴call/return的真实time.Now，不能新设context或使用日志输出时刻。

persist的多个写共享原500ms context，另附固定 `batch_elapsed_at_call_ns`、`batch_elapsed_at_return_ns`、`batch_budget_ns`，以同一次persist开始的单调时间计算。由此能区分调用前预算已耗尽与本次调用期间耗尽，不能把每个job的500ms重新算一次。诊断分类使用 errors.Is/As 与原 ctx.Err，取消状态必须在原defer cancel之前捕获；不按任意错误文本猜阶段。已有错误字符串保持既有行为，新字段不重复输出它或任何实体/SQL/DSN。

这一级只能确认“哪个上层写入口失败、共享预算何时耗尽”；`store.Serialized.run` 内的队列等待与数据库事务执行仍合在 duration 内，不能声称已区分。暂不改 store API、序列化队列或数据库连接参数。只有下一原件仍需区分队列/SQL时再单独规格化可选内部阶段见证。

测试侧 EntityCardinality 只补当前 `index`、stage=`policy_write/registration_lookup/token_create/native/export_compare` 的失败事件，以及原 API 调用起止、真实 HTTP/code、最近已完成实体index；不能为诊断再建token、重试Discover或额外查询StoreView。在原 g.consumer内部 token 失败不能返回局部信息时，用本场景的薄 try helper 暴露原同一次请求结果；不改共享consumer缓存和调用顺序。

## 静态/模拟回归及界限

建议新增聚焦辅助测试：完整返回/Do无响应/读body失败的时间区间；关闭前未取得200但关闭后+2仍失败；只有429/stop已尽/200不足2/after未执行不误称成功；诊断数组达到容量不覆盖；所有失败路径先冻结事实再关闭并join，panic与Fatal均保存。准备记录反例包括错query身份、未来时刻、另一轮sample、缺角色及把最后不合格样本换成较早合格样本。PG用门闩区分共享context先耗尽与调用中耗尽，并验证原error/state/log次数不变、诊断不延迟恢复/关闭。

以上测试仅验证诊断真实性；原d9与此前失败不能因新读取器变成通过。下一正式manifest只追加实际新增具名根/子项、绑定新源码，不删原向量或改门槛。先完成聚焦回归，再进行一次新身份的两后端 G2-11/12 开发观测并保存失败即时记录和完整服务日志；Active 原因优先使用既有透明 trace 的独立诊断，不把诊断样本拼成正式通过。若仍失败，按取得的具体边界选择修复，不盲目重复完整正式运行。
