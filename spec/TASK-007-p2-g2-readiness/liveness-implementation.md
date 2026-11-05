# TASK-007 数据腿活性修复实施计划

依据[ADR-006](../../docs/decisions/ADR-006-data-leg-liveness.md)与[原始采用](liveness-approval.json)，固定data-leg-liveness scope不变。本项修复阻挡当前负载/后续G2的共同数据腿行为，沿用当前任务feature；不是另一套业务或新应用功能。先行文档cf18ec已提交回读，三端接线已完成开发检查，完整同源复证尚未完成。

## 接线与所有权

1. 固定上游依赖副本与许可证/来源/补丁清单，go.mod本地replace指向仓库受管路径；只加有界PING/PONG帧写入口，内部沿原frame锁、mask、错误与关闭，跳过旧5s wrapper。旧默认Ping/自动Pong/控制会话保持。上游现有测试与新增125B/opcode、真实帧、ctx/Close/错误测试先通过；新增本地补丁代码纳入有效测试统计，不能借第三方排除掩盖自有逻辑。
2. 在内部wsio层实现唯一Reader回调到固定pending PONG、单控制worker与一个PING owner；固定125B pending/125B in-flight/8B nonce上界，copy与claim/retire序列明确，有限公平选择。真实写错误只通知原终态协调者，不在worker同步join自己；库不能通过取消某次观察误杀仍活连接。
3. OIS沿每腿单调t0及固定tick进行窗口归约，busy为具体DATA owner的来源转发/目的pending，覆盖enqueue→claim→finish并且只释放一次；sticky life记录两tick间活动。合法DATA和有效PONG分开，原channel.lastData不改。重排/晚PONG、退役write和长调度暂停不得制造历史发送或续D。
4. OIC与client的Accept/Dial回调显式启用同一新模式，控制会话保持原默认；三端transport真实关闭后解除写并join。正常Completed先按既有DATA/FIN排空、forceAt与原首终因，不在保存业务首终态时抢先CloseNow。现有读相位诊断保留真实故障分类，并增加必要有限新状态，不能用诊断构造新的成功路径。

## 验证顺序和边界

依赖API→控制owner/窗口纯状态与真实WS→三端有界接线→完整产品unit/race/静态/漏洞→固定新来源G1/G3/G4/G2开发及正式复证。确定性窗口用可控逻辑输入检验精确边界，真实socket/ctx/关闭另测；不改宿主或用户主机时钟。保持所有原半关闭/未知结果/许可/撤销/资源回收断言，按新准确合同更新原心跳时间预期并保留替代原因，不删失败路径来凑PASS。

旧版本矩阵区分正常wire兼容与完整背压能力，仅新端组合承接新策略；不静默回退或重试未知业务。控制worker/固定payload成本纳入原资源窗口及完整交付成本。未来正式清单加入新增根/叶与依赖源清单、当前准确语义，旧manifest/run/summary/Accepted源不重写。

当前固定Render已完成Windows与Linux metrics定向unit/race；六源G2夹具6ac的Linux开发检查已收回，完整事实见[原结果](six-source-linux.json)，整体exit1保持。两项检查不能替代新活性实现的全产品复证。alpha/main、用户主机部署和最终release门槛均按原授权/前置继续，不由本计划直接晋级。

正式采集的[嵌套模块验证适配计划](liveness-evidence-adapter.md)补齐独立测试、源清单、精确覆盖分母和上游漏洞版本映射；实施前先固定本计划，旧运行及其分析器保持可复算。

## 依赖、控制状态与OIS开发检查点

[来源与实测记录](liveness-control-local.json)固定原上游64文件与本地最小控制写扩展，三个自有生产文件共12条语句仍在分母。原生完整嵌套模块281节点PASS、2原条件SKIP，自有原生11/11覆盖；JS实际4节点通过，但Windows自动覆盖输出失败，不能称12/12自动覆盖。原生负对照恢复旧5s包装时真实失败。正式步骤必须显式运行嵌套模块并纳入源码、漏洞关联和补丁覆盖，根go test不会自动遍历它。

wsio按先行计划实现唯一worker、固定槽位/公平、nonce退役与写owner分离、半开固定窗口和两个DATA owner，完整Windows包191节点通过、276/276语句覆盖。OIS在原Attach时刻启探测；来源与目的DATA owner分别覆盖转发等待和enqueue至finish，原lastData只在原合法DATA接受点更新。Close/CloseNow才Stop，reader结束后在Hub锁外沿原forceAt join；注入时钟的剩余量锚定在锁前实际单调时刻，不从建ctx时续期。

OIS两包266节点PASS、10原Windows条件SKIP，2534/2654语句覆盖，vet通过。新增回归覆盖双向owner释放、阻塞控制写与Completed尾部FIN排空，以及后到真实控制错误不替换早已确定的Completed；旧5ms实时unit烟测改为50ms并等待实际probe，精确窗口断言由可控单调钟承担，产品配置与集成判线未改。

## OIC与客户端接线检查点

[端点开发记录](liveness-endpoints-local.json)保存11份源文件、独立只读审阅与原始运行指纹。ManagedConn在Dial前安装借用载荷回调，握手后启动同一控制worker；两个端点不主动探测。真实控制写错误先锁存，至多启动一次短期关闭协调者；它先释放WebSocket传输，再通知原Bridge结束本地阻塞读写，worker不等待自身。GotConn只绑定本次升级连接；实际强关时先关闭对应原生TCP，包括TLS.NetConn，解除已经阻塞的close_notify。

最早清理期限在首次终态或取消时保存；Bridge强关定时器在ABORT前建立，Forward等待实际获胜的关闭回调结束。OIC的三条READY早退和正常Run返回，以及Forward外层，在唯一原Reader返回后才补原期限内的drain/join。Stream的ABORT保留原2s上限并取首次截止的较早者，随后非Completed drain不重新计时。正常Completed保持原尾部排空；保存join截止不额外触发强关，超原D仍未退出的owner如实报告清理错误。

Windows四受影响包完整单测438节点唯一PASS，无FAIL/SKIP，vet通过；新增11根20子项全部实际运行。包内覆盖为client640/665、tunnel215/227、OIC relay1152/1239、wsio398/423，只作开发明细，不声称完整产品95%通过。旧Stream续窗负对照实际失败，新接线通过；原Forward回调未join及不成立的TLS活动Write负例也保留。正确TLS负例限定为空闲关闭自身的close_notify写受阻，不能把它推广为所有活动TLS写都使原Close卡住。Linux/race、旧版矩阵与完整产品同源检查仍待。

## 固定基础的Linux结果与限制

[183b基础开发检查](liveness-foundation-linux.json)已执行一次并完整回读：root三包unit/race各467项PASS、无skip，合并覆盖2816/2930；这不包含后续49b9端点接线。821份Git源码、59回收原件、runner及7个child的实际身份和空进程组均核对，原共享组D及2700s父预算没有续期。

嵌套unit虽然281具名项PASS、2原条件SKIP，包级TestMain仍因DNS后台协程未退而FAIL。原上游badTLS用例访问不存在的公网域名，原5s上下文返回后DefaultTransport/Resolver仍有后台工作；不把它误归为新增控制worker，也不能据此忽略泄漏检查。嵌套race在原Go120s开发watchdog到时仍有四个原mask循环运行，46项PASS、2原SKIP、215项已开始但未终结；未观察到DATA RACE不等于race套件通过。native自有11/11覆盖来自这个失败unit的原profile，JS1条未测，嵌套vet通过。

保留全部失败，不减mask的9999次循环、不修改64份原上游、不增加产品期限。前已固定的正式适配计划使用Go原默认watchdog，不能反向改变这次开发失败。随后[完整BadDials根窄检查](resolver-local.json)以单child解析器环境取得9节点及原TestMain包终结通过，原5s和120s保持；整模块及race尚未通过，不把该样本解释为旧失败的唯一原因。[完整套件环境接入](resolver-environment.md)已先行登记。PG同轮补充的只读日志仅见原写超时后的EOF/checkpoint，不能定位此前500ms消耗于排队、SQL、锁或I/O，StoreUnavailable原失败保持。

## 完整Linux检查中的关闭判据与质量补证计划

固定`2a81a22424ff3e717d923c1532062b5826c6aa9d`的[16步完整开发检查](liveness-complete-linux.md)正在单次执行；以下仅为已完成unit的中间事实，不是最终回收或正式run。该步骤完整结束，原child退出1、无超时、源码前后相等且进程组为空；1915个具名节点PASS，唯一失败子叶为`TestManagedDataTLSBlockedClose/managed_close_bounded`，父项随之FAIL。原stdout共1881235字节、SHA256 `1db95cec401ac592c3e75b750c7d3c93ab7624b225fd5b42a8c9ec187e8822c9`；第7218行在约0.11秒报告`failed to immediately close WebSocket: use of closed network connection`。该Fatal发生在后续原D/Join/Reader断言之前，不能据此宣称那些检查通过或清理超时。原完整根profile共384517字节、SHA256 `8b3447c6ba7f869c99fefcd615b2dcabe93f8550dde20232aa2538cd3b4e6e50`，7707/8119=94.92548%，尚未合并依赖自有12句，不能以旧局部覆盖替代95%。

独立源码复核确认：ManagedConn在关闭本次TCP后，原Read错误路径也会进入CloseNow；固定上游的重复关闭分支在等待内部goroutine完成后允许返回`net.ErrClosed`。当前原件不能确定具体关闭赢家。先修正该测试仅允许nil的断言，接受nil或可识别的已关闭终态；继续独立要求JoinCleanup严格无错误、控制worker已退出、原Reader在原截止内返回，且不得续期。新增同一原D内两次真实关闭的确定性回归，保留现有已关闭错误与DeadlineExceeded同时出现时仍失败、后续Join仍保留截止错误的负例。不归一化产品API、不修改64份上游文件，也不将一个`errors.Is(net.ErrClosed)`判断当作完整清理成功。

质量补证按实际未覆盖的行为选择：控制失败通知的业务回调尚在执行时，外层Join须等待其真实完成，原截止到期须保留失败；正常WebSocket关闭被真实传输阻塞时，原强关定时器须释放本次socket并完成其join。采用现有真实连接、可控阻塞与明确开始/完成见证，不增加产品hook、不复制已经由跨包场景覆盖的路径来堆数量。若新测试暴露实现缺陷，先保留失败与具体证据，再在本节既有生命周期契约内修复；不为提高数字过滤源码、改变覆盖命令、替换完整分母或降低95%。后续真实完整unit/race、完整依赖与业务结果仍须同一新固定来源重新验证。

本补充在修改测试/实现前提交并回读。正在运行的2a81归档、原D、16个步骤及所有失败保持，后续步骤继续独立收集；新工作区提交不改变客体固定来源，不重跑或拼接本次失败样本。
