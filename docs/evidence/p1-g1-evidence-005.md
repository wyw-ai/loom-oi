# ITER-004：P1纵向闭环与G1产品证据分析

2026-11-04 · 分析基线`f844b29c8b3af31d294fbdc2a77aaa642f7936fe` · 类型为实现级产品实验（G1）。**新增产品run 2个：主证据1个、被替代的失败run 1个。** 两个run都按[G1-P1预注册](../../experiments/manifests/G1-P1.json)在GitHub托管ubuntu-24.04与PostgreSQL 18.6上运行，raw、环境与重算摘要逐项入库，`verify.py`复算通过。机制夹具LAB-001/002未参与本轮判断。

## 输入、预注册与核验

| run | 被测提交 | 结论 | 用途 |
| --- | --- | --- | --- |
| [G1-P1-20261104T002624Z-35ec8b](../../experiments/runs/G1-P1-20261104T002624Z-35ec8b/report.md) | `9a42f4e` | observed / verdict=fail：TV-B2-11（postgres）与TV-B4-06（两后端）失败 | 被替代：暴露两项产品缺陷，修正后由下一行取代，不计入通过样本 |
| [G1-P1-20261104T014622Z-327df6](../../experiments/runs/G1-P1-20261104T014622Z-327df6/report.md) | `f844b29` | observed / verdict=pass：47项向量检查、G1五项判定、覆盖率与质量信号全部通过，零容忍事件0 | 主证据 |

预注册清单只在`5de16de`（2026-11-04T00:22:54Z）引入，早于两个run（00:25:56Z、01:45:50Z），两个run的`manifest.json`记录的清单SHA-256同为`3b41b090…0d82`，与该提交中的文件一致。CI检出为浅克隆，所以run中的`preregistration.committed_in`显示的是被测HEAD而非引入提交；判定以哈希和Git历史为准，工作流已改为完整历史检出。runner只执行固定步骤：`go test -json`单元＋覆盖率、`-race`、两后端集成向量（含O3负载`OI_IT_LOAD=1`）、两个模糊目标各60 s、`go vet`、staticcheck v0.8.2、govulncheck v1.9.0；任何步骤缺失或超时只能得到inconclusive。分析器只从raw重算摘要，verify按source-manifest登记的分析器哈希复算，两run均为`integrity: verified, recomputation: observed`。

## 首轮反证与修正

Linux CI与首个预注册run发现的问题都在本地Windows上不可见，或依赖竞态时序：

- **暂停后迟到的heartbeat复活许可（TV-B2-11，零容忍）**。OIC被暂停超过`L`期间，OIS发出的heartbeat积压在控制连接中；恢复后先被读取并按读取时刻续期，早于到期检查，OIC随后把暂停期间积压的18字节转发给T1（首run postgres：OIC日志`bytes_down 24`，暂停前只有`before`6字节）。测试先断言`LicenseExpired`，超时后零容忍检查未执行，所以该违例当时只表现为普通失败。B2§5写明“恢复后第一次检查即发现到期”：`c6c639f`改为许可已失效时先中止本代全部通道再续期，`f984329`把零容忍检查前移。主证据中两后端`forwarded_after_expiry`均为0，恢复到中止51 ms。
- **完成后过早切断腿（TV-B4-06）**。32×64 MiB时OIC本地已写完DATA与FIN、也收到FIN，却在`T_close`后强关腿，而数MB仍在OIC→OIS缓冲中，OIS判ChannelInterrupted、流被截断（以ABORT告知，非静默）。B1只用`T_close`约束Closing状态；`11e9f21`改为完成后等待OIS在转发两个FIN后关腿，只以许可失效截止，中止路径保留`T_close`。
- **CI先行发现**：coder/websocket的Close最多等5 s且CloseNow不能抢占，不应答的对端使OIS腿超出`T_close`（OIS改为持有TCP连接、以`T_close`为界）；OIC被拒附着连接留在keep-alive池，绕过测试代理的挂起并占用OIS描述符（TV-B1-11曾放出READY，OIC禁用keep-alive）；存储连接池在突发后多留空闲连接；SQLite目录路径在Linux被接受。
- **两轮代码评审**：吊销后仍可凭先前认证的请求生效、`last_epoch_seen`可推高epoch至无法重启（第二轮又纠正差一错误）、调用方取消被当成存储故障并在PG上释放单实例锁、DSN口令可能入日志、重载与重连并发时登记快照乱序，全部修正并加回归测试。文档代码一致性核对的D4–D13（观察记录不全、TV-B4-01只构建宿主平台、契约测试接受ErrBusy、默认值断言不全、F0下`check_materials`崩溃等）也已修正，见[一致性表](../../spec/TASK-004-p1-r-vertical/consistency.md#偏差与缺口)。

这些修正都落在已采用语义之内：没有改变B1–B4的契约，而是让实现符合原有规定或补全证据；判定标准、向量与预注册均未放宽。

## 主证据结果

| G1判定项 | 对应向量 | 结果 |
| --- | --- | --- |
| 真实注册授权双腿通道 | TV-B1-01、TV-B2-03 | 通过（两后端；TV-B2-03 1000通道交错零串路） |
| 双后端CRUD契约 | TV-B3-01、TV-B3-02、TV-B3-03 | 通过（SQLite 3.54.1与PG 18.6同一契约套件；并发写恰一成功一Conflict） |
| 目标替换拒绝 | TV-B2-04 | 通过（旧许可到达新目标0次） |
| 超时与资源清理 | TV-B1-14、TV-B4-03 | 通过；全部89次资源检查在`T_close`＋1 s内回到基线（中位3 ms，最大1018 ms） |
| 无业务实例化依赖 | TV-B4-04 | 通过；四平台各构建两次的二进制逐字节一致且`CGO_ENABLED=0`（TV-B4-01） |

P1完成判定另外四项同样由该run覆盖：全部P1向量在两后端通过（TV-B1-17、TV-B3-05、TV-B3-08按设计各只涉及一个后端），零容忍事件0；`-race`下222项单测无报告；两个模糊目标各60 s无失败；vet、staticcheck、govulncheck无告警；完整交付单元语句覆盖率97.4%。同一提交的CI运行`41756241232`六个作业（含Windows默认代码页下的Python检查，TV-B4-02）全部通过。

## 观察项（只记录，不设通过线）

- **O1准入**：P1有效模式恒为best_effort、readiness恒为unknown；require_ready一律412（TV-B1-18）。POST→READY为2–4 ms，READY→目标首字节<1 ms（单机回环）。授权拒绝（TV-B2-02：无discover为404、无open为403）与连接失败（TV-B1-15 TargetConnectFailed）分开记录；撤销与准备并发的200次随机时序中READY放出0次（TV-B1-11）。
- **O2存储**：400次审计写入经管理API的延迟，SQLite中位<1 ms、最大5 ms，PG中位1 ms、P95 3 ms、最大188 ms；冲突0。之后SQLite主库约156 KiB、WAL约4.0 MiB（未截断，维护与增长归G4/G5）。重启：OIS启动10–22 ms，放行OIC后到首个成功Open约1.7–1.8 s，无人工步骤；数据库回退由OIC报告推进epoch（TV-B3-10）。
- **O3路径成本（R列；H列不适用/未测）**：64 MiB慢速读取时OIS与OIC在途缓冲均为32 KiB（TV-B1-09）。固定负载下32×64 MiB约178–184 MiB/s，OIS约9.5 s CPU、RSS约25–33 MB，OIC约4.1–4.2 s CPU、RSS约16 MB；OIS在途缓冲峰值1 MiB，恰为32通道×`F`，与B4§3每通道每方向一条在途消息的上界一致；双向中继字节与负载一致。Go客户端与夹具、代理同处测试进程，其CPU/RSS不能单列为客户端成本。
- **O4联合故障**：撤销到双腿ABORT 1–2 ms（TV-B1-12），删除旧指纹到通道中止1–2 ms（TV-B2-07），控制连接被切后约2.27 s以LicenseExpired结束（TV-B2-10，`L`＝3 s扣0.5 s余量），代数替换后约0.7–0.8 s恢复（TV-B1-13），PG中断约0.8 s内管理写与新Open均503、已开通道继续、恢复后无需重启（TV-B3-05），全部无人工步骤。

## 结论、竞争解释与限制

**C36：P1 R单实例纵向闭环在固定提交上满足G1，证据为实现级。** 真实`ois`/`oic`/`oi`在两种后端完成注册、授权、双腿中继、拒绝、目标替换、超时与清理，零容忍事件为0。竞争解释“只在宽松时序或单后端下通过”已被排除：判定、向量与时序参数按预注册固定，两后端各跑一遍，资源回归在每个向量后执行。外推限制：GitHub托管4 vCPU回环拓扑与B4§3测试配置，不是1C1G目标环境（G5）、不是WAN，也不含探针（G2）、恢复工具（G4）、HA（G6）或真实兼容样本（G7）。推翻条件：代码、契约、依赖或环境变化影响任一向量，或新的零容忍反例。

**C37：资源回收不能依赖对端配合，正常完成也不能以`T_close`截断。** OIS对每次关闭握手以`T_close`为界并直接断开传输；端点在本地完成后等待OIS转发两个FIN再关腿，只以许可失效截止；HTTP keep-alive与存储空闲连接都可能在突发后滞留描述符。这三条来自首轮反证与Linux CI，只有在Linux描述符计数下才可见，后续阶段的资源向量须继续在Linux上验收。

**C38：许可期限按收到heartbeat的时刻计，已失效的许可不能被迟到的heartbeat复活。** 暂停超过`L`后恢复时，先中止本代全部通道再续期，保证“到期后转发0字节”；OIS侧离线判定与断腿仍是第一道边界（B2§5）。系统休眠时单调时钟不计时的已知局限不变。

## 产物联动与门槛

更新：G1台账记录与[门槛说明](../../experiments/GATES.md)、本分析、KB（结论/证据/谱系）、[TASK-004](../../spec/TASK-004-p1-r-vertical/README.md)、产品与模块状态文字、`experiments/README.md`，Issue #18、#3、#4、#11追加阶段评论。B1–B4采用片段与ADR不变。G1在综合分析、产物联动与Issue回读完成后才记为Passed；G2–G7仍受阻，P2继续按同一流程推进：观测/准入（G2）需要先收束探针与发布的具体契约，传输/失败（G3）与双后端恢复（G4，B3§8）按已采用契约实施并分别预注册。

## 实际检查与局限

两个run的raw、环境、执行与摘要均经`verify.py`重算；`check_iterations.py`覆盖全部run；`check_gates.py`、SDD/结构检查与Python单测在Linux与Windows默认代码页通过。检查器只证明记录一致与可复算，不证明测试断言完备；语义评审见两轮代码评审与一致性表。首个run保留为反例记录，未覆写或删除。
