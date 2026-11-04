# G2 观测、准入与导出契约（已采用，待实施）

2026-11-04 · DISC-009 修订 1 · 基线 `b50c08e228aa1c01686b35a59421ad2670154219`。

**状态：三组新增契约均已准确采用；未实施、未预注册、未运行 G2。** 本页是唯一详细契约，[TASK-007](../../../spec/TASK-007-p2-g2-readiness/README.md)只记录任务与验证，[DISC-009](../../changes/DISC-009.md)只记录来源/范围/追踪。采用范围与原问答见[ADR-005](../../decisions/ADR-005-g2-adoption.md)及[唯一确认凭据](../../../spec/TASK-007-p2-g2-readiness/approval.json)，绑定已发布提交deff207的六个原片段；片段内提案阶段措辞保留，当前状态以该凭据为准。C1、B1/B2及原G1证据范围不扩大。G3 可独立先行；本工作区已合入 TASK-005 最终产品与证据，实施须沿用其维护及存储拒绝新Open边界，不因冻结片段中的历史基线措辞删除已有行为。

**入口现状**：alpha `570b573` 已集成到本工作区，G1/G4以固定2ec3670及其新正式run恢复Passed，旧失败与适用性修正原样保留。P2前置已满足；G2尚未进入产品实施，不能把G1/G4通过当作G2验证。合并仍待提交和推送。

[三组决定](#decisions) · [Probe-HTTP](#probe-http) · [消息与展示](#wire) · [最终复核](#commit) · [Export-Pull](#export-pull) · [预算与验收](#acceptance)

## 1. 要解决的用户结果及范围

提供方把已经存在的服务暴露给获准消费者。OI 应能解释“接入在线”“该健康契约最近报告就绪”“此次获准建立通道”“业务执行成功”之间的区别：无健康接口的服务仍可 best_effort 连接；有有效负证据时不能靠 best_effort 绕过；要求就绪的消费者不会使用过期、旧代或错误来源的 Ready。慢探针、目录更新和外部监控不得拖住撤销、许可与正常控制。

正常场景：一个 HTTP 服务声明 `/health/ready` 是无副作用的健康接口，部署者明确配置 `200 → Ready`、`503 → NotReady`。OIC 采样、归约并独立发布；消费者 discover 看到有来源和时效的目录快照，require_ready Open 经本地复核、拨号和最终复核后取得 READY。此后业务请求仍可能失败，OI 不解释或重放业务载荷。

关键反例：Ready 样本尚有效时下一次探针超时，不能把超时当 NotReady，也不能给旧 Ready 续期；两腿附着期间 Ready 过期，则本次严格准入必须失败。另一个反例是 OIC 最终检查后、OIS 发 READY 前出现新的 NotReady：这要求明确两个组件之间的判断时点，不能用“再查一次”声称消除了竞态。

最小候选不新增业务托管、任意 shell、业务 SDK、自动重启、业务成功/取消保证、可靠历史事件总线、时序数据库、HA、H/D 路径、自动等待 Ready 或自动业务重试。提供方主动报告、带业务凭据的复杂探针可作为替代方案评审，不随“可选”二字默认纳入。

## 2. 已采用依据与不能扩大解释的部分

| 来源 | 已采用或当前明确约束 | 对本任务的影响 |
| --- | --- | --- |
| [H15 / #17](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6610997252)、[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)、[DISC-006](../../changes/DISC-006.md) | C1 默认 best_effort；Unknown 仅有界尝试；有效 NotReady 两模式拒绝；可配置 require_ready；持续观察效果 | 不重新询问 C1。H15 未一次批准全部字段、线格式、期限、配额、并发提交算法或业务保证 |
| [ADR-004](../../decisions/ADR-004-p1-adoption.md)、[DISC-007 修订 2](../../changes/DISC-007.md)、[#18 I18-P01](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6615315184) | B1–B4 采用；R6 仅限 B1–B3 实际承接范围；OIS 是 Channel 权威，READY 是提交信号 | 不用历史章节的 Pending 阻塞既有实施；也不把未定义探针/导出接口算成已采用 |
| [共同架构 §6](../alpha-v0.7.md) | attempt/有效样本分开；单调 TTL；序号绑定源/代数；过期/改版/来源失效成为 Unknown；OIC 最终判断；被动缓存不能严格准入 | 内部代数可直接细化和测试；分布式最终时点需要精确定义 |
| [B1 §2、§6–§8](../p1/b1-protocol.md) | 安全关键新语义不能被旧端忽略，须新子协议/路径；提交检查和错误规则；TV-B1-19 验证 NotReady/过期/乱序/两腿齐全后的复核 | 控制 v2 与迟到/重复/最终错误须形成端到端契约 |
| [B4 §4](../p1/b4-baseline.md) | P1 明确排除探针与 NotReady 证据，交 P2/G2 | G1 完成不证明 G2 完成 |
| [#3](https://github.com/wyw-ai/loom-oi/issues/3)、[#11](https://github.com/wyw-ai/loom-oi/issues/11)、[#18 I18-P02](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6620751696) | G2 先收束具体契约；LAB-005 要真实 probe→归并→发布→导出链及独立控制测量点 | 探针完成时间不是发布延迟；旧 LAB-001 只支持局部反例 |

### 已有实现事实

- `internal/ois/relay/channel.go:158` 的 `commitCheck` 注释明确 P1 尚无 readiness 检查。
- `internal/oic/relay/agent.go:450` 的 `prepare` 检查本地配置/代数/许可/额度，但没有就绪观测或两腿齐全后的 OIC 复核。
- `internal/protocol/control.go` 没有观测消息或最终复核请求/回应；现行 `prepare_channel` 也没有本次有效 admission。
- `internal/protocol/codes.go` 的 `ReadinessRequired` 仅有 HTTP 412，没有 ABORT 值；`frame.go` 会把没有 ABORT 映射的错误编码成 ProtocolError。必须补全准备后过期的真实错误传播。
- 架构中的 `AdmissionDenied` 尚无提供方报告契约及稳定码；不能把 busy、HTTP 错误或探针超时自行解释成该拒绝。
- 现有 OIC `diag_listen` 仅允许回环并提供 `/stats`，可作为最小导出的权限边界起点，不因此自动批准新格式。

<a id="decisions"></a>
<!-- sdd-scope:g2-decisions -->
## 3. 三组用户决定点

这三组分别对应 DISC-009-R1/R2/R3，只决定新增或尚未具体收束的契约；C1、OIS 权威、既有授权/绑定/许可、透明传输和无业务重放不重问。

| 组 | 推荐候选 | 实质替代 | 需要接受的代价 |
| --- | --- | --- | --- |
| A：Commit-Snapshot / DISC-009-R1 | 采用下文 Commit-Snapshot：OIC 最终检查线性化本次观测决定，OIS 随后保留授权与 Channel 提交权；控制 v2，旧控制 v1 维持 Unknown；新增准备失败的准确错误传播 | Commit-Strict：要求更强同时性；当前尚无完整可实施方案。不接受 Commit-Snapshot 则继续设计并暂停该契约，不把多一次查询或屏障当现成解法 | Commit-Snapshot 有明确的检查后传播窗口，Ready 仍可在到达前保守过期；Commit-Strict 增加跨机状态、延迟及撤销/故障处理成本 |
| B：Probe-HTTP / DISC-009-R2 | 本地管理员配置的单源 HTTP(S) 健康契约；独立 observation_revision；修改 probe 失效样本/check/Preparing，已 Open 长流保持 | Probe-Report：由提供方主动报告，需增加认证写入口、来源授权、重放/冲突与失效规则；其具体接口尚须继续设计 | Probe-HTTP 不覆盖所有非 HTTP 服务；独立版本增加字段和比较，但不扩大 B2 target revision 或旧流中断触发 |
| C：Export-Pull / DISC-009-R3 | 最新状态合并、独立发布；默认关闭的回环拉取指标；有限维度；采用下文测试候选预算做预注册，产品数值默认另据证据校准 | 主动 OTLP 等导出＋有界队列/凭据/重试；若要求历史无丢失，另需可靠日志/重放，扩大任务；也可只交本地诊断，但不能宣称已满足含真实 exporter 的 LAB-005 | 推荐会丢弃/合并中间展示事件，不承诺监控历史完整；回环导出需要外部采集代理；实验预算不成为生产 SLO |

可分别回复“采用 Commit-Snapshot”“采用 Probe-HTTP”“采用 Export-Pull”，或指出需要调整的一组；也可以一次采用三个推荐。
<!-- /sdd-scope:g2-decisions -->

<a id="probe-http"></a>
<!-- sdd-scope:g2-probe -->
## 4. 推荐 Probe-HTTP：本地单源 HTTP(S) 观测契约

### 4.1 配置与权限

方案名 `Probe-HTTP`，对应 DISC-009-R2。每 Exposure 可配置一个 `probe`；缺省不配置，能力为 Unsupported、就绪 Unknown。候选字段：

```json
{
  "kind": "http_status_v1",
  "url": "https://service.internal:8443/health/ready",
  "allowed_cidrs": ["10.20.0.0/16"],
  "tls_ca_file": "/etc/oi/probe-ca.pem",
  "ready_statuses": [200],
  "not_ready_statuses": [503],
  "interval_ms": 5000,
  "timeout_ms": 1000,
  "validity_ms": 15000,
  "jitter_ms": 500
}
```

上例与 §4.3 的初始配置候选一致，均待确认，不是现有产品默认或生产 SLO；§9.1 使用短周期配置进行可重复实验。

- 本地管理员通过既有配置文件设置；Open 请求、消费者令牌、OIS 注册描述都不能提供 URL、方法、headers 或命令。不会因为 OIS 拥有管理权限就获得额外的 OIC 本地探测权限。
- 方法固定 GET；部署方必须明确确认该端点无业务副作用。GET/TCP 成功本身不能证明安全或任意业务可用。
- URL 只支持显式 http/https、host/port/path；不接受 userinfo、fragment 或凭据型 query；候选最小版直接禁止 query。拒绝重定向，禁用环境代理，DNS 与实际连接按 B2 的本地网段许可检查；禁止通过重定向/二次解析逃逸。HTTPS 做正常名称和 CA 验证，不增加跳过验证选项。
- 本候选不支持自定义鉴权 header、cookie、客户端证书或业务响应正文匹配。需要受保护探针时，部署者可提供受限本地健康端点，或另选更完整的凭据契约；不能在 description/日志/目录中传凭据。
- Ready/NotReady 状态码集合显式配置、非空且互斥。只有合同列出的响应产生样本。未列出状态、网络/TLS/权限/解析错误记录 attempt 失败并保留未到期旧样本，不伪造负证据。
- 有限 reason 候选为 `matched_ready_status/matched_not_ready_status/unsupported/no_sample/expired/source_invalid/timeout/permission_error/tls_error/address_denied/connect_error/status_unmapped/malformed_response`；样本只允许前两项，其他项分属能力、失效或 attempt。具体平台错误归约到固定枚举，不能动态生成 label。
- 只在期限内完整解析最终响应头后按状态码归约；状态码集合只接受200–599的整数，自动重定向始终关闭。正文不参与判断、不等到EOF、不保存；响应头解析完成后立即关闭响应，最多保留HTTP读缓冲中已预读的4 KiB，不额外drain业务正文。限制header，到达终态后关闭响应；记录有限 reason 枚举，不把原始错误文本、URL/地址或响应内容发送到目录/指标。内部受控日志仍须脱敏。
- `interval_ms/timeout_ms/validity_ms` 为正整数毫秒，`jitter_ms` 非负；各自有确定最大值并防溢出。`timeout < interval`、有效期覆盖周期/抖动/正常耗时是配置建议，不随意把比例写成产品硬限制。禁用通过删除 probe 表达，不把零超时解释为无限。
- 配置包括 kind、目标、许可、TLS 身份和状态判定的变化会改变观测契约。**[B2 §4](../p1/b2-security.md) 已采用的 Exposure revision 只覆盖 target/protocol/transport，连 admission_mode 变更也只影响新 Open、不杀旧流；把全部 probe 变化纳入 Exposure revision 是新增中断触发，不能称为自动复用已采用范围。** 独立审阅据此将最小推荐收窄为下面的 Observation-Version；本轮不再提议扩大 B2 target revision。
- **Observation-Version：独立 observation_revision 与 contract_digest。** 每个Exposure在同一控制generation内从`ob_1`开始，任何probe配置变化递增一次，不能复用或回退；uint64耗尽则结束该session并重新登记，不能绕回。`contract_digest` 为完整规范化probe配置JSON的SHA-256小写十六进制；未配置probe对JSON `null`求摘要。规范化沿B2的主机/IP/CIDR格式，状态码与CIDR数组去重排序，JSON键排序/无空白，TLS信任用读取的证书内容SHA-256而非本地文件路径；包含kind/端点/许可/判定/周期预算。TLS文件读取失败拒绝配置重载，继续原合法配置并记本地错误，不发布半份新契约。
- 这两个字段进入register、query/check/result与binding；generation变化时序号可从1重新开始，但完整binding已不同。A→B→A的digest可相同，observation_revision仍不同；attempt/sample/update序号在同一generation内持续递增、不因配置回切重置。每次probe有不可复用work_id，取消后迟到结果一律丢弃，不仅比较digest。
- 更换观测版本原子清空有效样本，取消旧probe/check，取消仍Preparing的本地预留并发送ReadinessCheckFailed(reason=observation_changed)；已Open长流继续。实际target/protocol/transport或授权变更仍执行B2原有关闭规则。
- Observation-Version 下本地标记取消后的 Preparing 不能开始转发；OIS 得知版本变化后关闭旧 Preparing，未来检查只接受新版本。跨组件取消通知可能晚于已发送的 READY，因此必须把“收到 READY 后迅速被 ABORT”和检查竞争纳入 Commit-Snapshot 的明确窗口及测试，不能再声称任意两机事件顺序的原子取消。健康变化与配置/权限取消必须分开，本候选没有放宽真正业务目标授权。
- 不选把 probe 摘要并入 Exposure revision：它虽然减少一个版本字段，但会让仅改健康周期或预算也关闭业务长流，扩大 B2 已采用的中断范围。此轮不修改 B2 revision 输入。

### 4.2 归约对象及样本有效期

```text
binding = (access_id, epoch, generation, exposure_id, revision, observation_revision,
           source_id?, contract_version?, contract_digest)
attempt = (attempt_seq, Idle|Running, outcome, reason, duration_ms)
sample  = (sample_seq, Ready|NotReady, observed_at,
           sampled_monotonic, expires_monotonic, reason)
```

- `source_id` 为本地配置契约的有界标识，不是远程可选任意标签；首版每 Exposure 只有一个权威源，不需要多源投票。`generation` 是 OIC 连接代数，不冒充业务服务重启代数。
- attempt 和 sample 分开编号。probe 开始只改 attempt；只有明确正/负证据才产生新 sample_seq。探针开始、失败、推送、查询、重发、心跳都不延长旧样本寿命。
- 本候选保守以请求发起的单调时点为 sample 有效期起点；完成时已经耗尽 validity 则不能形成有效样本。如此不会把一次慢响应的完整网络等待加成新的有效时间。observed_at 可记录结果收到的墙钟时刻，仅作展示/审计，必须标清含义。
- 同一 binding 下较小 sample_seq 丢弃，相同序号只允许不可变样本字段一致的幂等重送、不续期；同序的 result/reason/observed_at/样本身份冲突属于协议违规。比较不包含随时间递减的 remaining_ms，也不包含另行编号的 attempt；否则正常查询也会被误判冲突。源、revision、epoch/generation 不同不能直接比较数字大小。
- revision/source 失效或 TTL 到期时，派生 readiness=Unknown 并保留 `last_sample` 供诊断；不能清空历史原因伪装成“从未探测”。未到期 NotReady 遇一次失败 attempt 仍是 NotReady。
- 重连时 OIS 缓存先 Unknown；OIC 本候选清空该连接绑定的有效样本，重新采样，不把旧样本换新 generation 复活。稳定 source 的将来复用若有价值，应另定义重新验证机制。
- 健康信息不创建服务锁。忙碌提示仍不自动拒绝；本任务不新增提供方 AdmissionDenied 接口。
### 4.3 Probe-HTTP 初始配置候选

这些是本修订 R2 的确定候选，尚未采用，不是现有生产默认/SLO；管理员调整后仍受本地有限上限约束。

| 配置 | 初始值 | 校验与取舍 |
| --- | --- | --- |
| interval/timeout/validity/jitter | 5000/1000/15000/500 ms | interval/validity 100 ms–24 h；timeout 50–30000 ms；jitter非负且≤interval/2；防整数溢出 |
| probe worker / 执行队列 / 到期标记 | 4 / 32 / 每Exposure 1个 | worker和队列均为正整数且≤MAX_EXPOSURES；没有任务不创建空转worker；每目标一个在途probe |
| header / 预读体缓冲上限 | 8 KiB / 4 KiB | 不额外读取正文；header超限只记attempt malformed_response，不造NotReady；预读缓冲有界并在关闭后释放 |

未配置probe完全禁用，不用零超时表示无限。周期短于timeout可合并tick，但不能并行探测同目标；建议timeout < interval。配置校验还拒绝重叠/空状态码集合、无允许网段、非法URL、未知字段。该配置只控制观测成本，不承诺业务SLA或1C1G容量。

<!-- /sdd-scope:g2-probe -->

<a id="wire"></a>
<!-- sdd-scope:g2-wire -->
## 5. 消息、独立发布与目录展示

### 5.1 为什么不直接信任推送 remaining_ms

被动推送在本地或网络中积压多久不可知，接收时把发送者的 remaining_ms 重新起表会延长寿命。仅靠 observed_at 比较跨主机墙钟也不成立。推荐把“状态已变化”的通知与“有可靠时效界限的快照获取”分开：probe 结果驱动独立通知；OIS 发带 nonce 的查询，OIC 立即返回现有状态，不等待新 probe；OIS 扣整段 RTT，得到保守目录期限。这比单向 push 多一往返，换来准确的展示时效。

### 5.2 候选控制消息

以下使用 mTLS 现有 Access 身份及 `oi.control.v2`，公共 envelope 的 epoch/generation/消息大小上限沿用现行约束。不得接受消息自报的其他 Access。下表字段名是本修订的确定候选，随相应需求一起确认。

| 方向/消息 | 主要字段 | 接收规则 |
| --- | --- | --- |
| OIS→OIC v2 `welcome` 扩展 | observation_limits，包括消息/待决检查/单次检查上限 | 与 OIC 本地限制取更严格值；不由服务器扩张本地 probe 网络权限；协商后的值进入诊断/实验记录 |
| OIC→OIS v2 `register` 扩展 | 每Exposure保留B1原字段，新增probe_support与完整binding | 只登记能力与来源外壳，不含URL/凭据；binding与原exposure_id/revision及本会话一致；观测版本变化按规则使旧缓存无效 |
| OIC→OIS `observation_changed` | binding, update_seq | 表示本地状态有新版本；每 Exposure 合并为一个最新通知；不能直接据此严格准入 |
| OIS→OIC `observation_query` | query_id, binding, deadline_ms | 只查询本 Access 已登记且绑定匹配项；OIC 回现有状态，不启动 probe、不等待正在运行的 probe |
| OIC→OIS `observation_snapshot` | query_id, binding, update_seq, attempt摘要, sample_seq?, readiness, reason, remaining_ms, observed_at? | 只接受当前待决 query、同代同版同源；OIS 按 query 发出至回应收到的完整 RTT 扣 remaining_ms |
| OIS→OIC `admission_check` | check_id, stage=preflight\|final, binding, effective_admission, deadline_ms, channel_id? | 见 §6；final 必须绑定当前 Preparing 的 channel，preflight 不授权目标拨号 |
| OIS→OIC v2 `prepare_channel` 扩展 | B1原字段，新增binding、effective_admission | binding与原channel/Exposure字段、当前本地配置及session匹配；不会从同target revision推断probe版本相同 |
| OIC→OIS `admission_result` | check_id, binding, channel_id?, decision_id?, effective_admission, readiness, reason, sample_seq?, remaining_ms, code? | 只有匹配当前待决检查才可用；目录快照/query_id 不可当 admission_result |

表内 `binding` 是必填JSON对象，字段固定为 `access_id, epoch, generation, exposure_id, revision, observation_revision, contract_digest`，已配置来源另有必填 `source_id, contract_version`；Unsupported仅省略后两项。所有列出binding的消息都携带完整对象，不依赖“上次register”的隐式字段补全。其access_id必须等于mTLS会话映射/该OIC身份，epoch/generation同时与公共envelope一致；原消息中已有exposure_id/revision的，二者也必须一致。OIS每个query/check和OIC每个预留保存完整binding，只接受完全一致的结果。A→B→A即使contract_digest回到原值，observation_revision仍不同，旧通知/检查/结果不得命中新工作。

v2消息的安全关键字段必须存在且类型/枚举有效，不能把缺失/未知值默认成Unknown或best_effort；未知非关键扩展字段仍按B1宽容忽略。epoch/generation沿原协议，observation_revision格式为`ob_`加正uint64十进制，contract_digest为64位小写十六进制；source_id固定为`local`，contract_version固定为整数1，probe_support仅Supported/Unsupported。Unsupported没有source_id/contract_version，但仍有观测版本与null契约摘要。登记未知观测契约版本时，该Exposure以VersionUnsupported拒绝，不保留其旧登记项；已获准条目的回应绑定不符按ProtocolError处理。

query_id/check_id/decision_id均为32位小写十六进制随机标识，同一session内不得复用；登记/检查只接受当前mTLS Access的命名空间。seq为正uint64，溢出结束session而不回绕；readiness只允许ready/not_ready/unknown，stage只允许preflight/final；effective_admission只允许best_effort/require_ready。final成功必须包含decision_id，preflight不得把结果冒充final决定；所有成功/失败回应均包含完整binding与check_id。序号、标识和有限reason不能作为动态指标标签。

消息组合也须校验：Supported的Ready/NotReady必须有匹配来源、有效sample_seq及正remaining_ms；Unsupported不能声称Ready/NotReady。Unknown的reason为unsupported/no_sample/expired/source_invalid之一，最新attempt另报超时/权限等原因。require_ready+Unknown不能形成成功final result，拒绝结果不携带可使用的decision_id。矛盾组合是ProtocolError，不能降为Unknown后放行best_effort。

`deadline_ms`是发出时的剩余单次请求预算，必须为正且不超过协商CHECK_MS/总D剩余期；OIC按本地单调接收时点设置其检查/预留截止，OIS仍独立执行自己的绝对截止。`remaining_ms`非负、不超过本地样本原validity；Unknown为0且不携带有效sample。收到超时请求不执行新的检查，回应ReadinessCheckFailed；已失效query/check的迟到回应只丢弃，不影响新操作。未知枚举/缺必填字段/当前待决回应同序内容冲突为ProtocolError并关闭控制连接；这不会放行新通道。

`update_seq` 对 attempt、样本、过期/失效变化均递增，使 OIS 能处理“sample 未变但 attempt 更新”。OIS对同一binding只采用递增update_seq；低序回包丢弃，同序仅允许不可变内容一致及正常递减的remaining_ms，不得回退last_attempt/能力/样本展示。不同query的同序回应可用更紧的保守期限，不能以重复回应延长已保存的目录期限。每Exposure最多一个待决目录query；新dirty通知在该query结束后再取最新快照。一次查询只对当前 binding；暴露数量、未完成 query 数量和消息尺寸有上限。没有未完成 query 的回应可按迟到丢弃；绑定与同序内容冲突按协议违规处理，计数并按现行连接错误策略关闭，不让无界错误日志耗尽内存。

sample_seq、update_seq、缓存期限与decision都绑定上述完整binding；不会只在register校验观测版本后，给之后的消息补用当前版本。

admission_result 返回OIC按本地配置与请求取更严格后的effective_admission；OIS把它绑定到操作/Channel，prepare与final继续携带该模式。后续检查只能继续收紧，不能降低已绑定模式；返回更宽松模式是ProtocolError。Preparing期间本地要求收紧可使尚未固定的本次检查拒绝；t_check之后单纯admission_mode变化不撤回已固定决定，作用于后续Open。已Running通道不因准入模式调整关闭。

未配置 probe 的 Unsupported 项不伪造 source_id/contract_version；这些字段仅在已配置来源时必填。其目录能力可直接按登记显示 Unknown，admission_check 仍检查当前本地配置并明确回 Unknown/unsupported。query/check 标识在同一 session 内不得复用，避免迟到响应命中新请求。

对查询回应：令 `t0` 为 OIS 发出时刻，`t1` 为收到时刻，`r` 为 OIC 形成回应时计算的剩余期，则 `safe_remaining = max(0, r - (t1-t0))`，所有时长取本机单调时间。OIS 到真正展示/提交时再扣除从 t1 过去的时间。整数毫秒向不增加有效期的方向取整。重复回应不得从新的接收时间重启 TTL；同样本的一次新挑战只能重新获得其实际剩余期，不能从初始 validity 重建。

OIC 不等待目录确认才能继续归约。OIS 查询拥塞时保存每 Exposure 一个 dirty 标记，轮转补取；初始登记完成也触发查询，周期补取防止通知合并/丢失导致永久不更新。probe、归约、通知、查询与目录写入分别记录时间，不用单个“probe latency”代替整条链。

### 5.2.1 控制与检查初始配置候选

这些是本修订 R1 的确定候选，且与 OIC 本地限制取更严格值。原 T_prepare/Channel额度等 B4 值不变。

| 配置 | 初始值 | 边界 |
| --- | --- | --- |
| 并发目录query / preflight操作槽 | 8 / 32 | 均为正整数且≤N_OIS；每Exposure最多一个目录query；满槽无无限等候队列 |
| 每操作HTTP等待者 | 16 | 超额相同key等待者429，不取消原操作；无key请求独立占操作槽 |
| CHECK_MS / 目录query期限 | 1000 / 1000 ms | 正整数，最大T_prepare；实际检查同时取总D/本地许可等适用剩余期更小者；到期不重置 |
| 遥测单消息 / 单次写期限 / 补取周期 | 4 KiB / 100 ms / 1000 ms | 这些为本修订有限控制负载值；部署先按固定值验证，修改则记录规格/预注册变化，不能静默引入无界批量 |

final检查不与新preflight争用操作槽，每个已Preparing的Channel最多一个待决final，数量受Channel额度限制；保留关键控制队列容量的调度见§7。目录/探针超时不能被当作观测NotReady。

### 5.3 公共响应与权限

保持 `GET /v1/exposures` 的 discover 授权过滤；加响应字段是展示扩展，旧消费者可忽略。候选对象：

```json
{
  "readiness": "ready",
  "readiness_view": {
    "authority": "directory_snapshot",
    "source": "http_status_v1",
    "revision": "rv_...",
    "generation": 8,
    "observation_revision": "ob_3",
    "contract_digest": "<64位小写十六进制>",
    "sample_seq": 12,
    "reason": "matched_ready_status",
    "remaining_ms": 840,
    "last_attempt": {"state": "idle", "outcome": "timeout"}
  }
}
```

- `readiness` 是 OIS 当前可保守证明未过期的目录判断；无可靠剩余界限/过期时为 unknown。`last_reported` 可保留上一次 Ready/NotReady，但明确是历史，不让 UI 用绿色“当前正常”误导。
- source 只显示有限契约类型/有界来源标识；不暴露 probe URL、IP、TLS 路径、原始响应、凭据或其他主体不可见目标。detail 仅进本地受控诊断。
- `access_online` 与 readiness 独立；`Running` 和 attempt timeout 不覆写尚有效样本。JSON/CLI 应能同时说明“样本仍有效、本次检查超时”。
- 本地 `oi discover` 原样输出这些结构；`oi connect` 成败仍来自本次 Open/READY/ABORT。目录 Ready 不保证随后 Open 成功；业务响应不改写成 OI 的成功计数。
- POST 的 `state=preparing/readiness` 明确表示预检时观察，不是最终 READY 承诺；增加 `readiness_stage=preflight`，本次有效模式使用OIC回应后的严格值。`GET channel` 的新增 `readiness_decision` 在完成最终判断后才出现，含有效模式、判断来源/样本号及最终原因，不含敏感目标信息。这样请求返回、最终拒绝、CLI 报错可关联，同一旧快照不会被重新显示为最终证据。
- 不加入手动远程触发 probe、自动等待 Ready 或自动降级参数。需要这些行为须另列权限、频率和期限。
<!-- /sdd-scope:g2-wire -->

<a id="commit"></a>
<!-- sdd-scope:g2-commit -->
## 6. Commit-Snapshot：最终复核时点、失败链及兼容

### 6.1 Commit-Snapshot 推荐：有限证据准入，明确观测判断点

这是建议的新具体契约，尤其下面的第 5 步与竞态窗口需要明确采用，不能标成既有 B1 的无争议实现细节。

1. 消费者 POST：认证、严格解码、discover/open 权限、expected_revision、Access 在线等既有检查先执行；客户端要求与服务器/本地模式只取更严格值。旧代或没有最终检查能力的 OIC 不得到伪造 Ready。
2. 对 v2 接入执行有界 preflight admission_check。OIC 以本地当前有效样本判断；有效 NotReady 两模式拒绝，Unknown 对 require_ready 拒绝，对 best_effort 只允许有界准备。目录缓存不替代此检查。preflight 不会触发目标拨号。
3. 预检成功后重查因远程等待可能变化的授权/版本/Access，再按既有事务保留配额、创建 Preparing、发票据和 prepare_channel。prepare_channel v2 携带 effective_admission；OIC 在实际拨号前再核本地权限/代数/版本/当前观测。客户端请求的更严格模式不能在这一步丢失。POST 的错误优先级与合并/取消按 §6.1.2 执行，不在网络等待期间继续使用旧授权。
4. 对control.v2的Channel，两腿齐全后，OIS不立即READY，发送绑定channel/binding的final admission_check。control.v1的Channel只走原B1提交检查，readiness始终Unknown且仅best_effort能进入Preparing；不发送旧端无法识别的新消息，不假造final decision。检查只读当前样本，不启动探针、不隐式等待 Ready；OIC 同一归约锁/事件序列处理本次最终检查与观测事件。
5. **Commit-Snapshot 线性化点是 OIC 成功处理 final check 的时刻 t_check。** 有效 Ready 形成有剩余期的 decision_id；Unknown 在 best_effort 下形成无就绪保证的有界决定；有效 NotReady 拒绝。t_check 之后的健康变化属于后续准入的输入，不保证能撤回这次已作出的观测决定，即使 OIS 已收到较新 NotReady 的目录更新而尚未发 READY，也不据该异步健康更新改判这个已经固定的 decision；它只影响后续 Open。这个窗口必须对用户和验收明示，不借“无 TOCTOU”字样掩盖与强解释的差异。
6. OIS 收到 final result，扣完整检查 RTT；仍在本地协调者中重查 policy/credential、revision、epoch/generation、Access、总准备期限等既有提交条件。两模式均须在本次 final-check 从发出时起的固定截止前完成提交，不能让已经收到的 Unknown 决定无限等待；require_ready 还要求保守样本剩余期大于零。全部通过后才按既有先提供方后消费者顺序提交/排入 READY。OIS 始终是 Channel 权威，OIC 的 decision 不能单独打开 Channel 或放行字节。存储/维护对新 Open 与既有 Preparing 的边界保持 B3/G4 既有契约，不借本次复核扩大终止范围。
7. READY 之后的健康变化只影响后续 Open；授权撤销、revision/代数变化、许可到期仍按既有规则立即进入关闭，不受上述健康决定冻结影响。目标拨号本身可能有副作用，准备失败不能宣称业务未执行。

本算法只保证 t_check 的本地归约状态及提交时保守 TTL；不保证服务在 READY 到达或收到业务字节时仍健康。若无法接受“t_check 后、READY 前新的 NotReady 不一定取消本次准入”，不能采用 Commit-Snapshot。

### 6.1.1 OIC 本地取消、READY 与字节放行

OIC 的每次 final 决定有本地状态 `CheckedAwaitReady → Running` 或 `CheckedAwaitReady → Cancelled`；状态只绑定当前 channel/binding/decision_id。对该状态的更新、观测配置 reload 及接收 READY 后的放行检查在同一锁/事件序列中处理。

| 本地顺序 | 动作 | 跨组件含义 |
| --- | --- | --- |
| t_check 之后仅出现新的健康样本 | 冻结的 decision 不重算；更新下一次 Open 的观测 | 即便 OIS 先收到新 NotReady，再处理原 final result，也不能用目录快照翻转本次观测决定 |
| observation_revision/source 失效先于本地接收 READY 的放行检查 | 设 Cancelled，取消检查/预留，关闭目标 socket/附着腿；目标载荷 0；发送 prepare_failed/ABORT ReadinessCheckFailed(reason=observation_changed) | OIS 处理通知时关闭仍 Preparing 的 Channel；若通知晚到，OIS 可能已经发送 READY，消费者可先见 READY 再见 ABORT，但 OIC 不能运行字节桥接 |
| 本地放行检查先完成，状态成为 Running，之后 probe 配置改变 | 新配置失效旧样本和后续检查，已 Running 通道保持 | 不扩大 B2 已 Open 健康/准入配置变化的关闭范围；目标/授权/代数/许可变化仍独立执行既有关闭 |
| 冻结 Ready 的本地原期限或 final-check 期限在放行前耗尽 | 不启动桥接，ReadinessRequired 或 ReadinessCheckFailed 终止 | OIS 可能已提交，因 READY 传输迟延而随后中止；不能宣称跨机同时有效 |
| 控制/腿损坏使精确错误不可送达 | 本地取消仍执行，现有传输错误/ChannelInterrupted 向消费者暴露 | 不承诺故障网络下永远收到精确新增错误码；绝不把未知结果显示成业务未执行 |

OIS 在自己的协调者处理授权/目标变化、观测版本更新、期限与 final result；它只能线性化已经收到的事件，不能声称“远端发生在 READY 前的一切配置变更绝不发 READY”。观测配置变更在本地取消与 OIS 获知后的关闭是两个明确时点。以上既没有给业务目标授权增加离线宽限，也没有把 latest health sample 再次加入 OIS 提交条件。G2-07/08 必须用 barrier 分别验证两个本地顺序，而不是依赖 sleep 猜跨机时序。

### 6.1.2 预检顺序、总期限与在途幂等

以下是加入 preflight 后的确定候选，新增在途操作只存内存，没有 Channel/ticket/目标连接，也不改变成功创建后的 B1 Channel 去重。

1. 每个 HTTP 请求先完成身份/凭证有效性、严格请求解码。随后在 OIS 协调者内查 `(subject, client_request_id)`：若已有 Channel，参数 `(exposure_id, expected_revision, request admission)` 不同则 409，相同则按 B1 返回同一 Channel 的当前状态和仍未消费的票据；没有新 preflight。有效主体的原有 Channel 重放不重新申请新通道。
2. 若已有在途操作，同参数请求加入该操作、共享一个 check_id 和最终结果；不同参数立即 409 IdempotencyConflict。没有 client_request_id 的请求各自独立。每操作最多 16 个 HTTP 等待者，超出者 429 ResourceExhausted，不改变原操作；断开的等待者不占槽。相同参数只指客户端参数，不能通过换令牌替换原操作绑定的创建凭证。合并等待者即便属于同一subject而使用另一凭证，返回的票据仍只绑定原创建凭证；另一凭证不能附着（B2 TicketInvalid），这保持B1按subject去重与B2按credential绑票据的既有组合，客户端换凭证要用新的client_request_id。
3. 新操作按顺序执行：OIS受控停止→目标可见/存在(404)→open权限(403)→expected_revision(409)→Access在线(503)→取得pending-check槽(429)。此处只用短锁；无网络等待。新操作绑定发起凭证、subject、epoch/generation、Exposure revision及观测版本。用同一协调者应用它与管理员撤销/变更的顺序。
4. **总准备预算从上述新操作成功分配 pending-check 槽的单调时刻 `t_start` 开始**，总截止为 `D=t_start+T_prepare`。preflight 截止为 `min(D, 发出检查时刻+CHECK_MS)`；final同样从其发出时刻计算并受D限制。等候槽不另排无限队；满则立即429。已创建Channel的 `created=t_start`、deadline=D；POST票据/prepare_expires_in_ms和prepare_channel只携带D的实际剩余时间，不再重新给完整T_prepare。
5. v1 接入不发新消息，立即按 Unknown 应用 C1；v2 发 preflight。结果完成后先在协调者内重新检查发起凭证/主体授权、目标可见/允许、revision、Access/代数与观测版本。凭证无效401；不可见404；无open403；目标revision409；Access失效503；观测版本变化503 ReadinessCheckFailed(reason=observation_changed)。这些已应用的变化优先于迟到positive结果。接着检查总D（超时504 PrepareTimeout），单次check截止/回应有效性（503 ReadinessCheckFailed），本地结果（NotReady503；严格Unknown412），Channel配额（429），最后既有存储可用性（503）。本修订不引入TASK-005尚未合入的维护逻辑。
6. 全部成功时，在同一临界区创建唯一 Preparing Channel、预留Channel配额、生成票据，并原子地把该key从pending操作映射换成既有Channel幂等映射。pending-check槽在此释放，后续final检查由Channel自身唯一待决槽承担、总数受Channel额度约束。最初请求若仍在等待则返回201；合并的同参数请求返回200，使用它们各自响应时的Channel当前状态/票据。即使最初请求已经断开，其余等待者仍可收到200的同一新Channel；不要求必须有人收到201。
7. preflight失败、总期限或单次期限耗尽：在协调者内记录本操作的终态供当前等待者读，释放槽并立即删除该key的pending映射；广播同一个已固定错误，不创建Channel/ticket/目标连接，不保留失败幂等记录。随后同key新请求可以重新尝试。每个操作有不可复用的内部id；旧回调仅能删除仍指向自身的映射，不能删除后来同key的新操作。
8. HTTP取消只移除对应等待者。若首个HTTP取消但还有等待者，检查继续并使用原D和原创建凭证；其余等待者不会得到新的预算。若最后一个等待者在Channel创建前取消，立即取消check、释放槽、删除pending映射，不创建Channel；迟到回应丢弃。若Channel已在协调者中创建，取消HTTP不回滚Channel或重置幂等，沿B1等待附着/超时/关闭。此竞争只有一个协调者内的先后顺序。
9. 每个等待者返回结果前还检查自身凭证；已经失效者只收到401，不泄露成功票据，也不改变其他等待者的结果。若原创建凭证在创建前被撤销，即使其他等待者另持有效凭证，该在途操作仍按步骤5失败；删除后可用有效凭证重新Open。创建后的撤销继续作用于绑定创建凭证的Channel，不被合并请求换掉。

成功Channel的Closed/RETAIN、票据消费及重启失效完全保留B1。所有在途检查和等待者在session/进程终止时失效，无持久Exactly-once；失败重试不会自动重放业务载荷。

### 6.2 超时、错误与清理

| 事件 | HTTP 前阶段 | 已 Preparing/附着阶段 | 不允许的降级 |
| --- | --- | --- | --- |
| 有效 NotReady | 503 TargetNotReady | prepare_failed/ABORT TargetNotReady | 不改 Unknown、不让 best_effort 绕过 |
| require_ready 无有效 Ready，或提交前保守剩余期耗尽 | 412 ReadinessRequired | 新增 ReadinessRequired 的稳定 ABORT 值，0x0107；GET close_reason 同名 | 不编码为 ProtocolError，不自动等待或重试 |
| OIC 检查无回应/未能获得可靠本地决定 | 新码 ReadinessCheckFailed，HTTP 503 | 同码 ABORT，0x0205，有限 reason=timeout/unavailable | 两模式均不能把无法复核误报成“已知 Unknown 可尝试”；也不虚构 Access 已离线 |
| 本次总准备预算耗尽 | 明确 PrepareTimeout 的 HTTP 映射 504 | 既有 PrepareTimeout / 0x0405 | 不在重试每个子阶段时续命 |
| 授权、绑定、在线状态、许可变更 | 保持对应既有码和检查顺序 | 对应 Revoked/RevisionChanged/AccessOffline/LicenseExpired | 不因旧 positive decision 绕过 |
| 缺能力/无共同协议 | v1 Unknown 时严格请求 412；无法协商握手按 VersionUnsupported | 不建不具备安全语义的通道 | 不把 unsupported 当故障 NotReady |
| query/check 重复、迟到或旧代 | 忽略已取消/完成请求；计有限诊断 | 无状态复活、无二次 READY、无 quota 再释放 | 不因迟到 positive 复活 Closed |

已核对基线 `internal/protocol/codes.go`：0x0107 与 0x0205 未占用，本修订将它们确定为上述候选，不修改现有码含义。仅新增 ABORT 原因，不新增 READY/DATA 帧。旧客户端安全中止，但当前 `waitReady`/`Stream.Read` 将未知 ABORT 映射为 ProtocolError；不能承诺旧端识别新码或向应用保留原数值。OIS 仍保留最先确定的准确 close_reason，消费者可通过 `GET channel` 查询；收到旧端回应的 ProtocolError 不覆盖已关闭通道原原因。新客户端在库、CLI stderr/退出码及 GET channel 中交付准确错误。§6.4 的旧二进制兼容向量验证这些限定，不以源码阅读充当兼容通过。

所有终止都作一次性状态转换，取消 pending query/check，作废票据，关闭已拨 socket/腿并释放槽位；不依赖迟到消息配合。控制连接重建立即使全部旧 binding、decision/query/check 无效。decision 不持久化、不跨重启使用。

### 6.3 Commit-Strict 替代：保留更强的跨组件提交解释

如果要求“发生在 OIS READY 前且已被 OIC 归约的新 NotReady 必定使本次 Open 失败”，Commit-Snapshot 不满足。只添加 seq、再查询一次、推送 invalidation 或扣 RTT 也不能保证 invalidation 在 OIS 提交前到达。

可研究有界分布式屏障/预留：OIC 暂存特定版本的准入预留，OIS 提交前建立双方明确的提交/取消关系，并处理预留期间的新观测、Ready 自然过期、控制断线、响应丢失、撤销与重复消息。但“加屏障/预留”本身没有解决最后确认消息之后的新 NotReady 窗口，不能将其写成已可实施且满足强语义的算法。若通过延迟归约新负证据来冻结版本，也必须明确“已观测但尚未生效”的时间和对其他 Open 的影响，不能用排队隐藏已知故障。若把最终 Channel 权威移到 OIC，则进一步改变既有 OIS 权威/撤销线性化，不应作为小实现选择。

Commit-Strict 的产品意义是更强的同时性要求，但增加远程等待和故障状态；须先独立完成状态机/反例证明，不能承诺本草案已经给出完整可实施协议。推荐 Commit-Snapshot 的原因是就绪本来是有限证据、业务可随时变化；若实际使用者要求严格的提交窗口保证，则推翻 Commit-Snapshot 推荐并继续 Commit-Strict 设计。

### 6.4 兼容与升级候选

- OIS 在原 mTLS 控制端点支持 `oi.control.v1` 与 `oi.control.v2`；新 OIC 默认请求 v2。v2 使用现有 envelope 与已有消息，新增字段/消息只在 v2 执行，不寄望旧端忽略未知字段来安全降级。
- 旧 v1 OIC 仍按 P1 行为登记为 Unknown：best_effort 可有界连接、require_ready 412；不会从未存在的 probe 得到 Ready。任何已在 v2 配置 probe/strict 语义的接入不能在重连失败后自动降为 v1。
- 新 OIC 若配置了 probe 或要求 v2 行为，连旧 OIS 必须明确 VersionUnsupported 并停止该会话；无静默 best_effort 回退。是否提供显式 legacy-v1 启动开关可后续评审，本候选不增加它。
- HTTP `/v1` 现有请求保持；响应只加可忽略的展示字段。控制 v2 的新强制语义由子协议标识承载。新错误码保持机器可读、明确重试含义，未知错误不触发自动业务重放。
| OIS / OIC / 消费者 | 控制与Open行为 | 错误展示/支持边界 |
| --- | --- | --- |
| 新 / 新 / 新 | control.v2；preflight/final；HTTP /v1、tunnel.v1 | 识别新增ABORT及目录字段；完整G2推荐组合 |
| 新 / 旧v1 / 新或旧 | control.v1，无probe；Unknown best_effort可尝试、strict412 | 沿既有错误；不从旧端构造Ready |
| 新 / 新 / 旧v1消费者 | OIC仍control.v2执行安全语义；消费者HTTP /v1、tunnel.v1 | 响应新字段可忽略；新增ABORT安全中止但旧CLI/库可能显示ProtocolError，准确原因可GET channel；不承诺旧界面完整诊断 |
| 旧v1 / 新 / 任意 | 无共同control子协议，握手400 VersionUnsupported；不自动回退 | 不登记、不创建新通道；新OIC只请求v2 |
| 旧v1 / 旧v1 / 新 | 仅已有P1语义；新消费者不从缺失字段推断Ready | strict仍412；新消费者兼容既有HTTP/tunnel.v1，不承诺G2来源或导出存在 |

表中“旧”固定为本任务基线的P1二进制，其他版本不自动进入支持矩阵。G2-13用实际旧二进制验证安全中止和close_reason，不需要为仅新增可识别ABORT原因更换tunnel帧协议；如果反证出现，则本推荐兼容判定失败，保留结果并修改下一版规格。
<!-- /sdd-scope:g2-commit -->

<!-- sdd-scope:g2-fairness -->
## 7. 有界调度、独立发布和公平性

- 每 Exposure 最多一个在途 probe 和一个待调度标记；到期任务轮转，不能让同一慢目标的重复 tick 填满队列。全局并发 P、待执行 Q 与最大 Exposure 数共同限制内存。队满不无限堆积；保留/合并 pending 标记并计延迟，不造 NotReady。
- probe 网络调用不持有 OIC 全局配置/通道锁。完成归约是短临界区，然后独立发 dirty 通知；一个慢目标不阻止其他目标发布，也不等待整轮扫描结束。
- 控制 writer 不能把遥测塞进普通 FIFO 后宣称隔离。授权/撤销/heartbeat/prepare/final-check 有独立有界容量；遥测按有限批量出队，不能无限占用 writer。反过来控制持续满载时，也不能宣称观测永不饥饿，须按下面的负载范围验收。
- 候选写入策略为每轮最多 8 条关键控制后允许 1 条已就绪遥测；没有关键控制时正常发送遥测。每条消息写操作有 deadline。此策略是实现候选而非实时调度证明；如果一条大遥测的写入会阻塞关键控制，必须进一步限消息大小/写时限或拆独立连接，后者会增加连接及认证成本，不属于本修订的已选机制，出现反证时再修订提案。
- admission_check 不执行网络 probe，只做本地快照判断；不能因健康接口慢而把 Open 的控制处理挂在 probe goroutine 上。所有挂起检查受独立槽位和总期限限制。
- 严格控制/遥测饱和、持续创建/删除 Exposure 的病态负载不能用无限排队吸收。拒绝或断开要可诊断并释放全部资源，不能把“有限资源”解释成无限可用。
<!-- /sdd-scope:g2-fairness -->

<a id="export-pull"></a>
<!-- sdd-scope:g2-export -->
## 8. 最小导出与观察指标

方案名 `Export-Pull`，对应 DISC-009-R3。推荐在现有 OIC 回环诊断监听器上增加显式开关的 `/metrics` 拉取格式；OIS 使用同等回环/默认关闭的诊断边界。格式候选为 Prometheus 文本 exposition 0.0.4；2026-11-04 已核对[官方格式](https://prometheus.io/docs/instrumenting/exposition_formats/)的 UTF-8/LF、Content-Type、转义及 HELP/TYPE 规则。正式实现仍须用独立解析器核对输出，查询格式文档不是本产品兼容通过。可先用标准库实现有限格式，不自动引入完整 SDK。

本修订 R3 的导出初始配置为：默认关闭，仅显式回环监听；同时抓取2个，响应最多64 KiB，写期限200 ms。这些是待确认初始值，不是SLO。生成完整有界响应后再写出；内容超限返回HTTP503并计数，不发送半份成功指标；并发槽满返回429。正式接口不提供远程监听或无限超时开关。

- 抓取时先复制有界快照，网络写出不持有状态锁；限制同时抓取数、响应大小和写 timeout。慢抓取超时或过载拒绝/截断必须记录有限计数，不能生成无界重试。
- 标签只用固定有限的 result/reason/protocol/stage 枚举。禁止 exposure_id、channel_id、subject、URL、IP、sample_seq、错误文本作为指标 label；详细关联保留在已授权目录/本地诊断。指标名使用单一单位，duration 指标输出秒、size 输出字节；配置仍用毫秒，不能混写。格式与单位命名参照[官方命名建议](https://prometheus.io/docs/practices/naming/)，是否采用该导出格式仍是 C 组提案。
- 计数/直方图覆盖：probe 调度等待/attempt 结果与耗时、sample 产生/过期、dirty 合并、快照查询 RTT/发布可见延迟、最终检查耗时/原因、Open 模式/结果、关键控制耗时、导出超时/拒绝/丢弃。明确 counter reset 的进程作用域，不把重启后的数值下降解释为业务恢复。
- O1 用真实链记录请求→预检/准备失败/READY→第一原生响应；授权拒绝、就绪拒绝、拨号失败与业务响应分开。无提供方证据不统计“业务成功”。
- 可选主动导出的替代需同时确定收件地址许可、TLS/凭据存储、queue/batch/retry/backoff 上限、关闭 flush 截止及丢弃规则；不能把阻塞网络写放到 probe/授权路径。
- 本推荐最新状态合并及拉取意味着不保存完整历史。若历史不可丢失成为产品要求，须新增独立可靠记录契约，不扩成隐式时序数据库。
<!-- /sdd-scope:g2-export -->

<a id="acceptance"></a>
<!-- sdd-scope:g2-acceptance -->
## 9. G2 / LAB-005 预注册候选

以下是具体可讨论的实验配置和判定候选，尚未预注册/运行，不是默认 SLO、容量或 1C1G 承诺。采用后仍须在首次 run 前固定源码、依赖、OS/CPU/资源、拓扑、重复、故障驱动、时钟测量点和原始证据格式。发现预算不合理应保留失败、修改下一版预注册再运行，不可回改旧判定。

### 9.0 初始配置与实验值的关系

初始配置分别唯一维护于 §4.3（Probe-HTTP/R2）、§5.2.1（Commit-Snapshot控制/R1）、§8（Export-Pull/R3），随对应组采用，不让某组实施暗中依赖另一组尚未确认的默认值。下表是独立实验覆盖值；它们可以比产品候选短/小以制造竞态和饱和，不能反过来冒充生产SLO。真实完整G2验证依赖三组采用。

### 9.1 配置与理由

| 参数 | 首轮实验候选 | 理由/适用限制 |
| --- | --- | --- |
| probe worker P / 执行就绪队列 Q | 2 / 4；最多 16 个实验 Exposure；每项另有一个到期待调度标记 | 队列满时保留有限标记，轮转补入空槽，不复制排队项；可强制队满并验证公平，不能据此宣称生产容量 |
| interval / timeout / jitter / validity | 500 / 200 / 50 / 1500 ms | 同时容纳数轮正常采样并快速构造到期；慢目标强制超过 200 ms；使用 barrier 而非纯 sleep 制造竞态 |
| probe header / 预读缓冲 | 8 KiB / 4 KiB，响应正文不存储、不额外drain | 验证响应有界；实用健康接口兼容性留给后续真实样本 |
| 每 Exposure 待发布快照 | 1；dirty 通知合并；补取周期 500 ms | 显式限制历史积压；观测新变化不等待整轮探针完成 |
| 并发目录 query / admission check | 8 / 16；check 单次上限 500 ms | 可制造队满；检查仅内存判断，不因 probe 慢而耗满预算 |
| T_prepare / T_close / H / L | 2 s / 1 s / 1 s / 3 s；许可余量 0.5 s | 复用 B4 已有向量值，新增阶段纳入同一总预算 |
| 遥测单消息 / writer deadline | 4 KiB / 100 ms | 限单次占用；关键控制与遥测共用连接仍有成本，需实测 |
| 指标抓取并发 / body 上限 / 写 timeout | 2 / 64 KiB / 200 ms | 用不读响应的客户端检验阻塞清理；不持状态锁 |
| 主动故障轮数 | 各确定性竞态至少 20 次；时序/饱和场景 60 s × 3 | 前者验证状态序列，后者观察短时公平；不是尾部概率/长稳可靠性证明 |

所有持续时间同时记录实际值，不把配置 deadline 误当测得耗时。调度、发布与控制的判定测量需独立于被测 worker；同进程测点使用单调时间，跨组件以 challenge RTT 或明确同机测量方式建立边界，不比较未校准墙钟。

### 9.2 必测场景与判定

| 向量 | 输入/操作 | 判定候选 |
| --- | --- | --- |
| G2-01 / TV-B1-19 准入矩阵 | Ready/NotReady/Unknown × 两模式，含本地/请求收紧 | Ready 仅继续准备；NotReady 两模式拒绝；Unknown best_effort 有界尝试、严格 412；不存在降级绕过 |
| G2-02 attempt/sample | 旧 Ready 尚有效，开始慢 probe、超时、再过期；NotReady 同样轨迹 | Running/timeout 不抹旧证据、不续期；expiry 到 Unknown；新明确样本才替换 |
| G2-03 顺序/绑定 | 重复、低 seq、同 seq 不同内容、旧 generation/revision/source 回应；重连 | 不复活过期/旧代/旧目标；内容冲突按协议规则处理；重连先 Unknown |
| G2-04 查询 TTL | 延迟 query/response、非对称延迟、缓存等待、重复回复、墙钟跳变 | 按完整 RTT 和后续本地时间保守扣减；任何延迟不能延长样本；墙钟不影响有效期 |
| G2-05 最终复核 | 两腿到齐前后用 barrier 使 Ready 到期或 NotReady 先于 t_check 生效 | 不发 READY、准确错误、无目标载荷；已拨连接按 T_close 清理 |
| G2-06 Commit-Snapshot 窗口 | final 结果生成后挡住回程，再产生 NotReady；解除阻挡 | 记录 t_check 与 OIS commit；按已确认 Commit-Snapshot 可提交旧决定，但不能把结果写成“覆盖提交前全部最新观测”；TTL 到期仍拒绝。若选 Commit-Strict，判定必须相应改为拒绝 |
| G2-07 权限并发 | final 后至 READY 前撤销凭证/策略、换 revision/代数、到许可期限 | 既有安全行为不被 Commit-Snapshot 窗口放宽；不得提交或继续未经授权流量 |
| G2-08a 消息与回收 | 检查超时、丢响应、重复/迟到结果、Closed 后 positive、pending 槽满 | 无二次 READY/状态复活/重复释放；终止后 T_close+1 s 槽位、票据/通道回到基线；错误贯通到 CLI/GET channel |
| G2-08b 观测版本先取消 | final成功后挡住READY；OIC先应用observation_revision变化，再收到READY；另反转OIS收到register/旧result的顺序 | OIC Cancelled且目标载荷0；允许消费者短暂READY后ABORT；OIS获知后终止旧Preparing/已短暂提交Channel，不覆盖第一终因；不声称远端配置变化瞬时阻止OIS发READY |
| G2-08c 本地先放行 | OIC先处理READY并进入Running，再更换probe配置；保持目标/权限不变 | 已Running通道继续；旧样本/check失效，后续Open使用新观测版本；不把probe更新扩成B2 target revision |
| G2-08d 在途幂等/HTTP取消 | 同key同参并发（同/异credential）、不同参409、首HTTP取消但仍有等待者、最后等待者取消、创建前后取消竞争、失败后同key再试 | 每操作一个check/最多一个Channel；期限不续；失败立即释放key；旧回调不能删新操作；创建后同Channel/票据/RETAIN保持B1且不换绑定credential；异credential附着401；无无主pending槽 |
| G2-09 独立发布 | 一目标响应持续卡住，其余健康/负样本正常完成 | 健康结果在慢目标结束前到达 OIS；归约→目录可见 <=500 ms（无注入控制网络延迟场景）；各段原始时间独立记录 |
| G2-10 饱和公平 | 16 目标轮转，其中多数每次耗满 200 ms；持续 60 s；重复 3 次 | 不重复堆积同目标、不超过 P/Q；每个持续到期目标在 2500 ms 内获得一次执行机会；无永久饥饿。该界限仅此固定负载 |
| G2-11 控制/导出竞争 | 满 probe/dirty 队列，两个不读的 scrape，连续正常 Open/close 与显式撤销 | 控制不会等 probe/导出完成；heartbeat ack 和已接收撤销处理各 <=500 ms；既有许可/撤销零容忍行为保持；没有误续许可 |
| G2-12 exporter/标签 | 持续更换大量 target/channel/subject，慢读/断开 scrape | label 集合大小不随实体数量增长；响应/并发/队列受限；拒绝/丢弃可观测；网络写无全局锁等待 |
| G2-13 兼容 | 新/旧 OIS/OIC/client 受支持组合；旧端收到新ABORT；v2缺字段/未知安全枚举/未知contract_version/越namespace/旧check_id | 不静默忽略安全语义，不从v2回退；非法消息无新状态/拨号；旧v1 Unknown规则保持；新增ABORT安全终止，准确原因可查询 |
| G2-14 端到端诊断 | discover Ready/过期/timeout-history，严格失败、尽力成功后业务失败 | 目录快照、准入、READY、业务结果分开；无地址/凭据泄露；O1 分组可重算 |

500 ms 等实验迟延线是首次候选，为识别明显控制阻塞并留出 CI 调度噪声；它们没有现有性能数据背书。若托管 runner 无法隔离噪声，应先更换/记录可复现条件，或对下一轮修改实验预算，不能事后排除失败样本。

### 9.3 证据出口

- 功能向量在 SQLite 与 PG 共同后端条件跑相同 OIS/OIC/消费者；观测本身仍是有界内存状态，不引入数据库持久化或恢复旧 Ready。
- G1 适用回归、完整产品单测 >=95%、race、静态/安全检查沿用已采用要求；G2 触及 READY/版本/错误行为时不能只跑新包测试。
- 新控制 decode/state reducer 添加有意义的模糊及性质测试：序号/TTL 不能复活，错误输入不创建未授权状态。无需为草案本身编造测试结果。
- manifest 在 run 前提交；每次 fail/pass 都保留固定源码/环境、原始计时、判定与回收记录。每个 G2 criterion 对应非空真实 vectors，不能用模型行或已有 G1/G4 结论冒充。
- 实验后 raw 重算、独立复核、综合分析、产品文档/时序与状态图/Issue/ITER/gates 联动完成后才评 G2；发布和采用仍按正式任务授权执行。
<!-- /sdd-scope:g2-acceptance -->

