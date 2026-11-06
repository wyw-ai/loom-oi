# G2-11 单次探针路径重载的准备输入

本页在已采用 Probe-HTTP 和 G2-11 的范围内固定下一夹具机制。先提交、推送并逐字回读，再实现；不是产品契约或验收标准的新决定。依据[固定cbd94ce9原生结果](native-early-arm-result.md)，替代[early-arm](early-arm-input.md)的单代准备见证，沿用其唯一早arm及所有原判线。没有产品修改、成功保证或原样重跑授权推论。

## 1. 为什么检验这一机制

两个失败叶首批八pending的必要80ms窗口只有约25.60/21.12ms；窗口内backlog为15/13，缺失源在最新query分配时尚未到期。正常50ms周期物化与该错位相容。全部145/147个已采Full点也没有真实八pending仍满足80ms而仅latest8错选的反例。32Drop在首个Full时只消耗8条，不能把后来的额度耗尽当成首窗根因。上述均是采样范围内结论，不证明原3秒所有时刻无机会。

现有 `internal/ois/relay/control.go:onRegister` 在同一Hub锁内撤销变化绑定的旧pending、替换整组exposure后才一次 `pumpQueries(now)`；新16源同为dirty，首8个分配使用同一处理起点，余8个保留dirty。这与移动Set/Drop、恢复preload_joint或从Stats物化周期工作不同。OIC探针配置变更已由DISC-009-R2采用，保持目标Revision与已Running流，只换独立观测版本并取消旧样本/Preparing。

本候选通过一次真实配置写入改变16个probe URL的path，调用现有重载路径，不构造Register、复制消息、hold控制流、暂停进程、改时钟/ticker或加入产品hook。OIC逐job更新仍非原子，实际网络Received可能分散，故不推定P2/Q4、Full80或后续输入必定成立。

## 2. 不变项与唯一顺序

产品及依赖不变；原16源/同端点/允许网段/权限/目标/全部probe和观测参数不变。固定两健康、六预热慢及八后置慢源、两批8Set且每源仅一次保持；唯一早arm32、单query500ms、准备3s、实际helper入口1s、80/200/300ms、Full16/pending8/P2/Q4、两socket各2048GET/原写入与200ms、Active40ms/连续429、native→GET→DELETE→Wait、控制500ms、最后Set后完整60s与B4原2s全部保持。

唯一顺序为：

1. 原环境/资源baseline/victim实际Running→原prepareStart/D→唯一原Snapshot cursor→一次32Arm+Drop→首8Set→原P2/Q4 preload。沿用实际PID、epoch/generation、control连接及16源旧Register/Ack见证。preload仍不增加Full/young条件。
2. 在原cycle0所有者中取得原exporter baseline。**尚未执行末8Set或连接socket**，在同一原D内进行一次path-only配置写入；每个URL使用其原Health端点加 `/g2-competition-reload/<round>`，round只为0、1、2。不使用query或fragment，不换host/port/协议/网段，不改任何其他配置。Linux沿既有SIGHUP加速通知，Windows沿原文件轮询；不改变ReloadEvery，不重复写入或重发信号。
3. 在原D内按原10ms间隔有界观察既有proxy Snapshot，等待完整实际新Register/Ack；不发额外HTTP、Stats或业务请求，不等16个新健康请求，也不添加新超时预算。新注册必须满足下节完整过渡条件，才能进入末8Set。实际观察/解码和配置I/O成本全计入原3s，迟到即失败。
4. 原末8Set→原preDial独立Full80→原实际helper起点/D→原Zero/release/exporter资格→独立final200→独立critical300及原业务。后续query必须使用新注册库存，不能拿旧preload binding放行。原60s仍从最后一次Set返回起算，不从reload、prepared或native重起。

期间只允许一次实际配置变更；整个准备和60s保持同一产品PID、epoch/generation与control ConnID，不允许新重连/第二次Register改变绑定。任何变化都失败，不能补一次reload、补arm或重做victim。原清理/原首次失败冻结保持；新阶段失败时尚无慢socket，真实部分字段保留、未完成为null，不吞panic/Goexit，不把失败回调写成成功。

## 3. 两代真实注册见证

新strategy为 `six-source-probe-reload-pre-dial-fresh-v1`，`competition_preparation`增加准确声明 `reload_witness: path-only-register-transition-v1`；其余现行字段及108组/130普通叶/106resource/原clock声明保持。新旧声明严格隔离，缺键、旧值、未知字段、错类型不得回退。

保留原全部公共时点和preload witness；新增有界 `probe_reload_witness`，成功full/complete逐值相同，失败保存实际部分。字段应包括：唯一request序号1、固定新path、16项旧/新URL与未变probe配置的有限投影；配置写入/通知的实际起止及结果；首次请求前真实proxy cursor；实际观察次数与末次Snapshot返回时点；旧/新Register/Ack的事件/连接/收写时间投影；恰16项旧/新完整binding；接受时刻。无真实观察时不推算或补填，最终wire不可回填原Snapshot。

请求前与接受Snapshot各自必须有原 `event_count/last_event_id/returned_ns`，空前缀取0；逐项核ID连续、完整前缀每个Received不晚于该返回、请求前返回≤实际配置写入/通知开始，且新Register/Ack在接受前缀中。不能只给一个无法定位原前缀的时间。观察次数与这两份真实前缀区分，接受前缀不能由最终完整wire回填。

配置写入前从现有内存配置作独立深拷贝，严格核16个URL恰为原Health端点；修改后逐字段核除这16个URL path外整份配置语义相同。记录完整probe有限投影及前后配置SHA作诊断，不输出证书/私钥内容；SHA不能代替原始Register证明。Go固定实际变更代码及反例，沿既有Compile计算每源旧/新预期contract digest；Python按固定HTTP/loopback输入独立规范化完整probe投影并重算两份SHA。必须与实际旧/新Register相等，不只检查两个digest不同。最终库存仍来自原始Register，不能用预测ob序号生成注册证据。

注册验真要求：

- 旧Register/Ack恰为原preload已接受对象；新对象位于本次请求前cursor之后、原观察前缀内，原生、同lab-a/control、正确方向/类型、非注入/非Drop/非WriteFailed，实际成功转发。时间顺序沿原消息并发规则，允许原Write返回晚于对端已开始回应；禁止用未来Write完成冒充原Snapshot当时已见完成。
- 新Ack接受恰16个原固定源且无拒绝，Register恰16个完整Supported绑定；Access/epoch/generation/ExposureID/目标Revision/source/contract version与旧代保持，每源observation revision从原规范ob_N严格变成ob_(N+1)，contract digest为上述独立重算的新值。拒绝跳号、仅不等或任意新SHA。原protocol类型/格式和非bool整数检查保持。连接和资源PID均不变。
- 完整wire证明仅这一组旧→新过渡；不能选后来有利的Register，不能忽略中间拒绝、另一代或重连。preload、reload、preDial、final各自使用原观察前缀与实际返回时点，不相互借用后来的完整性。
- 原preload在两次Set之间的P2/Q4事实仍保留；它不证明重载后P2/Q4。重载后的preDial/final/critical仍用各自真实组合Stats和完整八query检查，绑定新库存，数值门槛不改。

共同顺序必须覆盖：原prepare→arm→首Set→preload接受→exporter baseline结束→reload请求/写入→新注册接受→末Set→preDial接受→原helper及后置资格→prepared≤原D。内部HTTP、wire、socket和callback的全部原细约束继续独立校验。

## 4. 不遗失旧Drop，不混算重载取消

保持原32个广义snapshot matcher，不新增binding过滤器或修改matcher语义，不追回已经消耗的旧额度。全部实际Dropped仍合计≤32，前缀cursor前无Drop；关键消息、其他方向/角色/连接、注入或已转发的Drop均拒绝。每条Dropped必须找到原成功转发query，response/query的完整binding一致，并属于其因果上的**真实旧或新Register**。旧query可在新Register之后才返回旧snapshot，必须计入32且按旧query绑定验证，不能按snapshot时间粗切后丢掉。新query不能借旧注册，旧query也不能冒充新八条young；未登记或第三代binding整叶失败。原query write/Received竞态、预算和总数核验保持。

这里的注册因果至少要求对应真实 `Register.WriteStartedNS≤query.ReceivedNS`，最终完整wire保留该Register成功Write及对应Ack；不错误增加 `Register.WrittenNS≤query.ReceivedNS`，也不要求旧query的Received早于新Register。Dropped query在最终完整wire须 `WrittenNS>0`、`0<WriteStartedNS≤response.ReceivedNS`；原Snapshot取样时query.Written仍可为0，不能强求其Write返回早于response或该原Snapshot。新Register/Ack的接受见证仍必须在其接受Snapshot实际可见且已完成，后续完整wire不代填这份见证。

Health完整历史全部保留，原 `StartedNS∈[lastSet,windowEnd]` 的全集、attempts和timeouts公式一字不放宽。新增更严格条件：该窗口内**每条**实际请求的RequestURI都等唯一新path；任何旧path/其他path出现即整叶失败，不能筛掉该条再计数。窗口前旧请求本来不计入该全集，仍保留其真实取消/完成时间。

依据现有 `reconcileProbes` 全程regMu串行及 `Scheduler.Update` 同完整binding直接返回，本次唯一变更不会再次取消已按新path执行的任务。RegisterAck不证明旧HTTP已join；无需宣称它做到这一点。若旧任务迟到进入窗口，新增全集条件会拒绝。这只排除本次reload取消对窗口计数的混淆，**不把所有CancelledNS升级为已独立证明200ms期限触发**，不新增或删除原业务判定。

## 5. 实现与有效反例

仅修改G2-11夹具/有界见证与对应分析器和测试；产品cmd/internal/pkg、依赖和通用Stats行为不变。新增helper应有单一状态所有者，原snapshot-drop/preparation/12类socket清理测试全部保留。新增测试至少覆盖：

1. 唯一路径写入与完整顺序；保持整份其余配置、源/Set/arm次数，拒绝第二次请求、错误URL/query/fragment、目标/权限/参数变化、原D前后边界。
2. 真实两代Register/Ack投影、原前缀及binding全字段；拒绝未登记、第三代/同代未变、部分接受/重复源、错session/PID/epoch/gen/目标Revision、未来或回填事件。允许真实合法的原Write返回竞态。
3. 旧query晚到snapshot仍计数且正确归旧代；新query归新代；32恰界/33拒绝，旧关键消息Drop/错误query/未写或写失败拒绝，不忽略任何旧Drop。80/200/300恰界及超1ns、latest8和完整源匹配回归保持。
4. 真实新path窗口全集保留原attempts/timeouts结果；旧path在窗口内即使会增加timeouts也拒绝；窗前取消仍不计但保留。不得过滤旧请求使计数相等，不以path相等代替实际Version/Status/时点检查。
5. 写入、通知、观察、接受各阶段error/panic/Goexit/超原D，保持部分数据/null和唯一freeze/cleanup；不创建未开始的socket、不更新D、不假定Ack后scheduler已全部完成。

修改当前分析器前，逐字归档cbd94ce9的原八模块及source-map，实际调用recorded loader验证闭包；旧19run/14ITER及全部旧原件精确保持。Python必须用完整正例和逐字段错误变体验证独立复算，当前模块不兼容降级旧声明；失败阶段只增加真实新枚举/有限字段，不把缺记录当PASS。

Go沿固定1.27.3、GOMAXPROCS2/-p1/-count1、原helpers Go3m/外层240s及独立RUNDIR，核原342唯一节点和68文件闭包加实际新增库存；fmt/vet不得改无关源码。Python完整相关套件及旧19/14复算分别保存退出/skip/原失败。局部测试不替代完整95%或原生六叶。

## 6. 下一次实测与停止条件

最终源码固定、监督差异与实际库存复核后，只在新UUID执行原七步、两后端各三叶，仍可仅该G211子进程显式开启既有pending诊断。沿原9000/9030父期限、共享构建/集成期限与短child机制，不在旧目录重放。既有36正常身份增加本cbd的runner/七child/专用PG九项为45；另4旧失败、7机制身份及start/exit未知candidate分别保留来源，fresh检查重新执行，权限缺口不补成全系统无进程。

本次新增证据要区分：实际新注册是否批量形成同代查询、原Full80与P2/Q4能否同时满足、32额度剩余、是否有旧path晚入窗口，以及所有原导出/critical/60s/B4条件。任一失败即保留完整反例，不继续相同配置撞成功、不拼接旧四叶、不过滤不利请求或降低判线。没有本策略实测结果时，不把源码推演写成通过。

正式门槛和alpha/main保持。完整首版仍需最终同源G1/G3/G4/G2、完整质量与已授权目标上的SSH/Bash G5/G7；本夹具变化不替代这些工作。
