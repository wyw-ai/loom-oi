# G2-11 beforeWrite 联合输入先行规格

本规格基于固定 `9c229d2717c5b1694c4b73a1bb7d37712eff9199` 的唯一原生执行及 `coverage/native-g211-execution-01/g2-diagnosis-01/diagnosis.md/json`。先行规格814c1d4已提交回读；现已完成夹具及解析实现、局部回归和独立审阅，固定7b5f750已完成唯一原生六叶：257helpers通过，六业务叶均在cycle0失败；[实际结果](native-before-write-result.md)记录具体反例。本页保留该次已执行策略，下一候选另行先行固定。旧原件、旧分析器及失败结论全部保留。

## 1. 本次只试图区分什么

原生六叶中 SQLite1、PG0 完成 118/117 cycles 与各自完整60s；SQLite0/2、PG2 在合格429之后又等约418/527/476ms才开始critical，前后真实Stats的 exporter Active 已为0，后置抓取实际200。PG1的70次final样本中，9次Active2均不Full，36次Full均遇到超过200ms的query，32Drop只用了14。两个成功叶的 exporter资格到critical约82ms。

候选把**首轮慢GET的发送**推迟到一个真实Full/young准备点之后，避免主动占用exporter的原200ms时间去等待尚未建立的query/backlog。它不创建query、不使Health.Set立即产生Changed、不保证Full持续、也不保证后置final/critical成功。不能把timeout=2绑定到初始两个200响应。

这是cycle0的夹具准备排序及一个必要的新准备观察阶段，不是产品修改。保留原slot数量/年龄条件、所有业务验收线。尤其不能声称“完全没有新增Stats”：若要在GET前证明真实Full/young，同时保留GET后的独立final200，前置阶段必须有自己的Stats/Snapshot；这部分成本明确消耗原共享D。

## 2. 精确顺序与原期限

前八Set（2健康、6慢）、真实P2/Q4和Register/Ack preload、唯一32个Snapshot Drop装配完全不动。cycle0仍为同两slow socket的唯一owner，按如下顺序：

1. 原200 metrics baseline，之后用原dialer连接原两socket（RCVBUF1024）。未发送慢GET。
2. 进入已有`g2BeginSlowScrapesDiagnostic`的beforeWrite回调；保存其**实际传入ctx的原Deadline**。原helper在首个Dial前就建立1s D，并把同一D设置为socket WriteDeadline，它可能早于`first_connected+1s`；不得从回调、Set或放行时重起。
3. 在两socket尚未启动writer时，唯一执行原末八Set。每次调用前/最后返回仍检查原D，保留部分版本和真实完成边界。最后Set返回值仍是`slow_input_applied_ns`，总共16次Set，不重设前六慢、不重新选source。
4. 新阶段`before_write_full`：在原D交集内，用与现final相同的双角色Stats顺序、Full五分量和200ms young谓词，取得真实8query准备见证。沿原eventually的10ms节拍及短路；同一已有512窄历史记录phase，容量溢出仅原诊断不完整，不新增判线。不得新增`/metrics`观察或启动writer来探测资格。
5. 前置见证真实成立后，才调用原`slotInput.beforeWrite`：原唯一Zero Stats确认Active0，紧接记录`WritersReleasedNS`并检查原ctx/D。该调用必须在新gate之后；不可先写WritersReleased再去等待。回调正常返回后，原helper启动原两writer，发送原各2048个HTTP请求。
6. 原全部写入完成、两首真实200头、非export Stats Active2且Youngest≥40ms、原连续至少5次429且跨度≥40ms，全部保持。仅有前置query见证或首429均不能跳过这些条件。
7. 原**后置独立final** full/young≤200ms循环照常执行，真实新Stats和新Snapshot、原完整binding/PID/代数验证，不能复制前置见证。随后原**独立critical** full/young≤300ms，以及native、原GET→单次DELETE、两个Wait、后置Full/429、关闭前实际失败计数、资源清理，均不变。

前置gate的D为`min(原prepareD, 原beforeWrite ctx.Deadline, first_connected+1s)`；正常结构中ctx.Deadline本已不晚于前两者，但必须保存真实值而非推算。后置export/final保留原`min(prepareD, first_connected+1s)`，socket原更早WriteDeadline绝不延长。critical/业务沿原cycleD及原各ctx，不增加备用时间。

不添加“必须留足N毫秒”的新通过条件、不固定sleep对齐、不让未成立的前置gate靠超时成功返回、不重试业务或重建socket/32Drop。若原D不够，真实失败；不能只把query年龄锚改成更晚时刻。

**60s仍从真实最后Set返回开始。** 本候选的前置等待与export资格期间已经是14慢输入，计入同一60s；循环结束点仍`slow_input_applied+60s`，不从writer放行、final或cycle0返回重新起算。每个源的实际attempt/timeout继续只认该严格窗口；旧Version请求跨Set正常完成仍保留，不能补进新窗口计数，也不能因Finished晚于窗口删除已合法attempt/timeout。原每叶20至512cycles/heartbeat准备起点与全部既有规则不变。

## 3. 最小新声明及原始证据

新完整声明固定为：

```json
{
  "strategy": "six-source-before-write-v1",
  "target_window": "all-slow-applied-v1",
  "query_witness": "before-write-final-critical-query-snapshot-v1",
  "preload_witness": "probe-queue-register-snapshot-v1"
}
```

不能让旧late-set声明接收新排序，也不能在新声明缺字段时退回旧校验。先逐字归档固定9c229d2的分析模块与邻接依赖，旧run/开发原件只用其原固定闭包复算。

在现`competition_full`增加一个有界`before_write_query_witness`：沿原独立完整sample、ois_pid/oic_pid、sample起止/return、Snapshot前缀count/lastID/return、实际evaluated/returned、200ms、result、attempt/callback-return和最多8条实际被检查query投影；另带该次原完整sample。保存时深拷贝sample及其可变映射/Observation、8条投影，不让后续final采样覆盖；仅成功callback成立才保存，未成立保留失败历史，不伪造8条。512只限诊断历史，不限制真实Attempt；即使历史溢出，实际成功的独立见证仍保存，不新增Attempt≤512条件。其完整16源注册binding、控制连接和角色PID必须与原preload和B4 baseline相同；前置/final之间允许真实新query ID，不能人为要求同一批或重置原query D。

另保存两个标量：`before_write_context_deadline_ns`（原ctx实际Deadline）与`before_write_accepted_ns`（该次回调实际成立时刻）。full/complete只重复共有标量并逐值相等，不重复三份大见证；新完整sample/见证只存一次。失败记录沿现phase/history及未执行null，增加这两个实际可空标量/成功指针即可，不新建数组或schema豁免。

独立parser必须核以下顺序，所有旧Active/header/429内部条件、原query前缀与binding反例继续保留：

```text
prepare start ≤ 原前8Set ≤ preload接受 ≤ 唯一32arm完成
≤ baseline.start ≤ baseline.end ≤ 两socket Connected
≤ 末8Set开始 ≤ slow_input_applied
≤ before_write.sample开始 ≤ sample结束/返回 ≤ 原Snapshot返回
≤ before_write.evaluated/returned/callback-return ≤ before_write_accepted
≤ 原Zero.started ≤ Zero.completed ≤ WritersReleased
≤ 两原writer实际Started/Completed ≤ 原headers/Active2/完整429资格
≤ 独立final.sample开始 ≤ final.returned ≤ prepared
≤ 独立critical.sample开始 ≤ critical.returned ≤ native.started
```

前置完整链不得超过上述前置最早D；新ctx D必须不晚于prepareD和firstConnected+1s，真实socket WriteDeadline由固定helper原计算及回归绑定。后置链继续核原D而不沿前置accepted重建。前置/final/critical样本分别独立，attempt与真实时刻不能复用；真实同一批query ID及相等wire前缀允许，不强迫生成不同query或新增wire事件；完整wire仅用于核原投影及可见前缀，不替早期Snapshot补写后来的WrittenNS。前置见证不称exporter已经阻塞，因为GET尚未发送。

## 4. 失败所有权：新增等待进入旧回调的必要修补

当前`g2BeginSlowScrapesDiagnostic`在beforeWrite正常返回error时会关闭done和两socket；但直接panic/Goexit时，这条分支不会执行，cycle正常slow-owner defer还未安装。**不得把现g.eventually或preparation.sample的Fatal式循环直接搬进回调。** 普通前置loop采用原D内返回error的薄包装；不吞错误，不重造调度框架。

仍必须为beforeWrite本身的异常加最小退出defer：在writers尚未启动的确定边界，先幂等freeze实际phase/Set部分版本/原ctx及socket事实，再完成本两socket和done，正常error、panic、Goexit均可有限join。panic继续传播、Goexit继续退出，不伪造normal return。原失败observer只通知一次，observer自身异常也不能跳过资源关闭。未启动writer时原Started/Completed=0、WrittenBytes=0应如实保留，新可空动作保持null，不伪称GET已发。

失败事件必须在本owner清理后仅发射一次；不因新回调在cycle原defer之前而漏掉`competition_failed_cycle`。可提前注册本cycle现有失败owner，或在helper的回调边界完成等价的有限交接；实施只选一种，不能双owner/双Close/双emit。原正常成功及后续cycle清理顺序、失败前冻结、原2s关闭/回收均不变，不加新watchdog当通过预算。全source环境兜底清理不能替代该局部所有权证明。

## 5. 最小改动范围及可验收反例

- `vectors_g2_competition_test.go`、`g2_export_first_helpers_test.go`：将原末8Set从post-export final拆到cycle0 beforeWrite准备；原后置final仍独立；后续cycle不执行新phase。
- `g2_failure_diagnostics_test.go`与必要小helper：同512历史新phase与一个成功见证、两个原标量；不新增产品统计/请求类别或公共env行为。
- `vectors_g2_export_test.go`：仅旧beforeWrite回调异常退出所有权的必要修补；G2-12成功输入顺序不变。纯函数/helper测试可独立新文件，真实环回socket测试验证异常清理。
- 当前`g2_analyze.py`与对应准备测试：完整新声明、前置/后置证据独立和旧recorded closure归档。正式manifest不在本稿直接修改；未来库存按实际新增节点固定。

必要反例不是镜像实现：

1. 实际环回两socket、beforeWrite直接panic/Goexit及正常error：服务端未收到任何GET字节，两socket与done确实结束；freeze看见原ctx状态，失败observer恰一次，正常return标记为false。原成功回调不触发异常清理，最终仍仅两原writer。
2. 以显著更早的原dialCtx D证明前置不能改用connect+1s；跨原D、Set中途失败、前置成功但Zero/实际写入超D均拒绝，保存部分版本；不从accepted重建3s或1s。
3. 前置真实Full/young成立，但post-export final已老化、Full变空或critical>300ms：仍失败。新schema缺前置、拿旧Snapshot/未来write补足、角色PID/绑定串配、前置样本冒充final均拒绝。
4. Zero/WritersReleased被提前到前置wait之前、writer在gate前已写GET、只首429就开始final、后置200、未实发GET却用idle socket冒占槽，均不得通过。
5. 每源仍只Set一次、cycle1不重复gate；60s从最后Set，不能从放行GET开始；旧Version跨Set和晚Finished原合法边界保持。原184+新增joint41 helpers全部保留，新节点按实际名字登记。

## 6. 可推翻条件与不必要改动

本次可证伪预期仅是：若原query/full相位在剩余D内可建立，把GET发送推迟到它之后，有机会缩短“真实exporter资格→业务”之间无关准备等待；之后必须仍实测成功。若前置仍等不到8条≤200ms、前置已接近200ms而export资格又使其变老、后置重新等待造成Active归零、32Drop先耗尽、六慢P2/Q4丢失、或connect原D耗尽，候选失败应完整保留，不继续调整source/年龄或挑有利叶。

本轮不做Stats并发、删除GET、缩短429/Active观察、改probe/dispatcher、增加Drop/hold或更快retry；这些与候选区分性无必要关系。也不增“server收到GET精确时刻”产品字段：只能证明本地原writer尚未放行，handler的原200ms计时随后按原实现从实际处理开始；不能把writer release当服务端精确起点。

先做上述helper/parser与独立审阅，固定完整源码和225旧节点加真实新增库存，先行规格提交回读后按既有Autopilot授权实施；唯一同源双后端六叶的实际运行由主代理在源码/监督审查完成后执行，无需新用户许可。不把既有两叶成功/四叶失败重标或晋升门槛。

## 实施检查点与来源

本规格收束[固定原生结果](native-g211-result.md)的三个占槽输入失效与一个年轻查询未成立反例。只修改测试夹具及其严格证据适配，产品源码、默认参数、正式门槛和用户采用不变；先提交回读，再沿既有Autopilot授权编码。现有225个helper节点保留，新增有效反例按实际库存登记；Python适配必须保留旧闭包，并复算旧19run。[本地实施记录](before-write-local.json)保存257个Go节点全PASS（旧225保留、新32）、vet通过、Python139相关测试及最终41准备测试、4个错误变体实际被拒和旧19run/14ITER复算。原3次Go失败分别为新断言编译错误和测试虚拟时钟模型错误，全部保留。旧9c八模块逐字归档，loader与旧证据未改。新的六叶已运行且全部失败，具体边界见[实际结果](native-before-write-result.md)，不得将局部回归称业务通过。
