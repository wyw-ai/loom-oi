# G2-11 同次 Stats 的 pending 身份诊断

承接[固定33f实际结果](native-fresh-query-result.md)及[四单结果回读](../../docs/issue-history/2026-11-06/TASK-007-fresh-query-native-published.json)。本次是既有授权内的窄调试：补同一次Stats的真实query身份，不修改产品调度、准入、权限、HTTP响应或G2判线，不把新诊断当成功输入方案。先提交并逐字回读本规格，再实现。独立只读评审为本地 `coverage/g2-pending-identity-plan-01/{runtime,recovery}.md/json`；没有新的人类采用请求。

## 已知反例与唯一新增问题

33f有两个round0完成233cycle及各自60s/B4，另外四叶在原pre-Dial 3s失败。527次Full后的实际前缀均遇到超过80ms的query。首末Full采样时刻之间，更宽的wire必要条件只有4/5/4/7个fresh来源；加快Stats不足以改变这段轨迹。初始与晚段仍有年轻历史query候选，邻近实采不Full，过渡盲区保留。

现wire的Written是代理写返回，不能证明OIS已归约Snapshot；现Stats只有pending计数，不能说明同次锁内的八个owner是谁。新增诊断仅回答：**该次Full对应哪些真实pending、原始年龄/剩余期限、当前binding/session及dirty/refresh状态；它们与最新历史八条是否相同。** 不重做已完成的wire分析，不从跨时刻样本拼接资格。

保留原两条不读scrape、Active40之后再串行五次429/40ms、32Drop/16Set、P2Q4、80/200/300/500ms、原helper1s/prepare3s、末Set起60s和资源2s。受控读及资格重叠不属于本次；不重置query的Received、started、deadline，也不挑有利backend或round。

## 服务端接缝与默认关闭

复用已有 `log_level=debug`、已有已认证 `GET /v1/admin/stats` 与现有JSON日志。内部关联header固定为 **`X-OI-Diagnostic-Sample`**：恰一个值，恰32个小写十六进制字符。缺失、空、重复、逗号拼接、大小写/非hex/长度错误均忽略诊断，仍按原HTTP语义响应；不回显非法原文。header不赋予权限，不新增endpoint、公开Stats字段、配置项、服务器去重表或稳定支持契约。

只有原admin认证成功、debug Enabled且header有效，才进入可选诊断路径。匿名、consumer、错误方法或其他endpoint不产生该事件。普通路径保留 `Stats()`，不额外读取诊断now、复制数组或编码。可窄提取共享的原锁内统计核心，但不能先Stats()再另锁取身份。

在**同一次原 `h.mu` 临界区**计算原counts，并用一次 `h.now()` 按值复制有限身份。不得为诊断调用pump、expire、drop、更新dirty/refresh、分配query ID、控制消息、probe或数据库操作。排序、JSON编码及日志必须在解锁之后。Process/atomic统计与ExporterActivity原本在解锁后取，保持原顺序，不能把整个HTTP响应或OIS/OIC双角色称为原子快照。

使用固定debug事件 `observation_pending_snapshot`，内部schema `g2-pending-identity-v1`，一请求最多一条。同步写在原HTTP响应前，不增加worker、ring或后台落盘。现slog共享handler/sink仍可能间接阻塞其他日志及控制路径；锁外日志不等于无扰动。成本进入原HTTP/prepare D，不扣除、不续期。sink阻塞、写错误、缺失或迟到日志均保留原失败，不能靠异步输出隐藏时间。

## 有限快照

pending最多8条、Supported source最多16条、query_order最多16项；这是本次诊断的记录上限，不是产品配置上限。使用硬cap切片/固定数组，在锁内只复制有限值，不先复制或排序无限集合。完整总数仍取原Stats，不裁成8/16。超限时明确 `complete=false` 与有限reason/省略数，可省明细；不分页补拍，不影响原HTTP状态。

| 内容 | 实际来源与语义 |
|---|---|
| sample_id、PID、run_id、epoch、schema | 当前请求、真实进程及Hub身份，不从fixture猜填 |
| observation_capacity/backlog、pending_queries、dirty_marked | 同次HTTP原Observation四项计数，逐值配对 |
| query_slots/query_ms/refresh_ms | 当前真实配置，用于解释原预算，不修改参数 |
| pending的query_id、完整binding | 原 `directoryQuery` 的ID及binding值，不能拿exposure新binding替旧owner |
| age_ns、remaining_ns | 同一now减原started、原deadline减同一now；允许负remaining，不clamp或续期 |
| binding_current、session_current、online | 同锁实际关系；失配照实表达，不修复owner，不当新发送资格 |
| source的完整binding、query_id、dirty、periodic_only | 与原Stats容量统计同一Supported集合；无query用null，不混同空字符串与有效ID |
| source的session online/control version/current generation | 同锁当前access/session，无对象用null；不能从binding猜当前代次 |
| refresh_at_set、refresh_remaining_ns | 零refreshAt为false/null，已设则真实有符号余量；0与未设区分 |
| query_order、query_cursor | 有限副本及原游标；不修改或重新排序原轮转顺序 |
| complete、有限异常/overflow说明 | 表达记录完整性和非法值；真实关系失配另标，不把它改成匹配 |

合法身份沿现canonical界：access≤63B、exposure≤127B、revision29B、observation_revision≤23B、query_id32小写hex、digest64小写hex、source_id≤5B、control version≤13B、run_id30B。先检查长度再校语法；异常值省略/用固定占位并置invalid及complete=false，不能记录任意长原文或把截断前缀当身份。单条诊断payload编码上限64KiB；若超过则改发有限的incomplete摘要，不输出越界原payload。最大合法值须由机械编码回归核验。

不记录Authorization、token/ticket、请求URL、全headers/body、target/probe地址、任意label/description、SQL、私钥、raw控制消息或内存指针。复制后不能保留可变map/slice/query/exposure指针；后续释放、重登或采样不能改写旧快照。

## Fixture 仅增加关联，不增加请求

测试专用环境开关 **`OI_IT_G211_PENDING_DIAGNOSTICS=1`** 仅在本次后续原生G211子进程显式启用。fixture经既有configureOIS开启debug，并仅给原preparation采样中的既有OIS Stats请求加关联ID；包括preload、preDial及若实际到达的final准备采样，不扩到Zero/Active/critical、后续cycle或B4采样。默认、其他向量及正式运行不启用，保持info/noheader。

每个实际调用生成新32hex ID，在独立诊断元数据保留backend/round/test、OIS PID与原OISCall开始、返回、错误。元数据可窄加入已有准备诊断，不进入成功谓词；默认不输出新字段。ID生成失败只记诊断缺失，原请求仍执行，不因日志重发、并行轮询、等待pipe reader或改原10ms节拍。不得借诊断改变失败freeze-before-cancel、一次owner或cleanup顺序。

现有原进程日志及收集链保全debug事件；关键区不等待内存日志列表追上。完整原80/200/300等业务分析保持，不把诊断缺失转换为产品成功，亦不因缺日志把原业务失败改名。必要的分析适配只能解析/关联新增诊断，必须证明旧19run/14ITER与原33f判定不变；不先假设需要改主G2分析器或归档所有模块。

## 离线配对与时间边界

按固定源码、测试/round、原进程PID/run_id/epoch及sample_id唯一配对；核完整binding、有限schema及原四计数等于该次HTTP返回。零条、多条、错进程/代次/身份、截断、非法或缺关联元数据均为该项诊断缺失/冲突，不能取最近一条、上一次响应或空pending顶替。日志完整性与原业务判定分别报告。

设成功完整OIS HTTP请求在fixture单调轴为 `[S,E]`，日志原query年龄a、剩余r，则原started只能夹在 `[S-a,E-a]`，原deadline在 `[S+r,E+r]`。refresh同理。不得使用slog墙钟、区间中点、组合Stats结束或另一个进程的裸monotonic起点声称精确时点。

失败、取消、Goexit或未完整读取响应时，服务端采样可能晚于fixture返回；即使稍后出现同ID日志也不能套用成功区间。保留partial记录，时间界unknown，不挪回原D。相邻快照间query消失原因仍可能不同；本次不新增结束原因ring，不把聚合timeout分摊到某query。

## 有区分度的验证与下一实际运行

至少验证：同锁一致而非两次采样；复制后变化不污染旧记录；normal/debug/header各种组合下原响应shape与状态不变；匿名/consumer/错误方法无诊断；非法多值header不泄露原值；8/16精确与9/17明确incomplete；最大身份编码、异常身份、负remaining、零refresh、旧binding/离线关系；锁外阻塞logger仍允许另一个Hub只读操作取得锁且测试worker完整join；超时后迟到日志不得获得成功request-window；重复/缺失/错PID/epoch/关联/计数的离线反例；默认无header、启用仅原prepare调用的接缝。检查若只镜像实现或无断言，不计有效回归。

准备新的独占原生环境，种子仍9c工具/PGprefix/modules；fresh核前9c、7b、33f三轮共27个真实旧身份/组。同boot、无残留/PID复用、原余量门槛后才建新UUID；不动旧root或VM。新cache/PGdata/凭据，原parent9000/host9030、首传输1800、装配600、预构建共享900、helpers240/G2111200及integration共享5700、stop与collect共享300保持。仅为27身份和G211子进程诊断开关做窄派生，纯mock及AST/源码差异审查后固定。

局部Go、解析负例、现有helper与历史复算、独立交叉审查后固定新源码，再执行完整七步/双后端六叶；新增helper按真实库存绑定。所有失败保留，不挑叶重跑。记录诊断实际请求数、日志数/字节、完整/缺失/迟到与原调用耗时；日志扰动下结果只解释这次诊断配置。该运行提供身份信息，不保证Full80成立，也不能替代默认配置正式质量或G2门槛。

现已完成[局部实施与交叉审阅](pending-identity-local.json)：产品新增39个单测节点、夹具298个helper节点及独立解析负例通过，完整仓库vet通过；Windows race未执行的环境限制保留。新原生运行尚待固定源码与现场核验；真实debug日志成本未实测。完整质量、同源正式G1/G3/G4/G2及授权SSH/Bash验证仍须继续；G1Failed/G2Blocked/G3G4Inconclusive、旧run/ITER及alpha/main保持。
