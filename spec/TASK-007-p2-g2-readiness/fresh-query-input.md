# G2-11 连接前的新鲜查询输入

本次承接[固定7b5f750实际失败](native-before-write-result.md)及[四单回应](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6668359501)，只调整已采用竞争场景的夹具准备和严格证据适配。产品、服务端期限、业务判线、权限及支持范围不变。两个独立评审分别保存在本地 `coverage/native-g211-before-write-execution-01/consistency-diagnosis-01/diagnosis.md` 和 `fresh-input-review-01/runtime.md`。先提交回读本规格，再实施；本页不是通过证据。

## 事实、目的与取舍

上一轮六叶均在cycle0失败。SQLite2/PG0的全8查询前置年龄仅7.680/53.628ms，但两socket已连接约586/601ms才发送GET，首次响应头均EOF。OIS原ReadHeaderTimeout为200ms，尚未发送GET不暂停这个期限；客户端完整写入返回也不证明服务端执行。该机制与原件相容，因缺服务端逐socket关闭记录而不认定唯一原因。OIC读头为5s，不能直接套用该解释。

SQLite0/1及PG1原前置最老查询约158/160/155ms。SQLite0/1再花约100ms完成原导出资格，原同批已超过final200；等待下一批后，final/critical真实Stats中的两个角色exporter均Active0，后置实收200。其native约10–11ms，GET→DELETE合计约2ms，不支持把这两叶归为控制500ms超限。PG2在原dial1s内从未取得全8≤200ms，不能推断它在3s内一定能有更鲜的批次。

本候选同时移除“已连接后等Full”区间，并以**全8查询年龄≤80ms**作为更严格的准备条件。80ms不是产品承诺或充分条件：原Active≥40ms和五次429跨度≥40ms之外仍有Dial、Zero、往返和调度成本；资格多花时间、Full消失、查询换批或exporter退出，后置独立判定仍可失败。只移连接而继续接受158ms批次不能解决已知相位冲突；只收紧年龄而继续连接后等，又不能解决首头期限冲突。因此一次完整六叶区分这个组合，失败后不盲目原样重跑。

## 唯一顺序与期限

cycle0保持原首八Set（两健康、六慢）、真实P2/Q4及16源Register/Ack预热、一次32Drop arm、原metrics baseline。然后，在**尚未进入慢socket helper、尚无两socket**时：

1. 唯一执行原末八Set，逐次和最后返回检查最初prepareStart+3s。真实最后Set返回仍是slow_input_applied_ns，14慢源的完整60s从这里起算。
2. 在原prepareD内移动同一个有界Stats/Snapshot loop到`pre_dial_full`：保留双角色Stats顺序、Full五分量、原10ms节拍、原query倒序选择/完整binding/PID/前缀语义，只把该准备阶段的maxAge收紧到80ms。不能挑有利的八条、改年龄起点、改变query或source。没有额外metrics抓取、请求类型或并行轮询；更严格条件和可用窗口会改变实际Stats次数，不宣称成本不变。
3. 真实Full/8query≤80成立并保存独立见证后，进入原helper一次。helper在真正入口读取一次单调时间，建立原1s并与已有parent/prepareD取更早者。两个Dial、真实socket写期限、原Zero Stats、release及两writer继续用这个D。不得accepted后建一次D、进入helper又重建，不得第二Dial/Zero/release续期或失败后重拨。
4. 原Zero仍在两socket连接后且紧邻release，不能在其间插回Full等待。原每socket2048 GET、RCVBUF1024、全部写入完成、两首真实200响应头、Active2且youngest≥40ms、至少五次连续429跨度≥40ms不变。
5. 原后置final Full/query≤200ms和独立critical Full/query≤300ms仍重新实采。原native、GET→DELETE、两个Wait、post Full/429、真实失败计数、关闭/资源2s及20至512完整cycles均保持。前置80不替代任何后置条件，实际新批query也必须按原规则验证。

**准确的预算变化：** 814c/7b把新增Full准备放在已连接后，受min(prepare3s,真实dialD,firstConnected+1s)交集限制。本候选把该准备移到尚未dial时，故这部分只受原prepare3s，不再夹尚不存在的dial1s；不得写成所有约束位置等价。最初prepareStart不重置，3s仍覆盖全部准备和cycle0后置输入。真正进入helper后，其原1s与原socket/cycle期限不放宽、不续期。后置输入仍取原min(prepareD,firstConnected+1s)，写入还必须受更早的真实helperD约束。

后续cycle不重复Set/arm/pre-Dial准备，保持原独立两socket输入。不得从accepted、Dial、release、final或cycle0结束重起60s；旧Version跨Set返回、窗口内实际attempt/timeout和晚Finished处理不改。

## 声明、原件与严格复算

当前完整声明改为：

```json
{
  "strategy": "six-source-pre-dial-fresh-v1",
  "target_window": "all-slow-applied-v1",
  "query_witness": "pre-dial-final-critical-query-snapshot-v1",
  "preload_witness": "probe-queue-register-snapshot-v1",
  "pre_dial_query_max_age_ms": 80
}
```

`competition_full`单独保存`pre_dial_query_witness`，结构沿原完整sample、8query投影、PID、16源binding、Snapshot前缀、Attempt/callback-return/实际evaluated/returned；其max_age_ms必须80，成功时深拷贝。`pre_dial_context_deadline_ns`和`pre_dial_accepted_ns`作为共有标量在full/complete逐值相等，前者在本版本必须精确等于最初prepareD，是该阶段真实ctx D，不能填未来dialCtx或另造较短D。新成功声明拒绝旧before_write字段/缺新字段回退；失败允许尚未发生动作null。后置final200/critical300仍各用独立sample，允许同query ID/相等wire前缀或真实新批，不允许复用sample/时刻冒充新观察。

在每个G211 cycle的`export_slot_input`增加`helper_started_ns`和`helper_context_deadline_ns`。起点来自实际构造now+1s的同一次time.Time，不能事后近似或从ConnectedNS倒推。cycle0核`helperD=min(prepareD,helperStart+1s)`；本版本后续cycle沿原Background parent无deadline，精确核helperD=helperStart+1s，不接受未声明的更早parent分支。实际两socket同一个WriteDeadline与helperD一致，由共享单一deadline赋值及真实Go socket回归验证；Python原件核起点/D算式和写入界，不声称独立读取了内核或服务端deadline。accepted≤helperStart≤首Dial/Connected，Zero/release/writer完成≤helperD。原Connected+1s/cycleD另存另核，不用较晚者替代较早写期限。

完整顺序须为：末Set真实返回≤前置sample/快照/评估/回调返回≤accepted≤helperStart≤两Connected≤Zero采样≤release≤两writer开始/完成≤首头/Active/完整429资格≤独立final≤独立critical。所有关联继续以原角色PID/注册代数绑定，禁止拼接不同连接、准备或run。

512历史仍只是有界诊断，溢出不成为新的业务拒绝条件；真实Attempt可大于512，独立成功见证不能因此丢失。旧7b分析闭包在修改前逐字保存（八模块及source-map），原9c及其他旧链不改；旧19正式run/14ITER和本次7b失败按各自来源解释。新完整strict注册表仍108组/130普通叶，不能删case、弱化失败或伪造父PASS。

## 失败所有权与反例

前置失败时没有slow socket或worker：原preparation owner在任何清理前冻结phase、部分Set/原ctx/历史，实际未进入helper、Dial、Zero、release均保持null或原未执行形状。不得伪造已开始的竞争cycle，不新增第二个竞争失败事件；前置error/panic/Goexit均由唯一原准备owner完成收尾。

进入helper后仍保留7b已核的单owner保护：早期Dial/Zero/error/panic/Goexit及observer异常不泄露两socket或done，不二次通知，不吞首次异常。helper已进入但尚未到Zero时，失败observer可从helper实例读取已存在的真实起点/D，不能把已经发生动作写成未知。正常handoff后仍由原cycle owner关闭并join，不删除既有异常回归。

至少核以下有区分度的行为，避免实现镜像测试：

- 准备耗时超过200ms但在原3s内时，真实环回服务端尚无这两连接；仅接受后出现连接并按原条件读真实200头。另保留慢Dial/Zero导致原期限失败的反例，不豁免OIS200ms。
- 全8恰80ms可接受，80ms+1ns、只一条过界、未来Received、未完成Write、错PID/binding或挑年轻子集拒绝。前置79–80ms成立而后续超过final200/critical300仍失败。
- Full80只在prepareD后出现或整个3s均不出现，均不Dial；末Set部分完成/Stats错误/panic/Goexit冻结一次并保持后续null。成功后重复准备/helper调用不得变成重试或续D。
- prepareD只剩短余额时helper1s必须夹紧；两socket真实D、Zero/release/write返回超D拒绝。实际helper起点被倒推/未来填写、用Connected+1s覆盖更早helperD均拒绝。
- 前置sample冒充后置、深拷贝被后续覆盖、资源/后置429缺失均拒绝；相同ID/相等前缀但不同实采以及真实新批合法。512溢出而原D内真实接受仍保留独立见证。
- 最后Set很晚时60s仍从其真实返回起算；不得从Dial重计。原12种callback/observer异常清理保障继续核验。

先完成局部Go/解析器反例、独立交叉审阅及旧证据精确复算，再固定最终源码执行原生完整七步/六叶，不选有利backend/round。helper库存依实际测试树登记，不强行保留已经失去用途的旧名称或把改名当新增行为。

## 下一次环境与项目收束

下一轮继续原授权主机的独立开发环境，按[native复用规则](native-g211-reuse.md)保留所有旧原件。seed仍为9c那一根已验工具/PGprefix/modules归档；fresh核对9c及7b两轮共18个真实旧进程身份/组，无残留、PID复用或boot变化后才创建新UUID。新cache、PGdata/socket/凭据/原parent9000，host9030、传输1800、装配600、预构建共享900、helpers240/G2111200、integration共享5700、stop与collect共享300均保持。builder从两轮保全prepared/result/manifest取身份；不复用旧PG、不续旧parent、不增加网络依赖fallback，停止仅针对新root核准PG。

准备脚本只为18旧身份和新source/helper库存做窄派生，guest执行/源码安装/收集语义不变；先纯mock核绑定/漂移/期限与原脚本差异，再真实准备、固定源码七步、停止与收集。任何未知SSH结果不重放；失败六叶与分析原件完整保留。

本轮不修改产品调度、500ms query或200ms exporter预算，不删GET，不并发Stats，不增加Drop/源数/请求管线，不使用环境归因豁免。没有新正式run/ITER或G1/G2/G3/G4晋级，alpha/main保持。若新候选无法在原准备窗建立共同输入，继续以真实反例重新论证，不放宽80、200或300来制造通过。完整质量与后续同源正式门槛、既有SSH/Bash首版验证仍必须完成。

## 局部实现状态（2026-11-06）

514a917先行规格回读后，已完成10个integration Go文件及12个Python/归档文件的局部实现，精确22文件SHA和原始命令见[本地开发记录](fresh-query-local.json)。前置Full80在原prepareD内、actual helper起点/D在接受之后，原Zero/release及后置final200/critical300保持独立。前置失败无socket，进入helper后的原12种异常清理回归保留，未执行事实不伪造。

Windows Go1.27.3两次完整helper均277节点PASS（41根236子、0FAIL/0SKIP），原225逐名保留；新增4根18子，旧前置deadline/witness按新顺序更名或收束后净增20节点。integration vet通过。最后只有vectors三行旧顺序注释修正，测试前后SHA与最终注释SHA分开保留，去注释代码等价；随后fmt无差异，没有重跑语义未变的测试。

Python142个G2相关测试及其中最终44个准备测试通过；6个故意错误变体被28个断言拒绝，旧7b八模块583613B逐Git归档，19旧run/14ITER复算精确。两份Go/Python交叉审阅无高置信阻断。18旧身份准备器完成41个纯mock（首次路径常量失败保留），root只读核验53引用、25原guest函数及原执行/传输不变；这不是现场fresh检查。

该局部检查点后，固定33f436a已完成一次native六叶：[实际结果](native-fresh-query-result.md)为两round0叶完整行为/B4通过，另四叶在原preDial准备窗失败，六strict组仍FAIL。942源与生命周期已核验回收；本页保留已执行策略，完整质量、正式门槛及下一输入机制仍待完成。原失败、门槛与alpha/main不变。
