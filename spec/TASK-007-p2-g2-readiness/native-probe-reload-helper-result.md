# 单次探针重载原生运行：helper 门提前失败

固定源码 `1817e94b9cd4508a96c2e46a000d6f08ae9aff80`、先行规格 `40f4173689061ddedd1bf7415ab3ac3542d614e7`，新UUID `e66f1ea3-639e-4937-9303-2b671391ce35`。993源码与70个helper源码闭包精确。产品与依赖相对cbd未变。[结构化结果](native-probe-reload-helper-result.json)绑定原始输出、期限、进程与独立审阅。

## 实际结果

只执行前六阶段，退出码为0/0/0/0/0/1，总耗时176.774749438秒。428个helper节点全部实际启动并取得唯一终态：426 PASS、2 FAIL、0 SKIP；失败为 `TestG2PreDialHelperMetadata/zero_after_deadline` 及其父，package也FAIL。原Go JSON第606行报告 `socket did not retain shared deadline 1 <nil>`，第608/610行为子/父FAIL。

原执行器在helper门停止，保留 `ValueError: helpers failed; business not started` 与 `commands_complete=false`。**G2-11未启动，六叶执行数为0，没有重载业务、60s或B4结论。** 不运行依赖这些不存在原件的runtime/pending分析，也不构造完整业务记录。

## 失败解释与下一步

原测试在 `ctx.Done()` 后立刻对两条TCP连接各写一字节，并要求都返回timeout。helper在回调前已为两socket设置同一裁剪后的期限；随后自身仍按原gate返回DeadlineExceeded，未启动GET请求writers。Go 1.27.3的context与netpoll使用独立timer，Done关闭不构成两个socket超时标志已发布的同步保证；Unix Write可在检查标志后直接完成一次syscall。这与本次结果相容，但日志没有socket编号及各定时器时点，不能宣称唯一还原了本次调度。

下一步修复这一测试同步假设，同时保留helper原期限拒绝、零请求writer、一次失败通知和回收断言，并以实际阻塞写验证两个socket期限。必须有移除setter、仅设一socket、错误延长到1s和移除gate的有效反例；不得通过sleep、重设原socket deadline或删掉期限覆盖来获得通过。产品、业务夹具核心和原80/200/300/500ms、3s/1s、32Drop、60s与B4判线保持。新完整helper库存固定后，才可在另一新UUID首次执行该来源的完整七步；本次业务没有被重放。

## 原件与回收

准备阶段486项独立核验成立；装配14个子命令均退出0、耗时42.781466198秒。最终生命周期1271项核验覆盖六个实际子进程的原期限、源码后检、wait与空进程组；runner仅按原身份范围Gone，不冒称完整组扫描。提前停止导致外层全局source/platform后检未执行，该缺口保持，不能补成完整pipeline。

执行收集64文件加manifest，130836480B归档逐字回读；准备最终91文件加manifest，ready与final四份固定准备材料完全相同。真实专用PG进程3981813/start2008849587已停止、组空，stop耗时0.147197797秒，stop与collect共用原清理截止。

首次host stop入口因相对 `--out` 被再次拼接目录而在本地mkdir失败，尚未构造SSH；终端观察转录与实际捕获原件明确区分。随后用绝对路径执行的一次真实远端stop退出0，未重放产品或远端stop。全部原失败保留。

19个旧run、14个ITER及正式状态保持：G1 Failed、G2 Blocked、G3/G4 Inconclusive；alpha/main未移动。下一步继续修复并验证，不进入依赖尚未通过门槛的交付阶段。
