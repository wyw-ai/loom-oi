# G2准备与存储修复的Linux开发检查

本计划用于[probe-first准备](six-source-probe-first.md)、[registry批量写](registry-batch-insert.md)及[关闭测试修正](liveness-close-tests-local.json)的首次共同反馈，不替代[完整16步质量范围](liveness-complete-linux.md)或正式门槛。先固定最终Git来源与实际测试库存，再运行一次；不自动选HEAD，不传dirty源码，不拼接2a81或Windows结果。

沿完整轮已经审阅的source archive、transport、guest绝对截止与进程所有权实现，在独立目录保存本次驱动字节和差异。guest总9000s、host9030s、integration共享5700s不变；依次运行完整`./internal/wsio ./internal/ois/store` unit（atomic覆盖，1800s）、同两包race（2400s）、全仓vet（900s）、本次新旧G2 helpers完整库存（240s）、G2-11双后端六叶（1200s）、G2-12双后端四叶（600s）。所有Go用原`-p=1 -count=1`；unit/race沿Go默认watchdog，integration沿原90m。各步骤与父/共享截止取最小，源码检查、建进程和写结果都计入原D，不补时。

所有步骤同一源码、同一现有VM/boot与PG18.6/32MiB小盘，Linux CGO保持；不修改时钟、重启VM/PG或部署用户主机。打包前检查实际VM/PG身份与旧进程组已空，不把旧报告冒充当前空闲。单次child非零且来源/期限/退出完整时保留并继续后续独立步骤；超时、输入漂移或身份不明立即终止。新结果、准备器/驱动/测试库存和全部原stdout/stderr/进程/命令/回收清单独立保存，旧目录只读。

helpers只证明夹具和解析接线。store在真实PG与SQLite都需完整实际执行；原OS条件skip须逐名核，不能把缺PG的skip写成通过。G2-11按新显式strategy重算原件，最终full/young200、critical full/young300、原500ms、32Drop、完整60s和原2s回收保持；G2-12按原四叶及必需父项判定。关闭测试在Linux实际验证，但两包覆盖不能声称全产品95%。当局部反馈有效后，继续固定来源完整质量、Load及正式G1→G3/G4/G2复证；本检查没有正式manifest/run/ITER或门槛晋级。

最终SHA、inventory和驱动先由主代理明确固定并核验，再执行。驱动准备可先做纯文件/mock检查，无需重新请求用户批准普通修复或其验证。

## 单次固定61396fe的实际结果

2026-11-05，固定`61396fe12f6f0bb223026fe62671231c6f87b945`已完成唯一一次真实guest执行。[完整开发记录](g2-repair-linux.json)保存878个Git源码、304087040B archive、211个回收原件及manifest（共212文件183441371B）、实际命令、原期限、独立复算和失败诊断。guest总751.4858486s，实际exit1；六步exit依次为0/0/0/0/1/0，没有超时、源码漂移或监督错误。

|检查|实际结果|边界|
|---|---|---|
|完整wsio/store unit|374个唯一PASS，0SKIP/FAIL|97根完整，新增registry31及关闭9节点均齐|
|完整wsio/store race|同374个唯一PASS，无DATA RACE|真实SQLite/PG均执行|
|全仓vet|exit0|本轮没有staticcheck、漏洞或依赖质量步骤|
|全部G2辅助节点|162个唯一PASS|只证明夹具接线|
|G2-11六叶|六叶严格FAIL|全部失败于final_selection；未进入critical/native/撤销及60s|
|G2-12四叶|四叶及必需父项严格PASS|业务和原资源4checks/12samples均通过|

本次PG批量0/1/3/64/65行、块内/跨块约束失败、namespace错误次序和audit原子回滚均已真实执行。原PG实体数量场景现通过完整32实体，与SQLite及双后端慢读四叶共同通过；这只证明本次样本，不推断任意请求500ms的生产SLO。4个回收check各3连续样本，共36个role样本FD差0、goroutine差0..1。两产品包实际profile为1708/1843=92.67498644%（store1305/1420，wsio403/423），不作完整产品95%声明，不与旧完整profile拼接。

六个竞争叶全部通过真实probe-first预热与一次32arm；预热接受耗98.7333–569.0074ms，原注册16库存/PID/代数及原采样时序独立复核成立。全部失败时原3s尚余1409.4119–1874.6771ms，是首socket原1s先到。原200基线、socket写入、Active/headers及持续429输入均成立；首选429之后剩余final窗口787.920–846.336ms。

所有已记录final采样均为P2/Q4，不能外推采样间连续状态；PG round1有3次pending7，PG round2有1次pending6、2次pending7，其余为pending8。SQLite round0/1从未采到backlog16；SQLite round2和PG round0/1/2分别仅1/2/2/3次五项full为真，8次实际young调用的原投影均因203.6509–407.9357ms超过原200ms而拒绝。没有漏接受已记录的完整合格callback。实际Drop依六叶为16/19/16/16/16/16，未耗尽32；不能归为Drop额度不足或预热耗完3s。六叶没有完整回收check，不计资源通过，也不由此直接推断泄漏。

宿主真实deploy/execute/collect退出0/1/0，来源和原D核验通过；runner身份Gone（collector未单独扫描其组），六child实际进程组均空。最初宿主`execute-01`误把部署目录传给需文件的参数，在任何SSH前失败；原件保留，`preflight-03`再次证实guest未开始，纠正参数后的`execute-02`才是唯一产品运行。实时前置的55个历史身份和5旧目录当前UID1000使用观察分开保存，旧目录缺完整PID记录的限制保持。

停止后续完整16步执行，先根据原wire/样本核dirty、query轮换与Changed的实际相位及原验收来源，形成有区分力的下一修复。没有盲重跑六叶、拼接通过叶或放宽200/300ms等现有判线。正式19run/14ITER、G1 Failed/G2 Blocked/G3及G4 Inconclusive保持；本次无新正式run/ITER、alpha/main改动或G5/G7部署。
