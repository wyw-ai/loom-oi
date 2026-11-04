# ITER-012：G3首轮正式证据的辅助日志分类反例

2026-11-04。固定源码 `15deeab60e455a5e42032d5da5cf0cc543698118` 的首轮G3为 **observed/fail**：所有Go步骤退出0，G3资源检查通过，但4条辅助预演日志冒用了正式行为标识，严格分析器拒绝其身份/schema/时间字段。当前G3为Failed；G1/G4保留既有bff7aa2范围的Passed，G2及后续门槛未放行。本报告不将Go通过、资源通过或内存诊断替代正式行为判定。

## 原件与来源

- 原始run：[G3-P2-20261104T141622Z-01edf5](../../experiments/runs/G3-P2-20261104T141622Z-01edf5/report.md)，[Actions 41845796987](https://github.com/wyw-ai/loom-oi/actions/runs/41845796987)，固定ref为`refs/heads/evidence/G3-P2`。
- 原10文件逐字复制入库；raw为10,599行、7,211,114字节，SHA256SUMS自身为`3715f2065934120301841d82468b5df71ec1a2ddfea96fd5b645dd4e34450042`。原manifest、summary、report、raw、环境及执行记录保持，不修改run内部任何字节。
- [预注册](../../experiments/manifests/G3-P2.json)固定于`bff7aa278e997cf4777f62bc5e0f6a001d2c6090`，SHA-256为`f9bc3e15a6b532ee41acb6051993da82f888fdc4bce18c10605c9808fe786e33`。其提交时间13:10:25Z，subject提交13:56:02Z，run开始14:16:22Z，预注册先于运行且为subject祖先。运行manifest除runner附加的subject/preregistration外与固定case一致。
- 139项source-manifest分别与subject的Git对象逐字SHA核验一致。`verify.py`精确复算原observed/fail；[被测分析器归档](../../experiments/sources/product-g3-first/analyze.py)取自该Git对象，105,193字节，SHA-256为`5aacb961dca5b2071c2eef8b68054c80d5362fbf13d6cc88d4defeec79d7c51d`。归档不借后续分析器重写历史结论。

## 实际通过与失败的边界

环境为GitHub hosted Linux `7.0.0-1017-azure`、2逻辑CPU AMD EPYC7763、MemTotal 8122984 kB、Go1.27.3、SQLite3.54.1、PG18.6（Debian18.6-1.pgdg13+2），PG工具容器及32MiB小盘夹具。它是受控回环，不是G5目标主机、WAN、HA或G7真实应用样本。

unit/race各392项测试通过，完整覆盖96.1%；vectors为384项测试PASS、5项非必需/不适用skip，无失败测试。frame/control两个主动fuzz、vet、staticcheck、govulncheck均退出0且无超时、无零容忍记录。8步实耗依次为unit97,829 ms、race122,732 ms、vectors573,356 ms、frame fuzz104,554 ms、control fuzz61,193 ms、vet14,219 ms、staticcheck45,717 ms、govulncheck14,452 ms；这些执行耗时不是产品SLO。

G3资源独立复算为pass：34正式叶的38个profile、62个正式清理phase，另10个仪器profile和8个预演恢复phase；共70次清理、399个采样，清理用时215–1402 ms。附带原B4资源解析的79个必需leaf/至少85次清理要求满足，原始共103次完成、539样本，107–1156 ms；额外检查不增加必需场景数量。原FD精确相等、goroutine双向±10、固定故障前基线、三连续样本及各绝对2秒窗口均保持。

28个向量ID中只有合成项`G3-BEHAVIOR`失败；原34个G3 Go叶及392项必需Go检查通过不能替代它。由于原四项criteria均依赖完整原始行为复算，**半关闭与背压、响应丢失不重放、撤销时限、清理与限额在原summary均为fail**，不按局部Go结果重新宣布其中任一正式通过。

## C54：辅助预演误标为正式行为，严格拒绝是正确结果

原raw的1-based行号及归属如下；四条都来自`tests/integration/g3_env_test.go:425–427`的同一发射点。

| raw行 | Go test归属 | 预演采样剩余许可 |
| --- | --- | --- |
| 3577 | `TestG3ResourceTopologyProfiles/environment_control_blackhole/sqlite` | 1794 ms |
| 3650 | 同一仪器/postgres | 1799 ms |
| 6881 | `TestTVG3_05_RevokeAndControlFailure/sqlite/control_blackhole` | 1797 ms |
| 7144 | 同一正式叶/postgres | 1790 ms |

发射点把`event=blackhole_phase, stage=preparation`写成`kind=G3-fault`；通用`g.obs`只补test/backend，未提供正式`schema`、`package`、`observed_ns`。此外该辅助event不在固定G3_EVENTS枚举，仪器根本来也不是34正式叶之一。因此`_g3_behavior_analysis`按原schema拒绝，而非实际网络故障或权限断言失败。这里1600–1800 ms是无业务的预演profile采集门闩；正式控制黑洞仍独立要求施加前许可≥2000 ms，不能把这四个预演数值误写为正式故障输入违约。

竞争解释检查：严格分析器不是因为Go skip、资源超时或分片丢失误判；4个问题逐一对应完整原始误标行。在内存中仅隔离这4行、仍使用被测原分析器做诊断后，未发现其他problem。该操作没有写回文件、没有产生新run、没有改变原summary；它只能收窄当前编码缺陷，不能证明修正源码已正式通过。

## 窄修、回归与重启条件

D21仅把该发射点的`kind`改为`G3-preparation`，原stage及所有诊断数据保持。正式发射器、解析器、schema、34叶/38profile/62phase、预算、原四项criteria及预注册manifest均不改。新增`test_product_g3_preparation.py`通过真实主入口的分片重组与正式行为分析验证四条边界：辅助日志被保留且不污染完整正式证据；旧缺schema的G3-fault仍拒绝；仅诊断和Go PASS不能替代必需行为；伪装诊断却声明正式schema仍拒绝。资源数学由既有完整schema测试覆盖，新路由测试只隔离资源组件，不假称再次运行真实产品。

上述4项与原G3分析测试共32项本机PASS。本轮未修改产品行为、未降低任何门槛，也未启动新的正式复验。先提交本失败run与ITER-012及修正，再固定新源码运行同一预注册；新run须独立保存、复算、综合分析和发布闭环后才能重新判断G3。失败证据与最小修正在f568bde同次保存，39个远端文件逐字回读；Issue追加闭环已完成，新正式复验尚未运行。

## 共同承诺与联动

OI承诺受控连接、有限授权、失败不隐式重放及可解释的回收。验收日志缺少身份就必须拒绝，不能用实现自报PASS消除这一检查。本轮修正验收仪器分类，不扩展业务执行、取消/回滚、持久去重或支持矩阵；冻结B1–B4、已采用G2六scope及所有旧run/ITER/报告/回执保持。

本轮新增1个正式失败样本，归ITER-012、E76、C54与EDGE-047。ITER输入用途使用既有枚举`inconclusive`保留反例，不改变run实际observed/fail；在三单追加和逐字回读后完成状态为complete。当前架构/模块/图06与台账只同步G3 Failed和复验条件，G1/G4旧验证对象及其他门槛范围不变。[I3-P10](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6634213228)、[I11-P10](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6634214130)、[I15-P07](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6634214809)已追加并逐字回读，三单元数据和31条旧评论保持，9个唯一固定链接解析通过；[真实回执](../issue-history/2026-11-04/TASK-006-g3-first-published.json)保存原文/摘要。图06中文与布局已人工核验。G3保持Failed；新正式run必须单独核验和建轮。
