# TASK-005：P2双后端单实例恢复（B3§8、TV-B3-11，G4出口）

2026-11-04 · 基线`b50c08e228aa1c01686b35a59421ad2670154219`（alpha，G1已记录）· 工作分支`feature/TASK-005-p2-g4-recovery` · 集成目标`alpha` · 关联[#4](https://github.com/wyw-ai/loom-oi/issues/4)（存储与恢复）、[#11](https://github.com/wyw-ai/loom-oi/issues/11)（门槛）、[#18](https://github.com/wyw-ai/loom-oi/issues/18)（采用）。

## 基线与授权

- 本会话用户指令（原话见[TASK-004](../TASK-004-p1-r-vertical/README.md#基线与授权)）：启动Autopilot完整开发并交付已确认首版，完成必要验证与评审后可合入alpha并继续；已确认事项无需重问。
- 采用来源：[B3§8](../../docs/architecture/p1/b3-storage.md#backup)（ADR-004/DISC-007-R3：备份、恢复与维护模式、SQLite→PG显式迁移为“P2实现并在G4验证”的契约）、TV-B3-11及其引用的[共同架构§8.1.1](../../docs/architecture/alpha-v0.7.md#deployment)（H8双后端、H15/C2单实例与显式停写迁移）。起步时P2入口`python experiments/check_gates.py --stage P2`为RECORDED PASS（G1，ITER-004）；本轮评审发现旧资源断言缺口后，G1须暂降为Inconclusive，先按下节补证，不能沿用旧通过记录晋级G4。
- 本任务只实现已采用范围。下表“实现细化”是落实B3§8所需的接口细节（同TASK-004对管理API、错误体等的细化方式），不改变已采用语义；在#4阶段评论中列出供核对。没有新的产品取舍，因此不建立`approval.json`。
- 基线同步：顺带更正[共同架构§11.1](../../docs/architecture/alpha-v0.7.md#delivery)中G1通过前的状态句与“目录尚未创建”的旧说明（TASK-004遗漏，只改状态文字）。

## 目标与边界

实现B3§8：SQLite一致备份、两后端恢复与维护模式、跨后端停机迁移；按§8.1.1补齐SQLite规模与维护控制（WAL/空闲页可观测、低峰检查点与增量回收、审计导出后修剪）及磁盘满/只读的fail-closed与自动恢复；以TV-B3-11（细分为a–i）的真实实现证据申请G4。

不含：探针与就绪发布（G2）、真实路径传输故障（G3）、容量数值承诺（G5）、PG主备与选主（G6）、由OIS执行的PG物理备份（按B3§8使用部署方`pg_dump`/`pg_basebackup`）、自动周期维护调度、零停机或双写迁移。

### 已采用范围内的实现细化

| 项 | 实现细化 | 对应规格 |
| --- | --- | --- |
| 维护模式拒绝码 | 新Open返回503，错误码`Maintenance`（客户端处理：退避，等待管理员恢复）；与`StoreUnavailable`（存储故障）区分 | B3§8“新Open一律503”、B1§7错误表 |
| 进入/退出维护 | `PUT /v1/admin/maintenance` `{"maintenance":bool}`，必须带`If-Match`，作为配置写入（config_version+1、审计`maintenance_entered`/`maintenance_resumed`）；`ois admin resume`按B3§8退出，`oi admin maintenance on/off`为通用管理命令；凭据与URL同`oi`（`OI_URL`、`OI_TOKEN`、`OI_CA_FILE`或对应参数） | B3§8、B3§3 |
| `ois backup` | `--config F --out PATH`：只读连接执行`VACUUM INTO`（OIS可运行），对副本做`integrity_check`、核对meta与模式版本，输出含SHA-256、store_id、config_version、epoch、模式版本的JSON；输出路径已存在则拒绝；失败删除未完成副本。PG后端拒绝并提示使用`pg_dump -Fc`/`pg_basebackup` | B3§8备份 |
| `ois restore` | 只在取得单实例锁（OIS停止）时执行。SQLite `--from PATH`：校验副本后原子替换，旧库与WAL改名保留为`<db>.replaced-<UTC>`；`--external LABEL`：数据库已由部署方工具（`pg_restore`、文件系统快照等）恢复，只做标记。两者写审计`restored_from`、置`maintenance=true`，epoch取max(副本epoch, 被替换库epoch, `--min-epoch`)；副本store_id与被替换库不同则拒绝。需`--yes` | B3§8恢复、§6前推epoch |
| `ois export`/`ois import` | 取得单实例锁后导出版本化JSON：来源后端/引擎/store_id/模式版本/meta，逐表行数与SHA-256及全部行（规范化：主键排序、UTC微秒时间、结构化列键排序、二进制base64）。导入只接受空目标（无meta行），在一个事务内建表后写入全部行、保留store_id/epoch/config_version/审计序号，事务内回读重算逐表计数与哈希一致才提交，并追加审计`imported_from`。方向不限（SQLite→PG为主，PG→SQLite用于回切）；需`--yes` | B3§8后端迁移 |
| 存储观测与维护 | `GET /v1/admin/store`：后端、引擎、可写状态与最近写入错误类别；SQLite另含库/WAL字节、页大小、页数与空闲页，PG含数据库大小。`POST /v1/admin/store/maintain` `{checkpoint:"passive"|"truncate", incremental_vacuum_pages:N(≤10000), optimize:bool}`经单写队列执行并返回前后对比；PG只支持`optimize`（ANALYZE）。离线`ois maintain`执行同一操作 | §8.1.1 WAL积累、删除后文件不缩小 |
| 审计保留 | 不自动删除。`oi admin audit export --through-seq N --out F`导出`seq≤N`并给出规范化SHA-256；`POST /v1/admin/audit/prune` `{through_seq,sha256}`在同一事务内重算哈希，相符才删除并追加审计`audit_pruned` | §8.1.1审计增长（导出失败不静默删除） |
| 写入失败与恢复 | 写入返回Unavailable（SQLite磁盘满/只读/IO错误，PG只读/磁盘满/连接失败）后进入不可用并按类别告警；此后探测改为一次极小写事务，成功才恢复，避免只读探测把仍不可写的存储判为可用 | B3§6、§8.1.1磁盘耗尽 |

## 实施与验收

1. **文档先行**：本文件提交到`feature/TASK-005-p2-g4-recovery`并回读，再开始编码。
2. **M1 存储**：`ProbeWrite`、维护标志变更、存储统计与维护、审计导出哈希与修剪（`internal/protocol`共用规范化编码）、SQLite备份、恢复、导出/导入；两后端契约测试。
3. **M2 OIS**：维护模式准入与`Maintenance`码、写失败后的写探测、管理API三组端点、`ois backup/restore/export/import/maintain/admin resume`。
4. **M3 oi**：`oi admin maintenance`、`oi admin store [maintain]`、`oi admin audit export|prune`。
5. **M4 向量与文档**：`tests/integration`实现TV-B3-11a–i；产品文档补备份、恢复、迁移、维护与容量告警操作及参考。
6. **M5**：CI（Linux集成准备小容量tmpfs与PG工具）、G4预注册与证据运行、ITER综合分析、评审与一致性表、Issue阶段评论、G4记录、合入alpha。

### 测试向量（TV-B3-11细分）

判定来自B3§8与§8.1.1；未注明后端的向量两后端各跑一次。零容忍：维护期间放行任一Open、导入/导出丢失或改变任一已提交行、恢复后epoch不大于恢复前所见、哈希不符仍修剪审计。

| ID | 场景 | 判定 | G4判定项 |
| --- | --- | --- | --- |
| TV-B3-11a | OIS运行且持续管理写入时备份：SQLite `ois backup`；PG `pg_dump -Fc` | 副本`integrity_check`为ok、模式版本与meta可读、审计中最大config_version等于meta；备份期间写入不失败 | 备份与重启恢复 |
| TV-B3-11b | 备份后吊销令牌T、删除策略P、再写若干配置；停止OIS并恢复到备份；SQLite `--from`，PG `pg_restore`后`--external` | 审计`restored_from`、`maintenance=true`；维护期间发现与管理API可用、新Open一律503 `Maintenance`；重新吊销T、删除P后`ois admin resume`：T为401、P不再授权、其他授权照常Open | 备份与重启恢复 |
| TV-B3-11c | 恢复前存在打开的通道与未用票据，OIC记住更高epoch | 旧通道已终止、旧票据附着410/401；OIC以新代数重连，OIS epoch大于恢复前任一已发epoch（SQLite由restore前推，PG由`--min-epoch`或OIC报告前推） | 旧任期拒绝 |
| TV-B3-11d | 含Access/指纹/令牌/策略/注册/审计的库停机`ois export`→`ois import`到空库并切换配置启动：SQLite→PG与PG→SQLite；目标非空、行被改动、导入中途失败 | 逐表计数与哈希一致；同一组发现/Open/拒绝结果与迁移前相同；epoch不回退；非空目标与改动文件被拒绝且目标仍空；失败后源库不变 | 备份与重启恢复 |
| TV-B3-11e | SQLite库在受限容量文件系统上写满；SQLite库目录只读时启动；PG运行中切换为只读 | 写满/只读期间管理写与新Open为503 `StoreUnavailable`、已开通道继续、告警日志带类别；恢复空间或可写后无需重启即恢复；只读目录启动被拒绝并给出原因；`integrity_check`为ok | SQLite规模维护拥塞及磁盘不足 |
| TV-B3-11f（SQLite） | 外部只读事务长期持有，期间持续管理写入 | 写入与撤销照常生效；`/v1/admin/store`显示WAL增长且检查点未完成；读事务结束后`truncate`检查点使WAL归零 | SQLite规模维护拥塞及磁盘不足 |
| TV-B3-11g | 并发管理写超过`WRITE_QUEUE` | 超出部分快速返回503 `StoreBusy`而非无界等待；吊销经重试在`SQLITE_BUSY_MS`＋`STORE_PROBE`内生效；同期Open与OIC心跳不受阻 | 并发版本冲突；SQLite规模维护拥塞及磁盘不足 |
| TV-B3-11h | 写入大量审计→导出→按哈希修剪→增量回收；哈希不符；导出未覆盖全部行 | 哈希不符不删除；修剪数与`audit_pruned`一致；SQLite空闲页被回收、库文件变小；记录前后大小 | SQLite规模维护拥塞及磁盘不足 |
| TV-B3-11i | `ois backup`、`ois import`、`ois restore`执行中被强制终止 | 源库不变且可启动；未完成的备份不留下可被误用的文件或无法通过校验；导入回滚后目标仍为空可重试；恢复中断后原库或完整新库之一可用 | 备份与重启恢复 |

G4的“并发版本冲突”“备份与重启恢复”“旧任期拒绝”还在同一运行中回归TV-B3-02、TV-B3-04、TV-B3-10。观察（O2，只记录）：备份耗时与大小、恢复到可受理Open的时间、导出/导入耗时、长读期间WAL大小、回收前后文件大小、拥塞下吊销生效时延。

### 覆盖率与质量

**单元测试覆盖率（完成门槛≥95%）**：受管交付单元与统计命令同[TASK-004](../TASK-004-p1-r-vertical/README.md#实施与验收)：`cmd/`与`internal/`全部非测试Go源码；`go test -count=1 -covermode=atomic -coverprofile=coverage/unit.out ./cmd/... ./internal/...`后`go tool cover -func=coverage/unit.out`的total，以带PG服务的Linux CI为准。

**其他质量信号**：`-race`、两个协议模糊目标、`go vet`、staticcheck、govulncheck、四平台`CGO_ENABLED=0`构建、Windows构建/单测/Python检查；G1全部向量继续在CI两后端运行（回归）。

**G4证据**：预注册清单`experiments/manifests/G4-P2.json`在运行前提交，复用`experiments/product/`运行器与分析器（必要的通用化修订保持对G1-P1 run的复算不变）；Linux CI（ubuntu-24.04、PostgreSQL 18.6）生成，审阅后入库，ITER综合分析与Issue回读后记录G4。小容量文件系统与PG工具由工作流准备（tmpfs挂载、服务容器内`pg_dump`/`pg_restore`），缺失时向量失败而非跳过。

**文档代码一致性**：正式评审前在本目录记录“B3§8/§8.1.1/本表细化→实现符号→验证→结论”，覆盖命令、端点、错误码、默认与上限、失败与恢复、操作文档。

### D14：先修复G1资源证据前置（本任务评审内的必要修正）

2026-11-04复审确认：旧`oicStats`把请求错误返回为零值，双方未核对HTTP/JSON完整性；`settled`只检查goroutine/FD上界，部分故障结束后重新采样基线。该缺口已存在于G1被测提交`f844b29`，85条G4资源记录也没有OIC快照及原始基线，不能事后补证B4§4。它不推翻G4四项独立恢复断言，但使G1“超时与资源清理”的完整结论不足；在此项恢复前暂停G4晋级及依赖P2的新增实施，独立规格准备可继续。

按原B4验收修正，范围不涉及产品取舍，不降低通过线：

1. 统计采集必须有界，验证HTTP 200、完整JSON及必须字段/有效范围；进程异常退出、连接/解析失败不能转为零。正常等待注册可以重试明确的未注册状态，不能吞掉诊断故障。
2. 在故障前完成预热并保存基线；现有向量结束时恢复同一双OIC在线拓扑，沿用故障前online profile，禁止故障结束后重采来吸收泄漏。重启后同角色新进程对照原基线并记录PID变化；跨后端迁移提前准备目标后端参考profile，仍存活OIC沿用原基线。故障中的offline profile属于G3后续范围，不作为此次新增要求。
3. 每次资源判定检查OIS的Preparing/Open/Closing与票据、所有预期存活OIC的通道均为0；goroutine在基线±10，Linux FD精确等于基线。Windows无法提供FD时明确未测，正式证据以Linux为准。按B4“每个失败向量结束后”定义`vector_cleanup`为统一起点，必须在`closeIdle`等夹具清理动作前记录；撤销/许可到期仍由原向量各自验证时限。所有资源采样须在绝对`T_close+1s`内；至少三次连续合格采样，不重新起算或延长时限。
4. 原始OBS保存profile、vector_cleanup时刻、截止时刻、双方基线及每次完整统计、进程身份与采样时间，使分析器能独立复验计数、范围和时限；统计HTTP错误/JSON损坏、进程退出、缺字段、goroutine下界、FD少/多以及越时都补确定性反例。
5. 新预注册`G1-P1-R2`保持G1全部五项判定与原必需向量/质量步骤，补资源证据的明确机器复核，不修改冻结`G1-P1.json`、旧run、ITER-004或归档分析器。新run以ITER-006入库；G4本run在ITER-005中保留四项直接支持及前置缺口，不伪造恢复失败run。
6. 先提交/推送/回读本补充规格，再改夹具及必要的实现缺陷；CI发现产品问题则按原标准修复和重跑。中间`check_gates --stage P2`预期exit2，明确记录真实依赖阻断，不改CI检查来伪装绿色；最终恢复G1及G4后仍须六作业全绿才能合入alpha。

## 进度检查点

| 项 | 状态 | 证据/备注 |
| --- | --- | --- |
| 文档先行 | 完成 | `e92c99b`推送并回读 |
| M1 存储 | 完成 | `9055e1c`：备份、恢复（`--from`/`--external`）、导出/导入、用量与维护、按摘要修剪、写失败分类；两后端契约测试 |
| M2 OIS | 完成 | `358d932`：维护模式与`Maintenance`码、写失败锁存与写探测、三组管理端点、`ois backup/restore/export/import/maintain`；`ois admin resume`由`cmd/ois`转给`client.OISAdmin`（server不依赖client） |
| M3 oi | 完成 | `358d932`：`oi admin maintenance`、`store [maintain]`、`audit export\|prune` |
| M4 向量与文档 | 完成 | `3fb9043`：TV-B3-11a–i、管理写按`WRITE_QUEUE`限流、CI挂tmpfs并在PG服务容器内运行`pg_dump`/`pg_restore`；`ff520d3`：产品文档。Linux CI首跑（`41766803137`）暴露两项并已修正：SQLite对内容相同的`UPDATE`不写页，`ProbeWrite`在磁盘满时也“成功”而误判恢复（改为同一事务内翻转两次并加WAL增长断言；向量在填满前先截断WAL，排除失败写留下的可复用空间）；TV-B3-11g依赖到达时序（改为外部持有存储写锁，结果恒为1×200、7×409、32×503） |
| M5 CI、G4、评审与合入 | 进行中 | `ff520d3`：G4-P2预注册、证据工具通用化（P1分析器归档于`experiments/sources/product-p1/`）。接手基线`05ca85d`的CI `41778335334`六作业通过，11a/b/c/e两后端实际PASS。独立代码评审及[一致性核对](consistency.md)发现并修复D1–D9：探测覆盖并发故障、维护未知提交提前开放、维护CLI、导入schema/meta/数据单事务、孤立WAL、检查点busy、含WAL恢复源、十进制摘要精度与文档。新增确定性回归及聚焦复审通过。`67a164a`已提交产品修复，CI `41779568260`六项通过，完整cmd/internal覆盖率95.9%，11a/b/c/e两后端实际PASS；证据工具复审另修D11–D13（失败证据晋级、fuzz空匹配、未提交源码绑定）。`6c7d5ad`已完成D11–D13与聚焦复审，CI `41780457025`六项通过，Linux完整覆盖率95.9%，11a/b/c/e两后端实际PASS；已从该提交启动G4预注册运行 `41781459890`已完成：原始字节、112项源码指纹及摘要复算一致，13个向量ID/75项必需父子检查通过。随后发现D14旧资源夹具假通过缺口，影响G1前置；G4直接四项证据保留，门槛暂不晋级。待：资源夹具修复与G1-P1-R2补证/ITER-006、ITER-005闭环、Issue阶段评论、恢复G1并记录G4、合入alpha |

D14发现前的验证（6c7d5ad及之前）：Windows本机全量单测覆盖率95.9%（含PG18.6，完整cmd/internal分母）；go vet、结构、8份DISC review、采用范围、P2入口、ITER/物料、35项tools与58项experiments单测通过。本地受影响集成向量：11b/c的SQLite、11d双向迁移、11g/11i两后端通过；同次命令11b/c的PG因缺pg_dump/pg_restore明确失败（日志coverage/task005-local-integration.log），未当作通过，需Linux CI服务容器工具验证。最终覆盖率及race/fuzz/静态检查仍以修复提交的Linux CI为准。

### D14实现检查点（待Linux正式补证）

补充规格已在`b0f0c7a4c0a282aa639ef176dce376822f1a309c`推送，36个变更文件及G4 run/ITER-005逐字节回读一致，再开始编码。现已实现严格统计HTTP/JSON/必需字段及进程检查、有界采样、原角色固定基线、精确FD与goroutine±10、绝对vector_cleanup截止和三个连续样本；补齐原漏检失败向量，迁移参考在故障前采集。修复clientFor覆盖连接池与临时admin客户端的idle残留，生产cmd/internal源码没有改变。

独立复审又复现复算器两个假通过：换profile_id重采资源上升后的基线、把同一时刻样本重复算三次。现对G1正常测试冻结唯一profile，B1-17只允许首check前增补真实OIC且原OIS快照不变；迁移只派生已冻结角色/进程快照；同一瞬时样本不能重复计数。新`G1-P1-R2`保持旧G1完整case、五项判定及O3负载，仅新增严格资源复核，覆盖81个必需leaf及至少97次清理检查。G4分析器已原字节归档到`experiments/sources/product-p2-g4/`，旧7个run/5轮仍可复算。

本机有效验证：7项资源采集/判定顶层回归通过；G1可运行部分两后端40个顶层测试通过（4个skip为Linux SIGSTOP/SIGCONT及原后端专属项），双向迁移通过，11f/g/h/i共4个顶层测试通过（11f/PG按SQLite专属跳过）；77项experiments、35项tools、go vet及记录检查通过。Windows FD为明确的-1未测标记，不算LinuxFD验收；暂停、tmpfs及PG备份工具在LinuxCI验证。G1仍Inconclusive、P2检查预期exit2、G4仍Blocked；尚无G1-P1-R2 run或ITER-006，不宣称补证完成。

### D15：Windows门槛输出编码修正

[CI 41787861636的Windows作业126434051696](https://github.com/wyw-ai/loom-oi/actions/runs/41787861636/job/126434051696)在cp1252输出环境打印中文阻塞原因时触发`UnicodeEncodeError`，掩盖了门槛检查本应返回的exit2。这是检查工具的输出编码缺陷；G1为Inconclusive、G4为Blocked的判断不变。

`experiments/check_gates.py`入口将可重配置的stdout/stderr显式设为UTF-8，保留StringIO调用兼容；不修改判定条件、门槛状态或CI阶段。`test_chinese_blocker_with_legacy_redirected_codepage`以真实子进程覆盖cp1252、ASCII、GBK三种重定向环境，断言返回2、中文可按UTF-8解码且没有编码异常。

本机以HEAD旧实现复现cp1252下异常退出1；修复后，当前真实`--stage P2`在三种编码下均输出G1前置缺口并返回2，含中文的未知阶段仍按输入错误返回1。78项experiments、35项tools单测通过。该CI的integration已成功、unit覆盖率95.9%；Python和Windows门槛作业仍应因G1前置未恢复而阻断，不能据此声称整轮CI通过或G1补证完成。待提交后复核WindowsCI输出，不绕过正确的P2阻断。

## 交付

完成时追加：最终范围、源码与报告入口、实际检查与限制、G4记录及Issue阶段回执。
