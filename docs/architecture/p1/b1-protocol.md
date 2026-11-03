# B1：v1协议、Channel状态机与错误码

状态：已采用 · 设计已确认、实现未验证（[DISC-007](../../changes/DISC-007.md)-R1，采用范围见[ADR-004](../../decisions/ADR-004-p1-adoption.md)）· 起草基线`ca0dae6` · 承接[#3](https://github.com/wyw-ai/loom-oi/issues/3)。本页是v1线格式、状态转移、错误码与幂等的唯一详细规格；对象、职责和路径的共同边界仍以[共同架构§3–7](../alpha-v0.7.md#objects)为准。文中大写符号（`F`、`T_prepare`等）的初始值统一见[B4§3](b4-baseline.md#config)。

<!-- sdd-scope:p1-b1 -->
## 1. 已采用约束与本页边界

| 已采用约束 | 本页的落实方式 |
| --- | --- |
| H9/K1：OI不实例化业务功能（[#3原回复](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6564999241)） | 三个平面只传字节和通道元数据；没有exec/spawn、命令、业务会话或业务结果字段 |
| H13：业务状态外置、准确绑定、运行状态可重建（[ADR-002](../../decisions/ADR-002-state-and-binding.md)） | Channel固定subject、credential、exposure/revision、epoch/generation；Channel与票据只在内存，重启或换代即作废，任何情况下不重放载荷 |
| H15/C1：缺省best_effort，有效NotReady两模式均拒绝，可配require_ready（[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)） | 预检与提交检查按§6、§7执行；P1没有探针，readiness恒为Unknown，所以require_ready在P1一律返回ReadinessRequired |
| H8＋H15/C3：R可信中继为基线，已开流失败后新通道 | 两条数据腿都连同一OIS；Channel失败只关闭，由消费者重新Open；不迁移已开流，不实现H/D |
| H12：OIS/OIC首版Go（[ADR-001](../../decisions/ADR-001-core-language.md)） | 只采用Go标准库与[B4](b4-baseline.md#pins)锁定的WebSocket库可直接实现的编码 |

身份、票据和目标安全见[B2](b2-security.md)；持久化、重启与存储不可用见[B3](b3-storage.md)。

## 2. 三个平面与版本规则

| 平面 | 端点与监听器 | 编码 | 版本 |
| --- | --- | --- | --- |
| 消费者/管理API | `https://<ois>/v1/...`，api监听器（仅服务端TLS） | JSON，UTF-8 | 路径前缀`/v1` |
| OIC控制 | `wss://<ois>/v1/access/control`，access监听器（必须mTLS） | WebSocket文本消息，每条一个JSON对象 | 子协议`oi.control.v1` |
| 数据隧道 | `wss://<ois>/v1/channels/{channel_id}/attach`；消费者走api监听器，OIC走access监听器 | WebSocket二进制消息，首字节为帧类型 | 子协议`oi.tunnel.v1` |

1. **请求严格解码**：OIS收到带未知字段或重复字段的HTTP请求体，返回400 `InvalidRequest`，不创建状态。新客户端追加的收紧条件不会被旧服务静默忽略。
2. **对端消息宽容解码**：响应和控制消息中的未知字段由接收方忽略。不能被忽略的新语义只能通过新子协议或新路径版本（如`oi.control.v2`）引入。
3. 握手中没有双方都支持的子协议：拒绝升级，HTTP 400 `VersionUnsupported`，不创建任何状态。
4. 所有WebSocket禁用permessage-deflate。线上时间戳只用于显示和审计；所有期限以相对毫秒（`*_ms`）传递，由接收方按本地单调时钟计算，不比较跨主机墙钟。
5. 请求体上限`HTTP_MAX`，控制消息上限`CTRL_MAX`，DATA载荷上限`F`；超限分别返回413 `PayloadTooLarge`、关闭控制连接或ABORT。

## 3. 消费者API

鉴权见[B2§2](b2-security.md#identity)。字段名即v1线上名。

| 接口 | 请求 | 成功响应 | 说明 |
| --- | --- | --- | --- |
| `GET /v1/exposures?limit=&cursor=` | `limit`≤`PAGE_MAX` | 200 `{items:[{exposure_id, access_id, revision, protocol, transport_profile, description, admission_mode, readiness, access_online}], next_cursor}` | 只列出本主体获discover授权的项；按exposure_id稳定排序；cursor不透明 |
| `POST /v1/channels` | `{exposure_id, expected_revision, client_request_id?, admission?}`；`admission`只能为`"require_ready"`（只能收紧） | 201 `{channel_id, state:"preparing", exposure_id, revision, admission_mode, readiness, attach:{url, ticket, expires_in_ms}, prepare_expires_in_ms}`；幂等重放返回200 | 预检见§7；`ticket`为消费者腿一次性票据 |
| `GET /v1/channels/{id}` | — | 200 `{channel_id, state, close_reason?, exposure_id, revision}` | 仅通道所有主体可见，否则404；Closed记录保留`RETAIN` |
| `DELETE /v1/channels/{id}` | — | 202 `{state:"closing"}`；已关闭返回200及最终状态 | 幂等；关闭原因ClosedByClient；不代表业务已取消 |
| `GET /v1/channels/{id}/attach`（WebSocket升级） | `Authorization`头＋`OI-Attach-Ticket`头＋子协议`oi.tunnel.v1` | 101；随后先收READY再收发数据 | 票据规则见[B2§3](b2-security.md#ticket)；通道已终止返回410 `ChannelGone` |

不可见或不存在的exposure对Open返回404，不泄露其存在；可见但无open权限返回403 `Denied`。`admission_mode`是本次有效模式：Exposure配置与请求取更严格者。

## 4. OIC控制协议 `oi.control.v1`

每条消息为`{"type":"…","seq":<uint64>,…}`，`seq`由发送方从1递增，仅用于诊断和应答关联。

| 方向 | type | 主要字段 | 语义 |
| --- | --- | --- | --- |
| OIC→OIS | `hello` | `oic_version`, `last_epoch_seen` | 连接后的第一条；身份只来自mTLS（[B2§2](b2-security.md#identity)），消息内不声明Access |
| OIS→OIC | `welcome` | `epoch`, `generation`, `run_id`, `heartbeat_interval_ms`, `license_ttl_ms`, `limits` | 分配本连接代数。同一Access已有控制连接时，旧连接收到`error{code:"Superseded"}`后关闭，其通道全部中止 |
| OIC→OIS | `register` | `exposures:[{exposure_id, revision, protocol, transport_profile, admission_mode, description}]` | 全量快照，整体替换该Access此前登记；本地配置变化后重发 |
| OIS→OIC | `register_ack` | `accepted:[exposure_id]`, `rejected:[{exposure_id, code}]` | 不在本Access命名空间、超数量或超大小的条目被拒（[B2§4](b2-security.md#target)） |
| OIS→OIC | `prepare_channel` | `channel_id`, `exposure_id`, `revision`, `subject`, `epoch`, `generation`, `prepare_ttl_ms`, `provider_ticket` | 请求OIC本地复核并拨号；不含任何目标地址 |
| OIC→OIS | `prepare_failed` | `channel_id`, `code` | 本地复核、拨号或额度失败；成功不发消息，以提供方腿附着表示 |
| OIS→OIC | `close_channel` | `channel_id`, `code` | 幂等；对未知channel_id忽略 |
| OIS→OIC | `heartbeat` | `license_ttl_ms` | 每`H`发送一次；OIC据此续本地许可（[B2§5](b2-security.md#license)） |
| OIC→OIS | `heartbeat_ack` | `ack_seq` | 连续`OFFLINE_MISSES`次缺失则OIS判定Access离线 |
| 双向 | `error` | `code`, `detail` | 协议错误；发送后关闭连接 |

顺序约束：OIS在完成`hello→welcome→register`前不发送`prepare_channel`。OIC只接受本连接`epoch/generation`的`prepare_channel`，其他一律`prepare_failed{code:"ProtocolError"}`。控制消息不得承载业务数据。

## 5. 数据隧道 `oi.tunnel.v1`

| 类型字节 | 帧 | 载荷 | 发送方与时机 |
| --- | --- | --- | --- |
| `0x01` | READY | 空 | 仅OIS；提交后向每条腿恰好发送一次，先提供方腿、后消费者腿 |
| `0x02` | DATA | 1..`F`字节；消息边界不承载任何语义 | 两条腿，READY之后 |
| `0x03` | FIN | 空；表示本方向不再有DATA | 两条腿；OIS原样转发给对端 |
| `0x04` | ABORT | 2字节大端原因码（§7）＋至多128字节UTF-8说明 | 两条腿及OIS；任一方向出现即终止整个Channel |

- 以下情况一律ABORT(`ProtocolError`)，超过`F`为ABORT(`PayloadTooLarge`)，Channel进入Closing：READY之前出现DATA或FIN；FIN之后同方向再出现DATA或FIN；腿发送READY；文本消息；未知类型。
- **背压**：OIS在每个方向最多持有一条在途消息（≤`F`），写不出时停止读取来源腿；OIC与客户端同样以阻塞写目标/本地连接形成背压。v1不设应用层credit帧，内存上界见[B4§3](b4-baseline.md#config)。
- **OIC对目标**：READY前不读、不写已拨通的目标socket；收到FIN对目标`CloseWrite`；收到ABORT以RST关闭目标（`SO_LINGER=0`），不得把截断伪装成正常结束。客户端的本地转发连接采用相同规则。
- 腿在未发FIN/ABORT时断开，等同ABORT(`ChannelInterrupted`)并转给另一腿。OIS每`PING`向各腿发送WebSocket ping，连续2次未收到pong按断开处理。

## 6. Channel状态机与唯一提交点

OIS是Channel状态的唯一权威。单一协调者（P1为OIS进程内一把锁）线性化所有状态转移、策略版本的应用及撤销执行。

| 当前状态 | 事件 | 条件 | 结果 | 动作 |
| --- | --- | --- | --- | --- |
| （无） | `POST /v1/channels` | §7预检全部通过 | Preparing | 预留额度；生成两张票据；向当前代数的控制连接发`prepare_channel` |
| Preparing | 一条腿附着 | 票据有效且首次消费、身份与角色匹配（[B2§3](b2-security.md#ticket)） | Preparing | 记录该腿；两条腿齐全时立即执行提交检查 |
| Preparing | 提交检查通过 | 见下 | Open | 先向提供方腿、再向消费者腿发送READY |
| Preparing | 提交检查失败、准备超期、`prepare_failed`、腿断开、撤销、DELETE、停机 | — | Closed（具体原因） | 向已附着腿发ABORT；发`close_channel`；释放额度；未消费票据作废 |
| Open | 某方向FIN | 该方向尚未结束 | Open | 转发FIN；两个方向都结束→Closed(`Completed`) |
| Open | ABORT、腿断开、撤销、revision变化、代数替换、许可到期、空闲超时、DELETE、协议错误、停机 | — | Closing | 向仍连接的腿发ABORT(原因)；发`close_channel` |
| Closing | 两条腿均已关闭，或经过`T_close` | — | Closed | 强制关闭剩余连接；释放额度 |
| Closed | 经过`RETAIN` | — | 删除 | 对应幂等记录同时失效 |

**提交检查**（在协调者内一次完成，任一失败即Closed且不发READY）：①未超过准备期限；②当前`policy_version`仍允许（subject, exposure, open），版本已变化则按新版本重评并更新绑定；③消费凭证未撤销、未过期；④注册表中该exposure的revision仍等于绑定值，且仍由同一Access的同一generation登记；⑤Access在线且generation未变；⑥准入：存在有效NotReady则拒绝（两种模式）；require_ready需要有效Ready（P1恒不满足，已在预检拒绝）。

**线性化保证（单OIS）**：撤销、禁用或凭证吊销在协调者内应用新版本，并在同一临界区把受影响的Preparing通道转为Closed、Open通道转为Closing。**该管理调用返回时不存在受影响的Preparing/Open通道，此后的提交检查必然看到新版本。** 腿和目标socket在`T_close`内异步关闭；撤销前已经转发的字节无法撤回。

**为什么READY是提交信号**：READY与后续DATA在同一条WebSocket上按序到达，接收方无需协调控制连接与数据连接之间的竞争。OIS在同一临界区把两条READY分别排入两条腿的有序写队列，所以对端转来的DATA不会先于READY到达任一腿。OIC和客户端在READY之前都不向对端或目标传递任何字节，失败的准备不会让载荷到达目标。TCP建连本身仍可能触发提供方行为，这一点不因READY而改变（[共同架构§5.2](../alpha-v0.7.md#open)）。

## 7. 预检顺序、错误码与幂等

`POST /v1/channels`预检顺序：认证→请求解码→exposure可见（404）→open授权（403）→`expected_revision`（409）→Access在线（503）→准入模式与就绪（412/503）→OIS/主体/Access额度（429）→存储可用（503，[B3§6](b3-storage.md#unavailable)）。

| code | HTTP | ABORT原因码 | 触发 | 消费者应做 |
| --- | --- | --- | --- | --- |
| `InvalidRequest` | 400 | — | 请求不合法或含未知字段 | 修正请求 |
| `VersionUnsupported` | 400 | — | 无共同子协议/版本 | 升级客户端 |
| `PayloadTooLarge` | 413 | `0x0301` | 请求体或DATA超限 | 修正客户端 |
| `Unauthenticated` | 401 | `0x0101` | 凭证缺失、错误、过期或已吊销 | 重新取得凭证 |
| `TicketInvalid` | 401 | — | 票据不存在、过期、角色或身份不符 | 新Open |
| `Denied` | 403 | `0x0102` | 无open权限 | 不重试 |
| `NotFound` | 404 | — | 不可见或不存在 | — |
| `RevisionMismatch` | 409 | — | `expected_revision`不是当前值 | 重新发现后新Open |
| `IdempotencyConflict` | 409 | — | 同一`client_request_id`参数不同 | 换ID |
| `TicketConsumed` | 409 | — | 票据已被消费 | 不重试 |
| `ChannelGone` | 410 | — | 附着时通道已终止；`GET`可见`close_reason` | 新Open |
| `ReadinessRequired` | 412 | — | require_ready但无有效Ready | 等待就绪或调整策略 |
| `ResourceExhausted` | 429 | `0x0204` | OIS或OIC额度耗尽 | 退避 |
| `TargetNotReady` | 503 | `0x0104` | 有效NotReady（两模式） | 等待 |
| `AccessOffline` | 503 | `0x0105` | Access离线或代数在准备期间被替换 | 退避后新Open |
| `StoreUnavailable` | 503 | — | 存储不可用时拒绝新Open（[B3§6](b3-storage.md#unavailable)） | 退避 |
| `RevisionChanged` | — | `0x0103` | 准备或打开期间目标配置变化 | 重新发现 |
| `Revoked` | — | `0x0106` | 策略、凭证或Access被撤销/禁用 | 不重试 |
| `TargetConnectFailed` | — | `0x0201` | OIC拨号失败 | 业务结果未知；可新Open，不重放 |
| `LocalDenied` | — | `0x0202` | OIC本地无此exposure、已禁用或revision不符 | 不重试 |
| `TargetAddressNotAllowed` | — | `0x0203` | 解析结果不在允许网段（[B2§4](b2-security.md#target)） | 不重试 |
| `ProtocolError` | — | `0x0302` | 帧顺序、类型或控制协议违规 | 修正实现 |
| `ChannelInterrupted` | — | `0x0401` | 腿或目标异常断开、控制连接被替换 | 业务结果未知，不自动重放 |
| `LicenseExpired` | — | `0x0402` | 本地许可到期（[B2§5](b2-security.md#license)） | 新Open |
| `IdleTimeout` | — | `0x0403` | 两方向超过`IDLE`无DATA | 新Open |
| `ClosedByClient` | — | `0x0404` | DELETE或客户端主动关闭 | — |
| `PrepareTimeout` | — | `0x0405` | 超过`T_prepare`未完成提交 | 新Open |
| `Shutdown` | — | `0x0406` | OIS/OIC受控停止 | 新Open |

ABORT原因码只说明传输阶段，**从不表示业务未执行**。任何中断后的业务结果由提供方/消费者的原生协议判断（C03、[共同架构§7](../alpha-v0.7.md#failure)）。

**幂等Open**：（subject, `client_request_id`）映射到一条Channel记录，有效期等于该记录存续期（含Closed后的`RETAIN`）。参数（exposure_id, expected_revision, admission）完全一致→返回同一channel_id与当前状态；消费者票据尚未消费时返回同一票据，已消费则`attach`为null；参数不同→409 `IdempotencyConflict`。OIS重启后不去重，客户端须把重启后的重试视为新Open。

## 8. 测试向量

除标注外均属P1，须在SQLite与PostgreSQL两种后端各自通过（[B4§4](b4-baseline.md#acceptance)）。“零容忍”项任一次出现即判失败，不按比例折算。

| ID | 场景与输入 | 判定 | 门槛/观察 |
| --- | --- | --- | --- |
| TV-B1-01 | 消费者经E1读取T1的只读HTTP | 响应来自T1；双向FIN后Closed(Completed)；通道/票据/socket计数回到基线 | G1；O3 |
| TV-B1-02 | 客户端只提供`oi.tunnel.v2` | 400 VersionUnsupported；无Channel状态变化 | G1 |
| TV-B1-03 | POST含未知字段；控制消息含未知字段 | 前者400且无状态；后者被忽略并正常处理 | G1 |
| TV-B1-04 | DATA载荷为`F`+1字节 | ABORT(PayloadTooLarge)；目标收到RST；两腿关闭 | G1 |
| TV-B1-05 | READY前消费者发送DATA | ABORT(ProtocolError)；目标收到0字节（零容忍） | G1 |
| TV-B1-06 | 请求发完即FIN，目标随后完整响应 | 消费者完整收到响应再收FIN；Closed(Completed) | G1 |
| TV-B1-07 | FIN后同方向再发DATA | ABORT(ProtocolError)；迟到字节未到达目标；目标见RST而非FIN（零容忍） | G1 |
| TV-B1-08 | 目标在响应中途异常断开 | 消费者收ABORT(ChannelInterrupted)，本地连接见RST而非EOF | G1；O4 |
| TV-B1-09 | 64 MiB响应，消费者限速读取 | 任一时刻各组件通道缓冲≤[B4§3](b4-baseline.md#config)上界；字节逐一一致 | G1；O3 |
| TV-B1-10 | 同`client_request_id`重放；改参数重放；票据消费后重放；OIS重启后重放 | 同一channel与票据；409；`attach:null`；新channel | G1 |
| TV-B1-11 | 一腿已附着时撤销策略，另一腿随后附着；随机时序重复200次 | 从不发READY；已附着腿ABORT(Revoked)；后到腿410；目标0字节（零容忍） | G1；O1/O4 |
| TV-B1-12 | Open状态下撤销策略 | `T_close`内两腿ABORT(Revoked)、目标RST；随后新Open为403 | G1；O4 |
| TV-B1-13 | 同一Access控制连接被替换（G→G+1），旧代通道X打开中 | X中止；新代通道Y正常；迟到的旧代`prepare_failed`/清理不影响Y（零容忍） | G1；O4 |
| TV-B1-14 | 提供方腿始终不附着 | `T_prepare`后Closed(PrepareTimeout)；额度释放；OIC已拨的目标连接关闭 | G1 |
| TV-B1-15 | 目标端口未监听 | `prepare_failed(TargetConnectFailed)`；消费者看到该原因；票据随即失效 | G1 |
| TV-B1-16 | 额度16时并发20个Open | 恰16个成功、4个429；无额度泄漏 | G1；O3 |
| TV-B1-17 | `register`前或以旧代数下发`prepare_channel` | OIC拒绝（ProtocolError），无拨号 | G1 |
| TV-B1-18 | Exposure为require_ready；另一项为best_effort | 前者412 ReadinessRequired；后者打开且`readiness:"unknown"` | G1；O1 |
| TV-B1-19（P2） | 有效NotReady、样本过期、乱序与两腿到齐后的最终复核 | 两模式均拒绝NotReady；过期不当作Ready | G2；O1 |

## 9. 本轮采用的具体化与取舍

| 项 | 共同架构原表述 | 本规格 | 理由与替代 |
| --- | --- | --- | --- |
| D1-a | “初版只需DATA/FIN/ABORT”，OIS“发ChannelReady” | ChannelReady以隧道内READY帧送达两条腿，先提供方后消费者 | 同一连接保证READY先于DATA。替代为经控制连接或轮询通知，需另处理两连接间的竞争 |
| D1-b | 未规定 | HTTP请求体严格解码；控制消息与响应宽容解码；新语义升版本 | 防止收紧条件被静默忽略。代价是新增请求字段要服务端先发布 |
| D1-c | “有界背压” | 每方向一条在途消息＋阻塞写，不设credit帧 | 实现简单、内存上界明确。高时延吞吐若受限，由O3数据决定是否引入credit窗口 |
| D1-d | 未规定 | ABORT以RST关闭目标与本地连接；FIN对应半关闭 | 防止截断被当作完整结束；代价是部分目标日志出现连接重置 |
| D1-e | “同一活动任期内有界去重” | 去重有效期＝Channel记录存续期，不跨重启 | 内存有界、语义清楚；不提供持久Exactly-once |
| D1-f | 接受更高代数时停用旧通道 | 明确：控制连接重连即中止全部旧代通道，不迁移 | 控制抖动会中断数据流；跨代迁移需新状态，属未采用的M范围 |
| D1-g | 错误类型列表 | §7完整错误表与ABORT原因码 | 实现与诊断需要稳定编码；新增码只能追加，不改既有含义 |
<!-- /sdd-scope:p1-b1 -->
