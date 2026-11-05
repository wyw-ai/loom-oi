# 部分响应中断的首终态诊断（编码前固定）

2026-11-05；Preparation。原 d9 G1 尚在执行，本稿仅从既有原件与固定源码制定下一步取证，不修改原run、不认定唯一根因。本文对应实现尚未开始；已完成独立源码复核，规格提交和远端回读后才编码。所属任务见 [TASK-007](README.md)。

## 问题与保持范围

两后端TV-B4-06末组32×64MiB中，SQLite四条、PostgreSQL两条流已发送完整15B请求和FIN、收到部分响应后ChannelInterrupted。OIC真实终态remote=false，但leg Read/Write、本地目标Read/Write等多条路径均可产生同码。现有D19仅首I/O边界，不包含最后失败操作；目标后续reset可能是终止清理的后果。原资源12次清理均满足采样和时间要求，但两叶Go FAIL仍令独立资源判定失败，不能抵消截断。

固定coder/websocket v1.8.15的Ping可在已取写锁后的control frame写入期间因ctx到期关闭共享连接；只等待pong或等待写锁超时没有同样含义。OIS现先Ping再看activity/forwarding，因而这是值得区分的源码候选，原件未证明本次走过此路径。不得直接写为CPU、TCG、PING、目标或数据库的唯一故障。

本次仅增加有界本地诊断，不改变任何线协议/API、产品状态转移、权限、错误码、PING/IDLE、许可、背压/缓冲、调度/重试、已有Go向量负载或其时限；不新增载荷、凭证、票据、任意error.Error内容。原正式run全部完成并封口之前，不跑新Go/guest检查、不改运行中Python源。

## OIC/端点Bridge的winning终态

扩展既有Diagnostics的单个terminal字段（固定对象，不是数组），随原channel ended/io_boundaries记录输出，不增加sink调用或worker。记录必须在原sync.Once winning闭包内、任何ABORT发送/socket关闭之前形成，不能拿较早失败但未赢的goroutine覆盖真正首终因；后来的清理错误不覆盖。必须先执行原Completed→allow复核，再冻结实际code/remote；若复核改为许可拒绝，operation=license_check，不为诊断再次调用allow。

terminal固定字段：at_ns（与既有StartedAt相同单调原点，正数）、operation、error_class、context_state、code、remote、fin_send_state、fin_received、bytes_up、bytes_down。code/remote与实际首终态Result完全相等；bytes是该决策时atomic计数，不要求等于稍后排空完成的Result计数。fin_send_state为not_started/writing/written/failed，context_state仅active/canceled/deadline；不得为诊断额外调用allow()或改其结果优先级。

operation为有限枚举：external_abort、wait_ready_read、wait_ready_frame、leg_read、leg_frame、remote_abort、target_write、target_read、leg_write、fin_write、license_check、fin_complete。每个实际finish入口使用准确分支名称；合法Completed、主动Abort及协议错误同样有真实终态，不能全部兜底为I/O错误。只对有真实err的入口分类；成功或纯协议决定error_class=none。

error_class按errors.Is/As及websocket.CloseStatus分类：none、deadline、canceled、eof、closed、timeout、websocket_close、other。优先级按此顺序固定；包装错误保留同类别，不输出错误字符串或CloseReason。若需要close status，只记录由CloseStatus返回的有限有效数值/未取得0，不将任意用户close文本加入记录。新增诊断状态只通过不可变对象/atomic发布，Diagnostics在运行和结束时并发调用无data race；未终止terminal=null。

OIC provide已创建Bridge但bind因先前abort而拒绝的分支，原ch.bridge仍为空。done必须接收本次provide实际Bridge的快照，正常绑定和bind拒绝均准确归属；不得为日志把拒绝对象写入ch.bridge、改bind/取消结果或在Agent.mu内Abort。未曾创建Bridge的失败仍保持null，不补造终态。

原finish保持现有控制动作及次序，必要时薄封装转到同一once实现，不能为了日志改变首次错误选择。正式终态时间代表本进程决定点，不虚称底层网络首次失效时刻。

## OIS两条腿的错误与关闭来源

每条leg新增固定容量诊断对象：分别最多一个Read错误、Write错误、Ping错误，以及第一次显式CloseNow调用的来源；成功路径不积累每帧记录。使用不可变对象atomic保存各操作首次错误，记录的先后只是各本地操作观测，不推断跨进程原因。附到现有closing/closed日志的两条腿字段；另允许每条leg恰一次最终leg ended记录，用原Read/Write/Ping三个worker的退出计数在最后退出者中同步发射，以取得终态日志之后才返回的错误。最多每channel两条，无新增worker、每帧或每次Ping日志。不得等待诊断后才关闭通道，也不把logger的耗时从实测中扣除。

每个错误对象固定started_ns/completed_ns（同一leg创建时time.Now单调原点）、error_class/context_state、activity_before/activity_after、forwarding_before/forwarding_after；实际调用之前/返回之后紧贴采样。Read/Write错误仅在出现错误时保存，但为取得实际区间每次调用需本地time.Now，不能改ctx/超时/消息或等待。Write额外message_type仅READY/DATA/FIN/ABORT/unknown，不保留frame内容/长度。Ping记录原misses_before/misses_after、原判定使用的activity/forwarding、ping_phase，必须复用原逻辑的实际读数，不为了记录重复Load之后冒称同一次决策。context_state紧贴返回且在原cancel()之前保存，避免把主动清理误记请求超时。原短路分支未读取forwarding时decision_forwarding为null；不得额外Load冒称原判断已经读取。

Ping的error阶段仅诊断固定依赖：write_lock/control_write/pong_wait/connection_closed/unknown。使用固定v1.8.15的错误包装链识别（例如failed to acquire lock、failed to write control frame、failed to wait for pong），只输出有限枚举，绝不输出原Error文本。优先写锁归write_lock；未知包装保持unknown而不猜测。只匹配固定外层包装层级，不能对子层恶意Error作任意substring搜索；control_write只表示已过写锁的写阶段，该库在setupWriteTimeout之前还会检查closed，分类本身不证明进入了socket.Write或由Ping关闭。若本地conn适配器可提供类型化阶段，优先用窄的可选接口；不得把此诊断分类用于关闭或产品判定。真实库可控门闩测试要证明阶段分类与发生位置对应，合成字符串测试本身不证明真实库行为。

first_close={at_ns,source}只记录本实现显式调用CloseNow的第一个入口，source为有限枚举write_error、ping_misses、queue_full、reader_cleanup、force_close、shutdown、other_owned。不宣称捕获了websocket库内部ctx关闭；未经过本实现CloseNow时保持null，不能用reader_cleanup冒认首个底层关闭。所有现有本leg关闭入口须按实际调用位置记录，再执行原CloseNow；已存在的TCP/TLS close握手行为不改。

OIS诊断保存至少leg创建原点、role、ChannelID和实际有限操作对象。closing日志时并发操作可能尚未返回，必须保留null；closed日志可有后续本地事实，不倒写closing、不要求两者相同。reader_cleanup发生在现有finalize之后时，不把它提前伪记为已调用；最终leg ended在原三worker均退出之后输出实际有限快照，作为后续事实。若首终态前错误尚未返回，不能为了等诊断延迟终态；本轮下一个开发样本需同时收原closing/closed、最终leg ended和OIC记录。三个worker计数在启动前初始化为3；最终日志必须先于各worker原conns/loops.Done，不增加worker或waiter。snapshot只取不可变identity及atomic对象，不回取Hub.mu，不能引入关闭锁等待。诊断不新增管理接口或指标标签。

## 回归与下一次区分性观测

新增测试须有区分度，不只镜像字段：

- 两个受控并发finish入口，先win后另一错误完成：终态的操作/原错误/时间保持，主动Abort/许可优先级和正常Completed无行为变化；Diagnostics运行中并发读取，未终态为null。
- 错误包装deadline/canceled/eof/closed/timeout/CloseStatus/未知归有限类，恶意含凭证/载荷的Error或close reason不可出现在诊断JSON。
- Read/Write真实失败、Ping两次无生命的原关闭与活跃/背压仍使用原判定；固定操作容量不随重复失败增长。
- 真实coder/websocket受控socket写门闩：Ping已进入写阶段再触发原ctx截止，确认连接关闭和control_write分类；另独立等pong超时仍能后续发送数据、等写锁超时不关闭已健康连接。不能靠固定sleep猜进入阶段，不能修改OS时间或依赖包。
- 原首次I/O边界、缓冲/FIN、Close/清理及全部已采用产品断言保持。新增根/子项要进入下一四manifest真实库存，完整覆盖率分母包含新增产品诊断代码。

修后先聚焦本地/隔离Linux回归及一组新身份的原两后端Load开发观测，保存完整三服务日志/Go raw/配置/源绑定；不盲重复直到通过。若新样本仍中断，用同Channel/tuple的OIC winning终态及OIS具体错误/关闭入口区分候选；若不再出现，只能记本次未复现，不能认定已修好。生产修复须有确定性反例、先固定具体修复语义，再重新绑定最终完整G1/G2/G3/G4。原d9失败和旧D19/D20反例永久保留。
