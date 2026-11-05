# 数据腿活性：有界异步控制写与空闲观测窗口

状态：**DISC-010-R1 Pending；人类尚未采用，不是实施授权。** 这是对B1/B4数据腿心跳条款的完整候选替换；旧Accepted文本与指纹保持。 本稿只收敛数据腿心跳策略，不引入credit、新业务帧或独立控制传输。源机制依据固定 `eb64801ab74346b6541f58b6a6941f88233d2155`、coder/websocket v1.8.15及本轮完整Load报告；本规格不代表源码已实现或实测通过；讨论与发布状态见[DISC-010](../../changes/DISC-010.md)。

## 用户场景、证据与范围

消费者经OI选择获准Exposure后，原生SSH连接通过OIS中继到OIC，再到既有SSH/Bash服务。SSH仍负责业务认证、命令和退出码，OI只转发；本选择不新建远程shell执行器。大响应或接收端暂时变慢时，已有一F有界DATA转发可以占住同一WebSocket写锁；仍在读另一方向的端点必须能处理控制帧，且未知执行结果不得重放。

固定`eb64801ab74346b6541f58b6a6941f88233d2155`开发运行的164条Load工作流有161成功、3失败，失败均记录OIC winning `leg_read/deadline/context_active/read_phase=pong_write_lock`。这证明失败处是同步自动PONG等锁；没有记录确切锁持有者、连续5s socket Write或根因归属CPU，不能将候选当成已修复。详见[TASK-007固定诊断](../../../spec/TASK-007-p2-g2-readiness/local-linux-diagnostics-second.json)。G1/G2仍未通过。

正常路径：双方合法DATA继续沿原有界转发；自动PONG由唯一控制worker尽快写回，Reader不再同步等待写锁。失败路径：真实读写错误立即保持原首终态；无DATA证据的两个空闲窗口、原IDLE、授权或撤销照常终止并关闭transport、回收worker。正常数据持续流动不能证明业务命令成功。

<!-- sdd-scope:data-leg-liveness -->
## 推荐与原新差异

推荐把以下两项作为不可拆开的选择：**OI数据腿采用连接级有界异步PING/PONG写；OIS采用两个连续空闲观测窗口的活性判定。** 不再把库内部自动PONG的同步等锁5s当成业务Reader失败，也不把探测等待窗口到期当成取消真实socket控制写的理由。

这明确替换B1 §5/B4 §3原“每PING发ping、连续两次未收到pong断开”的适用定义：合法DATA进展或真实DATA背压期间不启动新探测、不积累空闲miss；连续两个没有这些life证据且没有当前有效PONG的观察窗口终止整腿，**即使实际只发出一条PING、PING还在等帧锁或正在写，也可终止**。不能把新规则描述为保持两次实际PING未答的旧语义。

保留OI DATA/FIN/ABORT字节、READY准入、每方向一F应用缓冲、未知结果不重放、原许可/撤销、原IDLE时点及原清理总截止。PING配置值和miss阈值2不新增数值；但固定节拍、忙时免测与空闲窗口起点是行为变化，应连同依赖扩展维护成本一次确认。

## 时间与窗口的精确定义

设 `P` 为既有配置PING间隔，`t0` 为该条数据腿开始现有生命周期时保存的单调时刻。首tick仍在 `t0+P`，此后逻辑tick固定为 `T_k=t0+kP`，不采用“本次操作结束再等待P”重起节拍。第一个观察窗口为 `[T_1,T_2)`；`[t0,T_1)`只收集life供首tick选择，不计miss。只有实际存在的attached且未终止腿参与，Preparing期间既有prepare期限、READY与其他首终因仍优先，进入Open不重起此时钟。

每窗按半开区间处理。状态归约时在同一局部协调点取真实单调时间，先结算已到截止的窗口，再接纳当前事件；恰在截止才被归约的PONG/活动属于下一窗口，不能回填刚结束的一窗。真实回包入口与归约时点可以分别诊断，不把它们写成同一时刻。调用被调度迟延时不重起P，也不补发历史PING；依原窗口和已记录状态推进，miss达到2即终止。常数状态与算术跳过空窗即可，不能为长暂停生成无界tick队列。

初始无DATA、能运行的完全空闲腿，在T1启动首探测、T2结算第一次miss、T3结算第二次miss并决定终止。这是最早的**逻辑**路径，不承诺线程在该纳秒执行，也不等同旧循环实际墙钟时间。终止后的原清理D从真实首终态起点计算，不能从join或后续记录重新计时。

每个当前窗口只保存：`life_seen`、`valid_pong_seen`、窗口序号/截止，以及 `idle_misses`（0..2）。在窗口任何时刻出现合格life即置 `life_seen=true` 并将miss复位0；窗口开始时busy已真也置life。不能仅在tick瞬间检查布尔，否则busy在两个tick之间开始并结束会漏计。

窗口结束依次判定：

1. 原许可/撤销/IDLE/关闭等已经赢得终态：保持该原因，执行终止流程，不再探测。
2. `life_seen`或本窗`valid_pong_seen`为真：miss=0。
3. 否则miss加1；到2时按既有数据腿中断映射终止，不需要伪造第二条PING已上wire。
4. 无论本窗成功或失败，退役本窗nonce。进入下一窗后，只有当前不busy、刚结束窗没有合格life且旧PING wire-owner已释放，才可启动一个新探测；不因旧Write刚返回就在窗中途额外起新探测。

“刚结束窗有life则本tick不新探测”提供明确的无额外参数空闲选择：活动停止后先有一窗确实没有life，才在下一tick尝试探测。该无life窗即使未发PING也按上述观察窗口计算miss，这是新合同的组成部分，不宣称每一个miss都对应已发送PING。

## 哪些事实算life，哪些不算

同一数据腿的合格life仅来自合法DATA：

- 本腿完整收到并通过状态/格式检查、由原通道归约接受的DATA；或本腿一条DATA真实Write成功返回。只统计DATA，不把READY、FIN、PING/PONG、Close、错误返回或无效帧计为DATA进展。
- 本腿作为来源，当前一条已接受DATA正在等待另一腿实际转发完成；或本腿作为目的地，一条合法DATA已进入其有界队列/正在真实Write。它们必须绑定具体唯一outMsg/owner，且完成、丢弃或终止只释放一次，不能用无主的永久bool。

`leg.forwarding`只覆盖第一类转发等待，不能代替本腿出站DATA pending。实现应在enqueue成为可见工作之前发布pending，在finish之后释放，覆盖enqueue→claim及Write返回→释放的整个生命周期；现有queue容量不变，不新增DATA槽。busy从0→非0也置本窗life，busy持续跨tick每窗都算life；pending还没完成不等于DATA已经交付。

控制写自身pending/writing、连接存在、探测等待、异步worker存活都不算busy/life，否则卡住的PING会永远阻止空闲终止。普通应用层“开始写目标”也不自动计DATA完成；只沿上述既有已接受DATA及其真实owner记录。

IDLE仍只使用原 `channel.lastData` 更新点：READY提交的原初始化、OIS接受合法DATA时的原更新。新liveness状态、busy、控制帧、探测成功或失败都不得更新lastData。转发等待可免空闲miss，但没有后续原DATA时点更新就仍会在原IDLE结束。相反方向合法DATA原本可更新整个channel的IDLE；本选择不新增每方向IDLE，也不声称每个停滞方向都有独立短时检测保证。

## probe与wire-owner的两个生命周期

仅OIS主动发起腿探测；每连接至多1个未退役probe。建议使用固定8B连接内单调计数nonce（从1开始，连接身份隔离，不回绕；耗尽则安全终止），不是每次分配map条目。每次nonce只对应一个窗口和一条连接，窗口不跨越续期。

probe观察状态为 `none → queued → claimed/writing → awaiting_pong → retired`；wire-owner独立记录这一次控制写是否已经被worker claim及是否真实返回。二者不能共用一个“done”布尔：

- 排队未claim时窗口到期：在同一协调状态下移除待发PING并退役nonce；worker以后不能再取到它。
- worker已claim（包括等帧锁，尚未进入socket Write）或正在socket Write时到期：只退役观察nonce，不取消它的连接ctx，不覆盖in-flight payload，不释放wire-owner。该原Write仍可随后成功或失败，但不能再给旧窗口补成功。
- 旧wire-owner未返回：不再排新PING，不另建worker；下一空闲窗口照常结算miss，达到2就终止整腿。于是“只一条PING/还在等锁”路径有明确出口，不会为了凑第二条实际PING无限等。
- 旧Write返回成功且已退役：释放owner即可，不复活nonce、不清miss。若返回真实错误：立即按原首终态规则终止整腿；若已有终态则不得反改。
- 发送成功后进入busy：本窗life复位miss；当前nonce仍仅活到原窗结束。可以记录窗内有效PONG，但它不改DATA/IDLE事实；窗末照常退役，下一个忙tick不启动新探测。

有效PONG必须在当前窗口截止前被归约，来自同一实际连接，payload精确匹配当前未退役nonce，且对应PING至少已经被worker claim。queued未claim、错误nonce、重复PONG、其他连接、已退役nonce均不能兑现成功。PONG可能在PING Write返回之前已到达，不能要求先看到Write成功返回才能接纳；它只置valid_pong_seen并清空闲miss，绝不置DATA的life_seen，不抑制下一tick正常新探测，也不伪记本地Write已返回或完整写出。nonce用于关联和排除迟到ABA，不是对peer诚实性的密码学证明。所有回包和计时只在有限协调状态中处理，不保存无限nonce墓碑。

## 异步控制写与资源上界

固定coder v1.8.15没有公开Pong/WriteControl；OnPingReceived返回false只抑制同步自动回应，无法自己安全补PONG。因此推荐一个固定、可审计的小依赖扩展，仅对OI数据腿显式启用；库默认行为与OI控制会话保持，不擅自patch运行机module cache。具体选择为仓库内固定的上游v1.8.15副本及最小补丁，以go.mod本地replace引用，保留原模块身份、上游版本/hash、许可证及补丁清单；不发布或依赖一个可移动的远端分支。扩展提供有限PING/PONG写入口，复用库内部writeFrame及其帧锁、mask/编码、错误与关闭路径，并验证opcode与125B上界；该入口专门不经过旧writeControl内部无条件WithTimeout(ctx,5s)包装，连接ctx才真正决定此模式的写生命周期。旧writeControl和其他调用保留原5s默认。不允许任意opcode/raw socket写。OI内部显式安装原OnPingReceived/OnPongReceived回调并管理上述生命周期；其他库调用默认不变。上游升级、离线源包、完整源码/补丁回读、静态与漏洞扫描（含本地替换源码及上游版本关联）由项目维护；不能把本地replace视为免扫描。

入站PING仍由唯一Reader完整验证，复制最多125B payload后放入固定pending PONG槽，立即返回，既不持锁做网络IO也不把该Reader的内部5s ctx交给后台。尚未claim的多个PING只保留最新待回应payload；已经claim的PONG不可覆盖，必须immutable。依[RFC 6455 §5.5.2–§5.5.3](https://www.rfc-editor.org/rfc/rfc6455.html#section-5.5.3)，响应应尽快实用可行地发出、回显对应载荷；尚未响应的多个PING可只回应最近处理者。不宣称每个PING均产生一个PONG。该规则不允许跳过格式验证，也不保证已被底层DATA占用的TCP可被抢占。

每条数据WebSocket只新增**一个**连接级控制write worker。资源封顶：pending PONG≤125B、in-flight PONG≤125B；一个8B本地PING nonce及有界元数据；至多1个active probe，且旧probe被退役但wire未回收时禁止新probe。一个worker同一时刻最多一个control Write；pending PING与pending PONG同时可发时交替选择，禁止某类持续到来把另一类永久饿死，不抢占已经开始的frame。控制payload上界为258B/连接（125+125+8；库本身的既有编码/TLS缓冲另计），应用DATA仍每方向一F。OIC/client只需PONG路径，不额外主动启动probe。

真实PING/PONG帧写在库内部继续使用同一帧锁、mask/编码、错误和关闭规则，但ctx来自连接生命周期，不带单次观察窗口/原自动PONG5s的写期限。原控制payload读取限长/验证/读取失败保持；不是吞Read错误，也不是把所有控制操作改成无限期限。该更换明确取消“存活连接上的控制写仅因一个观察窗口到期而被取消”的路径；如果连接合法终止，ctx取消导致active control Write结束/关闭transport是允许且必须的。

业务首终态与transport真正关闭点分开：先保存原首终态，Completed仍走原DATA/FIN drain、close-after-queue及既有forceAt，不得因新增worker提前CloseNow截断正常尾部。原中断路径同样保留ABORT/关闭的既有次序。只有原流程实际决定关闭该transport时，禁止新的控制enqueue、清空未claim控制待办，再按原机制CloseNow/取消连接ctx解除等锁或socket Write，并join唯一control owner；不额外等待一个控制响应才关闭。不得先join再Close，也不得让worker同步join自己；worker只报告有限错误并退出，协调者在锁外关闭/join。仍沿从原首终态计算的T_close、既有forceAt及原集成资源总D，不因drain、worker、超时通知、释放门闩、诊断或第二次关闭重起预算。FIN/ABORT顺序、原许可复核、Completed优先级和未知结果处理不变。

## 典型时序及边界

| 场景 | 规定结果 |
| --- | --- |
| T1空闲，probe A写出，T2前收到A的PONG | 本窗成功、miss=0；T2退役A，可为新窗创建B；A的任何重复回包不能满足B。 |
| A等帧锁跨T2，窗内无DATA/busy | T2 miss=1且退役A，但保留A的wire-owner；新窗无新PING；T3仍无life则miss=2，先终止后Close/取消/join。 |
| A已写出，窗内开始真实DATA forwarding，之后在T2前结束 | 即使tick时busy已经false，该窗life_seen仍真、miss复位；T2不启动新probe。IDLE只看原DATA时点。 |
| A窗口到期后其Write才返回，接着收到A的PONG | 只回收旧owner；迟到PONG不清miss，不补写旧成功。下一probe只可在符合条件的下一tick发起。 |
| 观察结束前出现真实控制Write错误 | 直接走原中断/首终态，不等待第二窗口；不能把真实write错误降格为一个可忽略miss。 |
| PONG worker被DATA帧锁卡住，Reader又收到PING | 更新至多一个pending最新payload；没有新的worker/无限队列，Reader无需同步等PONG。 |
| 一直busy但无新的合法DATA时点 | 心跳miss可复位，但IDLE照常到期并强制退出；busy不能给IDLE续期。 |
| 同tick先到IDLE/撤销与PONG | 按既有终态协调点判定，先终态不可被PONG恢复；到期事件不回填到早先窗口。 |
| 调度恢复已越两窗 | 不补发两条PING冒充真实尝试，不把当前回包追认旧窗；按原窗口已有life/nonce状态结算，保持原许可/IDLE优先。 |

此策略消除“正常Reader必须同步等本连接PONG写完”的依赖，不保证底层DATA/控制Write永远可运行，也不保证任意操作系统调度停顿下不断流。它保留真实网络错误、许可、撤销、IDLE和空闲窗口引发的安全终止。

## 兼容与必须确认的差异

WS及OI v1字节不变，正常读写、FIN/ABORT、错误码和原鉴权仍可互通；不新增业务重试或协议fallback。完整新背压行为只对相关数据腿均使用新实现的OIS/OIC/client成立。旧OIC/client仍可能同步自动PONG等锁5s失败；旧OIS仍按其原发送/超时行为结束；新端延后PONG也可能被旧严格端断开。混合版本保证范围是可安全结束且无重放，不是相同存活能力。当前没有新增能力协商，本选择不擅自要求或承诺一个新wire版本。

人类确认应明确覆盖：

1. 将“每PING/两次实际未答”改为本稿固定节拍及两个空闲观察窗口，包含未写出/退役旧Write仍可被终止的路径。
2. 合法DATA进展和真实有界DATA pending/forwarding计life，忙时不启动新探测，但不续原IDLE/许可/撤销。
3. 控制写由连接生命周期管理，观察到期只退役nonce；原同步PONG5s写等待不再作为新模式Reader的失败边界。
4. 接受小依赖扩展维护与每数据连接固定worker/最多258B控制payload上界，且只保证新实现的完整行为；旧v1仅wire兼容及安全失败。

保留旧规则是明确替代：不采用以上变化，接受现有同流背压可触发中断，至多研究帧间公平/分片这种不改变合同的实现优化，不能承诺任意背压下持续存活。本稿推荐新策略，是因为它直接解除已实证的同步PONG锁依赖并保持有界背压；推荐不等于用户采用或负载已通过。

## 必须预注册的确定性反例与复证

以下是同一策略的实施验收要求，不是已运行结果：

- 窗口边界：D−1ns有效PONG、D时/之后拒绝；错误/旧/重复/跨连接nonce拒绝；已退役write返回不能复活；正确PONG可先于Write返回。
- busy覆盖：source forwarding、destination queued/write分别命中；enqueue→claim无漏窗、完成只释放一次；忙在两tick间出现也复位；控制worker/queued PING不能自称busy。
- 两空窗：PING尚未上wire、等锁、真实Write三态分别验证只一个owner且第二空窗结束；没有伪造第二次发送、不增worker、不重起D。
- 忙闲交错：发出PING后开始DATA、busy后迅速空闲、发送返回后进入busy；按life_seen与固定tick裁决，IDLE原D不动。
- 实际WS：握手后Write入口门闩持DATA；单Reader收到多个真实PING仍可继续；pending/in-flight payload不相互污染；释放后原DATA及双向DATA完整、最新待回应PONG真实发出。
- 真实PING/PONG socket Write挂住：观察到期不提前取消存活连接；空闲两窗、IDLE、撤销或许可实际终态后原清理总D内Close/join；早ABORT不被晚worker错误改写。
- 不合法控制帧/超长/读取错误、真实write错误仍fail closed；FIN完成与许可竞态保持；不得把截断伪EOF/Completed或自动重放。
- 固定旧二进制混合矩阵记录真实正常/错误路径，不要求旧端满足新life承诺；全部新端以原DATA/半关闭/撤销/资源标准证明无回归。

实现后需用新固定来源复验受影响G1/G3/G4/G2：完整unit/race/coverage/静态与漏洞检查，真实两后端行为、半关闭/黑洞/背压/取消/原资源窗口、恢复旧腿不可复活、准入及旧版本矩阵；Load继续记录所有成功与失败，不许靠改判线遮盖。B1/B4、实现文档、依赖供应/离线来源及新预注册同时更新；旧Accepted/run/归档分析器不改。G5/G7仍依其前置，不从新策略外推目标主机容量或最终部署成功。

<!-- /sdd-scope:data-leg-liveness -->
