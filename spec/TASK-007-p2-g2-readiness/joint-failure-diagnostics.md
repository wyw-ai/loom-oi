# G2-11 联合输入失败诊断：实施规格

基线 `f54f4e7164c0c301ec452245368668c7eb8da4d0`；业务依据为固定 `3be014a` 的唯一运行及 `coverage/g2-periodic-work-linux-01/query-phase-review-01.md/json`。本规格先提交、推送并逐字回读，再编码；实际双后端运行另按固定环境计划执行。只补原失败记录，成功事件/判线不变；保留原512历史、32 Drop、3s、connect+1s、200/300ms young、200ms exporter、500ms控制、完整60s/20cycles及原清理期限。无新增请求、轮询、sleep、连接、重试或谓词。

## 1. 原 Stats 请求的边界与512条窄历史

扩展 `g2PreparationRead` 两个可空固定对象 `ois_call`、`oic_call`，并原样投影到 `g2PreparationSample`。单个对象字段：

- `started_ns`：紧贴原实际 HTTP 调用前；请求尚未调用（构造/processAlive失败或前角色失败）为 null 对象。
- `returned_ns`：原 HTTP调用、body读取/关闭、解码/校验路径实际返回的时刻；尚未返回为 null。不能用整个双角色 helper 返回时刻替代 OIS 的边界。
- `context_deadline_ns`：原调用 ctx 的真实 Deadline，转为同 resourceOrigin 单调坐标；没有为 null。
- `context_state`、`error_class`：复用现有有限分类，在原 cancel 前捕获。请求已开始但没有正常返回时分类为 null，不能填 none。

OIS仍依次 `processAlive → statsRequest(context.Background(), g.stats, ...)`，OIC仍原1s context及 `hc.Do → ReadAll/Close → status/decode`。OIS的 http.Client 超时不是 Background 的 Deadline；错误可以是 deadline/timeout而context仍active。原 scheduler 合法性检查失败归双角色 helper，不伪称已经成功取得的 OIC HTTP 调用失败。所有错误仍沿原位置 Fatal；原 failure callback 必须在原 cancel 前冻结。

`g2PreparationOIS`、`g2PreparationOIC` 各加两个可空标量：`exporter_active`、`exporter_youngest_active_age_ns`，只复制该角色原 Stats.Observation 中的值。未取得 Stats/Observation 时为 null；实际0仍是0。字段不参与 Full/young/短路。沿原角色PID/epoch/gen与原样本区间绑定，不把两串行角色当同一瞬间。

仍是预分配512条窄记录，不复制完整Stats进历史、不另建数组；容量满仅置原overflow/incomplete，不增FAIL/改变原返回。`last_preparation`仍保留原最后完整/部分 Stats。不得从最终 wire 回填旧 query，或用 exporter timeout 累计值识别某条200/429写的生命周期。

## 2. critical失败只保留原来已发生的动作

`competition_failed_cycle` 增加一个固定、可空 `critical` 对象；未到critical为null。对象内部只包含：

| 字段 | 来源/空值规则 |
|---|---|
| `cycle_deadline_ns`、`input_deadline_ns` | 原首socket+1s与原min(prepareD,cycleD)，直接保存已算出的D，不新建ctx |
| `ois_pid`、`oic_pid` | 原进程对象内PID，纯内存读取；连同已有round/cycle/role与原baseline核对 |
| `before_stats` | 原一次critical起始Stats的 `g2PreparationRead`，含部分失败/两个role边界；未调用null |
| `after_stats`、`after_stats_attempts` | 原“实际撤销已应用”循环最后一次Stats读及原调用次数；不增加请求/历史数组，未进入为null/0 |
| `native` | 原唯一 request 的 `started_ns`、真正正常返回的 `returned_ns`、channel_id；原bg Deadline为null。若内部操作先失败，另存 `error_observed_ns`、有限 `phase=dial/write/close_write/read`、error_class/code/context_state；helper经Fatal/Goexit未返回时 returned_ns必须null |
| `revoke` | 原外层开始/真正返回、`config_get`和`token_delete`两个固定原调用记录；每项为原started/returned、ctx Deadline/state、HTTP status、code/error_class；未调用null。当前必须保留原GET→单次DELETE顺序，不优化、不重试 |
| `provider_wait`、`control_wait` | 原两个barrier Wait的实际开始/返回、原inputCtx Deadline/state和有限error；未执行null。成功仍用原wire见证，不新增Snapshot/Wait |

保留现有 `critical_query_witness`，不把新诊断当成功witness。两个原Stats包装可返回原部分 read，再由本地薄包装在相同错误点 Fatal；原请求顺序、contexts、HTTP客户端均保持。native可复用/窄扩现有 `g2FailureRequest` 的“原错误返回后、原stream.Close前”callback接缝，但不能为记录而重放请求；成功仍经历原 Close 后再记录真正返回。panic/Goexit的defer不得把默认返回值记为成功。

冻结顺序不能只依赖目前Cycle外层defer：它注册较早，现有 `controlGate.Cancel`、`providerGate.Cancel`、`inputCancel` 反而先执行。最小接法是在**每个原cleanup defer的原注册位置**包一层：若 `!cycleReturned || t.Failed()`，先幂等 `freezeCritical`，随后调用原同一个Cancel；不挪原注册位置、不合并/逆转两个barrier或ctx的清理顺序。最先执行的包装保存完整局部事实和当时inputCtx状态，后续包装不覆盖。`cycleReturned`仅在原成功末尾置true，所以Fatal/Goexit/panic均会先冻结；包装不recover，panic继续沿原外层recover/re-panic路径传播。若未先观察到具体error，有限分类记interrupted/unknown，不以err默认nil造成功；由外层原机制记录panic事实。

在更早的原错误返回点（Stats失败、native内部失败、GET/DELETE失败、Wait返回错误）仍先冻结再Fatal。native的stream.Close在其内部defer，故须用现有失败callback/同注册位置的before-close包装；不能等Cycle外层。Stats沿现有before-cancel错误callback；异常退出也在同原cancel位置先冻结、再cancel，不移动ctx创建或截止。只记录实际调用过的字段，未返回不能合成returned。正常成功不得为了诊断提前freeze。

清理后仅原一次 `competition_failed_cycle` 发射，清理错误不覆盖首事实。新的return/ctx状态不能在cancel之后补造。成功 `competition_cycle` 既有字段、原完成/失败判定均不改；不另开collector/schema豁免或正式通过入口。

## 3. 小范围可验收测试

建议在现有 `g2_failure_diagnostics_helpers_test.go` 或一个明确的新 `g2_joint_failure_diagnostics_helpers_test.go` 中补三组纯辅助测试，真实HTTP接缝使用既有受控RoundTripper，不跑六叶业务：

1. 两role原调用恰各一次及顺序；成功、OIS失败、OIC Do/部分body失败；独立起止/缺角色null；callback看到原ctx active，函数返回后原cancel生效；Observation缺失不变成Active0。
2. critical在before Stats/native/GET/DELETE/两个Wait处中断时，已经发生的对象保留、后续null；正常返回与Fatal/Goexit/panic区分；沿实际defer注册顺序验证先freeze，再原inputCancel→providerCancel→controlCancel（以真实注册栈为准），ctx在freeze时仍是原状态，后续取消不覆盖；原stream.Close也晚于对应freeze。只emit一次，不改变原cause/调用次数。
3. 512边界沿原测试扩展：多出的标量/role边界随同一sample保存，无跨轮/跨角色串配，overflow不改变原谓词；success event保持原字段集合。期限检查使用原D，不因诊断生成新deadline。

测试库存以实现后真实名字登记；本稿不预称通过。静态/辅助验证后由 root 决定唯一固定源实际运行。旧失败记录及同源分析器原件不回填、不重标。

## 4. IfMatch DELETE 省GET：仅评估，推迟实施

现有 `newToken` 返回 `TokenCreated.ConfigVersion`，来自 `Hub.CreateToken` 成功 `write/ApplyConfig` 的提交后版本。`newG2Env.finishSetup` 已用 `e.consumer(alice/carol)` warmup；当前每cycle的victim token创建之后，Lookup/Dial、health.Set、metrics、Stats、原native使用已缓存alice客户端，不含另一个配置写。后台 `UpsertRegistry/AppendAudit/RaiseEpoch` 不递增 config_version。每cycle是顺序执行，下一次token创建在当前撤销之后。

因此当前隔离夹具正常路径可用该token的创建版本执行**同一原 If-Match 的单次DELETE**，省去 `env.version()` 的GET；仍由server/Store原子比对版本。其条件是：创建成功且版本>0、对应同token/本轮、warmup缓存已存在、期间无其他admin配置mutation。若未来出现第三方配置写或缓存缺失触发新token创建，旧版本应真实409失败；不得刷新版本/重试或改无条件删除。无须额外GET证明条件，否则省GET失去意义；可用夹具结构和一次请求计数反例验证。

这是业务调用序列变化，不能混入本轮只诊断改动；本轮仍保留GET与DELETE，并分别记录其实际耗时。未来若单独采用，应保持同原ctx、HTTP返回/首终态/期限，仅删GET；用“无插入配置写成功、插入配置写409且DELETE仅一次、创建响应错轮/版本拒绝”验证。它最多消除一次控制API读取/往返，不保证回收时间或修复500ms写超时，也不能解决Full/query/exporter联合相位。

## 实施路径与出口

- `tests/integration/g2_failure_diagnostics_test.go`：原role调用边界、history两组标量、有限critical记录与before-cleanup接缝。
- `tests/integration/vectors_g2_competition_test.go`：只将原critical本地事实接到失败recorder；原请求、GET/DELETE、Wait、判断、成功raw保持。
- 既有 `tests/integration/g2_failure_diagnostics_helpers_test.go` 或新增 `tests/integration/g2_joint_failure_diagnostics_helpers_test.go`：上述辅助反例。实现时选择一个最小位置，不复制已有大fixture。
- 不改产品、协议、metrics、通用env.write/request、现有成功parser判线、manifest/旧证据。后续独立诊断读取器只读这些有限失败字段，不承载新的通过判定。

本轮只补原观察的保存接缝，保留现有产品源码、成功分析器与原输入顺序。先运行完整现行184辅助节点和新增诊断节点，再执行integration vet；无变更的Python分析器/产品包沿用已记录结果，不移作新来源通过。最终实现提交、精确远端回读后，在单独固定的原生Linux隔离计划内执行同源SQLite/PostgreSQL各三叶。此次不再次在相同TCG环境盲跑，不将环境差异视为已证实根因。仍须原严格业务/资源解析和独立复核，任何失败保留；不修改正式门槛或合入alpha。

实际开发验证按[原生Linux计划](native-g211.md)准备；两份规格共同先行固定，不更换测试入口。
