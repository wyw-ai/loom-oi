# G2-11 准备失败的有界历史记录

2026-11-05，编码前固定，尚未实施或运行。依据固定 `7da7159a339986659b18842350ae1edc242e958a` 及本轮开发失败原件；仅扩现有失败诊断，不改变产品、输入顺序或成功判据。

## 本轮已知边界

SQLite round0 已经过 healthy_backlog，后在 slow_full 失败；PG round0 未离开 healthy_backlog。两叶真实用尽 32 个 Drop，分别在 prepare+1625.4856ms、+1934.8595ms 消耗最后一条；最终样本均 backlog=0/pending=0，但前面的每次 sample 没有保留，不能判断早期具体缺哪个条件。

现有两次回调见 `vectors_g2_competition_test.go:490/:513`。每次已经执行一次 OIS/OIC stats 组合采样、一次 proxy Snapshot；`g2PreparationFailure` 只覆盖最后一次。healthy 阶段要求 capacity16/backlog16/pending8 和 young，不要求 P2/Q4；slow_full 阶段才同时要求 P2/Q4。当前准备阶段 `preparation.young` 使用 **200ms 最大年龄**；关键操作前的独立检查使用 **300ms**（同文件 :270）。两者不可混写、互换或放宽，query 的原 500ms 期限也不变。

## 仅扩原失败事件

在现有 `competition_failed_preparation` 加 `preparation_history`、`history_overflow`、`history_omitted`，保持现有最后样本与失败字段。固定预分配 `[512]` 窄 struct，顺序保留最先取得的原始回调；到容量后只累计遗漏/overflow，继续原流程及原最后样本，不新增失败条件、等待、重试或容量判线。512 是诊断存储上限，不是新采样次数要求。

每项保存原本已经取得的内容，不增加任何 HTTP、Snapshot 或 timer：

| 字段 | 精确含义 |
|---|---|
| `attempt` / `phase` | 原全局回调序号及实际 healthy_backlog/slow_full；不把两阶段各自重新从1编号 |
| `sample` | 只拷贝原 sample 的开始/完成/实际返回、角色完成位、失败role/有限错误/context，以及下述固定数值/身份；没有取得的角色为null，不复制整个 OIS/OIC maps |
| `full` / `full_evaluated_ns` | 同一份 sample 的五个原子谓词和实际评估时刻；stats 失败未执行时 null |
| `young` / `young_evaluated_ns` / `queries` | 原短路判断确实调用时才填写，使用原同一次 now、原检查顺序和至多8条 query 投影；未调用保持 null，不为诊断额外运行一次 young |
| `predicate_returned` / `callback_returned_ns` | 原回调的真实结果及返回边界；Fatal/panic 未正常返回则 null，不补 false 冒充完整返回 |
| `wire_event_count` / `wire_last_event_id` / `wire_snapshot_returned_ns` | 从本回调原来那份 Snapshot 取得的事件数量/末ID及返回时刻；stats 失败没执行 Snapshot 时 null。它不是与 OIS/OIC stats 原子同时的快照 |
| `dropped_count` | 同一份 Snapshot 内已经观察到的实际Drop累计数，未取得Snapshot则null；引用公共固定Drop表的前缀，不逐项复制32个ID |

窄 sample 固定保存：OIS 的 PID（本env原proc）、epoch、observation存在位及 capacity/backlog/pending；OIC 的 PID（本env原proc）、access_id/epoch/generation、connected/registered/license_remaining_ms、observation存在位、probe_jobs/active/queued/max_active/max_queued。这涵盖原组合谓词与原 helper 有效性检查；缺字段不补零为有效样本。其余 Process/Closed/计数map仍只沿已有最后完整样本保留，不逐回调复制。full也用固定五bool和有效位，避免每行新map。

对现有代码的最小接缝是保留原 Snapshot 值而不是立即只取 `.Events`，由原回调收尾复制有限投影。公共预分配 `[32]` Drop表只保存首次见到的实际 event_id/query_id/received_ns。利用原Snapshot的事件序号游标，只扫描新出现的尾部，每条事件在整次prepare中至多检查一次；第一次原Snapshot可以扫其已有前缀一次，之后不重扫全量。不新增Snapshot、proxy锁或Raw解码；query的最多8投影仍只由原young已执行分支取得。

本次32个matcher的Drop决定均在首个准备Snapshot之前完成；固定proxy在命中时于原record同锁内写入Dropped，若已命中则Drop在同锁内更新原事件。这个不可逆状态在准备Snapshot首次观察相关事件时已经确定，因此可增量记录。不能推广为对任意事后改变barrier的通用增量规则。若ID不连续、公共表溢出或原wire本身overflow，只记诊断不完整，不额外终止原流程。原完整wire已在失败时保留；不把整份最多32768事件历史复制进每项，也不新增代理 API 或产品诊断。

为保留相位切换，另加三个有限字段：`healthy_accepted_attempt`、`healthy_accepted_ns`、`slow_set_started_ns`。前两者只在原 healthy eventually 返回并通过既有 deadline 检查后记录；第三个紧贴原16次 `health.Set` 的首调用之前。复用已有最后一次 Set 返回后的 `slow_input_applied_ns`。Set 次数、顺序和原3秒D不变，不插入中间stats。若健康阶段已合格、Set中途失败，保留实际字段组合，不伪造完整切换。

失败仍先复制当前事实并固定首失败，再原取消/关闭/join，最后只发一次失败 OBS。特别是原 OIC stats 失败，必须沿既有失败回调在 context cancel 前完成部分样本记录；不能因新增 history 破坏已修的 freeze-before-cancel。成功 raw 和成功判据保持，不把失败 history 当有效成功轮。

## 独立读取可以回答什么

1. **已采样始终未满**：逐项重算该阶段实际谓词，列出缺失条件；不能升级为“整个连续时间段从未满”。
2. **已采样满，但 young 拒绝**：原 actual `young` 为 false，按其实际 query 投影核重复ID/源、真实转发、DeadlineMS、ReceivedNS 和原200ms年龄；不能事后换一组更年轻的8条。
3. **健康阶段已接受，切换后首慢样本排空**：用 accepted attempt、Set 起止及首 slow sample 对齐；准确说“两个观察之间发生变化”，不能断言一定在某个 Set 内排空。
4. **Drop 用尽后的变化**：公共Drop表前缀（由每项 `dropped_count` 引用）必须关联同叶 lab-a proxy 的原 EventID、QueryID/Binding、snapshot 类型、Dropped=true、无 Injected、无 Write；query 投影关联同一完整原 wire。最终 wire 的 WrittenNS 可能比某次旧 Snapshot 更晚，不能把事后完成状态回填为当时已经转发。
5. **完整采到已合格但未转阶段**：先核实际 `predicate_returned=true` 与 accepted 字段；若真实自相矛盾才报告控制流程/记录问题。不能拿一次原子 full=true、但 young=null/false 的样本声称向量错过合格输入。

正常回调可能在原D之后才返回，当前最后错误样本就有这种情况；记录真实时间，不把 D 后样本当合格，也不重写成D。三段时间顺序、phase、attempt、wire引用、null和实际bool可由独立诊断读取器严格交叉检查。它只处理明确失败事件，仍不能使主 G2 analyzer 接受失败叶。

## 最小测试和范围

只需针对历史容器/回调接缝补纯辅助回归：跨两个phase的窄数值/身份不被覆盖且不持有stats map；短路young保持null；健康接受再切换的真实序号；stats错误先记录再cancel；512条后overflow仅记录且原返回不变；真实EventID/query/drop引用一致、增量游标不重扫/重复计数，未来wire完成不得倒填；D后回调原值保留且不变成成功。可用既有测试fake和已保存原件检查投影，不需要业务重跑证明这些记录机制。

最小代码范围为 `g2_failure_diagnostics_test.go` 的准备记录、两处原准备回调和其聚焦辅助测试；不改产品、32 Drop、3s prepare、query 500ms、准备200ms/关键检查300ms、慢连接1s、60s窗口、原调用次数或顺序。当前 Active 三失败已保留完整数组，暂不扩它。没有必要先改变 fixture 时序或立即再跑六叶。
