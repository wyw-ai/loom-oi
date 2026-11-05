# Janitor 周期待办实体化：实施与验证规格

本规格承接已采用 G2 §5.2 的周期补取与现行 dirty/pending 统计口径，属于现有授权内的记账实现澄清。基线为 `cc653e0554c57fd13857e74e431e011c4e219dfd`；[上一轮六叶失败](late-slow-input-linux.md)保持原判。本规格先提交、推送并逐字回读，再编码；目前尚无本变更的实现或实测结果。

## 一、依据与范围

已采用 `docs/architecture/p2/g2-observation.md` §5.2 明确“周期补取防止通知合并/丢失导致永久不更新”。现 `pumpQueries` 本已把 `!dirty && refreshAt<=now` 当成可取工作，但槽满时不进入循环，周期工作未实体化为源级 dirty。拟把这项已经到期的工作在 janitor 记账，不创建新的发送资格、不直接扩 Stats 并集、不在 timeout 后强制重试。

只改 `internal/ois/relay/observation.go` 与 `control.go` 的必要位维护；新增私有 `directoryObservation.periodicOnly bool`。不改配置、消息、OIC、scheduler、query参数、ID生成器、writer、Stats公式、指标HELP或G2判线。

## 二、唯一新增状态转换

在 `sweepObservations(now)` 原 operations/checks/query超时回收完成后，原 `pumpQueries(now)` 之前，遍历当前有界 exposure 集合；仅对 binding非空、Supported、所属Access online/registered且session为v2、无pending query、当前!dirty、`!now.Before(refreshAt)` 的源写 `dirty=true, periodicOnly=true`。当前合法绑定仍沿原登记/代数不变量，不新增第二套协议校验或重建queryOrder。

使用**同一个传入now**；不得调用新的 `h.now()`、更改refreshAt、申请ID/槽、发消息、增加goroutine/队列/定时器。shutting时保持原不发送，建议materializer亦直接不新增工作。遍历顺序不参加分配，query排序与游标仍仅由原pump控制。其成本为原受配置上界约束的 exposure 数量。

原三种 dirty 写入——首次/新binding登记、有效Changed、sendTelemetry拒绝——都同时令 `periodicOnly=false`。allocation同时清两位，再沿原now更新refreshAt及query D。onChanged仅在 `dirty && !periodicOnly` 时加原DirtyCoalesced；之后置正常dirty。旧/同序通知的提前return完全不动。

samebinding重登必须复制整个旧 observation，包括private bit；不能在复制之后无条件清位。新binding只保留原history，初始dirty是正常来源。失联仍沿原endSession/dropQuery：不新增普通dirty清零、不从旧会话发query；offline源不再实体化周期工作，新的代数/绑定不会继承旧provenance。现失联也会保留普通dirty，不能顺便改成另一种离线Stats语义。

## 三、旧新行为等价的边界

`refreshAt`当前唯一写点是allocation，且当场清dirty；这保证periodicOnly代表“旧代码在相同now本来就能周期取”的源。在槽满时实体化不会调用ID；有槽后，原pump在相同游标/now对旧新两状态选择同一候选、同一ID及deadline。

计数关系为：新代码 `dirty && !periodicOnly` 等于旧真实dirty。纯周期工作之后第一次有效Changed原本从false变true，不加coalesced；第二次才加。登记/拒绝产生的旧dirty虽不是纯通知，也必须保持原计数结果，不顺带重释HELP。既有DirtyMarked/ObservationBacklog会因新增真实待办自然变化，这是本候选**唯一预期可见差异**；其它协议、目录、计数/错误输出均须等价。

QueryMS/RefreshMS三类只作为合法测试输入：250/500、500/500、750/500ms。前者到query截止但未到refresh时不能标周期dirty或更快retry；相等时先原timeout回收再原到期分配；后者refresh先到但pending仍在，不能标/发第二条同源query。实际G2的500/500不变。

必须覆盖真实stale-now接缝：janitor已捕获t=500，等待Hub锁期间另一路让h.now=800；源refreshAt=650且无pending/dirty、有空槽。正确materializer和旧pump(500)都不发query。若错误改读h.now，便会先置dirty再让旧pump(500)申请ID，导致错误的started/deadline；该变体必须被确定性反例抓住。另覆盖捕获now后其它路径已allocation、刷新refreshAt/保存pending，旧janitor不得再次标记。

生产control handle在h.mu内重读now，唯一janitor顺序运行。不能拿任意人工倒退时钟宣称真实协议变化；但也不能据此漏掉上述可达的陈旧sweep参数。

## 四、最小验证范围

拟新增四根，分支如下，均使用现有manual clock、observed fixture和有界fake telemetry，不使用sleep或OS跳钟：

- `TestObservationPeriodicWorkMaterialization`：到期/未到期、槽满、pending、已有正常dirty、offline/unregistered/v1/unsupported/missingbinding/shutting。只确认真实周期工作变成一个源槽，重复janitor不重复申请。
- `TestObservationPeriodicWorkTraceEquivalence`：三种Q/R、早Snapshot、恰截止/迟Snapshot、低update、同binding/新binding登记、断线后新代、sendTelemetry拒绝、callback截止前/恰界/之后。对照cc653e固定旧路径，不用当前实现反推期望。
- `TestObservationPeriodicWorkDirtyProvenance`：周期后两次Changed、登记/拒绝后Changed、被忽略旧通知、同binding复制和新binding重置，逐事件核原coalesced。
- `TestObservationPeriodicWorkUsesSweepNow`：上述两个真实陈旧sweep参数反例。

差分比较实际消息的完整binding、query ID及消耗顺序、pending map、queryOrder/cursor、started/deadline/refreshAt、callback返回/编码remaining、目录内容及原RTT/合并计数。测试可在无真实网络的两个“同一虚拟session的备选实现”中，使用只在测试内部复制的初始ID生成器状态，各自独立counter；逐query ID及最后额外一次Next对比防隐藏消耗。不能在真实会话共享生成器，也不为测试增加产品注入点。若该方法不能无侵入实现，先报具体限制。

保留并运行已有 `TestObservationDirtyNotificationAndSessionFence`、`TestObservationQueryCancellationAndLateExpiry`、`TestObservationIdentityCapacityAndQueuedExpiry/queued_query_original_deadline`、`TestDirectoryQuerySlotsAndRoundRobin`、`TestDirectoryRTTExpiryAndHistory`、`TestDirectoryOldUpdateAndConflictingSample`、`TestObservationRevisionChangesCancelOnlyPreparing`、`TestObservationBacklogCountsSourceSlotsOnce`。它们覆盖既有身份/TTL/取消与原Stats公式，不重复造通用admission框架。

固定旧oracle源码及SHA独立保留；旧源码应能给出同一wire/counter序列，但不能给出新增周期待办的Stats标记。负例至少能拒“周期work当Changed重复计数”“改用h.now”“timeout即dirty”“samebinding丢periodicOnly”“拒绝后仍periodicOnly”“分配后不清位”。不得只写镜像布尔断言而不执行query/callback路径。

## 五、可推翻条件与出口

只要旧新差分出现新增/提前/延后query、不同binding或ID消耗/顺序、原D/剩余期/RTT outcome/DirtyCoalesced变化，即停止该实现并报告真实差异，不能用新helper期望覆盖它。若需要改Stats公式、timeout更快retry、query排序、G2任何预算或计数窗口才能继续，也不在本候选内。

本计划不保证G2-11输入联合条件会成立。局部确定性测试和完整relay回归后，仍须固定新源、同源实际业务及原严格解析；cc653e六叶失败与所有历史/门槛保持，不因规划或局部PASS晋级。

## 六、实际验证与结果边界

确定性测试中的两套 ID 状态仅代表同一虚拟 session 的旧新备选执行，不用于生产共享或跨 session 复制。基线 `ControlIDs` 含只读 AES block 与独立 uint64 counter，无 mutex；测试顺序执行，两份 counter 独立，比较完整 query ID 和最终额外 Next。无需修改产品 ID 生成器。旧 oracle 从上述固定提交提取，保存原字节及 SHA，独立审查差分执行是否覆盖真实旧路径。

本地先运行新增四根、已有八根及完整 relay 包单测，使用 Go 1.27.3、count=1；保存实际测试树、退出状态、全部修改文件的前后 SHA。执行适用 vet 和格式/结构检查。无论统计是否更易达到 G2 联合输入，均不缩减原 helper、原六叶或原判定；解析器及夹具沿用 cc653e 的原 3s/1s/200ms/300ms/32Drop/60s。旧分析归档及旧正式 run 不变。

在最终实现来源固定、推送回读后，使用既有独立 Linux VM 的同一工具链/PG/tmpfs，预注册以下完整五步开发反馈：

| 顺序 | 实际步骤 | 原单步预算 |
| --- | --- | --- |
| 1 | 完整 `./internal/ois/relay` 单元测试，独立 partial coverprofile | 1800s |
| 2 | 完整 `./internal/ois/relay` race | 2400s |
| 3 | 全仓 `go vet ./...` | 900s |
| 4 | 现行 G2 准备辅助库存全部 184 节点 | 240s |
| 5 | G2-11 SQLite/PostgreSQL 各三叶、严格业务/输入/资源解析 | 1200s |

沿用共享 integration 5700s、guest parent 9000s、宿主 9030s 以及 Go 90m、p=1/count=1 的现有上限；不得重启已开始的绝对 D。步骤失败继续收集并执行原定后续独立步骤，超时与源不符沿原监督失败路径处理。新 driver 和 inventory 在最终源码固定前不填写来源，不运行真实子进程；准备自检与实际产品测试分别记账。运行前核对旧 runner/child 身份、真实进程组、当前 UID/cwd/exe、磁盘、VM/PG 身份和固定工具链；部署后再做 prepared 核验。只收回原件并按实际首次结果分析，不以重新采样替换失败。

新增遍历为每次 janitor 一个受当前配置上界约束的 O(N) 扫描及每源一个私有布尔位。相同输入/时钟下的逻辑等价不代表真实 CPU、锁持有时间或调度开销为零；上述实际业务和后续完整 Load/资源检查必须继续反证代价。此五步只是开发反馈，不含完整质量分母、G2-12、fuzz、漏洞/静态扫描、Load 或 clock 正式运行，不替代完整 ≥95% 单测门槛与 G1→G3/G4/G2 的同源正式复证。

若差分证明失败，先保存反例，停止依赖该候选的产品运行并在现有范围排查；只有确需改变对外契约或验收时才提交新的具体选择。局部 PASS 不使 G1 Failed、G2 Blocked、G3/G4 Inconclusive 晋级，不合入 alpha，不改 main。
