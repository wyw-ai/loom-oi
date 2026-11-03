# B3：存储契约、单实例互斥与重启恢复

状态：评审中 · 采用待确认（[DISC-007](../../changes/DISC-007.md)-R3，整体确认入口见[P1规格包](README.md#confirm)）· 起草基线`ca0dae6` · 承接[#4](https://github.com/wyw-ai/loom-oi/issues/4)。本页是PG/SQLite共同存储契约、最小模式、后端设置、重启与存储不可用行为的唯一详细规格；部署与恢复的共同边界见[共同架构§8](../alpha-v0.7.md#deployment)。P1只实现第2–7节；第8节为P2（G4）实现的契约。

## 1. 已采用约束

| 已采用约束 | 本页的落实方式 |
| --- | --- |
| H8：默认PostgreSQL，无PG的环境支持SQLite并做优化（[#4原回复](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6564934039)） | 一个共同契约、两套实现、同一组契约测试；`store.driver`显式选择 |
| H15/C2：SQLite单OIS本地盘；PG先单实例；主备另验证；显式停写校验迁移；PG故障不得自动切到空SQLite库（[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)） | 两种后端都以进程级互斥保证单实例；存储故障时fail-closed，从不自动换后端；迁移只走§8的显式流程 |
| H13：运行状态可重建（[ADR-002](../../decisions/ADR-002-state-and-binding.md)） | Channel、票据、代数、就绪与幂等记录从不持久化，重启即作废 |
| H10：按节点实验，不越级 | P1出口只要求G1的CRUD契约与重启行为；备份、恢复、迁移与容量归G4/G5 |

## 2. 持久与临时边界

| 类别 | 内容 |
| --- | --- |
| 持久（数据库） | 元数据单行（store_id、后端、config_version、epoch、最近运行）、Access与证书指纹、消费/管理令牌摘要、策略、exposure注册快照、审计、模式迁移记录 |
| 临时（OIS内存） | 控制连接与generation、Channel、票据、幂等映射、就绪观测、心跳与许可计时 |
| 从不进入 | 业务载荷、Agent会话、监测时序、明文令牌或私钥 |

<a id="contract"></a>
## 3. 共同Store契约

授权判断一律读取OIS内存快照；数据库只在启动时整体加载，并在写入成功后同步更新快照。因此请求路径不逐次查库，存储延迟不进入Open的读路径。

| 操作 | 语义 | 错误 |
| --- | --- | --- |
| `LoadSnapshot()` | 读取全部管理对象及注册快照，用于启动 | Unavailable |
| `ApplyConfig(expected_version, mutations, audit)` | 单事务：核对`config_version==expected_version`→应用变更→写入审计→`config_version+1`。全部成功或全部不生效 | Conflict（附当前版本）、Unavailable、Busy、Invalid |
| `UpsertRegistry(access_id, snapshot)` | 以一个事务整体替换该Access的注册快照；只在内容变化时写，revision变化写审计 | Unavailable、Busy |
| `StartRun(run_id)` | 单事务：`epoch+1`，记录run_id与启动时间，返回新epoch | Unavailable |
| `RaiseEpoch(min_epoch)` | 若当前epoch小于`min_epoch`，在事务内提升到`min_epoch`并写审计（§6） | Unavailable |
| `Health()` | 轻量探测（读取元数据行） | Unavailable |

- 所有配置写入串行化：PG为`SELECT … FROM meta … FOR UPDATE`后执行变更（READ COMMITTED）；SQLite为`BEGIN IMMEDIATE`。OIS内由单一写入协程处理，队列上限`WRITE_QUEUE`，满则返回Busy。
- 管理API以`If-Match: "<config_version>"`传入expected_version；缺少时返回428，版本不符返回409 `VersionConflict`并给出当前版本。
- 错误映射到API：Conflict→409，Busy→503（可重试），Unavailable→503 `StoreUnavailable`，Invalid→400。
- 两种后端执行同一个契约测试套件；契约关心语义一致，不要求SQL字符串相同。

<a id="schema"></a>
## 4. 最小模式

| 表 | 主要列 | 约束与索引 |
| --- | --- | --- |
| `meta` | `id=1`, `store_id`, `backend`, `config_version`, `epoch`, `last_run_id`, `last_started_at`, `maintenance` | 单行 |
| `accesses` | `access_id`, `enabled`, `spki_pins`（≤2）, `max_exposures`, `labels`, `created_at`, `updated_at` | access_id为`[a-z0-9][a-z0-9-]{0,62}` |
| `consumer_tokens` | `token_id`, `subject`, `scope`, `secret_sha256`, `expires_at`, `revoked_at`, `created_at` | 索引`expires_at` |
| `policies` | `policy_id`, `subject`, `exposure_selector`（精确ID或`<access_id>/*`）, `actions`（discover/open）, `enabled`, `updated_version` | 索引`subject` |
| `exposures` | `exposure_id`, `access_id`, `revision`, `protocol`, `transport_profile`, `admission_mode`, `description`, `updated_at` | 外键`access_id`；索引`access_id` |
| `audit` | `seq`, `at`, `actor`, `action`, `object_type`, `object_id`, `config_version`, `detail` | 只追加；索引`at` |
| `schema_migrations` | `version`, `name`, `checksum`, `applied_at` | 主键`version` |

时间统一存UTC（SQLite为RFC 3339文本，PG为`timestamptz`）；结构化列在SQLite为JSON文本、在PG为`jsonb`。逻辑模式相同，DDL按后端分别维护。

<a id="backends"></a>
## 5. 后端设置与单实例互斥

| 项 | SQLite（[modernc.org/sqlite](b4-baseline.md#pins)） | PostgreSQL 18 |
| --- | --- | --- |
| 耐久性 | `journal_mode=WAL`、`synchronous=FULL`；建库时（建表前）设置`auto_vacuum=INCREMENTAL` | `synchronous_commit`不得为`off` |
| 其他设置 | `foreign_keys=ON`、`busy_timeout=SQLITE_BUSY_MS`；1个写连接，读连接≤4 | pgx连接池；配置写用READ COMMITTED＋`meta`行锁 |
| 启动读回 | 上述设置逐项读回；`sqlite_version()`必须等于[B4](b4-baseline.md#pins)锁定驱动内嵌的版本（3.54.1，已含WAL-reset修复），升级驱动时同步更新；任一不符拒绝启动 | `synchronous_commit`不为`off`，否则拒绝启动；记录服务器版本（P1只验证18.x） |
| 单实例 | 进程持有`<db>.lock`的独占文件锁；取不到→拒绝启动（`AnotherInstanceActive`） | 专用连接持有`pg_try_advisory_lock`；取不到→拒绝启动。该连接断开→进入Unavailable，重新取得锁后才恢复 |
| 不做 | 不把数据库放在网络共享文件系统；不提供跨机复制 | 不把advisory锁当作选主或HA；主备留待G6 |

PG连接失败时**绝不**创建或切换到SQLite（C2）。

<a id="unavailable"></a>
## 6. 启动、重启、恢复代数与存储不可用

**启动顺序**：打开存储→取得单实例锁→核对模式版本（过新或有待迁移即拒绝，见§7）→读回设置→`StartRun`（epoch+1）→`LoadSnapshot`→打开两个监听器。

**重启的效果**：全部Channel、票据、generation作废；OIC以新generation重连并重新登记；就绪回到Unknown；目录由持久注册快照恢复，`access_online`在OIC重新登记前为false。不存在“旧票据/旧通道在重启后可用”的路径，因为它们从未持久化。

**恢复代数回退检测**：OIC在`hello`中报告`last_epoch_seen`。若大于当前epoch（数据库被旧备份替换或数据丢失），OIS写审计`epoch_rollback_detected`、告警（每次运行只记一次），并执行`RaiseEpoch(last_epoch_seen+1)`：此后新建的控制连接使用提升后的epoch，已建立的连接及其通道保持原值。这使代数重新单调，但**不会恢复备份点之后的撤销**；该风险由§8的维护模式处理。

**存储不可用**（`Health()`探测失败，探测间隔`STORE_PROBE`；或写入返回Unavailable）：

| 行为 | 规则 | 理由 |
| --- | --- | --- |
| 新Open | 503 `StoreUnavailable`（fail-closed） | 期间无法提交撤销；继续放行会让“撤销不可执行”的窗口与新访问叠加 |
| 管理写入 | 503 | 同上 |
| OIC登记 | 内存中接受，恢复后持久化每个Access的最新快照 | 登记是运行事实，不应让OIC反复重连 |
| 已开通道 | 继续到正常结束、许可到期或空闲超时 | 不因存储抖动中断业务流 |
| 恢复 | 探测成功后自动恢复，无需重启 | — |

Busy（写队列满或SQLite忙等超时）不改变上述状态，只让该次请求返回可重试的503。

<a id="migrations"></a>
## 7. 模式版本与迁移命令

- 迁移脚本按后端嵌入二进制，编号递增，记录校验和。
- `ois serve`遇到数据库版本高于二进制已知版本→拒绝（`SchemaTooNew`）；遇到待执行迁移→拒绝并提示`ois migrate`；已执行迁移的校验和不符→拒绝。
- `ois migrate`显式执行全部待迁移（每个在一个事务内），执行前提示备份；不在`serve`时自动迁移。

<a id="backup"></a>
## 8. 备份、恢复与后端迁移（契约，P2实现并在G4验证）

- **备份**：SQLite用`VACUUM INTO`生成一致副本并对副本做`integrity_check`；PG使用部署方的`pg_dump`/`pg_basebackup`。不直接复制运行中的数据库文件而忽略WAL。
- **恢复**：只能在OIS停止时执行`ois restore`；写入审计`restored_from`并置`maintenance=true`。OIS以维护模式启动：管理API可用、新Open一律503，直到管理员核对并重新执行备份点之后的撤销，再`ois admin resume`。同时按§6前推epoch。
- **SQLite→PG**：停管理写入→`ois export`（带版本、逐表计数与哈希的JSON）→`ois import`到空PG库（单事务）→校验计数、哈希与权限→改配置并重启→OIC重连。任一步失败保留原库为权威；不双写；回切需要再次导出导入。
- 这些命令P1不实现；P1只保证模式与契约不阻碍它们（例如审计只追加、meta含maintenance列）。

## 9. 测试向量

| ID | 场景与输入 | 判定 | 门槛/观察 |
| --- | --- | --- | --- |
| TV-B3-01 | 契约测试套件分别对SQLite与PG 18.6运行：增删改查、分页稳定、冲突、审计同事务、错误映射 | 两后端全部通过且错误映射一致 | G1双后端CRUD契约；O2 |
| TV-B3-02 | 两个`ApplyConfig`并发使用同一expected_version | 恰一个成功，另一个Conflict；审计恰一条 | G1；O2 |
| TV-B3-03 | 在写审计之前注入失败 | 配置变更与审计都未写入 | G1 |
| TV-B3-04 | 两后端各自重启OIS，重启前有打开的通道 | 旧通道全部终止；旧票据附着410/401；epoch+1；OIC以新代数重连；目录恢复；就绪Unknown（零容忍：旧通道或票据复活） | G1；O2/O4 |
| TV-B3-05 | 运行中停止PG | 管理写503、新Open 503 StoreUnavailable；没有创建或切换到SQLite；已开通道继续；PG恢复后不重启即恢复 | G1；O2/O4 |
| TV-B3-06 | 第二个OIS指向同一SQLite文件或同一PG库；PG锁连接被断开 | 拒绝启动；锁连接断开后进入Unavailable，重新取得锁后恢复 | G1 |
| TV-B3-07 | 数据库模式版本高于二进制；有待执行迁移；已执行迁移的校验和被篡改 | 均拒绝启动并给出原因 | G1 |
| TV-B3-08 | SQLite设置或引擎版本不满足§5 | 拒绝启动 | G1；O2 |
| TV-B3-09 | 撤销调用返回后立即发起提交检查（与TV-B1-11/12交叉） | 必然看到新版本 | G1 |
| TV-B3-10 | 以较旧数据库启动，OIC报告更大的`last_epoch_seen` | 审计告警，epoch前推到更大值 | G1；O2 |
| TV-B3-11（P2） | 备份恢复、维护模式、SQLite→PG迁移、磁盘满/只读、长读阻挡检查点、WAL与文件增长 | 按§8及[共同架构§8.1.1](../alpha-v0.7.md#deployment)判定 | G4/G5；O2 |

## 10. 相对共同架构草图的待确认差异

| 项 | 共同架构原表述 | 本规格 | 理由与替代 |
| --- | --- | --- | --- |
| D3-a | `ApplyConfig(expected_version, changes, audit)` | 全局单一`config_version`乐观并发，审计同事务，管理API用`If-Match` | 实现与测试最简单；代价是全部管理写串行（P1写入量很小） |
| D3-b | “初版倾向WAL与保守耐久性”“有界单写队列” | SQLite：WAL＋FULL＋增量vacuum＋单写队列＋锁文件；PG：READ COMMITTED＋行锁＋advisory锁单实例，锁丢失即fail-closed | 具体化；advisory锁只防误启动第二实例，不是选主 |
| D3-c | 未规定 | 显式`ois migrate`；待迁移或版本过新拒绝启动 | 避免无意识升级损坏数据；代价是升级多一步 |
| D3-d | “恢复任期” | 每次启动持久epoch+1；OIC回报`last_epoch_seen`检测回退并前推 | 不持久任何票据或通道；回退只能检测和告警，不能自动找回被回滚的撤销 |
| D3-e | 未规定 | 存储不可用时新Open与管理写fail-closed，已开通道继续 | 正确性优先。替代为按内存快照继续放行：可用性更高，但期间无法提交撤销 |
| D3-f | C2已定迁移方向 | 备份、恢复（维护模式）与SQLite→PG步骤现定为契约，P2实现、G4验证 | P1聚焦纵向闭环；不提前实现未经验证的维护工具 |
