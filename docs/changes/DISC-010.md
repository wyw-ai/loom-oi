# DISC-010：数据腿背压期间的心跳语义修订提案

2026-11-05 · 修订1 · 基线`0e343c2d04e65babb025b6d6948b112707331bec`。**DISC-010-R1 Pending，产品实施Blocked。** 唯一详细候选为[数据腿活性规格](../architecture/p2/data-leg-liveness.md)。本轮只提交具体取舍供用户选择，不把Autopilot或本地验证授权当成新心跳合同采用。

## 来源与增量

- 用户交接要求新增契约先在相关Issue追加确认点并用ask_user确认；原话及附件字节定位见[DISC-009来源](DISC-009.md)。用户随后明确“转为本地，暂时无法处理，继续本地工作”，只改变验证执行位置。
- 已采用[B1](../architecture/p1/b1-protocol.md)与[B4](../architecture/p1/b4-baseline.md)规定每PING发送、两次无PONG终止；旧Accepted/ADR、失败raw和冻结分析器保持。
- 固定`eb64801`开发诊断：164条Load中161成功、3次PONG写锁等待超时；具体源与原件hash见[TASK-007诊断](../../spec/TASK-007-p2-g2-readiness/local-linux-diagnostics-second.json)。它没有证明锁持有者或此候选可通过负载。
- coder/websocket v1.8.15源码的同步自动PONG与真实frame写共用锁；公开接口不能用连接ctx单独写PONG。RFC 6455 §5.5.3允许只回应最新未回应PING，不能据此允许无限待办或吞真实错误。

推荐整体采用有界异步PING/PONG与两个连续空闲观察窗口：每数据连接一worker、最多258B控制payload，保留原DATA缓存；固定PING节拍、忙时life复位miss、nonce退役与wire-owner分离。第二空闲窗即使只有一条PING或尚未写出也可终止，初始最早逻辑T3；这实质替换两次实际PING未答，不是保持原超时时序。维护仓库内固定上游副本与最小控制写API扩展；旧v1字节兼容，但混合版本不承诺同等背压存活。

替代是保持现规则，仅做不改合同的帧间公平/分片研究；它不能保证抢占已阻塞的TCP写，也不能承诺相同负载不再断流。当前证据不足以承诺任一候选通过。只暂停新合同/依赖修改；Render优化和G2准备夹具修复沿原授权继续。

## 需求、验证与采用

DISC-010-R1覆盖唯一规格scope的全部窗口、life、probe/owner、资源、依赖维护、终态/清理、旧端兼容与复证要求，作为不可拆分选择提交#3；#11承接复证，#18只承接G2前置影响。没有新增Issue、负责人、关闭动作或自动采用。

V01为独立内容审阅与结构/SDD检查；V02为未来实现及真实产品验证。发布及精确用户问答在实际发生后追加记录，当前没有批准凭据。review通过只证明结构，implement在Pending时应拒绝，不能由此放行产品。

## 项目回写与反例

本修订保持选对目标、身份/许可/撤销、READY和业务外置；改变“可解释连接失败”中的数据腿活性时点与依赖维护成本。SSH/Bash成功与未知执行不重放仍由端到端测试验证；无限DATA背压仍可由原IDLE关闭，反方向DATA沿原共享IDLE可能保持一方向停滞，不新增每方向检测保证。控制worker也必须纳入原关闭总D、goroutine/FD及全成本，不只计算OIS。

待确认导航只放在本DISC与新规格，不改冻结B1/B4或把未采用时序画成当前架构。采用后须同步共同架构、模块/部署说明、依赖物料和适用G1/G3/G4/G2预注册；旧失败与归档源码保持。若不采用则保留原规则并继续有证据的实现调试；若采用仍须以新固定源码一次次实测，不能临场延长原资源期限或重命名失败。

## 发布前实际核验

独立复核已闭合三处接缝：有效PONG只置valid_pong_seen；有限新入口绕过旧writeControl的5s包装并复用writeFrame；Completed保留原drain/close-after-queue/forceAt，在原transport关闭点才取消并join控制owner。当前未见剩余高置信阻断，仍需用户整体采用。结构与10份DISC review通过；初次目录新增导致DISC-002 Pending目录指纹过期，已仅刷新该Pending指纹并复核，旧Accepted对象不变。DISC-010 implement按Pending拒绝，P2按旧失败退出2；物料与19run/14ITER检查通过，无新实验登记。Issue发布与精确问答尚待发生，因此V01整体交付尚不标Passed。
