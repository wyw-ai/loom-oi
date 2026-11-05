# G2-11 首轮导出输入前置实施计划

2026-11-05。沿已采用G2输入范围修复准备顺序，文档先行，未实施、未运行；不改变旧六叶失败或门槛状态。依据固定 `6ac7249593da35d61ff01b4a19de3ec1cd952b54` 的原件和任务`six-source-linux.json`所保存的独立诊断。本稿只处理已经证明的“final 年轻批之后还串行建立完整 exporter 输入”依赖；四个 `preload_joint` 失败仍是独立未知，不保证本方案解决全部六叶。

## 一、事实与选择

SQLite round2、PG round0 的 final→critical 分别284.6641/252.1936ms。首连接到 Active 合格分别159.7357/147.4716ms，之后五次429本身保留53.9873/52.3863ms的观察跨度；critical 两角色 Stats 组合分别30.3909/25.4359ms。完整 exporter 准备合格后，原 full 仍真，但旧批在真实300ms检查时已变老。40ms年龄和429观察是现有输入资格，不能统称无用等待或删除。

`g2TryCompetitionSampleObserved`（`g2_failure_diagnostics_test.go:243`）确为 OIS HTTPS 后 OIC HTTP 串行请求：OIS客户端禁keepalive，OIC客户端复用；两者数值没有数据依赖。不过原 raw 没有各角色独立请求起止，不能分摊这25–30ms或保证并发收益。并发还会改变“OIS先失败就不调用OIC”的原失败路径和freeze/join协调。本次不改这段，不加并发、额外采样或客户端策略。

`pumpQueries`（`internal/ois/relay/observation.go:86`）按各自分配时刻固定500ms，并按实际空槽/dirty逐项轮转；六慢源不使八个query天然同批。延迟proxy转发不会刷新OIS原截止，也不能重记Received年龄。现有四叶69次full真但young200不合格仍按原规则失败，不追加Drop、控制暂停、重试或重新选择年轻子集。

## 二、唯一排序改动

候选策略名 `six-source-export-first-v1`。固定16源、原两批8次Set、六源不重置、32个Drop、preload原full+young200全部不变。第二批最后Set返回仍是 `slow_input_applied_ns`，完整60s仍从此处起，不能从exporter合格或prepared重起。

首轮顺序改为：

1. 原一次victim准备和 `preload_joint` 联合接受。
2. 原第二批8次Set，保存slow起点；固定选择本轮 `fullRole`（偶数OIS、奇数OIC）一次。
3. 执行cycle0原有完整exporter输入：原独立 `before_export` 200基线→原两socket/各2048请求→原Active0与放行writers→原写完成/MSG_PEEK两首200→原Active2且最年轻≥40ms→原连续至少五次429且跨度≥40ms。参数、顺序、请求上限均不变。
4. 在原3s与本两socket原connect+1s的共同剩余期内，执行原 `slow_full` 回调；仍是每回调原两Stats、原一次Snapshot、原短路full+young200。取得它自己的final见证，再记录prepared/原competition_full。
5. 继续同一次cycle0：原独立critical两Stats+原一次Snapshot/young300，然后原native、撤销、后置full、后置429、关闭前两个导出失败计数、原close/recovery。critical不能复用final的sample、Snapshot或见证；原300ms不前移。
6. 原Drop取消/drain、后续cycle交替role、至少20且至多512次、target严格slow..finish计数、heartbeat与资源回收全部保持。

第一步已有的preload失败照旧停止，不创建慢连接。不能为了重试final而重连慢socket、重复baseline/Active/429输入，不能重做victim或native。最终年轻批可因自然轮转改变，但仍按原逆向扫描得到的完整八条、原Received时间和实际Snapshot前缀检查，不能人为挑八个较新条目。

## 三、Go最小接线与生命周期

首选保留 `g2CompetitionCycle` 为唯一慢连接所有者：把其既有“exporter资格完成→critical”之间加入只在cycle0调用一次的内部final选择闭包；上层在第二批Set后进入此函数，闭包执行原slow_full并发出competition_full。后续cycle传空闭包。可拆纯准备/业务局部函数，但不把仍存活的slow指针返回上层后再转移cleanup所有权。

需要明确保持下列边界：

| 项目 | 必须保持的行为 |
|---|---|
| 时间 | 原prepare D只建立一次；原TCP写截止（实际在dial前建立，可能比connect+1s更早）绝不后移。first input与final接受上界是min(prepare D, connect+1s)，任何已开始的原有有界调用若晚到，只保留失败，不据此续期。进入final前余额≤0直接失败，不让eventually的首次callback补做超期采样。 |
| 业务D | prepared之后业务仍沿本次原connect+1s、原撤销500ms和原产品期限；不从闭包返回时创建新1s/500ms。3s约束准备接受，不伪造为原业务新增可用窗口。 |
| before/after | `before_export`仍是连接前独立200及失败计数基线；`bounded_failure_before_close`仍须关闭前真实200且相对它增量≥2；`after_export`仍在原closedAt之后独立采样，Close耗时包含原2s恢复。三者不可合并、改用Active Stats、挪作同一采样或从别轮借计数。 |
| 持有与清理 | 现有cycle defer在慢连接产生后立即建立，覆盖final闭包的error/Fatal/panic/Goexit；先freeze所有已取得失败事实，再原Close/join，最后一次emit。不能因闭包Goexit把默认false记成正常返回。 |
| preparation失败 | 新增有限phase `cycle0_export_input`，原 `slow_full`保留；在cycle清理前同步调用preparation冻结点，保留原首次CapturedNS。不能仅靠外层t.Run defer，因为它会晚于内层slow.Close。两种原失败事件各自最多一次，未执行的final/critical/native字段为null，不能cleanup后补采。 |
| 业务终态 | 本重排不写Channel `forceAt`、不主动Abort/续许可或提前关闭victim。native/revoke的原第一次终态与Close位置保持；失败只清理本fixture拥有的慢socket/stream，不把强制清扫后的样本当2s通过。 |
| 完成/重入 | fullRole只计算一次；准备与业务使用同两socket、同baseline、同inputGate/slotInput和同绝对D。不得两次调用BeginSlowScrapes或把空对象当第二次初始化。 |

输入各原等待已有deadline参数时使用其与prepare D的较早者；不用增加请求探测剩余。socket既有写deadline不重设。每个接受点还核真实完成/返回时刻，避免一个原有HTTP调用跨过prepare D后被接纳；失败收尾可以晚于D，但不能记成合格或增加预算。纯内存role/address解析可提前，不用新增日志等待、HTTP或额外热身。

## 四、原始证据与解析器

沿现有 `g2-behavior-v1` 封套和全部原字段。full/complete的 `preparation_strategy` 与manifest `competition_preparation.strategy`同时精确改为新策略；`target_window=all-slow-applied-v1`、`query_witness=selected-query-snapshot-v1`保持。新解析器只接受新完整声明，缺键/null/未知值拒绝，不能按raw选择旧分支。130普通叶/108组/106资源和同run clock库存不变。

无需为了顺序再造一组重复采样字段：现有cycle0原始 `before_export`、`slow_writes`、`export_slot_input`、headers、`export_input_observations`、`actual_rejection`和三见证已经有足够真实区间。新增严格跨事件关系：

```text
slow_input_applied <= cycle0.before_export.started
<= before_export.completed <= first_connected
<= 原Active/headers/429全部合格时点
<= actual_rejection.completed <= final.sample.started
<= final.sample.completed <= final.witness.returned <= prepared
<= min(prepare_deadline, first_connected+1s)
prepared <= critical.sample.started <= critical.witness.returned <= native_started
```

其中Active/headers/429内部完整逐条时序仍用原validator核，不把“全部合格时点”当自报bool。final、critical仍逐角色PID/epoch/gen、全binding、同control、原Snapshot可见前缀与正投影绑定，年龄仍分别200/300；full和cycle0各自observed上界继续核对。fullRole由round独立推导，首轮之外不得伪造final闭包或critical见证。

`slow_scrapes`原baseline≤connect、rejection≤关闭前失败观察≤close、close→after≤2s，以及critical窗口prepared≤beforeStats≤native均保持；只有“原exporter输入应位于final之前”的新跨事件约束加入。旧分析器实际上尚未约束这个跨事件顺序，因此不能仅说旧parser也PASS就视为已预注册。改解析器前逐字归档当时旧模块和loader依赖，旧manifest/raw/summary经原recorded source精确复算，旧六叶失败不重写。

## 五、必要反例与未解决项

只针对接缝验证：同两socket且输入/业务各调用一次；final仍真实两Stats+一次Snapshot；prepared早于3s却晚于connect+1s拒绝及相反情况；final/critical年龄恰界与超1ns；final回调Fatal/panic/Goexit先freeze后Close/join且无native；业务后置429失败仍失败；原baseline/failure/after三采样互换或复用拒绝；新声明但保留旧“final后建socket”顺序拒绝；晚Written未来回填、另一role/round输入、重复Begin/重起D均拒绝。成功路径和失败路径不得额外请求，也不以helper模拟通过宣称真实输入成功。

四个preload失败没有本方案已证明的解法；不改Stats串行、不新增proxy门闩，也不保证新活性实现会自动消除它们。下一次固定来源真实运行必须保留所有叶的失败，不能拼旧叶或只报被改善的两个叶。H17是独立产品变更，其新源码原本就须完整复证；本排序按此先行计划实施后，先做必要开发检查，再将准确声明纳入最终同源正式清单；不改变任何旧正式运行或门槛。
