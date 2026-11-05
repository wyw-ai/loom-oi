# 原生 pending 诊断装配失败

固定源 `5be6c1d473d7cc1bbd63f6044cdd513ccda6e2c7` 的新环境 `9c8b9d34-e5c3-4399-a948-5a303fb23172` 在装配阶段失败；尚未执行七步产品检查，私有PG未初始化或启动。本次没有新的G211业务结果。

独立核对 119 项原件关系均一致。bootstrap、upload、collect 的宿主退出码为0；assemble为1，ready=false；无超时或原D越界。收集清单24项共48180B，另含manifest，tar共25个文件，逐项bytes/SHA及tar原字节一致。

同boot下前9c、7b、33f三轮27个已知身份均有fresh gone、live=null及空group记录。工具树15657项/244139206B、PGprefix651项/63589616B的旧前后及新副本摘要相等，普通文件独立inode；module seed167项/66709092B已核装配记录。Go1.27.3版本、GCC版本与gcc-link三个child均有真实started/result、exit0且原D内结束。

gcc-runtime留下15B stdout `gcc-runtime-ok` 加换行，stderr为空；紧接着读取 `/proc/3967202/exe` 抛FileNotFoundError。该identity调用位于run的try/finally之前，因此没有gcc-runtime.started.json或.result.json，也未记录精确start_ticks/完整进程身份和退出码。**不能以打印OK宣称该命令成功或生命周期已闭合。** 快速退出可解释该现象，但现有原件不能唯一还原退出状态。

装配失败及输出已完整保留。原parent9000s与assembly600s未重起；装配到失败约34.644366s。后续独立只读closure已收回：同boot下candidate PID/组、三个已记录child组与task-root进程的当次扫描均为空，pgdata为空且无PG started/environment文件；只读、未发信号。三份原已记录身份逐值相同。但这一后来观察不能补造gcc-runtime遗失的start_ticks、退出码或原wait，故仍不称该child命令成功。具体绑定见[失败记录](native-pending-preparation-failure.json)中的closure。

独立复核仅本地读取，root完成一次只读现场收尾；旧件未修改。这不是正式run/ITER或门槛晋级。事实、来源哈希与缺口见[失败记录](native-pending-preparation-failure.json)；下一步按[短命子进程先行规格](native-short-child.md)修复准备器，不重放本次装配。
