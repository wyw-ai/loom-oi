# B2：身份、授权链、一次性票据与目标安全

状态：已采用 · 设计已确认、实现未验证（[DISC-007](../../changes/DISC-007.md)-R2，采用范围见[ADR-004](../../decisions/ADR-004-p1-adoption.md)）· 起草基线`ca0dae6` · 承接[#3](https://github.com/wyw-ai/loom-oi/issues/3)。依赖[B1](b1-protocol.md)的消息、状态与错误码定义。本页回答“谁能以什么身份、对哪个目标、在多长时间内传字节”，是v1身份与目标安全的唯一详细规格；共同边界见[共同架构§4、§5、§7](../alpha-v0.7.md#registration)。

<!-- sdd-scope:p1-b2 -->
## 1. 已采用约束

| 已采用约束 | 本页的落实方式 |
| --- | --- |
| H9：OI不实例化业务，只登记与提供通道 | 目标只能来自OIC本地配置；协议中没有任何目标地址、URL或命令字段 |
| H13：准确绑定（[ADR-002](../../decisions/ADR-002-state-and-binding.md)） | 票据与Channel绑定主体、凭证、Access代数、exposure与revision；旧票据、旧代数不能复活 |
| 共同架构§4/§5.1/§7 | OIC主动出站mTLS；短时一次性票据不放URL；撤销＝主动通知＋有限许可 |
| H8＋H15/C3（[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)） | 只实现R路径；许可与撤销语义写成与路径无关，但P1不实现H/D，也不提供离线授权 |

<a id="identity"></a>
## 2. 身份与凭证

| 主体 | 凭证 | OIS保存什么 | 校验点 | 吊销、禁用或到期后 |
| --- | --- | --- | --- | --- |
| 消费者 | `Authorization: Bearer oi_<token_id>_<secret>`；secret为32字节随机数 | 只存token_id、subject、scope、SHA-256(secret)、到期时间、吊销时间 | 每个API请求、消费者腿附着、提交检查（[B1§6](b1-protocol.md)） | 立即拒绝新请求；该凭证的Preparing/Open通道关闭（`Revoked`；到期为`Unauthenticated`） |
| 管理员 | 同格式，`scope=admin` | 同上 | 只用于`/v1/admin/*` | 同上；管理令牌不能Open，消费令牌不能调用管理API |
| OIC（Access） | X.509客户端证书，TLS 1.3 mTLS | Access记录保存至多2个SPKI SHA-256指纹 | access监听器握手：证书链到`client_ca`，且指纹属于一个已启用Access | 移除指纹或禁用Access→关闭其控制连接，全部通道`Revoked` |
| OIS服务端 | 服务器证书 | 部署配置 | OIC与消费者按配置CA和服务名校验 | 由部署方轮换 |

- **两个监听器、同一进程**：api监听器只做服务端TLS，承载消费者与管理API及消费者腿；access监听器强制客户端证书，承载控制连接与提供方腿。两者属于部署A的同一OIS进程。
- TLS最低版本1.3。令牌比较使用常量时间；日志只记token_id，不记secret、票据或`Authorization`/`OI-Attach-Ticket`头。
- 消费令牌默认有效期`TOKEN_TTL`，最长`TOKEN_TTL_MAX`；首个管理令牌由`ois init`一次性生成并输出。
- 证书轮换：先为Access添加新指纹（此时2个）→OIC换证并重连→删除旧指纹；删除时仍以旧指纹认证的控制连接被关闭。证书过期导致握手失败，OIS据心跳超时判定Access离线。

<a id="ticket"></a>
## 3. 授权链与一次性票据

**每次Open的授权链**（任何一环失败都不进入下一环）：凭证认证得到（subject, credential_id）→ 策略在当前`policy_version`允许对该exposure的`discover`与`open` → exposure属于某Access的命名空间并在注册表中有当前revision → 该Access在线并有当前generation → OIC本地复核（exposure存在、已启用、revision相同、目标允许、本地额度）→ 两腿附着时票据校验 → 提交检查（[B1§6](b1-protocol.md)）。

| 票据属性 | 规则 |
| --- | --- |
| 格式与熵 | `tk_`＋32字节随机数的base64url；OIS内存中只保存其SHA-256 |
| 绑定 | 角色（consumer/provider）、channel_id、epoch、exposure_id、revision；消费者票据另绑subject与credential_id，提供方票据另绑access_id与generation |
| 有效期 | 不超过该Channel剩余准备时间；Channel离开Preparing时，未消费的票据立即作废 |
| 传递 | 消费者票据只在`POST /v1/channels`（及同一主体的幂等重放）响应中返回；提供方票据只经mTLS控制连接下发给对应OIC；附着时只放在`OI-Attach-Ticket`头 |
| 角色校验 | 消费者腿必须经api监听器且Bearer凭证的（subject, credential_id）与票据一致；提供方腿必须经access监听器且mTLS身份映射到该Access的当前generation |
| 一次消费 | 在协调者内原子地“校验并删除”：第一次有效附着成功，其后同票据返回409 `TicketConsumed`，且不影响已附着的腿 |
| 拒绝方式 | 票据出现在URL查询串→400 `InvalidRequest`且不处理；不存在、过期、角色或身份不符→401 `TicketInvalid`；通道已终止→410 `ChannelGone` |

票据泄露的影响被限制为：攻击者还必须持有同一主体的有效凭证（消费者腿）或同一Access的私钥（提供方腿），并在准备期限内抢先附着。

<a id="target"></a>
## 4. 目标安全

- **命名空间**：`exposure_id = <access_id>/<name>`。OIS只接受Access登记自己命名空间内的条目，且数量≤`MAX_EXPOSURES`、描述≤`DESC_MAX`；不符的条目在`register_ack`中拒绝，不进入目录。
- **唯一目标权威是OIC本地配置**：每个exposure配置`name`、`target.address`（`host:port`）、`target.allowed_cidrs`、`protocol`、`admission_mode`、`description`、`enabled`。消费者和OIS都不能提供或改写地址。
- **revision**：`rv_`＋对规范化JSON `{target.address, target.allowed_cidrs, transport_profile, protocol}`做SHA-256后base32编码的前26个字符。描述与准入模式不参与：修改它们只影响此后的Open，不打断已开通道（[共同架构§6](../alpha-v0.7.md#observation)：准入只决定新通道）。OIC无需持久存储，同一配置重启后得到同一revision；配置变化后OIC重发`register`，并以`RevisionChanged`中止绑定旧revision的全部通道。
- **主机名目标**必须配置非空`allowed_cidrs`。拨号时在期限内解析，只保留落在允许网段的地址，**直接拨这个IP**而不再次解析；没有允许地址→`TargetAddressNotAllowed`。IP字面量目标若配置了网段，加载配置时即校验其在网段内。
- OIC是字节中继：不改写Host/SNI/Origin，不跟随重定向，不解析业务协议。
- `prepare_channel`到达时本地复核：exposure存在且启用、revision相同、本地并发≤`MAX_LOCAL_CHANNELS`；否则`prepare_failed`（`LocalDenied`或`ResourceExhausted`），不拨号。

<a id="license"></a>
## 5. 许可、控制失联、长暂停与到期

| 情况 | OIC行为 | OIS行为 |
| --- | --- | --- |
| 正常 | 每收到`heartbeat`：本代许可期限＝接收时刻（单调时钟）＋`license_ttl_ms`－`LICENSE_MARGIN` | 每`H`发送`heartbeat`，携带`L` |
| 许可到期 | 每次转发前及定时器检查；到期即对本代全部通道ABORT(`LicenseExpired`)并RST目标，**到期后转发0字节** | — |
| 控制连接断开、数据腿仍在 | 不再接受新准备；已开通道最多保留到许可期限 | 连续`OFFLINE_MISSES`次未收到`heartbeat_ack`→Access离线，其Preparing通道Closed(`AccessOffline`)、Open通道Closing并断开中继腿 |
| 控制重连得到新generation | 立即中止全部旧代通道（[B1 D1-f](b1-protocol.md)） | 旧代清理只作用于旧代对象 |
| OIC进程长暂停超过`L` | 恢复后第一次检查即发现到期，中止全部本代通道，不再转发 | 同“控制断开” |
| 消费凭证到期或吊销 | — | 关闭该凭证的通道并拒绝新请求 |

R路径下两条数据腿都经过OIS，OIS侧断腿是撤销与失联的第一道边界，OIC许可是第二道。已知局限：部分操作系统在系统休眠期间单调时钟不计时，OIC本地许可可能偏长；R模式由OIS侧心跳超时兜底。将来的直连/双路径必须重新评估这一点（不在P1）。

## 6. 测试向量

除标注外均属P1，两种后端各跑一遍。“零容忍”项任一次出现即判失败。

| ID | 场景与输入 | 判定 | 门槛/观察 |
| --- | --- | --- | --- |
| TV-B2-01 | 缺失、错误、过期、已吊销的消费令牌访问API与附着 | 401；无状态变化 | G1 |
| TV-B2-02 | 主体无discover；有discover无open | 前者目录不含该项且Open为404；后者403 | G1；O1 |
| TV-B2-03 | 消费者A反复打开`lab-a/http`（T1），C打开`lab-b/http`（T2），各500次并发交错 | 每次响应的目标身份与所选exposure一致，串路0次（零容忍） | G1（V09种子） |
| TV-B2-04 | 将`lab-a/http`的本地目标由T1改为T3 | 已开通道ABORT(RevisionChanged)，T3收到0字节；旧`expected_revision`的Open为409；重新发现后新Open到达T3（零容忍：旧许可到达新目标） | G1目标替换拒绝 |
| TV-B2-05 | 同一消费者票据并发附着两次；关闭后再次使用 | 恰一次成功，另一次409；关闭后410或401（零容忍：两次成功） | G1 |
| TV-B2-06 | 消费者票据用于提供方角色、换另一主体令牌、用于其他channel、放入URL查询串 | 401或400；目标通道不受影响 | G1 |
| TV-B2-07 | 证书不链到`client_ca`；指纹不属于任何启用Access；Access已禁用；双指纹轮换后删除旧指纹 | 前三者拒绝控制连接；轮换期间新旧均可；删除后旧连接关闭、其通道中止 | G1；O4 |
| TV-B2-08 | OIC登记其他Access命名空间的条目、超数量或超大小 | `register_ack`拒绝；目录不出现 | G1 |
| TV-B2-09 | 主机名目标被测试DNS解析到允许网段以外的地址 | `TargetAddressNotAllowed`；没有任何到该地址的连接（零容忍） | G1 |
| TV-B2-10 | 测试代理只切断控制连接，数据腿保持 | 通道在`L`±1s内关闭，原因为LicenseExpired或AccessOffline（OIC许可与OIS离线判定先到者）；期间无新准备 | G1；O4 |
| TV-B2-11 | OIC进程暂停超过`L`后恢复 | 恢复后立即ABORT(LicenseExpired)；到期后转发0字节（零容忍） | G1；O4 |
| TV-B2-12 | 吊销消费令牌；令牌自然到期 | 其Open通道分别以Revoked、Unauthenticated关闭；新请求401 | G1；O4 |
| TV-B2-13 | 禁用Access后再启用 | 控制连接关闭、全部通道Revoked；重新启用后须新连接新代数 | G1 |
| TV-B2-14 | 管理令牌调用Open；消费令牌调用管理API | 均403 | G1 |

## 7. 本轮采用的具体化与取舍

| 项 | 共同架构原表述 | 本规格 | 理由与替代 |
| --- | --- | --- | --- |
| D2-a | 未规定端口布局 | api与access两个监听器、同一OIS进程 | access强制mTLS，消费侧无需客户端证书。替代为单端口可选客户端证书，配置与审计更复杂 |
| D2-b | “已验证证书身份映射到Access” | CA链＋每Access至多2个SPKI指纹 | 不依赖CN/SAN解析，轮换步骤明确。替代为按SAN URI映射，需要另定命名规范 |
| D2-c | “有期限、可吊销、仅存摘要的服务访问令牌” | 令牌格式、只存哈希、最长`TOKEN_TTL_MAX`、到期即关闭通道、管理与消费令牌分离 | 具体化已有方向；代价是长连接受令牌有效期约束 |
| D2-d | “修改目标/协议/安全约束增加revision” | revision为目标与传输配置的内容哈希（不含描述与准入模式）；exposure_id以access_id为命名空间 | OIC无持久存储也不会回绕；修改描述或准入模式不打断已开通道。代价是revision不能比较新旧，先后由generation与全量快照保证 |
| D2-e | “域名解析/目标重绑定约束” | 主机名目标必须配允许网段，并按解析出的IP直接拨号 | 关闭DNS重绑定窗口。代价是使用动态地址的服务要配置网段 |
| D2-f | “主动通知加有限许可期限” | 心跳续期的相对TTL许可；OIS心跳超时为第一道边界 | 无需跨主机时钟同步。网络分区时撤销最晚在`L`后生效；R模式下OIS断腿通常更早 |
<!-- /sdd-scope:p1-b2 -->
