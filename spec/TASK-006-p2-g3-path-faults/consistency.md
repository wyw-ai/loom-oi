# TASK-006：规格、实现与证据适用性核对

2026-11-04。当前为实现审查检查点；正式预注册、Linux run、ITER与Issue回读尚未完成。唯一行为规格见[任务README](README.md)。

## 原范围与实现对应

| 已有要求/本任务验证 | 实现与实际检查入口 | 关键失败边界 |
| --- | --- | --- |
| B1双向FIN、透明传输、背压 | `tests/integration/vectors_g3_transport_test.go`，G3-01/02；Stream/Bridge新增局部缓冲测试及原OIS缓冲测试 | 反向保留1MiB、真实64MiB写阻塞至少250ms；内容/FIN完成后立即起资源窗口，目标及通道回收在窗口内 |
| B1准备隔离、数据腿中断、不重放 | `vectors_g3_interruption_test.go`，G3-03/04 | 精确连接ID上的RST；Preparing载荷与READY均零；效果先发生且响应零，旧ID不重放，显式新ID/最终清理后再对账 |
| B1撤销与B2有限许可 | `vectors_g3_control_test.go`，G3-05 | 政策/令牌撤销、准备态晚附着；控制故障只操作原control，保留进程与数据腿；2–4s关闭和同代许可事实分开记录 |
| B1混合额度与重复故障回收 | `vectors_g3_quota_test.go`，G3-06 | 两轮8Open+8Preparing，第17条429；10周期每轮8条，RST/原PrepareTimeout/撤销按既定顺序，各有独立资源phase |
| 可核验故障仪器 | `g3_proxy_test.go`、`g3_instruments_test.go`、`tests/fixture/path_target.go` | 非复用accept ID、双方向pause、真正SO_LINGER=0及实际施加时刻；目标逐次计效果，绝不去重/重置掩盖重放 |
| B4故障前基线、完整统计、原2s清理 | `g3_env_test.go`、`g3_resources_test.go`；旧resource helper原样复用 | 34正式叶、38正式profile、62正式phase；另有10仪器profile与8预演恢复phase，不混计。PID/代数/连接绑定、FD相等、goroutine双向±10、三连续样本与原绝对D |
| 停止CLI关闭转发与通道 | `internal/client/stream.go::forwardBridge`与`cancellation_test.go` | 真实长连接已复现旧实现取消后挂住；显式取消发送ClosedByClient，原T_close兜底关闭，完成drain和回调回收，保留首终态及无取消协议错误的原关闭宽限 |
| 原始证据独立复算 | `experiments/product/analyze.py`及`experiments/test_product_g3*.py` | Go Package/Test精确绑定；原始阶段/身份/效果/时限/拓扑独立核验；父PASS或另一包同名不能补缺叶，坏样本不当作零或静默丢弃 |

产品仅修改客户端生命周期，不增加公共API、配置、业务执行器、自动重放或持久去重。其余新Go文件属于测试仪器/向量。`envOpts.beforeOIC/finishSetup`为默认nil的测试入口；旧B1–B4装配/采样路径与原阈值保持。目标脚本/代理接缝不进入OIC运行时代码。

## 已有开发事实与限制

独立复核还修正了预注册中12个正常完成phase的origin_event为实际`exchange_fin_observed`，并补齐04的真实非EOF错误和06的Open请求/目标白名单。分析器拒绝跨通道/跨轮连接ID复用、Preparing伪非空请求头及历史已关闭目标再变化；原反例均保留并由另一审查者复验拒绝。product分析器70项测试通过，34个场景的最新实际开发日志分别作行为对账通过；这不是拼接正式run，也不验证Windows缺失的FD。

初始完整双后端G3开发执行为34叶全部通过，38正式profile、62正式phase、266正式采样；最慢清理1287ms。随后收窄正常FIN→资源起点，22个受影响叶通过（50正式phase，最慢1313ms），另针对原PrepareTimeout上界及新增诊断执行窄回归。原始文件在ignored coverage，均是Windows/本机PostgreSQL 18开发回归，FD为-1，不能当作Linux正式资源证明。不能把多个开发文件拼接命名为一次正式run。

完整`cmd/`与`internal/`单元测试使用本机PostgreSQL执行，覆盖率95.9%；vet、staticcheck v0.8.2、govulncheck v1.9.0通过。取消回归七根/11子场景通过，包含已取消上下文、READY前、活动读、两侧阻塞写、完成后等待远端关闭、原ABORT与FIN竞争、定时关闭和回调回收；不以最终主动FIN掩盖退出挂起。具体Linux race、active fuzz、跨平台与最终CI仍需固定源码执行。

开发期失败原件均保留：CLI旧实现挂起的确定性反例；仪器初采与首个样本共用Windows时钟刻度的过严顺序判断；测试先等Closed再读ABORT造成自留socket；正常结束后的重复Close被误当业务失败；以及root本机命令未引用coverage参数、使用相对OI_IT_RUNDIR导致的执行配置失败。后两项分别以引用完整参数、绝对测试目录纠正；原日志与失败夹具移入ignored coverage保存，不改产品规则。诊断overlay源码以`.go.txt`保存，避免被`go vet ./...`当作普通包；原字节与overlay映射保留。

旧证据分析器在修改前按2ec3670原字节存入`experiments/sources/product-g1g4-d18/analyze.py`，SHA-256为`15df949aa31f7c26624492fff52f936b9e8dafcb24b852414e0b364997d0e093`。旧run、ITER、source-manifest、summary、SHA256SUMS、发布回执和已采用scope不修改；历史通过与失败继续原样复算。

## 当前门槛与正式顺序

客户端产品代码变化后，当前工作区G1/G4为Inconclusive，TASK-006入口c6c0500/546598e的完整validation对象原样保留，subject仍为2ec3670。入口提交曾补充TASK-005最终集成记录并更新其文档绑定指纹，不能误称与更早570b573的整个gate对象逐字一致；原run/ITER、源码与结果均未变。中间CI的P2门槛检查应如实阻挡；不改CI阶段、不吞返回值、不降低覆盖率或资源条件。当前alpha仍为已核验570b573，main未改。

先完成实现/原始分析器/独立审查，提交并回读独立预注册`G1-P1-R3`、`G4-P2-R2`，以同一干净最终源码完整复验原五项/四项criteria。新run与新ITER、综合分析、图文/KB和Issue阶段回读共同保存，恢复当前G1/G4；再运行独立`G3-P2`正式证据。G3采用同一已审阅仪器、34叶/38profile/62phase和质量条件，不用前两次回归中的额外G3执行替代其预注册闭环。最后真实门槛完成、六项最终CI全绿后，按既有授权快进alpha。

用户最新明确指令将现有SSH主机作为G5验证目标，不再等待另一台整机1C1G；这只改变后续目标环境选择。本任务不宣称G5/G7通过，不把实测2逻辑CPU/约1.58GiB或应用配额改写成整机1C1G。后续资源规格另保留原问题/答案及实际预算边界，其他性能条件不自动豁免。
