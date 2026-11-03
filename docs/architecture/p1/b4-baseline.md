# B4：首个任务、版本锁定与验收基线

状态：已采用 · 设计已确认、实现未验证（[DISC-007](../../changes/DISC-007.md)-R4，采用范围见[ADR-004](../../decisions/ADR-004-p1-adoption.md)）· 起草基线`ca0dae6` · 承接[#11](https://github.com/wyw-ai/loom-oi/issues/11)、[#15](https://github.com/wyw-ai/loom-oi/issues/15)。依赖[B1](b1-protocol.md)、[B2](b2-security.md)、[B3](b3-storage.md)。本页定义P1做什么、用什么版本做、怎样判定完成，是首个R单实例纵向任务的唯一任务基线；阶段规则见[实验阻挡墙](../../../experiments/GATES.md)。

<!-- sdd-scope:p1-b4 -->
## 1. 已采用约束

| 已采用约束 | 本页的落实方式 |
| --- | --- |
| H12：OIS/OIC首版Go（[ADR-001](../../decisions/ADR-001-core-language.md)） | §2锁定工具链与依赖；CLI语言与驱动不属于H12范围，本轮由ADR-004采用D4-a/b |
| H8＋H15/C3：R基线，H只研究（[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)） | 拓扑只含R；O3只记录R基线，H一列写“不适用/未测” |
| H15/C2：首版单实例 | 一个OIS，SQLite与PG各自单实例运行同一验收 |
| H10：实验未过不进入依赖阶段（[#11原回复](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708)） | P1出口只为申请G1提供证据；G1记录完成前不进入P2 |
| H9/H13 | 无业务SDK、无业务实例化依赖（TV-B4-04） |

<a id="pins"></a>
## 2. 版本与依赖锁定

2026-11-04按一手来源核对（[证据台账E61–E63](../../../kb/evidence.md)）。

| 项 | 锁定 | 说明 |
| --- | --- | --- |
| Go工具链 | `go 1.27.0`＋`toolchain go1.27.3` | 当前稳定主线（另一受支持主线为1.26.10）；本机旧的1.26.3由toolchain机制自动切换 |
| 模块路径 | `github.com/wyw-ai/loom-oi` | 单模块，`cmd/ois`、`cmd/oic`、`cmd/oi`三个入口 |
| PostgreSQL驱动 | `github.com/jackc/pgx/v5 v5.12.0` | 纯Go；要求Go≥1.25 |
| SQLite驱动 | `modernc.org/sqlite v1.61.1` | 纯Go、无需cgo；内嵌SQLite 3.54.1（含WAL-reset修复） |
| WebSocket | `github.com/coder/websocket v1.8.15` | 支持context、读上限与关闭压缩 |
| 测试用PostgreSQL | 18.6 | P1只验证18这一主版本 |
| 其余 | 标准库：`crypto/tls`、`net/http`、`encoding/json`、`log/slog`、`flag`、`embed` | 配置文件用JSON并严格解码；不引入ORM、迁移框架或YAML库 |

更新规则：安全修复按小步提交并跑全部测试；Go跟随1.27.x补丁；升级主版本或更换依赖需要单独评审并更新本表。

<a id="config"></a>
## 3. 初始测试配置

下列值是P1的默认与测试值，**不是SLO、容量或1C1G承诺**；O3/G5实测后再校准。

| 符号 | 含义 | P1默认 | 向量覆盖值 |
| --- | --- | --- | --- |
| `N_OIS`/`N_ACCESS`/`N_SUBJECT` | OIS全局/每Access/每主体的Preparing＋Open通道数 | 64/32/16 | 全局16（TV-B1-16） |
| `MAX_LOCAL_CHANNELS` | OIC本地并发通道 | 32 | — |
| `F` | DATA载荷上限 | 32 KiB | — |
| `HTTP_MAX`/`CTRL_MAX` | 请求体/控制消息上限 | 64 KiB/256 KiB | — |
| `PAGE_MAX` | 目录每页上限（默认50） | 100 | — |
| `MAX_EXPOSURES`/`DESC_MAX` | 每Access登记条目/描述长度 | 128/2 KiB | 3（TV-B2-08） |
| `T_prepare` | 准备期限，票据有效期不超过它 | 10 s | 2 s |
| `T_dial` | OIC拨号上限 | min(5 s, 剩余准备时间) | — |
| `T_close` | 关闭宽限 | 5 s | 1 s |
| `RETAIN` | Closed记录与幂等记录保留 | 60 s | 5 s |
| `H`/`OFFLINE_MISSES` | 心跳间隔/判定离线的连续缺失次数 | 10 s/3 | 1 s/3 |
| `L`/`LICENSE_MARGIN` | 许可TTL/OIC本地扣减余量 | 30 s/2 s | 3 s/0.5 s |
| `PING` | 数据腿ping间隔（2次无pong即断开） | 15 s | 1 s |
| `IDLE` | 两方向均无DATA的空闲超时 | 10 min | 2 s |
| `TOKEN_TTL`/`TOKEN_TTL_MAX` | 消费令牌默认/最长有效期 | 30 d/90 d | 5 s（TV-B2-12） |
| `WRITE_QUEUE`/`SQLITE_BUSY_MS` | 存储写队列/SQLite忙等 | 64/5000 ms | 1/— |
| `STORE_PROBE` | 存储健康探测间隔 | 5 s | 0.5 s |
| `FUZZ_TIME` | 每个模糊目标在CI中的运行时长 | 60 s | — |

内存上界：每个打开的通道，每个组件每方向至多一条在途消息，即OIS、OIC、客户端各约`2F`，另加WebSocket与TLS库缓冲（P1实测计入O3）。`N_OIS=64`、`F=32 KiB`时OIS通道载荷缓冲上界约4 MiB。

<a id="acceptance"></a>
## 4. 拓扑、范围与验收

**拓扑（单机回环，P1在两种后端各跑一遍）**：一个OIS（部署A：api与access两个监听器在同一进程；后端为SQLite文件或PG 18.6）；OIC-A（Access `lab-a`，`lab-a/http`→T1，`lab-a/alt`→T3）；OIC-B（Access `lab-b`，`lab-b/http`→T2）；T1–T3为确定性HTTP夹具，回应自身身份、记录收到的字节，并能按指令慢速响应或中途RST；消费者为`oi` CLI与Go测试客户端；每次运行生成测试CA。TV-B2-09用主机名`localhost`配合不含回环地址的允许网段触发；TV-B2-10用只代理控制连接的测试代理。两个OIC是逻辑上独立的接入身份，不代表主机级隔离。

| P1包含 | P1明确不含（各自归属） |
| --- | --- |
| B1–B3全部P1向量；`oi discover/connect/close`与`oi admin`（Access、指纹、令牌、策略）；Go库流接口；`oi connect`显式回环TCP转发 | 探针与NotReady证据（P2/G2）；H/D与存量迁移（#15研究）；主备与选主（G6）；备份、恢复、迁移工具（P2/G4）；MCP等真实兼容（P4/G7）；Unix socket/命名管道转发（P2）；1C1G资源结论（G5） |

**P1完成判定**（全部满足才申请G1）：

1. 全部标为P1的TV-B1/B2/B3/B4向量在Linux CI上对SQLite与PG 18.6分别通过；零容忍事件（串路、越权接受、旧票据/旧代数被接受、READY前或许可到期后的转发、载荷重放）为0。
2. 资源回归：每个失败向量结束后`T_close`＋1 s内，OIS/OIC的通道数与票据数为0，goroutine数回到基线±10，文件描述符回到基线。
3. `go test -race`无报告；模糊目标各运行`FUZZ_TIME`无失败；`go vet`、staticcheck、govulncheck无未说明的告警。
4. 完整P1产品交付单元（OIS/OIC/oi及其全部自有源码）的单元测试总体语句覆盖率≥95%，包含未执行文件；范围、命令和固定排除项在任务spec预先登记。不得混入集成/E2E覆盖率或临时缩小分母；覆盖率不能单独证明并发或恢复正确。此项是P1完成/正式评审门槛，不阻止中间开发提交。
5. 按[GATES](../../../experiments/GATES.md)完成G1证据：预注册（本页向量与配置）、原始运行记录、ITER综合分析、产物指纹与回读；之后才把G1记为已通过。

| G1判定项（[gates.json](../../../experiments/gates.json)） | 对应向量 |
| --- | --- |
| 真实注册授权双腿通道 | TV-B1-01、TV-B2-03 |
| 双后端CRUD契约 | TV-B3-01、TV-B3-02、TV-B3-03 |
| 目标替换拒绝 | TV-B2-04 |
| 超时与资源清理 | TV-B1-14、TV-B4-03 |
| 无业务实例化依赖 | TV-B4-04 |

<a id="ci"></a>
## 5. 构建、CI与交付顺序

- **Linux（ubuntu-24.04）**：`CGO_ENABLED=0 go build -trimpath`构建linux/amd64、linux/arm64、windows/amd64、darwin/arm64；`go vet`、staticcheck、govulncheck；单元测试与`-race`；store契约测试（SQLite与PG 18.6服务容器）；两后端集成测试；模糊测试；仓库Python检查。
- **Windows（windows-latest）**：构建与单元测试；在默认代码页下（不设`PYTHONUTF8`）运行仓库Python检查，守住[F0](README.md#f0)。
- 代码组织沿用[共同架构§11草图](../alpha-v0.7.md#delivery)：`cmd/{ois,oic,oi}`、`internal/protocol`、`internal/ois/{identity,catalog,policy,relay,store}`、`internal/oic/{config,registration,target,relay}`、`internal/client`、`tests/integration`。P1不建`observation`与`leadership`包。
- 交付顺序：M1协议编解码与模糊测试＋store契约与两后端实现→M2 OIS（身份、目录/策略、通道协调者、中继、管理API）→M3 OIC（配置与revision、控制、准备/拨号、中继、许可）→M4 `oi` CLI与集成夹具→M5两后端全部P1向量通过、CI、G1证据与综合分析。每个里程碑以测试通过的小提交推进。

<a id="observations"></a>
## 6. 观察项在P1记录什么

依据H15“加入观察清单”的要求（[观察清单](../../evidence/implementation-readiness-004.md#observations)）。P1只记录，不设通过线；零容忍事件除外。

| 观察项 | P1记录内容 | P1无法覆盖、留给后续 |
| --- | --- | --- |
| O1 准入效果（C1） | 每次Open的有效模式、readiness（P1恒Unknown）、结果与错误码、POST→READY耗时、READY→目标首字节；授权拒绝与连接失败分开统计（TV-B1-18、TV-B2-02） | NotReady与样本时效（G2） |
| O2 存储与恢复（C2） | 每后端`ApplyConfig`耗时与冲突数、重启到可受理Open的时间、向量套件后SQLite主库与WAL大小（TV-B3-*） | 备份恢复、迁移、增长与维护窗口（G4/G5） |
| O3 路径与总成本（C3） | R基线：固定负载下Open耗时、双向中继字节、OIS/OIC/客户端CPU、RSS、FD与goroutine（TV-B4-06、TV-B1-09）；H一列写“不适用/未测” | H对照（#15）、1C1G目标环境（G5） |
| O4 联合故障与维护 | 存储不可用期间的撤销尝试（按设计503）、重启、控制抖动、许可到期、证书轮换中的清理时间与是否需要人工步骤（TV-B1-12/13、TV-B2-07/10/11、TV-B3-04/05） | 升级/回退流程、HA（G6） |

## 7. 测试向量

| ID | 场景与输入 | 判定 | 门槛/观察 |
| --- | --- | --- | --- |
| TV-B4-01 | 同一提交在CI构建两次四平台二进制 | `go version -m`依赖一致；二进制不依赖cgo | G1 |
| TV-B4-02 | Windows默认代码页、未设`PYTHONUTF8`运行Python检查与Go单元测试 | 全部通过（F0回归） | — |
| TV-B4-03 | 每个失败向量之后检查资源 | 满足§4第2条 | G1超时与资源清理；O3 |
| TV-B4-04 | `go list -deps ./cmd/...`；把T1换成另一种TCP服务 | 依赖中无Loom、LLM或业务SDK；只改OIC配置与描述即可接入，OI二进制不变 | G1无业务实例化依赖（V09种子） |
| TV-B4-05 | 隧道帧解码与控制消息解码各模糊`FUZZ_TIME` | 无panic、无超出上限的分配 | G1 |
| TV-B4-06 | 固定负载：1/8/32个并发通道各传1 MiB与64 MiB | 记录O3各项；只记录不判定 | O3 |

## 8. 本轮采用的具体化与取舍

| 项 | 既有状态 | 本规格 | 理由与替代 |
| --- | --- | --- | --- |
| D4-a | ADR-001只采用核心语言，驱动与版本未批准 | §2的工具链与三项依赖 | 纯Go、无cgo，四平台交叉编译简单。替代：留在Go 1.26（应先升到1.26.10）；SQLite改用cgo驱动需C工具链 |
| D4-b | “CLI使用Go及共享协议类型仍为建议” | `oi`用Go，P1只做discover/connect/close与管理子命令，作为验收客户端 | 与核心共享协议包；公共SDK待API稳定后再提取 |
| D4-c | #15：部署A为低复杂度起点建议 | P1采用部署A（同进程、两个监听器） | 只决定P1，不预先决定以后的A/B/C部署 |
| D4-d | 未规定 | §3初始测试配置 | 测试值，不是SLO或容量承诺 |
| D4-e | “单测覆盖率按适用开发约定执行，辅以竞态与模糊测试” | §5的CI平台与质量信号；完整产品单元测试覆盖率≥95%作为完成门槛 | 恢复用户既定95%要求；本次未采用旧80%建议；覆盖率不替代向量 |
| D4-f | 共同架构§5.3优先Unix socket/命名管道 | P1只做Go库流接口与显式回环TCP转发 | 先完成纵向闭环；本机多用户风险按§5.3在部署中说明 |
| D4-g | G1判定项已在台账 | §4的完成判定与G1映射 | 让“P1完成”可检查，且不以文档或单测冒充G1 |
<!-- /sdd-scope:p1-b4 -->
