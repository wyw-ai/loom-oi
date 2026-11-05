# G2-11 固定六源预热与测量窗口绑定

2026-11-05，先行规格，已完成独立内容审阅，提交固定后方可实施；尚未实施或验证成功。它修正现有夹具的准备排序和证据接线，不新增产品契约、DISC或性能承诺。旧正式失败、开发失败及其固定分析器结果保持原貌。

## 1. 依据与不变边界

依据[第二轮开发检查点](local-linux-diagnostics-second.json)的固定 `eb64801ab74346b6541f58b6a6941f88233d2155`：三叶先接受健康阶段，切14慢后首个P2/Q4样本分别晚471.3225、376.9636、381.9005ms；慢阶段首样本仍有backlog16/pending8，不能声称Set立即排空。随后19个full真样本的原query年龄均超过200ms；没有采到完整合格callback却未接受。另三叶在后续Active输入失败。六叶均未完成cycle/60s，采样不能证明所有间隙的连续状态。

`tests/fixture/health.go` 的 `Health.Set` 只替换响应门闩，不唤醒OIC Scheduler；`internal/observation/scheduler.go` 的下一due仍来自上次实际执行加interval/jitter。因此先接受年轻query、再切慢等待P2/Q4，会消耗该批query的年龄。下述排序只消除这项串行依赖，不保证一定在原预算内成功。

长期要求仍以[已采用G2规格](../../docs/architecture/p2/g2-observation.md)§7、§9为准：16源、P2/Q4、probe interval/timeout/jitter/validity为500/200/50/1500ms，query为8槽/500ms，遥测写100ms；exporter仍2槽/64KiB/200ms。满态期间原生Open/close、撤销、heartbeat ACK及原500ms判线保持。

原32个snapshot Drop、同一3s准备截止、准备young 200ms、关键操作前young 300ms、慢连接2个×2048请求/原connect+1s、Active与429资格、每后端3轮完整60s、至少20且至多512个cycle、严格2s资源回收均不变。本页只替代[准备历史规格](preparation-history-diagnostics.md)所描述的旧准备排序，并补足独立复算；其512项历史、32项公共Drop表、增量游标、短路null及先freeze后cleanup原则继续适用。它不改变Active慢读机制，也不修改产品调度/控制协议。

## 2. 固定策略与两个准备阶段

新策略名为 `six-source-preload-v1`。固定16源顺序为 `lab-a/http`、`lab-a/alt`、`lab-a/competition-02` 至 `lab-a/competition-15`，不按观测结果临时选源。victim仍只执行原一次Dial并在准备前Running；失败不自动重试或重新装配。

1. 保持原 `prepare_started_ns`，唯一 `prepare_deadline_ns=prepare_started_ns+3s` 在注册Drop、任何Set和采样之前建立。原32个snapshot matcher仅注册一次，原方向/Drop语义不变，不暂停整个控制流、不注入dirty或重置query期限。
2. 第一批按上述顺序执行8次原Set：两健康源 `Set(200,false)`，固定六源02至07 `Set(200,true)`。08至15保留既有健康状态。阶段名改为 `preload_joint`，不能继续称全健康阶段。
3. 第一阶段回调使用原一次OIS/OIC组合采样、原一次proxy Snapshot及原短路顺序，必须实际同时满足capacity16/backlog16/pending8/P2/Q4和原young≤200ms。接受必须在原3s截止内，不由一次单项满态推定后续状态。
4. 联合接受后，第二批只对08至15执行8次 `Set(200,true)`。不得再次Set或Release已变慢的六源；全次准备仍恰16次Set、每源一次。第二批最后真实返回时刻为 `slow_input_applied_ns`。
5. 第二阶段 `slow_full` 在同一原3s余额内执行原full+young≤200ms完整复核，不能用第一阶段见证代替。原60s从 `slow_input_applied_ns` 起算，六源预热不计入。首个关键cycle仍在真实slow exporter输入后实采full及原young≤300ms，操作后保留原full与实际429；其余cycle、drain和cleanup顺序不变。

所有超时、Fatal、panic或Goexit都保留原首失败：先冻结已取得事实，再原取消/关闭/join，最后一次失败OBS。未执行字段为null，未正常返回的callback不能以默认false冒充完成。历史容量溢出仅记诊断不完整，不新增失败判线或改变原流程；成功所必需的有限见证缺失则不能被独立分析器认作有效证据。

## 3. 有限原始记录与三处query见证

沿用 `g2-behavior-v1` 事件封套与原round/leaf身份；以下字段只适用于本策略，均来自本来执行的调用，不增加HTTP、Snapshot、等待或产品字段。

`competition_full` 与 `competition_complete` 同时保存且逐值相等：`preparation_strategy`、`preload_set_started_ns`、`preload_set_completed_ns`、`preload_accepted_ns`、`final_set_started_ns`、`input_versions`，以及原prepare/slow/prepared/window字段。`input_versions` 是恰16个固定Exposure键的正整数Version映射，值是各自唯一Set的真实返回值；不按最终Snapshot推算。固定顺序和两批集合由策略定义，不另存无界调用数组。

必须满足：

```text
prepare_started ≤ preload_set_started ≤ preload_set_completed
≤ preload_accepted ≤ final_set_started ≤ slow_input_applied
≤ final sample.started ≤ final sample.completed ≤ prepared ≤ prepare_deadline
```

接受动作、回调返回及每个见证的实际时刻还须满足下述更细约束。原 `window_start_ns` 仍覆盖prepare前起的控制/heartbeat全范围，不缩短为slow起点。

三处现有young调用各保存自己的见证，不能交叉复用：

| 位置/字段 | 必需范围与年龄 | 与既有sample/流程绑定 |
| --- | --- | --- |
| `competition_full.preload_witness` | 第一阶段已接受回调，200ms | 独立保存该次组合sample及其真实返回、全局attempt/回调返回；绑定第一批完成之后、`preload_accepted_ns`之前 |
| `competition_full.final_query_witness` | 第二阶段已接受回调，200ms | 绑定同事件原 `sample`，以及其真实返回、全局attempt/回调返回；必须在slow之后、`prepared_ns`之前 |
| `competition_cycle.critical_query_witness` | 仅原 `full_backlog_required=true` 的cycle 0，300ms | 绑定同cycle原 `backlog_before`，在其采样返回后、`native_started_ns`前；其余cycle明确为null |

当前原代码只在关键操作前调用一次300ms检查；操作后没有第二次young调用。此次不添加后置Snapshot/young，也不把后置full和429称为第二份query年龄证明。

见证共用固定结构：`sample_started_ns`、`sample_completed_ns`、`sample_returned_ns`、`wire_event_count`、`wire_last_event_id`、`wire_snapshot_returned_ns`、`evaluated_ns`、`returned_ns`、`max_age_ms`、`result`、`queries`。前两处另含正整数 `attempt`、`callback_returned_ns`；preload另含原 `sample`。`sample_returned_ns` 紧贴原组合采样返回，`returned_ns` 紧贴原young返回。均为本测试进程原单调时基，不填墙钟或稍后重取的时刻。成功见证 `result=true`，queries恰8项；失败短路未调用时见证为null，实际失败可保留原已扫描的至多8项，不能补造剩余项。

queries按原helper逆向扫描选中顺序复制，复用现有投影并增加原事件的 `conn_id`：`event_id/query_id/binding/received_ns/write_started_ns/written_ns/deadline_ms/injected/dropped/decoded`。binding完整保留Access、Exposure、epoch、generation、target revision、observation revision、contract version/digest；不以Exposure名代替完整身份。只复制同一次原Snapshot中实际已转发的条目，不重扫最终wire挑更年轻的8条。

独立分析器从同叶lab-a完整wire、原角色PID/epoch/generation与相关sample核对：事件是同一控制连接的OIS→OIC原生query，未注入/丢弃/写失败；ID/QueryID/Exposure各自唯一，解码成功，binding逐字段一致；原 `deadline_ms` 在400至500内。要求 `0<received≤write_started≤written≤wire_snapshot_returned≤evaluated≤returned`，以及 `0≤evaluated-received≤max_age_ms`。sample开始/完成/返回须一致且先于该Snapshot返回；preload/final的young返回≤callback返回≤接受或prepared≤原D；critical返回≤native开始。max_age固定为该位置的200或300，不能自报更宽数值。

`wire_event_count/wire_last_event_id` 固定原Snapshot可见前缀，query event不得越过它；最终wire中的正向不可变身份/转发值与投影相等。最终wire后来才完成的Write，不能回填成见证内早已转发；不能仅凭最终WrittenNS>0或event_id在前缀内就认定当时可用。这里证明实际选中的八条及采样区间，不声称跨两个stats和Snapshot的状态是原子同时、也不推断间隙持续满态。

失败准备历史改用 `preload_joint/slow_full`，接受字段为 `preload_accepted_attempt/preload_accepted_ns`；新含义不沿用 `healthy_accepted_*`。上述Set锚点、实际已返回的Version部分映射与见证按执行进度保存，未发生部分保持null。原512/32历史无需在成功事件全量重复；full只保存两份至多8条query的接受见证，cycle 0只保存自己的至多8条。

## 4. 目标执行严格从14慢全部生效后计数

当前Go及 `g2_analyze.validate_competition` 按prepare前 `window_start_ns` 计数，不能直接沿用：六预热源的最终Version可在预热期已经出现timeout，从而掩盖完整慢窗口内未执行。

每条 `competition_target` 增加 `preparation_strategy`、`exercise_start_ns=slow_input_applied_ns`、`exercise_end_ns=window_end_ns`；后两项必须绑定同叶full/complete，且end−start≥原60s。上界仍为原最后业务cycle实际结束时刻，不新增等待窗口，也不改成强制精确60s截断。原 `version` 必须等于 `input_versions[exposure_id]`。

- `attempts` 只计 `Version==version && exercise_start_ns≤StartedNS≤exercise_end_ns` 的实际health请求；固定16源各自至少1次。
- `timeouts` 只计上述请求中 `StartedNS<CancelledNS≤exercise_end_ns && HeaderStartedNS==0` 的记录；固定14个慢源各自至少1次。它沿用fixture“真实无头取消”的含义，不声称单靠服务端时间证明整个probe恰200ms。
- 预热开始、窗口内才取消的记录不计二者；窗口内开始、结束后才取消的记录可计attempt但不计timeout。后续500ms heartbeat等待或cleanup不能补足timeout。
- 同一目标在测量起点之后开始的窗口内请求必须使用其最终Version；同Version慢请求若在窗口结束前实际开始发头，须拒绝慢输入被释放的轨迹，不能被另一个合格timeout抵消。窗口结束后的自然cleanup发头不反向否定此前输入，也不计为timeout。

parser从完整 `health.Records` 独立重算，精确校验16固定身份/14固定慢身份、health PID及同叶归属、唯一正整数记录ID/Version、无Overflow和正向时序；summary计数须完全相等，不选择有利子集。原health全历史保留，预热/晚完成记录只排除计数、不删除。health fixture与测试在同一进程，PID不能误要求等于OIC产品PID。

## 5. 明确声明、旧源码归档与实现范围

新 `case.g2_behavior` 必须显式包含：

```json
"competition_preparation": {
  "strategy": "six-source-preload-v1",
  "target_window": "all-slow-applied-v1",
  "query_witness": "selected-query-snapshot-v1"
}
```

新expected_spec整体精确匹配，130普通叶、108组、106资源及同run 20×20 clock库存不变。声明缺失/null/未知键值或类型不符直接拒绝；声明正确但任一必需见证/锚点/计数缺失也拒绝。raw自称strategy不能选解析器，新记录不能因缺字段降级到旧判断；非G2 case不由此隐含增加G2门槛。

在改动分析器前，将当时实际旧 `g2_analyze.py` 及recorded loader所需的相邻依赖逐字归档，保存原SHA/源版本映射并验证旧run精确复算。当前模块只接受新完整声明；旧manifest/raw/summary使用其recorded source路径，不在新模块按缺字段自动回退，不修改旧归档原件或重新解释历史PASS/FAIL。新manifest由本轮实现定稿后另行固定，不在本规格草稿中改写旧manifest。

最小实现范围是 `vectors_g2_competition_test.go` 的两批Set/三处原young见证/目标计数，`g2_failure_diagnostics_test.go` 的新阶段及有限投影，必要聚焦辅助测试，以及 `experiments/product/g2_analyze.py` 与对应测试/旧分析器归档接线。产品HTTP、Scheduler、OIC/OIS、control writer、Active与exporter输入流程均不因本项改变。没有新增控制暂停或重放。

## 6. 必需验证与未完成事项

纯Go辅助测试须覆盖固定六源/剩余八源与恰16次Set、旧六源不被Release、同一原D跨两阶段、第一阶段不合格不得前进、第二阶段独立复核、正常返回与Fatal/Goexit、三处见证复制不持有可变数组、未调用保持null、原未来Write不能回填及失败freeze先于cancel。数值边界保留200/300ms恰等、超1ns拒绝，不增加调用来构造证据。

Python必须用独立完整原始记录验证正例与精准反例：

1. 14慢中的一个只有预热timeout；预热开始但窗口内取消；窗口内开始但结束后取消；仅13慢、重复目标或Version/起止/计数被改。
2. 同Version慢源在窗口内真实发头；混入另一Version；合法预热加合法窗口请求；Started恰等slow、Cancelled恰等finish的边界。
3. preload满足但final已变老；final满足但critical超过300ms；缺任何一处见证、互借见证、改max_age、复用query/连接/binding、sample与见证来自不同attempt、改真实返回或原D。
4. query只在最终wire后来写完、ID越原Snapshot前缀、年轻事件替换原选中八条；full/complete策略或锚点不一致；新声明缺失/null/未知或伪装旧case。

归档与新解析器分别验证旧run精确复算、新格式空/缺证据拒绝；工具通过不能替代真实竞争。之后只能在固定源码的一次有界开发运行中验证六叶实际准备和完整窗口，失败原件全部保留，不拼旧通过叶。若仍不能建立原条件，继续从采样与原wire定位，不能增加Drop、延D、放宽年龄/满态/控制或60s判线。当前本方案未实施、未验证，旧失败与G1/G2/G3/G4实际状态均不变。
