# 原生准备器的短命子进程记录修复

承接[5be装配失败](native-pending-preparation-failure.md)。这是既有授权内的准备工具修复，不改变产品、公开契约、G2判线或资源预算。先提交并回读本规格，再实施新的本地派生脚本；已用过的准备器、目录、记录和失败结果保持原样。

## 实际缺口与选择

原 `run()` 在直接 `Popen` 后、`try/finally` 前读取 `/proc/PID/exe`。本轮 gcc-runtime 已打印15B，随后exe读取失败；该命令没有保存start_ticks、退出码或wait结果，不能补判成功。后来只读检查没有发现相关进程，PG未初始化或启动；这一观察不能还原丢失的身份与退出状态。

保留直接 `Popen`、原 stdin/data 与 `communicate(data, timeout=remaining(D))`，不引入launcher、额外worker、重启或新的等待期限。修复将稳定进程身份与可执行文件的当次观测分开：只有已证实退出的直属子进程，才允许exe观测缺失；实际身份、退出状态和原期限仍必须有效。

不采用先门闩后exec的launcher。它能在执行前提供launcher身份，但仍不能保证读到短命目标的exe，还增加exec错误通道和管道清理。这里的必要事实可由本父进程的子进程关系、真实stat身份及未消费的退出状态取得，无需改变命令启动机制。

## 窄实现边界

仅新准备器的临时命令 `run()` 采用下述处理；长寿命私有PG的启动、在线exe核验、停止及原七步执行器保持既有要求。

1. 核本固定解释器只有一个线程、SIGCHLD为默认处理；没有自定义handler、其他waiter或采样前poll/wait。所有Popen后路径进入同一个异常/最终记录边界。
2. 在任何reap之前，读取真实 `/proc/PID/stat` 的PID、PPID、start_ticks、PGID、SID及boot。要求PPID为当前父进程、PGID/SID为该独占child PID；前后稳定身份不能变化。状态字段单独保留，不把活跃到退出的合法状态变化误当PID身份变化。
3. 正常读到exe时记录当次真实值。只有exe读取确为ENOENT时，才检查 `waitid(P_PID, pid, WEXITED|WNOHANG|WNOWAIT)`：必须返回同PID、同UID的直属child终态（正常退出、信号终止或core终止），且复读stat身份保持。此分支将exe明确记为null及 `unavailable_after_exit`，保存原waitid字段；不从argv推填曾观测到的exe。PermissionError、其他错误、ECHILD、None、非终态或身份差异均不能走此容许分支。
4. 原communicate继续消费同一个child并保存真实returncode；已取得WNOWAIT终态时，必须与其退出/信号结果一致。非零、信号、超时、记录错误、未知身份或晚于原D仍失败，stdout中的OK不替代任何条件。不能借CPython在ECHILD时的默认returncode值补判成功。
5. 身份/started记录失败也必须进入最终记录并尝试原期限内的有界回收。只有已保存且现场复核相同的直属child身份与独占组才能发组信号；未知/失配不误杀。保存首异常、清理异常、真实wait结果或未完成状态，不用后续异常覆盖首错。任何清理余量只来自原assembly/parent D，不新发期限；迟到仍记录失败。
6. 不隐藏临时命令缺失的生命周期记录。最终started/result与输出hash应表达实际取得的事实和缺口；即使记录写入失败，也不能把命令判为成功。真实运行报告区分live exe实采与已退出null分支。

[Linux wait说明](https://man7.org/linux/man-pages/man2/waitid.2.html)支持以WNOWAIT读取而保留可等待状态；[proc stat说明](https://man7.org/linux/man-pages/man5/proc_pid_stat.5.html)定义上述身份和状态字段。[Python subprocess说明](https://docs.python.org/3.12/library/subprocess.html)区分Popen与实际wait/communicate结果。具体目标Python版本、实现文件及SIGCHLD/线程事实必须在新验证中采集，不能用在线最新文档代替现场版本；本方案不宣称目标exe已被观察。

## 失败装配的后续排除

三轮成功原生运行仍按原27条完整身份处理。另单独绑定本次失败装配的plan、prepared、24项收集manifest、三个已记录临时command身份、bootstrap身份和只读closure原件；不伪装成第四个完整七步运行。

下一bootstrap必须同boot重新检查这些已知PID/组为空，并检查候选3967202及其候选组为空。候选没有已保存start_ticks，不能猜填；即使只是PID复用也保守拒绝，留给明确重新调查。检查本失败目录没有活跃进程、pgdata为空、无PG started/environment；旧目录不删改、不补跑。以新的UUID/cache/PGdata/凭据和原9c只读种子开始一次新准备。

## 验证与后续运行

先做有区分度的局部回归：正常live、快速exit0、快速非零、信号退出、stat与exe之间退出、ENOENT但仍live/非直属/错误UID/身份变化/ECHILD、started或result写失败、原D超时、cleanup身份失配、stdin原字节、无第二次Popen、真实waitstatus不一致。失败原件保留，不能只以mock验证Linux行为。

在新的专用测试目录以有限Linux实测验证本机制，所有child使用独占session、原总D，最终证明已知进程组无残留；只运行小型测试命令，不接触VM、系统服务、时钟或旧证据。采集真实Python/平台事实。新进程helper与准备器差异经独立复核并固定哈希后，才开始新的完整装配；测试命令及其组也要在装配前fresh核为空。测试总期限最多120秒，宿主最多150秒，测试编译如有需要也计入同一总D，不续期。

通过后仍执行原完整七步、298个helper和SQLite/PostgreSQL六叶；仅g2-11 child启用pending诊断。parent9000/host9030、传输1800、装配600、预构建共享900、helpers240/G2111200与integration共享5700、stop/collect共享300保持。下一运行绑定真实最终源码与helper闭包，不能沿用旧SHA猜测一致。装配修好不等于G2输入或业务成功；完整质量、正式G门槛和已授权SSH/Bash交付继续。
