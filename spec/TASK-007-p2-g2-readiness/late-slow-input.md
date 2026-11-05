# 末八慢源在导出资格后设置的实施计划

本项承接固定61396fe的[六步实际结果](g2-repair-linux.md#单次固定61396fe的实际结果)，属于已采用G2合同内的单一夹具排序修正。先提交、推送、逐字回读本规格，再编码；目前未实施、未验证成功。保留原产品、配置、时限和全部判线，不新增用户决策请求。原probe-first六叶失败及其解析器保持，不用新规则重判旧原件。

## 区分性依据及唯一变化

61396fe六叶都通过真实P2/Q4预热、arm32及完整exporter输入，却全部在final_selection失败。所有已记录final样本P2/Q4为真；PG round1/2各有三次pending未满，其余pending8。SQLite round0/1未采到backlog16，其余四叶共8次full为真时原query投影已超过200ms。32Drop仅用16/19/16/16/16/16，原3s尚有1.4–1.875s，被首socket原1s先限制。这些不是产品违规或唯一根因的证明。

OIS每次发query清该源dirty，backlog统计dirty或pending的源数；各query沿自己的500ms期限轮换。OIC排入probe队列本身不产生Changed，实际归约变化才可能发布；Health.Set只改变目标响应行为。P2/Q4和pending8不保证另外8源dirty，Drop也不会自动产生它们。Changed可合并，不能把wire条数逐一冒称Begin/Finish的归约时点。

本次只把原末八次Set移至**cycle0原exporter全部资格成立之后、final原两Stats之前**。原前八次Set、六慢源、preload和唯一32arm不动。额外八源在既有exporter准备期间暂时保持原健康响应，可能更快完成实际请求、产生变化；之后仍各Set为慢一次。这个输入差异有源码和真实时间窗口依据，不是等待一个更有利的固定sleep，也不是重起query年龄。

只假设“该健康机会可能改善真实dirty/query相位”，不保证队列持续满、八条query同步或六叶成功。原完整wire和health_history足以检验这个差异，不先新增产品统计、HTTP请求或采样。若额外八源仍无实际执行机会，或真实变化增加却原full/young仍不合格，该候选即未解决问题，保留完整失败。

## 保持的契约与输入

固定16源、2健康/14慢角色及前六慢源身份不变；成功路径仍只有原两批各8次、共16次Set，每源一次。不挑选source、不释放或重设前六慢、不修改产品map/调度顺序。P2/Q4、500/200/50/1500ms探针参数、8 query槽及原500ms、两exporter槽/64KiB/200ms、每socket2048请求/RCVBUF1024、原Active/headers/持续429全部保持。

准备原3s仍早于任何本轮Set/arm。cycle0仍受最早socket首次connect+1s；末Set、final、critical和业务不重新起算。final full/young≤200ms、独立critical full/young≤300ms、原400–500ms wire预算、完整绑定及PID/代数检查不动。后置full/429、原控制500ms、许可零容忍、首终态、关闭前失败计数与原2s回收不变。最多32个原生Snapshot Drop，只装配一批；不新增hold、暂停、复制、重放、消息重排或业务重试。

**完整60s仍从真实最后一次Set返回的slow_input_applied_ns起。** 之前的六慢预热或另八源健康活动不能计作14慢负载。原每叶20至512cycle、每源实际attempt/timeout及两个健康源实际请求规则保持；原heartbeat观察起点仍早于prepare，不删除准备期间控制事实。

## 顺序、所有权和失败

外层原preload/arm完成后不再立即调用finishSlowSources。cycle0仍是同两slow socket的唯一owner；按原顺序完成200基线、连接、Active0确认、放行writers、全部写入、headers/Active2与持续429后，在现有final-selection闭包中唯一执行末八Set，再进入原final循环。

末Set前、每次调用前及最后返回后都检查已固定的`min(prepare_deadline, earliest_connect+1s)`，不得改写保存的3s deadline。若exporter资格失败，末八Set完全不执行；若Set中途或返回越D，先冻结实际部分版本/时间和owner状态，再沿原路径cancel/Close/join。未执行字段为null，不为成功schema补Set、16版本或假的60s。Fatal/panic/Goexit与正常false分别保留，清理不得先于失败冻结。

外层不能提前取尚不存在的slow_input_applied、输入版本或60s结束点；仅在真实末Set完成后保存，cycle0成功返回后使用同一保存值继续原drain和后续cycle。后续cycle不再次调用末Set或重建首轮输入。victim终态、首个D与forceAt不续期。

## 显式声明和原件约束

新case精确声明如下，原130普通叶/108组/106资源库存不变：

```json
{
  "strategy": "six-source-late-set-v1",
  "target_window": "all-slow-applied-v1",
  "query_witness": "final-critical-query-snapshot-v1",
  "preload_witness": "probe-queue-register-snapshot-v1"
}
```

修改解析器前，逐字归档固定61396fe旧模块及recorded-loader邻接依赖，并复算19旧run/14ITER。新模块只接受完整新声明，旧值、缺键/null或未知字段拒绝；旧开发失败继续用固定61396fe的原解析闭包解释。

沿用原Set/arm字段、preload注册库存、cycle0 before_export/slow_writes/actual_rejection、final与critical witness，不加网络调用。成功记录必须证明：

```text
prepare_started ≤ first_Set_started ≤ first_Set_completed ≤ preload采样/接受
≤ arm_started ≤ arm_completed ≤ before_export.started ≤ before_export.completed
≤ earliest_connect ≤ exporter原完整资格完成 ≤ final_Set_started
≤ slow_input_applied ≤ final.sample_started ≤ final.returned ≤ prepared
≤ min(prepare_deadline, earliest_connect+1s)
prepared ≤ critical.sample_started ≤ critical.returned ≤ native_started
```

exporter资格按原完整Active/header/持续429证明；原最后合格actual_rejection.completed不能早于任一必需资格动作的真实完成。若现有闭包在更晚的动作后才进入，使用其实际完成边界，不把首个429当完整输入。所有原内部时线校验保持；新排序不能放行在arm前创建的baseline或socket。

competition_full/complete共有的全部Set、版本和prepared锚点逐值相等；preload/final witness仍在full中，critical witness仍在cycle0中，分别核验而不新增复制。final/critical仍使用原同control、同16源注册完整binding与产品PID。最终wire不能补原Snapshot当时缺失的写入，旧前缀、布尔/浮点协议版本和32Drop反例保持。

Health.Set前开始的旧健康Version请求可能在Set后发头/结束，原记录必须保留；它既不算14慢窗口内attempt/timeout，也不能冒称新慢Version提前释放。只对严格窗口内开始的请求执行原最终Version及慢请求规则；Started在窗口内即计attempt，timeout另外要求Started<Cancelled≤window_end且HeaderStarted为0。Finished只核时序一致，允许其晚于window_end，不删除已满足原条件的attempt或timeout。此项保持原窗口含义，不扩大健康成功或慢超时计数。

## 验证及出口

先做实际helper与parser反例及独立审阅：export资格之前或只见首429就Set、遗漏/重复源、局部D续成新3s、跨connect1s、部分Set或未执行却补值、final/critical提前或复用、cycle1再次Set、Goexit及freeze/Close次序。保留原200/300ms恰界、16Set/32Drop、同binding/PID、前缀与严格60s反例；新增旧Version请求跨Set正常完成的正例和非法补数反例。

这次不改产品Go文件。待新Git来源和完整新旧helper节点库存固定、远端回读后，在同一专用Linux/PG环境只运行两步开发检查：全部相关helpers（原240s）及G2-11双后端六叶（原1200s）。保持原guest9000s/host9030s及integration共享5700s，逐步取实际最早绝对D，不增加Go watchdog、不重试未知结果。全源码archive、真实命令/原stdout/stderr/wire/health/resource及进程身份按既有实现保存；执行前实核当前环境和已结束的613进程，不把旧Gone直接当当前空闲。

两步不重标613的unit/race/G2-12为新源码通过，不声称新源完整质量。以同源完整expected_spec复算所选六叶，缺失其它组仍未通过。任一原full/young、critical/P2Q4、后置full/429、Drop额度、原D或60s目标计数失败都原样保留，不只挑有利叶。只有新反馈实际成立后才继续同源完整质量和正式G1→G3/G4/G2；现阶段无formal run/ITER、门槛晋级或alpha/main改动。
