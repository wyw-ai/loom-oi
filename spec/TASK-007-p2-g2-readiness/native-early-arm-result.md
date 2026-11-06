# 原生 early-arm 六叶结果

固定 `cbd94ce9c47fdc1e4b82b6bdb0d4bb8589517073` 在授权非生产资源 `target-host-1` 完成一次原定七步。前五步构建/构建信息和342个helpers通过，G2-11四叶局部业务与B4通过、两叶preDial失败。SQLite、PostgreSQL、总测试根与package均FAIL；六个strict组仍保留失败父级，完整108组声明中另102组未执行，完整strict仍FAIL。本轮不是正式证据或完整质量复证。

| 叶 | 实际结果 | cycles | 原慢输入窗口 | B4完成 |
|---|---|---:|---:|---:|
| SQLite 0 | FAIL，pre_dial_full | 0 | 未完成 | 无检查 |
| SQLite 1 | PASS | 116 | 60.133638454s | 121.583909ms |
| SQLite 2 | PASS | 116 | 60.039576945s | 179.580900ms |
| PostgreSQL 0 | PASS | 117 | 60.235295240s | 126.678176ms |
| PostgreSQL 1 | PASS | 117 | 60.264042405s | 123.451263ms |
| PostgreSQL 2 | FAIL，pre_dial_full | 0 | 未完成 | 无检查 |

四个完成叶各自在cycle 0取得独立前置80ms、后置final200ms、critical300ms的完整联合资格；共466个实际cycle均保持各自原helper起点/1s截止及原业务/导出期限。四个B4检查共13样本，各最后连续3次合格且在原2s内。两个失败叶均未建立慢socket、未进入export/final或cycle；没有完成B4不等于已证明泄漏。

## 来源、期限与回收

独立生命周期1367项核验完成：976份Git源码、68个integration源码闭包、342个唯一helper节点（50根292子）一致；原件164项334596022B加manifest，tar334776320B逐字回读。产品流水线438.516604946s，原七步退出为0/0/0/0/0/0/1，无监督errors、阶段超时或源码漂移。原parent9000s、共享构建900s、helpers240s、G2111200s及integration5700s的绝对截止保持，业务退出1没有被监督成功替换。

七个child组在完成及收集时均空；runner按原group=False仅核身份Gone，不宣称扫描了runner全组。专用PG真实postmaster身份已核，实际停止并组空；psql查询backend PID没有当成postmaster。准备阶段42.684530131s，14个临时命令真实wait0；36个正常旧身份、4个旧失败身份及7个机制测试身份的来源分别保留，未知candidate缺失的start/exit仍为null，权限可见性缺口不改成“全系统无进程”。

首次本地build-inputs的`--out`相对路径被基目录再次拼接，mkdir报FileNotFoundError，发生在任何远端动作前；原失败与独立成功02保持。此前生命周期handoff将它概括为CLI路径遗漏不够准确，本记录以原stderr为准。原本地审计PG-wrapper访问层次错误也保留，不计为产品失败。

## 702次诊断关联与两个前置反例

六叶请求数依次224/64/64/61/63/226，共702次原prepare OIS请求全部完整关联，无缺失、迟到、冲突或omitted。本轮固定parser原生接受preload_probe，没有沿用上轮临时枚举替换。身份来自原B4角色PID、启动run_id/epoch、真实welcome/register及完整wire；时间只按原HTTP区间与同锁age/remaining夹定，不相减跨进程时钟。完整关联本身不是业务通过。

SQLite0的202次preDial采样有145次Full；PG2的203次有147次Full，原young全部拒绝。拒绝前缀最后一项年龄分别81.380298–517.182332ms、80.019370–525.873389ms；这些不是全八项最大值。失败capture分别晚于原3s截止0.307614ms、0.733986ms，仍按失败保留，没有续期。

独立关联这些145/147个Full点的真实八pending后，115/117点的八条Received跨度已超过80ms；其余各30点的必要窗口也都在原HTTP开始前结束。全部已采Full点的必要80ms窗口与原HTTP、HTTP至callback及实际young评价时刻均无交集。该结论仅覆盖这些采样，不能推出整个3秒所有未采区间没有机会，也不改变主分析器的latest8规则。

第一批actual八pending的必要窗口仍分别有25.599943ms和21.119535ms。窗口内实际P2/Q4成立，但backlog仅15/13，缺1/3个周期待办。由同锁`min(pending.age)+idle.refresh_remaining`精确相消，缺失源的refreshAt比最新query.started晚0.376288ms及1.128452/1.316157/1.459645ms；因此最近分配时这些源尚未到期，不能称当次pump漏标。首次Full时只用掉8条Drop，young首拒约111ms；32条后期耗尽不能解释首窗错位。原50ms janitor相位与事实相容，但未记录其真实捕获/取锁时刻，不能唯一归因CPU、数据库、环境或某次锁等待。

## 成功对照与范围

SQLite1/2、PG1的前置八query与同次OIS pending集合交集为8；PG0前置交集为0：原HTTP采到旧八pending，随后proxy选到新八query。PG0后置final已实采新批次，四成功叶final交集均8。保留串行采样非原子性，不把邻近debug记录补成同一次critical pending快照；critical没有诊断header。

本轮仅G211子进程开启pending诊断，base/helper环境无该开关。dd62完整Linux质量的95.0922%属于另一源码/默认诊断关闭配置，不合并成本轮证据。后续单次probe reload候选由[独立先行规格](probe-reload-input.md)承接；本结果不证明该候选可通过，不支持原样重跑或放宽80/200/300、32Drop及验收。继续现有首版门槛与授权SSH/Bash交付，19旧run、14ITER、G1 Failed/G2 Blocked/G3与G4 Inconclusive及alpha/main保持。

[结构化结果](native-early-arm-result.json)保存来源、每叶期限/资源及原件SHA。大型原件仍在明确的本地ignored目录，没有冒充已入库正式证据。
