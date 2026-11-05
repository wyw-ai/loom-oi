# ITER-014：首轮本地 G1 复证的失败与证据边界

2026-11-05。本轮新增一个正式失败样本，归属 C56、E78、EDGE-049、ITER-014；原件核验与分析完成，Issue 追加发布回读待完成。 固定源码 `d9b884929aec6c8a14460876f13c14958239f652` 的本轮结果为 **observed/fail**；unit、race、vectors 实际退出 1。唯一封口者已完成原件保存及独立复算，来源监督 complete、errors 为空，primary 因产品未通过被拒。来源完整不等于产品通过；原 report 的 95.2% 来自缺失 metrics 包的残缺 coverage profile，也不能作为完整覆盖率达标证据。

本轮需要回答的是：消费者能否在准确目标、连续授权和明确失败边界内，获得有界、可解释的连接。现有证据包含部分响应中断、测试前置未建立和仪器测点缺口，不能证明该固定源码整体满足 G1，也不能把不同失败统一归因为 CPU、TCG 或 Ping。后续必须固定修正源码和新预注册重新完整运行，不能挑选通过片段替换本轮。

## 原件、预注册与唯一归属

- 本轮唯一 run 为 `G1-P1-R5-20261105T033158Z-5e02d0`；本地执行身份为 `local-4e0bb1ce-6b07-4d7a-a856-61cd941efbbb`。预注册为固定 d9 对象中的 `experiments/manifests/G1-P1-R5.json`，committed_in=d9，原 manifest SHA256 为 `c686a2c649659b8470acad05e224cefc951ff815840bd1bd570f30e3bc1c0b45`。本轮样本只归属ITER-014；不并入此前已完成 ITER，也不把若干开发窄测拼成另一个完整运行。
- 环境是明确登记的 `local-isolated-vm-v1` 专用隔离 Linux VM；用户已选择转为本地继续工作。它不是 GitHub hosted 运行，不是 G5/G7 用户指定的 SSH/Bash 主机。实际输入包括 Go1.27.3、CGO=1、SQLite3.54.1、PG18.6、32MiB 小盘、离线模块代理和固定官方漏洞库。普通 G1 的 clock 回执为 null，未执行真实跳钟；实际命令及身份以原 env/source/raw/attestation/host 回执为准。
- 原 unit/race 采用原默认包级并行调度，未改为后续提出的 `-p=1`。原 cmd/internal 覆盖范围与完整分母要求、race、主动 fuzz、所有必需项、产品期限及资源判线保持。附带执行 G2 普通测试并不把本轮扩大为 G2 正式验收，也不替代其普通行为、真实跳钟和同 run 资源复算。
- 本地保存了原始 JSON 行、按 step/package/test 重组的 Go Output，以及被测三服务日志。Load 与 G2-11/12 两份独立诊断分别核对了 9 个相关源码文件；本报告再次直接读取 d9 Git blob，两组各 9/9 与诊断记录 SHA 相等。封口者另核完整 232 项 source 与 d9 Git 对象精确一致，guest 为清洁 d9；不以当前脏工作树代替被测源码。
- 原件为[正式run](../../experiments/runs/G1-P1-R5-20261105T033158Z-5e02d0/report.md)，现共 15 文件，含漏洞库 ZIP/provenance；封口核验口径为 11 份 guest 原件与 host 字节一致、13 份 seal 文件，不能混称旧格式10文件。raw 为 86,338,042B，summary 为 88,539,045B，均保留原字节。两份Windows来源JSON原有CRLF，对其精确保存路径禁用Git文本归一化，提交blob须与原件逐字一致。`SHA256SUMS` 自身 SHA256 为 `048d0d209f441821bc019783855f5d5e5d9b9ca618336493bc169cb8edcd5c64`；execution receipt 为 `04e50a9270e33b5900cb06f26c249c8957f4d2ec1f59cf6cec8cc0a1e1553ebb`。
- 独立 `verify` 结果为 integrity=verified、recomputation=observed、execution_supervision=complete，复算精确再现 observed/fail。host/guest 均 exit1，errors=[]；runner 与八步骤真实进程组均 gone 且 remaining_pids=[]，无 step timeout，VM boot 未变。宿主入口从 03:31:56.317846Z 至 05:05:32.928758Z，实耗 93m36.611s。15文件已逐字复制入库；[独立核验](../../spec/TASK-007-p2-g2-readiness/local-g1-first-validation.json)保留来源、实际argv与失败计数。被测[分析器及其相邻依赖](../../experiments/sources/product-local-first/analyze.py)从d9原字节归档；接入新工具后再次按归档精确复算失败，不改原report/summary。

## C56 结论与整体影响

> 固定 d9 的首轮本地 G1 复证出现真实断言失败及末组负载的部分响应中断；现有证据不支持该源码满足完整 G1，也不足以唯一归因执行环境或 Ping。必须保留原失败，分别修正已定位的输入/测量缺口、补足首次终态见证，并在新固定源码上完整复证。

这是一项实现级反证及适用性限定，不是新产品取舍。原 summary 五项直接 criterion 中，“双后端CRUD契约”“无业务实例化依赖”“目标替换拒绝”“真实注册授权双腿通道”为 pass，“超时与资源清理”为 fail，其中 TV-B4-03 资源完整性不能成立；此外整体质量、控制与 Load 失败仍在 problems 中，不能把“四项 pass”改写为整体通过。历史 G1/G3/G4 Passed 仍只属于各自原固定提交和环境，不能自动覆盖本分支的新产品、诊断与夹具。旧 Failed run、旧报告和回执继续保持。当前G1保持Failed，G2为Blocked、G3/G4为Inconclusive；历史validation保持，当前门槛和KB同步本轮反例。

| 原始失败 | 直接证明 | 尚未证明的根因或产品结论 |
| --- | --- | --- |
| unit/race 的 metrics 活动槽测试 panic | 真实 Handler 在 render 后的原 200ms 截止检查拒绝，裸测试 goroutine 未由 net/http 隔离 Abort sentinel | 不能拆分 render、调度或 GC 耗时，也不能据此宣称正常导出总性能或 Activity 归属错误 |
| race 的许可、final、服务启动测试 | 各自没有完成目标断言所需的准备或启动事实 | 不能由 helper 超时推断产品延长许可/准备截止，也不能把 context canceled 直接归因为 CPU |
| B2 两后端控制切断 | 原测得 LicenseExpired 分别为 1.9532166s、1.9129249s，未满足原 2–4s | 原切断时没有完整请求窗口的剩余许可见证；不能唯一认定产品提前撤销或唯一认定夹具原因 |
| Load 两后端最后 64MiB×32 cell | 6 个已准入流收到部分响应后 ChannelInterrupted，未完整交付 | 没有首次 winning 终态的实际操作/错误阶段，不能唯一认定 Ping、IdleTimeout、目标先复位或执行环境 |
| 附带 G2-11/12 | 原必需 Go 路径存在输入资格失败、配置写拒绝或关闭前慢读证明未成立 | 不构成 G2 正式通过；也不直接证明 exporter 200ms、控制 500ms、实体基数或资源标准被产品突破 |

## unit/race：覆盖数值不能替代完整质量

原 unit coverage profile 独立计数为 **6925/7276 statements，95.1759208356%**，原 report 四舍五入打印 95.2%。**metrics 包在 panic 后没有产出其 coverage，7276 是残缺 profile 的分母，不是完整 cmd/internal 分母。** 实际命令没有缩包，但命令完整不等于 profile 完整；原数值不能作为完整 ≥95% 的证据。保留原 report/summary，不回填、删包或据其它环境数值修饰本次覆盖结果。

`TestExporterActivityExcludesRejectedWriter` 在 unit 和 race 均出现 `net/http: abort Handler`，栈指向固定源 `internal/metrics/handler.go:129` 与 `activity_test.go:64`。Handler 自入口开始使用原 200ms 预算；此栈证明 render 后检查已到截止。原件没有细分各段耗时。完整 raw 的 DATA RACE 报告数为 0，但 race 步骤仍因测试失败退出 1；“没有该类报告”不能写成 race 通过。panic 也导致后续活动槽根/子项缺失，必须保留 missing，而非将包内未执行项计为通过。

race 另有三项失败：`TestLicenseExpiry` 在已发送 READY 后未于原观察窗捕获 local READY gate（6.31s）；`TestFinalCannotExtendPrepareDeadline` 在 attach 前置失败（5.67s），尚未进入目标 final 断言；`TestServeCommand` 固定 sleep 后取消，返回 1 且 stderr 为 `ois: context canceled`（1.64s），原测试没有先证明真实 started。完整 raw 没有许可 D、final D 与首终态的全部时序，因此暂不把这些解释成同一个机制。四类测试的输入细化已先行入规格，实施状态见后文。

## 控制许可：保留 2–4s，补测点而不放宽

`TestTVB2_10_ControlCut/{sqlite,postgres}` 原错误分别为 `LicenseExpired after 1.9532166s` 和 `1.9129249s`。原环境基线的 lab-a 剩余许可为 2029ms、1982ms，服务日志中的 connected 到 license expired 墙钟差约 2.502s；这些与短许可在装配中已被消耗相容，但基线不是切断时刻，墙钟日志也不是完整故障单调时间证据，不能用它们倒造本轮注入资格。

旧测试在同步 cut 返回后才开始计时，且没有对同一实际 channel/control/target 的 Running 与故障时剩余许可作完整见证；shared `SampledNS` 又是在 stats HTTP 完成后采集，用它扣 elapsed 会遗漏请求 RTT。后续已固定的 request-window 方案保留原共享字段语义，另记录 request started/completed 和实际应用时点。许可整数毫秒按该许可契约，以 `max(0, remaining_ms×1ms−(apply−request_started))` 独立扣完整窗口；它不是 readiness 的另一套 floor 规则。

新输入必须在原准备预算内证明同连接目标实际收到数据、精确控制 tuple/代数与完整 stats 一致，再施加一次 cut。终态同时满足 `terminal−cut.completed ≥2s` 与 `terminal−cut.started ≤4s`；两次 Close 的区间不能从计时中丢掉。先只读确认对应 Access 已离线，再只做一次实际 Open POST 验证拒绝，不能通过重复业务 Open 等待状态。恢复必须对应唯一新控制连接，原 B4 基线、PID/拓扑、精确 FD、goroutine±10 和原 2s 回收全部保留。

G3 相同接缝只新增完整请求窗口与纳秒证据，旧 34叶/62phase、业务许可下界及原资源线不变。本轮未采到这些新字段；不能事后补进旧 raw，也不能以新 parser 的合成测试把旧控制失败改判。

## Load：准确绑定与资源归位不等于完整响应

两后端前五个 O3 cell 均完成 50 条流；最后 64MiB×32 cell 中，SQLite 完成 28 条、失败 worker 7/14/17/25；PostgreSQL 完成 30 条、失败 worker 21/24。六条均完成 15B 请求及请求 FIN，实际收到约 13–18MB 响应后 ChannelInterrupted。独立诊断已逐条关联 channel、OIC 目标四元组和唯一目标记录；未观察到串路证据。

OIC 首终态为本地 ChannelInterrupted，OIS 也以该码关闭；目标先记录完整请求和 EOF、首次响应 Write 成功，之后才有 Write error/Reset/Done。目标复位可能是 OIC 关闭的后果；现日志不足以决定谁先造成终止。两端转发字节计数差不能单独证明缓冲超限，也不能把部分响应视为 Completed。O3 吞吐只是观察：SQLite 最后 cell 约 28.94MiB/s，PG 约 29.12MiB/s；不增加吞吐或 32 连接生产容量承诺。

两叶共 12 个资源清理检查的实际测量均在原窗口内恢复原 PID/固定拓扑，最终连续三次满足 FD 精确相等、goroutine±10、通道及票据为空；SQLite 最慢 203.1286ms，PG 226.7741ms。独立资源重算仍保留两叶 Go FAIL，不因清理数值达标将整体结果改为 pass。这些局部资源事实不替代全 run 必需项/资源复算。

源码存在值得区分的 Ping 路径：OIS 即使检测活动/转发也先真实调用 Ping，coder/websocket v1.8.15 在**已进入实际控制帧 socket Write** 的 context 到期时可能关闭共享连接，随后 activity 判断不能恢复连接；仅等待写锁或等待 pong 到期则不能套用同一关闭结论。原件没有这些六条流的 Ping 阶段或首次 Close 来源，故这只是竞争机制，不能写成唯一根因或已证实修复方向。下一次需记录 winning 终态的 operation、有限 error class/context、FIN 状态与字节数，并关联 OIS 的实际 Read/Write/Ping 阶段及显式 Close 来源；保留正常 FIN、取消和许可优先，不改变传输行为或期限。

## 附带 G2-11/12：必须保留失败的前置阶段

G2-11 六叶全部 FAIL，成功 `competition_cycle`/`competition_complete` 均为 0。三叶未在原 3s 内取得 backlog16/query8/probe2+queued4/young 的组合；另三叶已取得组合，但随后 cycle0 在原 1s 的 `await_export_active` 阶段失败。后者有效样本虽多次 Active=2，YoungestActiveAge 最大仅 22.3765/27.624/22.2043ms，未达原 40ms。两条慢连接各 147456B、首头 HTTP200 已实采，但后续 429 observer 尚未开始，更未进入 native/revoke 关键处理。本轮不能把这些失败归因第三个 observer 抢槽，也不能将历史首个200或瞬时 Active=2 视为同两个写入持续阻塞。下一次应在原预算内保留最后组合样本和各谓词、真实 query 身份及写出/期限；如需追踪 exporter，使用已存在的透明诊断，以有限 acquire/release/Write/Flush 事件区分快速轮转与持槽阻塞，不能延长预算或增加连接取得资格。

G2-12 实体基数 SQLite 叶实际完成 32 个实体与两端 64 次后置 exposition，并完成原资源检查；PG 只完成 10 个实体，第 11 个创建 token 时 StoreUnavailable。OIS 先有 `store write failed: timeout: context deadline exceeded`，稍后才 available again；这支持失败锁存期间拒绝写的真实路径，不定位具体 SQL/连接等待耗时，也不证明 PG 关闭修复导致故障或数据库损坏。

两个 SlowReaders 叶都没有成功 `export_slow_round`：SQLite 失败文字虽打印 `rejected=true failures 0 -> 2`，其中 after 是 `slow.Close()` 之后读取，不能补成关闭前两次 write failure 的证据；PG 在第二段 observer 请求达到共用原截止。两叶的实际 native 请求已经 Completed，但慢读复合输入/判定没有成立。当前失败原件缺 role/round、原 stop、完整第二段 polls 与 close 前后顺序，下一次应先保存这些有限事实再清理，不以重抓或重试业务补造通过。现产品已有 Flush，不以“缺 Flush”解释这些失败。

## 八步骤状态与封口边界

下表抄录独立封口核验中的实际 step end；这些不是新的计时承诺。八步均已运行到终态，原前三失败与后五成功同时保留。

| 步骤 | exit | 原始耗时 ms | step timeout |
| --- | ---: | ---: | --- |
| unit | 1 | 174203 | false |
| race | 1 | 311873 | false |
| vectors | 1 | 4445434 | false |
| fuzz-frame | 0 | 300377 | false |
| fuzz-control | 0 | 64772 | false |
| vet | 0 | 20400 | false |
| staticcheck | 0 | 125104 | false |
| govulncheck | 0 | 33316 | false |

Frame/Control 两个真实 fuzz 目标均 PASS，原命令指定 `-fuzztime 60s`，均有 active marker 和4 workers；目标耗时分别61.08/61.19s，最终195023/69817次执行。步骤 wall duration 还包含构建等工作，不能与主动执行时长互换。安全扫描实际使用 `govulncheck@v1.9.0 -db file:///home/oi-vm/.local/task007/vulndb/20261104T174251Z ./...`，返回 No vulnerabilities found；4715-member 固定官方 ZIP/来源/索引与扫描前后目录哈希由封口者复核。原始 DATA RACE 和 ZERO-TOLERANCE 行均为0；这不抵消已记录的断言与传输失败。

Go Test 动作计数为 unit1596 PASS、race1593 PASS及3个显式Test FAIL（另有metrics包panic）、vectors735 PASS/20 Test FAIL/7 SKIP。这些包含父/子测试，不能当唯一叶数。七个向量skip为两个仅诊断根 `TestG2CompetitionActiveInput`、`TestG2CompetitionSlowInputTrace`，四个不适用后端叶 B1-17/postgres、B3-11f/postgres、B3-05/sqlite、B3-08/postgres，以及普通本轮不运行的 `TestTVG2_04_RealClock`；不将它们计为PASS。race另有三个无测试文件的cmd包级skip，与向量七项分开计数。完整明细保留于[诊断复核明细](p2-local-g1-first-details-014.json)。

资源整体不能通过，原因不只是两 Load 叶的 Go FAIL：两个 B2 cut 叶与失败的 G2-11/12 叶未到既定资源检查点，原 summary 如实记录缺少 passing Go test、checks低于登记数量。进程最终被监督器回收、失败测试 cleanup 执行或 Load 的局部连续样本达标，都不能补造缺失的原窗口资源证明。

## 静态修正与下一次完整回归

以下是**后续源码准备**，均不属于本轮 d9 运行，也不表示新 Linux Go 通过：

| 已固定规格与目前状态 | 保持不变的判线 | 下一次必须验证 |
| --- | --- | --- |
| 四处单测输入：metrics 用两 POST/NoBody 真实503持槽，许可先 Running 再一次原短心跳，final 在同一原 D 最后200ms施加，Serve 等真实 started 后取消；host 静态修改已审 | metrics200ms/容量2；原600ms有效许可；原总 D/min；原启动/停止观察预算 | 原四根与相关真实网络/截止测试、完整 cmd/internal/race；不能仅测修正四根 |
| 新 local unit/race 显式 `-p=1`；runner/analyzer 已实现并在原封口后接入主树，有 Python 反例验证 | 包范围、count、覆盖分母、race、1800/2400s及共享总预算；其它步骤原命令不变 | 新预注册声明与实际完整argv、唯一空GOFLAGS事实一致；原无声明 case 复算不变 |
| request-window 控制见证；Go 发射器已静态编写，Python 严格验证已有合成反例/兼容复算 | 六控制叶身份、原2–4s、原B4/G3资源与行为线，不强迫非G3 case承诺整34/62 | 实际新 Go emission 与独立 parser 对账、完整六叶及原资源检查；不是补写旧raw |
| 首终态/固定库 Ping 阶段有限诊断；产品与新测试尚处 host 静态阶段 | 不改关闭行为、产品预算、首终态优先或对外协议，不记录秘密/任意错误正文 | 真实 Ping 三阶段、许可/取消/正常 FIN 竞争、清理 join；最终全量及新 Load 原件 |

上述先行规格分别由 `13fdd25cd0ef568908daa32594a8f73966696633`（四输入）、`c68c380f5b559bf9fcf1d7aab2adcace722a4665`（控制见证）、`840b72ebedbd82e8d2aaddaa705b6984f8606544`（终态诊断）定位。G2失败即时记录另由 `a73a290df4c5b182e79df0b7fcf949ae73bdd001` 的[规格](../../spec/TASK-007-p2-g2-readiness/input-failure-diagnostics.md)限定；新Go代码尚未编译执行。封口后主树39项tools通过；experiments运行375项，其中10项Windows平台skip、其余通过；归档分析器实际重放本run核验通过。静态独立评审以及 Python 合成验证仅支持相应实现接缝，不能填入本轮正式成功栏。

本run原件与唯一ITER-014同次保存，写入C56/E78/EDGE-049并联动当前门槛、架构/模块入口；图06已准确表示G1 Failed及依赖关闭，本轮保持原图/SVG字节。随后按既有追加规则发布失败及下一步，不编辑旧 Issue 正文/评论、旧 18run/13ITER 或任何 Accepted 对象。新源码正式运行须重新固定 source/case/身份，保留本轮全部失败。G2 的三项人类采用不重复询问，G5/G7 原生 SSH/Bash 的原鉴权、实际主机规格与未知结果不重放边界不因本地质量工作改变；本轮没有这些部署或兼容性证据。

## 可复核来源与边界

- [原始run及报告](../../experiments/runs/G1-P1-R5-20261105T033158Z-5e02d0/report.md)、[完整raw](../../experiments/runs/G1-P1-R5-20261105T033158Z-5e02d0/raw.jsonl)、[固定预注册](../../experiments/manifests/G1-P1-R5.json)：原文件没有补写后续诊断或改判。
- [独立封口和232源码核验](../../spec/TASK-007-p2-g2-readiness/local-g1-first-validation.json)以及[逐流、G2输入、覆盖与原始计数明细](p2-local-g1-first-details-014.json)：后者是从本run提取/关联的分析材料，不是新增样本或passing primary；保留各原诊断JSON哈希。
- [控制许可见证](../../spec/TASK-007-p2-g2-readiness/control-license-witness.md)、[首终态诊断](../../spec/TASK-007-p2-g2-readiness/terminal-diagnostics.md)、[失败即时诊断](../../spec/TASK-007-p2-g2-readiness/input-failure-diagnostics.md)：分别限定后续修正和区分性观测，不倒写本run的根因。
- [ITER-014](../../experiments/iterations/ITER-014.json)将本产品失败输入按既有枚举记为inconclusive；这只是“不可作为passing primary”的用途，不更改实际observed/fail。I3-P12、I4-P08、I11-P14、I18-P10已追加并逐字回读；[真实发布回执](../issue-history/2026-11-05/TASK-007-local-g1-first-published.json)核验四单元数据、50条旧评论、8个固定文件链接和4个前序评论链接。ITER-014闭环完成，当前不晋级任何门槛。
