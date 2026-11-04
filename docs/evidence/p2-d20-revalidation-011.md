# ITER-010/011：有界消息缓冲修正后的G1/G4复证

2026-11-04 · 固定源码 `bff7aa278e997cf4777f62bc5e0f6a001d2c6090`。新G1五项和G4四项正式判定均为 **observed/pass**。本报告保留此前负载与存储失败，区分实现修正、同源新证据和CI准备失败；四单追加与逐字回读已完成，G1/G4据此恢复Passed；最终六项CI仍须全绿，未合入alpha，也未运行正式G3。

## 原件、预注册与质量

| 范围 | 固定输入与结果 | 完整性 |
| --- | --- | --- |
| G1 / ITER-010 / E74 | [G1-P1-R4 run](../../experiments/runs/G1-P1-R4-20261104T131129Z-686545/report.md)，[41835768830](https://github.com/wyw-ai/loom-oi/actions/runs/41835768830)，[独立预注册](../../experiments/manifests/G1-P1-R4.json) | manifest SHA-256 `3e0d69def11ec34128d92f2f829e1e785983976511806cb575820d188589c6a8`；SHA256SUMS自身 `6b9c9a4882d78a27e120b2a1734c08f6568f0d2254d02b28ed8c7e2addb2fabe`；raw 11,005行／7,620,755字节 |
| G4 / ITER-011 / E75 | [G4-P2-R3 run](../../experiments/runs/G4-P2-R3-20261104T131132Z-cdbb34/report.md)，[41835772115](https://github.com/wyw-ai/loom-oi/actions/runs/41835772115)，[独立预注册](../../experiments/manifests/G4-P2-R3.json) | manifest SHA-256 `b02477dd2fdf258f0fbf62e0e987b7502e42a63194f5ba597a898c28b81b74ec`；SHA256SUMS自身 `fab1e2d54dc9a61fd4d2750f8a68503433efacd88fadd74d03f8b27dc1e7ed44`；raw 10,583行／7,184,362字节 |

两份预注册保留之前全部criteria、负载与判线，另增加D20的11根/34子场景在unit/race两步的准确包名绑定；不是修改旧失败manifest。每run的139项source-manifest分别逐一匹配Git对象，原件SHA全部通过，`verify.py --current-source`及独立重组raw后的摘要复算与原summary相等。新run与其各自新ITER同次入库，旧14份run与9份ITER的145个文件保持原字节。独立复核还逐项核对原预注册摘要与case，运行manifest只附加源码/预注册链接；两run的D20新增unit/race根与子场景全部实际PASS。

完整交付单元unit/race各392个根/子场景全部通过，单测覆盖率96.1%；frame/control各主动fuzz至少60s，vet、staticcheck v0.8.2、govulncheck v1.9.0通过，无零容忍记录。G1为49向量ID/255项必需检查，G4为15向量ID/185项必需检查；这些数包含新增D20检查，不冒称增加了独立样本。

环境是两个独立GitHub hosted Ubuntu24 Linux回环job，各2逻辑CPU、约7.75GiB，Go1.27.3、SQLite3.54.1、PG18.6与32MiB小盘夹具；G1 CPU为AMD EPYC7763，G4为Intel Xeon8370C。两个原环境内核均为7.0.0-1017-azure。不是用户SSH目标资源，不证明G5容量或G7真实应用。四个后端不适用leaf显式skip：PrepareOutOfOrder/PG、LongReader/PG、PostgresDown/SQLite、SQLiteSettings/PG；G4另按原load=false跳过Load，不将skip当通过。

## C52：有界缓冲修正与G1新负载证据

73bb667的零响应反例保持。D19在916293a的另一次Linux诊断中确认17条失败流均已有请求FIN、目标首写、OIC首读/首DATA写，不能将该新信息倒写为73bb667已知的内部时线。1c6c116的运行时追踪又出现31完整/1部分响应的ChannelInterrupted，显示OIS写侧网络等待、PING写锁与GC分配等待同时存在；这支持减少消息路径分配的候选，但不证明每次失败都具有同一根因。

D20在原F/read-limit下使用有界可复用WebSocket消息缓冲：OIS值拷贝只共享reader指针，队列仍复制所有权；consumer必须耗尽pending才读下一消息；OIC上行等待同步Write返回后才复用DATA缓冲。原FIN/ABORT、写失败、许可、空闲2s、PING、半关闭、read-limit与配额不变。正dataMax大于既有1MiB约束在client构造时返回配置错误，零/负默认保持。实际32,769B无压缩WebSocket分配比较15→0次/read是局部测量，不是生产SLO。

固定def5abf的[开发诊断41833914644](https://github.com/wyw-ai/loom-oi/actions/runs/41833914644)原六组负载连续5轮、410流全部完整返回，原30次资源检查通过、最慢115ms；它只用于进入正式复证，不替代本轮独立run。当前新G1五项均通过，原1MiB/64MiB×1/8/32并发两后端全部执行；独立重组12组O3与164个worker后，164流各自唯一Channel、全部成功且完整实收；每后端2,794,455,040 B（2665 MiB），总5,588,910,080 B（5330 MiB），164个Target边界的EOF/Done和响应量逐项吻合。全部requested=received=successful，失败0；按原elapsed_ns重新计算两类吞吐均精确一致，没有使用旧失败轮的计划吞吐。

本轮G1严格B4资源分析完成115次检查、578个采样，回稳107.295–1149.055ms；每项至少连续3个合格采样，原故障前基线、FD精确相等、goroutine±10、2s截止与角色身份均保持。负载成功与资源回收分别成立，不能互相抵消失败。该固定源码取得受控环境下的新G1直接支持；不声称永久消除调度停顿，也不把一轮通过外推为WAN或目标主机保证。

## C53：G4复证与独立CI存储准备失败

同源G4的SQLite维护/拥塞/磁盘不足、备份与重启恢复、版本并发冲突、旧任期拒绝四项均通过，保持D18单读池与原恢复规则。G4自身load=false；其范围通过依赖共同G1的新独立支持，不能单独替代G1负载。另对G4原始b4-resource-v2记录按相同严格规则执行只读补充复算：101 profiles/103次检查、522个采样通过，回稳107.276–1146.681ms；该分析没有改写G4原manifest或summary，也不伪称其原先显式注册了resources。两run中各2条fd_diagnostic的ENOENT来自迁移后已停止参考进程，仅diagnostic_only；真正stats采样和必需资源检查均完整通过。

另一个同源普通[CI 41835698583](https://github.com/wyw-ai/loom-oi/actions/runs/41835698583)的integration首次执行失败，原件和分析保留。B1-02/PG在`newEnv`暖机前创建测试token收到503 StoreUnavailable，未进入子协议断言，未形成资源baseline。OIS记录13:13:09.653704注册，13:13:10.154795注册持久化write超出原StoreProbeMS=500ms，随后storeOK=false锁存，13:13:10.400626管理写被拒。13:13:10.402之后的PG EOF/管理终止发生在测试失败清理之后，不能倒置为首次原因。

这条“持久化失败→拒绝配置写”链符合既有B3 fail-closed；现有raw无法区分首次SQL/调度/磁盘慢写，不宣称“环境抖动”或定位到D20。也不自动resume存储、重试未知结果的token创建、修改500ms或隐去首次CI失败。同源新G1/G4均实际通过对应PG leaf，说明这次准备失败没有在两份正式运行中复现；其底层原因仍不确定。最终保存/发布后的完整CI须另行成功，才允许alpha快进。

## 共同承诺、联动与后续

修正服务于接对目标、守住授权、提供有界且可解释的连接；没有新增业务执行、自动重放或事务承诺。保持B1–B4和已采用G2三项取舍及六个冻结scope，G3精确路径故障仍须新正式证据。G2分支当前有新增产品/依赖，不能直接借本分支G1/G4结果宣布它已复证。

本轮只新增2个正式样本，各归ITER-010/011。KB的C52/C53、E74/E75与lineage承接C50/C51，原结论保留源码/时点范围；图06只反映当前G1/G4状态变化，依赖边及其余十图不改。用户已指定G5使用现有主机，真实2逻辑CPU/约1.58GiB照录，不等待另一台1C1G；G7仍以现有SSH→Bash经OIC原生通道验证。没有新增产品决定需要重复确认。

发布闭环：已在[I3-P09](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6632408825)、[I4-P07](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6632409487)、[I11-P09](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6632410179)、[I18-P07](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6632411148)追加并逐字回读。四单原正文/标题/状态/成员及38条旧评论保持，11个唯一固定链接全部解析；[真实回执](../issue-history/2026-11-04/TASK-006-d20-revalidation-published.json)保存原文、摘要和保全结果。ITER-010/011据此标complete，随后以本固定源码的新run恢复G1/G4 Passed。当前没有正式G3结果；最终六项CI及alpha快进进度由任务检查点另记，不把当前Passed等同完整首版交付。
