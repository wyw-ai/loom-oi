# pending诊断实现后的完整 Linux 质量检查

本规格固定下一次同源开发检查，尚未打包、部署或执行；没有新正式 manifest、ITER 或门槛通过结论。沿用 `coverage/liveness-complete-linux-01` 已审执行器及 [既有完整检查规格](liveness-complete-linux.md) 的 16 步。使用既有专用 QEMU 环境，不借本准备启动、停止或重配 VM、PG、小盘。

## 已核的源码与库存差异

- 产品基线为 `5be6c1d473d7cc1bbd63f6044cdd513ccda6e2c7`。完整 Git 原件共 954 文件、306,933,092 字节，逐 blob 身份、字节数及 SHA256 已保存于 `source-closure.json`；这不是 native 精简源码包。
- 主代理提供的最新包元数据固定 `780dd9f7e04dba25c7a7f91a9c28a6ac1210dc5e`，由 Go 1.27.3 的 `go list -json ./cmd/... ./internal/...`、目标 Linux/amd64、CGO1 获得。该树共 957 文件；相对 5be 仅 5 个 spec 路径不同，350 个 cmd/internal/tests/third_party/产品分析模块与 go.mod/go.sum 原件完全相等。执行主体仍待主代理明确固定，不自动跟随 HEAD，也不把旧执行重新标成新 SHA。
- 已核 22 包、183 个实际选中 Go/Test/XTest 源文件、435 个 Test/Fuzz/Example 根。旧 2a81 有 418 根，本次增 17 根、无删除，包括 registry 两根、periodic 四根、pending 八根及 wsio 三根。完整 unit/race 不能只运行新增 39 节点；39 是 pending 的 8 根/31 子专项必需清单，仍包含于完整包命令。
- helpers 改为实际 298 节点、45 根、10 个定义文件，并绑定全部 67 个 integration Go 文件。旧 Windows 298 节点原 JSON 与库存逐名一致，但仅用作预期名称来源，不借其 PASS 代替 Linux。
- 2a81→5be 的产品变化限 relay 周期工作与 opt-in pending 诊断、server 诊断入口、store 分块 INSERT；wsio 仅回归测试变化。cmd、第三方补丁、go.mod/go.sum 未变。当前 G2 主分析器为 fresh-query 策略；`execution-inputs.json` 保留完整 108 组 `expected_spec()`，另列本轮选中的六 G211/四 G212，不截短完整声明来制造 G2 PASS。

## 16 步与原期限

| 顺序 | 子步骤 | 外层最多秒 | 原命令范围 |
|---|---|---:|---|
| 1 | unit | 1800 | 全部 cmd/internal，`-json -p=1 -count=1 -covermode=atomic` |
| 2 | race | 2400 | 全部 cmd/internal，`-json -p=1 -race -count=1` |
| 3 | vet | 900 | `go vet ./...` |
| 4 | staticcheck | 1200 | 固定 v0.8.2，`./...` |
| 5 | govulncheck | 1200 | 固定 v1.9.0，显式同一快照 file URI，`./...` |
| 6–7 | fuzz-frame / fuzz-control | 各900 | 原两个协议 fuzz，各实际 `-fuzztime 60s` |
| 8–12 | dependency unit/race/vet/static/query | 300/600/120/300/120 | 最终同源 `dependency_quality.commands` 的完整嵌套包命令 |
| 13 | helpers | 240 | 当前 298 节点正则，原独立 integration child |
| 14 | Load | 5700 | 原12组合、164 flows、请求5330MiB，双后端各6次资源检查 |
| 15 | G211 | 1200 | 双后端各3叶，共6叶 |
| 16 | G212 | 600 | EntityCardinality/SlowReaders × 双后端，共4叶 |

guest 仍只有最初 9000 秒绝对 D；host 9030 秒只用于等待与保全。最后四步从 helpers 前启动一个共享 5700 秒 D，每步取自身预算、该共享 D、总 D 的最早者，不相加续期。源码哈希、DB核验、原件写盘、退出及清理仍消耗原期限。各独立 Go child 保留原 TestMain/build 成本，不共用预编译来挪走 child 内耗时。

四个 integration child 沿旧 16 步实际 argv 的 `-timeout 90m`；helpers 外层仍240秒。它与 Windows helper 的 Go3m 命令不同，记录中必须明说。根/嵌套 unit/race 保留旧完整命令的默认 Go watchdog，不复用 foundation 旧120秒失败命令，不添加 `-short` 或部分测试过滤。

## 唯一新增环境检查

普通质量执行默认不启用 `OI_IT_G211_PENDING_DIAGNOSTICS`。分别检查真实父进程 `os.environ` 和选定的 `config.environment`，任一来源包含该键（即使为空或 `0`）均在任何 child 启动前拒绝；child 构造环境再次显式移除并断言不存在。父及全部 child 原始环境记录需分别保存其真实 `present=false/value=null`；child事实取自即将传给 `Popen` 的实际env，不能只记录构造意图。该项是新增的窄接线检查，不伪称仅替换常量。

仅嵌套 unit/race 使用 `CGO_ENABLED=1`、`GODEBUG=netdns=cgo+1`、`RES_OPTIONS=attempts:1 timeout:1` 并核真实同测试 cgo resolver 输出。根和其它工具不继承 resolver 覆盖；所有步骤 `OI_IT_CLOCK=0`，只有 integration 开 `OI_IT=1`，只有 Load 开 `OI_IT_LOAD=1`。保留实际 PG 配置存在性，不把 DSN/账户写入公开报告。

## 覆盖、语义与失败保全

完整 root atomic profile 的原 cmd/internal 分母不删减，自有补丁 native 11 句与无自动覆盖 JS 1 句继续合入分母；用整数 `covered*20 >= total*19` 判断95%。合法零语句 block 按现解析器保留。历史2a81组合7718/8131仅是旧失败记录，不与后续 Windows或局部Linux覆盖拼接。尚无本源完整比率，不为达标添加镜像测试。

嵌套真实包终态、每个实际 run/terminal、固定具名允许 skip 和 no-test 包形状继续由固定 dependency helper 核验；根默认不接受额外具名 skip。完整435根是必需根清单，动态子项以本轮实际唯一 run/terminal 对账，不能借旧节点总数替代。Load 不设吞吐SLO；G211/G212用同源完整行为/资源分析器，各叶失败保留，未执行项不冒充通过。

官方 DB 沿旧已登记快照：ZIP SHA256 `7e893ac89f6b5d7e8d3431ee7cddabc358cea99c8822d3ac545882c38af26c3e`、4715成员。root scan 与 dependency query 使用同一 file URI，前后完整成员摘要、索引、provenance、ZIP及归档绑定不变。query 不能凭 exit0通过，仍解析完整 Config/Progress/OSV 与封口。64上游原件和72嵌套文件+root go.mod的73文件来源封套照旧。

复用旧 `run_step` 的原始 command/popen/process/result/argv/env/D 及退出/组清理记录；独立 stage result 与 outer 嵌入结果逐值相等。出现普通产品非零，只有来源、退出、组清空及原期限完整时才继续后续步骤；监督缺口不能当产品通过。保留各 integration 独立 it 目录中 OIS/OIC/目标/代理日志。全体 host HANDLE/actual exit、guest boot 与收集 tar 全件 SHA 另做独立审计，不能仅凭 `audit_quality` 的产品结论声称整体成功。

## 仍待实际绑定及最小改动清单

源码与元数据库存已收口；不再把 Go metadata 列为阻塞。剩余为最终执行SHA、唯一新run/guest目录、完整归档/transport清单、实际父/child环境、fresh VM HANDLE/boot、既有 PG 身份与版本、32MiB tmpfs 空闲状态、工具与模块/fileproxy缓存、同一DB原件，以及旧子组已退出和本次无并行测试的观察。主代理已读取宿主PID24088与创建FILETIME匹配旧值；这只是宿主当次观察，不能代替guest/PG/小盘就绪。

下一实现只在新 ignored 目录窄派生：prepare 固定源及新库存绑定；build_inventory 接入本次 metadata/298名称/39专项；guest 替换 helper 正则并增加上述显式环境拒绝与原件记录；audit 使用新完整库存及同源分析器。host/transport/生命周期/预算机制优先逐字复用；仅针对差异补纯 mock（诊断环境存在、误继承、少节点、错误源码/DB/原件绑定）。本轮尚未改或运行这些执行器，也未创建可直接执行的config；`execution-inputs.json` 的 `/__BOUND_GUEST_ROOT__` 是明确占位符。

首次纯本地计划生成误读旧 `prepared-01/config.json` 路径，在无Go/SSH之前 FileNotFoundError；原脚本/失败记录已保留。更正为原 `payload/config.json` 后生成成功，先写的来源/库存 JSON 逐值相等且未覆写。该错误不属于业务执行。


先提交并逐字回读本规格，再实施新ignored执行器差异。root已核原期限、完整分母、实际源库存和默认环境边界；这属于既有开发授权，不申请新产品采用。完整本地准备索引为 `coverage/liveness-complete-linux-02/handoff.json`（SHA256 `21ef3b758d4a65150e4de1cf08ec1f2c6b6012592d3c6849554d342a9ece00f7`）。该目录中的原准备计划/输入/库存先保留，新增执行器和验证分开记录，不回写旧运行。
