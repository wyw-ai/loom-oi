# G2-11 探针队列先建立的输入计划

2026-11-05。既有 G2 契约内的夹具排序修正，先行596f335推送回读后已实施，Windows开发检查见文末；真实Linux六叶尚未验证成功。依据固定 `2a81a22424ff3e717d923c1532062b5826c6aa9d` 的唯一完整 Linux 开发轮及 [完整开发记录](liveness-complete-linux.md)。旧六叶失败、原始记录、旧分析器及门槛状态保持。本页替代 [六源预热](six-source-preparation.md) 的额外 preload 联合接受条件，并接续 [导出输入前置](six-source-export-first.md)；不修改任何产品配置或时限。

## 1. 原要求与本次修改的边界

[已采用 G2](../../docs/architecture/p2/g2-observation.md#acceptance) §9.1/§9.2 要求在满 probe/dirty 队列、两个不读 scrape 下验证真实控制竞争。P2/Q4、16 源、500/200/50/1500ms 的 probe 配置、8 个 query 槽及原 500ms 期限、两 exporter 槽/64KiB/200ms、各后端三轮 60s 和控制处理 500ms 都保持。原生 Open/close、显式撤销、heartbeat ACK、许可零容忍及回收要求不变。

后加夹具策略要求“六源预热阶段也先取得一次 full+8 条 young≤200ms”，而后再取得正式 final 与 critical 见证；这份额外联合预热并非冻结 G2-11 的第二次业务验收。本次只将它改为真实探针 P2/Q4 预热见证。**正式 final 的 capacity16/backlog16/pending8/P2/Q4 与 young≤200ms、cycle 0 的独立 critical full 与 young≤300ms、操作后的 full/实际429全部保留。** 不将单项预热当作正式满态。

其他既有输入边界也保持：固定健康源 `lab-a/http`、`lab-a/alt`，固定预热慢源 `competition-02` 至 `07`，其后慢源 `08` 至 `15`；两批各8次、总16次 Set、每源一次且不释放前六慢源；最多32条原生 snapshot Drop；原准备3s；两 socket 各2048请求、RCVBUF1024、原 connect+1s；原 Active/headers/429资格；每叶20至512个cycle；原2s清理。所有数值不变，不重新定义生产 SLO。

## 2. 事实与不能外推的内容

固定 2a81 六叶全部准备失败。SQLite round0、PG round1在原3s未取得 preload 联合条件；其余四叶接受 preload 时已观察到16/16/24/24条 Drop，建立原 exporter 输入再耗约195–227ms，final在两socket原1s内未同时合格。SQLite round2完整wire只出现28条Drop，说明“32耗尽”不是统一根因。

OIS backlog 是每源 `dirty || queryID!=empty` 的并集，Drop仅使已发 query 等原期限，不会使另8源自动 dirty。query开始时清该源 dirty；probe实际开始/完成等真实变化再产生 Changed，8个query各自到期、按游标轮转，不能预设同步批次。原采样出现过年轻query的可能时窗但 backlog 不满，或 backlog满而P2/Q4不满；实际 full 为真时 young 的拒绝均由年龄超过200ms触发。完整wire后来发布的状态不能回填为原采样当时可见的值。

六个固定慢ID不等于调度器中的连续六项：当前 OIC 把jobs放map后遍历调用 Scheduler.Update，内部顺序没有对外固定承诺。不得按最终观测临时挑六个“更有利”源或擅自改产品调度顺序。也不将16条真实 Changed 暂存后绕过critical补发：单条control的全局Seq与有序传输不允许如此重排；本计划没有控制暂停、伪造、复制或重放消息。

新排序减少在探针队列尚未建立时消耗 Drop，以及为非必需 preload joint 等待的成本；它不保证新 final 必定成立。若仍失败，只能据新原件继续定位，不续期、不重复装配或拼接旧通过叶。

## 3. 唯一执行顺序

新策略名为 `six-source-probe-first-v1`。

1. 原一次环境、初始资源profile及victim Running准备保持。按原位置建立 `prepare_started_ns`，唯一 `prepare_deadline_ns=prepare_started_ns+3s` 必须早于任何本轮 Set 或 Drop arm；不能把预热搬到此D外。
2. 按原固定顺序执行首8次 Set：两健康源false，02至07为true。保存真实起止、各自返回Version。此阶段尚未 arm 本轮32个snapshot Drop。
3. 在同一原3s内执行原组合Stats采样、原一次proxy Snapshot；只以真实同代 `ProbeJobs=16、ProbeActive=2、ProbeQueued=4` 及有效身份/库存接受预热。OIS capacity必须16且在线拓扑正确；backlog/pending按实际记录，不要求本阶段已满。保留全 sample 与 full 各项诊断，但不调用本阶段young，`young/queries` 明确null。每次原callback及10ms间隔保持，不额外加一轮“确认”请求。接受后立即检查实际返回仍在原D内。
4. 取得预热见证后，才按原方向/类型一次注册原32个matcher并逐个决定Drop；记录首个Arm调用前 `drop_arm_started_ns`、全部32个Arm+Drop返回后 `drop_arm_completed_ns` 及实际完成数32。没有第二批额度、重试arm、pause或hold后补发。迟到或部分失败先冻结事实并原清理，不能继续输入。
5. 立即执行原最后8次 Set，只改08至15，记录最后一次真实返回 `slow_input_applied_ns`；原完整60s仍从这个时刻起算，不从exporter合格、prepared或native重起。
6. 同一个cycle 0所有者建立原完整 exporter 输入：独立 before_export 200基线、同两socket/写入、Active0/放行、真实headers、Active2且最年轻≥40ms、连续至少5次429且跨度≥40ms。原请求上限、写截止、清理所有权不变。
7. 在原3s与本两socket最早connect+1s的共同剩余期内执行正式 final。仍用原两Stats、原一次Snapshot、原full短路young≤200ms，取得独立 final 见证后记录prepared。实际接受晚于任一D即失败，不以eventually首次callback补做超时采样。
8. 原cycle 0继续独立critical两Stats及Snapshot/young≤300ms，然后原native、撤销因果见证、后置full/429、关闭前两次导出失败计数及原close/recovery。critical不能借final样本，业务仍受同一原connect+1s及撤销500ms，不因prepared重新起算。后续Drop取消/drain、role轮换、target窗口计数、heartbeat与资源检查不变。

P2/Q4预热失败不得先创建慢socket。final失败不得重复baseline、连接、Active或429输入，也不得重做victim/native。原prepare失败与cycle失败各至多一次，先冻结真实部分状态，再cancel/Close/join；未执行字段null，Goexit不伪记正常false。

## 4. 原始证据与身份

`competition_full`、`competition_complete`保留且逐值一致：原prepare/slow/window字段、`preload_set_started_ns`、`preload_set_completed_ns`、`preload_accepted_ns`、`final_set_started_ns`、完整16键 `input_versions`，新增上述两个arm时点和实际arm数。必须核：

```text
prepare_started ≤ first_Set_started ≤ first_Set_completed
≤ preload_queue_sample.started ≤ sample.completed ≤ sample_returned
≤ preload_queue_witness.returned ≤ callback_returned ≤ preload_accepted
≤ drop_arm_started ≤ drop_arm_completed ≤ final_Set_started
≤ slow_input_applied ≤ before_export.started ≤ before_export.completed
≤ first_connected ≤ actual_rejection.completed ≤ final_sample.started
≤ final_witness.returned ≤ prepared ≤ min(prepare_deadline, first_connected+1s)
prepared ≤ critical_sample.started ≤ critical_witness.returned ≤ native_started
```

其余原Active/headers/429内部时线独立校验，不以这些简写取代原validator。

新 `preload_queue_witness` 明确是队列见证，不沿用旧 `preload_witness` 冒充8条query。有限字段包括：实际 `ois_pid/oic_pid`、原完整组合 `sample`、`sample_returned_ns`、原 `attempt/callback_returned_ns`、`wire_event_count/wire_last_event_id/wire_snapshot_returned_ns`、原 `evaluated_ns/returned_ns`、`result`，以及 `control_conn_id`、同一次Snapshot内实际 Register/RegisterAck 的正向转发投影和恰16项完整binding库存。投影仅含原event/conn/received/write_started/written身份与时间，库存从这份原Register字节解码；不增加HTTP或Snapshot，不从cleanup最终wire补造当时状态。解码/复制成本也属于原3s。

分析器必须逐项绑定：

- 两role PID等于原资源baseline；sample的OIS/OIC epoch及OIC generation一致、connected/registered及在线拓扑成立，OIS capacity与OIC ProbeJobs均16，P2/Q4是实际值。
- 原Register/ack是同lab-a control的真实非Injected/非Dropped/非WriteFailed记录，正向转发时间在见证Snapshot之前；事件在原可见前缀内。ack接受的恰16固定源无rejection，binding字段完整、Supported、源/Access/epoch/gen/revision/digest正确；不能只核16这个数量或用proxy/health PID替代产品PID。
- `input_versions`仍来自恰16次实际Set。后续final/critical的每条query使用该同control/同库存的完整binding，PID/epoch/gen不变；不能换session/revision后借旧预热。
- 本轮Drop不得出现在preload见证原Snapshot的前缀内；原完整history只能有同方向/类型、原生未转发的至多32条，实际已观察公共表/游标不丢失。`ReceivedNS`在proxy获取记录锁之前采集，不能额外硬断每条Received必晚于arm开始/结束；以原前缀、真实arm顺序和已观察Drop状态约束，防止错误时序断言或最终状态回填。

预热历史仍用512项有限记录，但阶段名为 `preload_probe`。该阶段记录所有实际full分量而不以它们作接受条件，query相关字段null；P2/Q4谓词及正常返回状态独立记录。原 `slow_full`继续保存完整full/young200。32项公共Drop表在arm之前必须为空，arm之后增量扫描未来事件；检查原“每条首次进入Snapshot时Drop决定已固定”的条件仍成立，不把旧注释“所有arm早于第一次准备Snapshot”原样照搬。失败与成功原事件形状的策略差异均显式版本化，原失败历史保留。

## 5. 显式选版与冻结旧证据

新 `case.g2_behavior.competition_preparation` 精确声明：

```json
{
  "strategy": "six-source-probe-first-v1",
  "target_window": "all-slow-applied-v1",
  "query_witness": "final-critical-query-snapshot-v1",
  "preload_witness": "probe-queue-register-snapshot-v1"
}
```

新模块只接受上述完整声明。缺键/null/旧值/未知字段或混用旧preload字段都拒绝；不能由raw自称策略选择宽分支。final和critical仍采用原完整查询投影校验：真实8条不同query/源、全binding、原收到/写出/可见/评价时点、400–500ms wire预算、200/300ms恰界及原资源PID绑定，不降低任何正向证明。

在改分析器前归档实际旧模块及其recorded loader邻接依赖，逐字保存固定来源/SHA并复算旧正式run。固定2a81的本次失败及其旧分析器结果不变；新策略只能由新源码/新声明验证。130普通叶、108组、106资源及同run clock库存不变，不能把只跑六叶当完整G2；新helper库存据实际实现另外固定。

## 6. 实施范围与验证

最小实现范围：`vectors_g2_competition_test.go` 的预热谓词/arm顺序、`g2_failure_diagnostics_test.go` 与六源helper的有界见证/历史、必要独立helper测试、`experiments/product/g2_analyze.py`及对应反例与旧模块归档。产品Scheduler/OIC/OIS/HTTP、原代理传输与控制排序不改；不调整source集合、连接池、采样并发或keepalive策略。

有意义的helper与parser反例至少覆盖：

1. 首8 Set→实际P2/Q4接受→仅一次32Arm→末8 Set，未接受或原D耗尽不得Arm/final/socket；每源恰一次，前六慢源不释放。只有P2、只有Q4、错误ProbeJobs/capacity、采样晚于D均拒。
2. 新preload的backlog/pending不满且young未调用可合法接受；final同样不满或young200不合格必须仍失败，critical300与后置full/429不变。旧策略字段不能进入新模块。
3. 缺binding、少/多/重复源、另Access/epoch/gen/PID、错误control、未成功转发/晚写的Register或Ack、拒绝项、库存与final query不一致；最终wire不能补原Snapshot当时缺失的成功时间。
4. 首个Arm发生于preload前、Arm数量不是32/重复配额、已有Drop混进preload前缀、假将某条Received在arm锁前的合法记录判错；公共历史跨arm游标正确且不漏真实Drop。
5. final虽在3s内却晚于connect+1s，或反之；200/300ms恰等与超1ns；借用final作critical、重做socket/基线或续D；真实Fatal/panic/Goexit先freeze后原取消/Close/join。
6. 14慢源仍按slow..finish的完整60s实际请求/无头取消计数，预热timeout与cleanup不得补数。六叶/round/role、20至512cycle、原心跳/撤销因果/2s资源与全部旧反例继续保留。

先做聚焦Go helper与Python正反例、旧run精确复算及代码独立审阅，再固定一次新的Linux开发来源，完整运行SQLite/PG各round0/1/2六叶，保留全部Go输出、每次原准备历史、wire、health和资源原件，并以同源完整expected_spec复算所选六叶。不得把Windows/helper或本次方案视为真实输入已成立，不自动重复未知结果，不改旧正式门槛；最终正式G1/G3/G4/G2仍按共同源码依赖另行复证。

## 7. 已完成的开发检查

[结构化记录](probe-first-local.json)保存7个Go文件、Python最终字节、实际命令和独立审阅。Windows helper实际162节点全部PASS（25根137子，含新4根47子），0FAIL/SKIP；integration vet、格式及差异检查通过。新解析器与反例完成后，完整experiments实际436项中426通过、10个原OS跳过，tools39项通过。19旧正式run/14ITER全部精确复算、无待分析run；归档的8个旧模块571569B与固定596f335逐字一致。

独立审阅的缺失前缀事件、跨control借旧Register及Python布尔/浮点协议版本漏检均已修正并保留原反例记录；final和critical现都与原注册完整库存绑定，200/300ms恰界及原D保持。没有把测试构造的队列见证当真实Linux输入。P2门槛检查仍因G1 Failed退出2，旧失败、130普通叶/108组/106资源与alpha/main状态保持。按[聚焦Linux计划](g2-repair-linux.md)固定新来源执行六叶与原G2-12后，才据原件判断本轮修复效果。
