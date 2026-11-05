# G2准备与存储修复的Linux开发检查

本计划用于[probe-first准备](six-source-probe-first.md)、[registry批量写](registry-batch-insert.md)及[关闭测试修正](liveness-close-tests-local.json)的首次共同反馈，不替代[完整16步质量范围](liveness-complete-linux.md)或正式门槛。先固定最终Git来源与实际测试库存，再运行一次；不自动选HEAD，不传dirty源码，不拼接2a81或Windows结果。

沿完整轮已经审阅的source archive、transport、guest绝对截止与进程所有权实现，在独立目录保存本次驱动字节和差异。guest总9000s、host9030s、integration共享5700s不变；依次运行完整`./internal/wsio ./internal/ois/store` unit（atomic覆盖，1800s）、同两包race（2400s）、全仓vet（900s）、本次新旧G2 helpers完整库存（240s）、G2-11双后端六叶（1200s）、G2-12双后端四叶（600s）。所有Go用原`-p=1 -count=1`；unit/race沿Go默认watchdog，integration沿原90m。各步骤与父/共享截止取最小，源码检查、建进程和写结果都计入原D，不补时。

所有步骤同一源码、同一现有VM/boot与PG18.6/32MiB小盘，Linux CGO保持；不修改时钟、重启VM/PG或部署用户主机。打包前检查实际VM/PG身份与旧进程组已空，不把旧报告冒充当前空闲。单次child非零且来源/期限/退出完整时保留并继续后续独立步骤；超时、输入漂移或身份不明立即终止。新结果、准备器/驱动/测试库存和全部原stdout/stderr/进程/命令/回收清单独立保存，旧目录只读。

helpers只证明夹具和解析接线。store在真实PG与SQLite都需完整实际执行；原OS条件skip须逐名核，不能把缺PG的skip写成通过。G2-11按新显式strategy重算原件，最终full/young200、critical full/young300、原500ms、32Drop、完整60s和原2s回收保持；G2-12按原四叶及必需父项判定。关闭测试在Linux实际验证，但两包覆盖不能声称全产品95%。当局部反馈有效后，继续固定来源完整质量、Load及正式G1→G3/G4/G2复证；本检查没有正式manifest/run/ITER或门槛晋级。

最终SHA、inventory和驱动先由主代理明确固定并核验，再执行。驱动准备可先做纯文件/mock检查，无需重新请求用户批准普通修复或其验证。
