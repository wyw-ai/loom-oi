# WebSocket 读取失败的有限阶段诊断

2026-11-05，编码前固定。沿用首终态诊断的内部开发范围；本页不改变已采用 B1/B4、控制许可、错误码或验收标准。

## 输入事实与目标

固定 `7da7159a339986659b18842350ae1edc242e958a` 的一次 Linux 开发 Load 中，SQLite 最后一格 32×64MiB 有四条中断，PostgreSQL 的82条流均成功。四条失败通过 channel 和目标 TCP tuple 联接到 OIC 首终态：`operation=leg_read`、`error_class=deadline`、`context_state=active`、`remote=false`。首 target read/write 和收到请求 FIN 后的 CloseWrite 已成功，目标最终 reset 发生在本地终态清理之后，不能反向作为目标先断的证据。

固定 coder/websocket v1.8.15 的控制读取含独立5s上下文：读取控制 payload，收到 PING 后等待共享帧写锁并写 PONG。这些分支均可能使外层仍 active 的 Read 返回 deadline。现有记录没有包装阶段，不能选定唯一根因。OIS早期 `pong_wait` 失败和活动增长也不能代替 OIC 首终态；同轮已 Completed 的通道随后出现 `control_write/closed`，说明标签本身不证明业务失败的原因。

下一步只区分错误已经经过的固定库阶段，保留原错误与状态决定。冻结 B1 §5 和 B4 的每 PING/连续两次无 pong 条款保持；历史 TASK-004 的活动/背压存活解释不自动成为新豁免。不得据本页跳过 Ping、延长期限、吞掉 Read 错误、重放业务或把 DATA 当永久 pong。

## 有限字段与判定边界

新增纯函数 `wsio.ReadPhase(error) string`。输出只允许下表六个值，不输出错误全文、payload、地址、凭据或对端 Close reason。只在真实失败返回后调用，不在健康逐帧路径新增分配、日志或网络操作。

先严格消费**恰一个** `failed to get reader: ` 或 `failed to read: ` 外层。前者是 `Conn.Reader` 的错误，后者是消息体读取/后续 fragment 的错误；`Conn.Read` 和 `io.ReadAll` 不再添加外层。随后仅按以下固定层序分类：

| 外层之后的实际 Unwrap 链 | 输出 | 局限 |
| --- | --- | --- |
| handle control frame opPing → write control frame opPong → acquire lock | `pong_write_lock` | PONG未取得帧写锁的失败分支；不证明同时存在持续DATA进度 |
| handle control frame opPing → write control frame opPong → write frame | `pong_control_write` | 已经过取锁后的包装；可能是closed前置检查，不证明socket Write或超时回调已执行 |
| handle control frame opPing/opPong/opClose → read frame payload | `control_payload` | 控制payload读取包装，不证明具体底层原因 |
| handle control frame opPing/opPong/opClose → 直接等于 `context.DeadlineExceeded`、`context.Canceled` 或 `net.ErrClosed` | `control_payload` | 固定库setup/payload返回分支；finishRead可能覆盖内部包装，不能声称payload Read实际开始 |
| read frame header | `frame_header` | 仅明确保留下来的header包装 |
| read frame payload | `frame_payload` | 仅明确保留下来的body payload包装 |
| 其余，包括Read外层后直接deadline/closed或不完整链 | `unknown` | 不按错误类别推断隐藏阶段 |

每层须存在真实 `errors.Unwrap`，且外层 Error 文本恰等于固定前缀加内层 Error；控制opcode和层序必须精确。只比较已知包装，不递归搜索任意链、不用substring/正则查叶文本、不遍历Join。额外层负例针对必需层序之间；已确定write frame阶段后，允许库实际flush/header等子包装，不继续猜测底层操作。直接sentinel使用对象相等，只有相同文字的错误不算。公开error接口无法鉴别完全伪造的fmt包装，因此该函数只服务固定真实调用点的诊断，不承担认证或产品判断。

`finishRead` 会在上下文到期或连接关闭时覆盖 header/payload 的内部错误；只有Read外层加裸deadline不能区分首帧、continuation header或body payload，必须保持unknown。PONG写必经自己的writeControl包装，不能把缺失该包装的错误猜成PONG写。

`tunnel.TerminalDiagnostic` 增加可省略的 `read_phase`，仅winning `operation=leg_read` 从原cause计算。保留once、最终许可优先和清理顺序，其他终态不填。OIS `legOperationDiagnostic` 同字段仅用于首read失败，保持原immutable发布与最后worker日志；写/Ping记录不填。没有新sink、历史队列、公开metrics标签或重试。

## 有效验证

纯分类测试覆盖两个允许外层、三个控制opcode、全部正链、直接sentinel、header/payload，以及nil、无Unwrap的伪全文、伪sentinel、错opcode、额外层、错顺序/大小写、Join、恶意leaf含阶段词等负例。负例必须unknown，任何输出不含敏感标记。终态/腿接线测试要求只有实际leg_read/read失败有字段，许可优先及非read终态保持；原 ErrorClass/ContextState 不变。

真实固定库测试包含两项独立、一次执行的路径，不patch库或缩短库的5s期限：

1. **PONG等锁。** 真正完成HTTP/WebSocket握手后，才在net.Conn.Write入口arm门闩。启动真实DATA Write并确认opcode=2已进入门闩；它持有库帧写锁。只启一个客户端Reader，使用无deadline且可取消的外层ctx。peer实际Ping；测试 DialOptions.OnPingReceived 只将真实控制ctx/Deadline和接收时刻送入有界1元素通道，立即返回true，不等待、取消或保存payload。等待该原控制ctx自然Done以及Read返回，要求deadline、外层active、`pong_write_lock`。释放DATA门闩后验证原DATA和后续DATA可实际往返，区分等锁失败本身与连接关闭；不据此声称已经复现Load调度。
2. **PONG写帧。** 同样先完成握手，只在真实opcode=10 PONG底层Write入口持住门闩。原控制5s自然结束后，观察实际连接Close及Read返回，要求有限阶段与真实包装一致、外层仍active；明确断言控制ctx.Err为DeadlineExceeded且Read错误class为deadline，不能仅凭Done或Close推定自然到期，不得在deadline之前由测试主动close制造成功。该路径证明真实分类接线与共享transport关闭机制，不能追认原Load的唯一根因。

每项新仪器使用10s有限总watchdog，失败也须释放门闩并关闭/join本次所有worker和HTTP handler；原Ping测试的5s watchdog保持。测试watchdog不是产品期限或SLO。不能固定sleep、用更短fake ctx代替内部5s、叠加第二个Reader、让旧握手Write晚返回消耗新arm，或recover错误后跳过断言。

先验证新分类、接线及真实两路径，再检查受影响产品包的unit/race、fmt/vet/staticcheck；完整交付覆盖分母和95%要求保持。新增具名结果按实际源码登记到下一正式候选清单，不预填通过。后续固定新源码的Load区分性观测另行登记；当前四失败、旧d9正式失败和所有开发原件均保留，不拼接为正式通过。

## 整体收束

此改动使消费者的断流原因更可区分，不改变获准目标、许可、业务成功含义、背压或未知结果的处理。G1 Failed、G2 Blocked、G3/G4 Inconclusive及关闭的下游保持；未执行G5/G7部署，不推进alpha。若实测证明当前心跳与背压契约需要改变，先提出具体反例与替代方案，只确认受影响语义，不能靠诊断字段自动扩大采用范围。
