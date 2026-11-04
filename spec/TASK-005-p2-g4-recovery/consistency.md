# TASK-005 文档代码一致性

2026-11-04。核对基线为 `05ca85d6434bd2a29380ce92ecb70517078b8a01` 相对 `b50c08e228aa1c01686b35a59421ad2670154219` 的实现，以及本轮评审修复。来源为已采用的 [B3§8](../../docs/architecture/p1/b3-storage.md#backup)、[共同架构§8.1.1](../../docs/architecture/alpha-v0.7.md#deployment)、[任务实现细化与向量](README.md)、[产品运维指南](../../docs/product/operations.md)和[接口参考](../../docs/product/reference.md)。路径相对仓库根；行号是本轮核对定位，后续修复以同名符号为准。

结论取值：**一致**＝已核对实现与规格；**解释**＝沿用既有语义并在后文说明；**偏差**＝实现、文档或验证缺口，列入 D 编号；**范围外**＝本任务不承诺。表中的测试名称表示验证入口，不自动表示该测试已经在当前提交及全部后端通过。

验证状态：D1–D18已完成修正与聚焦独立复审。历史6c7d5ad G4支持、b7 G1失败、5668 G1通过/G4失败全部保留；当前固定2ec3670的新G1/G4正式运行均通过，完整覆盖95.9%，unit/race各313项PASS，包含FIN九门闩、SQLite实际FD正负对照及11h双后端精确资源回归。详见[ITER-006](../../docs/evidence/p1-g1-resource-evidence-007.md)与[ITER-007](../../docs/evidence/p2-g4-resource-evidence-008.md)。当前G1/G4仍Inconclusive待Issue回读和门槛恢复，不将证据工作流成功当成全部发布完成。

## B3§8 与任务实现细化

| 规格条款 | 要求 | 实现符号（file:line） | 验证测试 | 结论 |
| --- | --- | --- | --- | --- |
| B3§8 备份；任务 `ois backup` | 运行中 SQLite 以 `VACUUM INTO` 生成一致副本，检查完整性、meta 与迁移版本，校验后发布最终文件 | `internal/ois/store/backup.go:43` `Backup`；`:100` `inspectSQLiteFile`；`:136` `schemaVersion`；`internal/ois/server/cli.go:206` `cmdBackup` | `TestBackupWhileWritingAndRestore`、`TestBackupOfStoppedDatabase`、`TestInspectRejectsBadBackups`；TV-B3-11a | 一致；PG 工具边界见解释1 |
| 任务备份文件规则 | 已存在输出拒绝；失败删除临时副本；输出 SHA-256、字节数、store_id、config_version、epoch、schema_version | `internal/ois/store/backup.go:20` `BackupInfo`、`:43` `Backup`、`:173` `fileDigest` | `TestBackupFailures`、`TestBackupCancelled`、`TestRecoveryCommands`；TV-B3-11i | 一致；强杀可留下 `.partial`，不是正常返回成功 |
| B3§8 恢复仅在停止时执行 | SQLite 全过程持有 `<db>.lock`；外部恢复标记使用后端现有单实例互斥 | `internal/ois/store/backup.go:256` `Restore`；`internal/ois/store/sqlite.go:183` `openSQLite`；`internal/ois/store/postgres.go:129` `openPostgres` | `TestBackupWhileWritingAndRestore`、`TestRestoreSQLiteEdges`、`TestRestoreExternal`、`TestRecoveryCommands` | 一致；数据库工具本身由部署方停机操作 |
| 任务 SQLite 副本恢复 | 核对来源、store_id，复制到私有临时库并标记，保存原库后原子替换；不得把旧 WAL 用在新库 | `internal/ois/store/backup.go:288` `restoreSQLite`、`:363` `markSQLiteFile`、`:464` `previousSQLite`、`:501` `requireStandaloneBackup`、`:522` `checkpointSQLite`、`:572` `installSQLiteRestore` | `TestRestoreSQLiteEdges`、`TestRestoreConvertsAutoVacuum`；TV-B3-11b、11i；D5–D7五项新增回归 | 一致；D5–D7 已修复并通过含PG存储单测，6c7d5ad CI通过，D14前置另列 |
| B3§8 恢复标记 | `restored_from` 与 `maintenance=true` 在同一事务内写入；`--external` 只标记已经由外部工具恢复的库 | `internal/ois/store/backup.go:418` `sqlCore.markRestored`；`internal/ois/server/cli.go:223` `cmdRestore` | `TestRestoreExternal`、`TestBackupWhileWritingAndRestore`、`TestRecoveryCommands`；TV-B3-11b | 一致；外部恢复的历史 epoch 下界见解释2 |
| B3§6 / 任务 epoch 下界 | 恢复取备份、可读原库与 `--min-epoch` 的最大值，启动再加一；OIC 报告仍能前推 | `internal/ois/store/backup.go:288` `restoreSQLite`、`:418` `markRestored`；`internal/ois/store/sql.go:263` `StartRun`；`internal/ois/relay/control.go` `startSession` | `TestBackupWhileWritingAndRestore`、`TestRestoreExternal`、`TestRegistryAndEpoch`；TV-B3-11c、TV-B3-10 | 一致；恢复标记本身取最大值，开始新运行才加一 |
| B3§8 / 任务维护拒绝码 | 新建通道在维护模式拒绝，503 `Maintenance`；发现与管理 API 可用 | `internal/ois/relay/hub.go:315` `Hub.Open`；`internal/protocol/codes.go:60` `CodeMaintenance`、`:86` HTTP 映射；`internal/ois/relay/background.go:20` `Start` | `TestMaintenanceModeRefusesOpens`、`TestRestoredSnapshotStartsInMaintenance`、`TestRecoveryAPI`；TV-B3-11b | 解释：既有认证/权限错误与幂等返回优先级不改变，见解释3；未知写结果恢复窗口见 D2 |
| 任务进入/退出维护 | `PUT /v1/admin/maintenance`，必填布尔值、If-Match、版本加一与审计；恢复后管理员重施撤销再 resume | `internal/ois/server/api.go:246` `setMaintenance`；`internal/ois/relay/maintenance.go:18` `SetMaintenance`；`internal/ois/relay/admin.go:50` `write`、`:183` `applyMutations` | `TestMaintenanceMutationAndProbeWrite`、`TestMaintenanceModeRefusesOpens`、`TestRecoveryAPI`；TV-B3-11b | 一致 |
| 任务维护 CLI | `ois admin resume` 读取 OI_URL/OI_TOKEN/OI_CA_FILE；`oi admin maintenance on/off` 尊重参数与并发版本 | `cmd/ois/main.go:12` `main`；`internal/client/admin.go:66` `OISAdmin`、`:19` `Client.SetMaintenance`；`internal/client/cli.go:288` `cli.admin` | `TestRecoveryAdmin`、`TestMaintenanceCLIHonorsFlagOrderAndVersion`；TV-B3-11b | D3 已修复并通过客户端包回归；6c7d5ad CI通过，D14前置另列 |
| B3§8 停写导出 | 取得单实例锁；版本化 JSON 行，包含来源/模式/meta、业务对象及审计；逐表计数/摘要 | `internal/ois/store/export.go:221` `Export`、`:230` `sqlCore.export`、`:167` `canonicalRows`；`internal/ois/server/cli.go:250` `cmdExport` | `TestExportImportAcrossBackends`、`TestExportImportEdges`、`TestRecoveryCommands`；TV-B3-11d | 一致；导出表与 schema 的处理见解释4 |
| 任务规范化 | 主键升序，时间 UTC 微秒，二进制 base64，结构化对象排序，SQLite TEXT / PG jsonb 摘要一致 | `internal/ois/store/export.go:167` `canonicalRows`、`:525` `eachAudit`；`internal/protocol/canonical.go:19` `CanonicalJSON`、`:57` `exactNumber`、`:103` `CanonicalTime`、`:121` `CanonicalAudit` | `TestCanonicalJSON`、`TestCanonicalTimeAndAudit`、`TestAuditDigest`、`TestExportImportAcrossBackends`、`TestExportImportPreservesExactAuditNumbers` | 一致；D8 已修复，含PG四组合回归通过，6c7d5ad CI通过，D14前置另列 |
| B3§8 / 任务导入事务 | 空目标；建模式、meta、数据、核对与审计在单事务中完成；失败不留下半导入 | `internal/ois/store/export.go:285` `Import`、`:294` `importFrom`、`:410` `prepareImportSchema`、`:434` `requireEmpty`；`internal/ois/store/migrate.go:200` `applyInTx` | `TestImportSchemaAndDataAreAtomic`、`TestImportRejectsBadFiles`、`TestExportImportEdges`、`TestExportImportAcrossBackends`；TV-B3-11d、11i | 一致；D4 已修复，含PG回归通过，6c7d5ad CI通过，D14前置另列 |
| 任务导入保真 | 输入摘要匹配；事务内回读逐表计数/摘要再匹配；保留 store_id、config_version、epoch、审计 seq | `internal/ois/store/export.go:294` `importFrom`、`:395` `sameSummary`、`:449` `insertRow`；PG `OVERRIDING SYSTEM VALUE` 与 setval | `TestExportImportAcrossBackends` 四方向、`TestImportRejectsBadFiles`；TV-B3-11d 双向权限矩阵 | 一致；D4 同时改为从事务读取实际meta计算摘要；成功新增 `imported_from` 审计见解释4 |
| B3§8 权威切换 | 源库不修改，校验完成后显式改配置并重启；不双写、不在线热切换 | `internal/ois/store/export.go:221` `Export`、`:285` `Import`；`docs/product/operations.md` §9 | TV-B3-11d 重导出源库摘要相等、切换前后发现/Open/拒绝矩阵相等、epoch 前进 | 一致 |

## 共同架构§8.1.1 与运行态

| 规格条款 | 要求 | 实现符号（file:line） | 验证测试 | 结论 |
| --- | --- | --- | --- | --- |
| 规模诊断 / 任务存储观测 | 区分主库、WAL、页大小、总页和空闲页；PG 报数据库大小；暴露可用状态及失败类别 | `internal/ois/store/maintain.go:33` `Usage`；`internal/ois/relay/maintenance.go:31` `StoreView`；`internal/ois/server/api.go:262` `storeView`；`internal/protocol/api.go:183` `StoreUsage` | `TestUsageAndMaintain`、`TestMaintainAndPruneThroughHub`、`TestRecoveryAPI`；TV-B3-11e/f/h | 一致；查询结果为观测值，不是容量保证 |
| 写竞争 / B3§3 | 配置写在进入 adminMu 前受 WRITE_QUEUE 限制；超出立即 StoreBusy；数据库写经单写队列，不持有控制锁等存储 | `internal/ois/relay/admin.go:50` `write`；`internal/ois/relay/hub.go:105` `adminSlots`；`internal/ois/store/serial.go:51` `Serialized.run` | `TestAdminWritesBoundedByWriteQueue`、`TestSerializedQueueAndClose`；TV-B3-11g | 一致；队列两层职责见解释5 |
| WAL 监测与检查点 | 长读期间正常写与吊销继续；检查点结果含 busy、log_frames、checkpointed_frames；结束长读后可 truncate | `internal/ois/store/maintain.go:63` `Maintain`；`internal/protocol/api.go` `CheckpointResult` | `TestUsageAndMaintain`；TV-B3-11f | 一致；在线维护完成审计可能重新产生少量 WAL，见解释6 |
| 增量回收 / 任务上限 | auto_vacuum=INCREMENTAL；每次 0..10000 页；先 vacuum/optimize，再 checkpoint | `internal/ois/store/sqlite.go:101` `prepareFreshSQLite`、`:150` `readbackSQLite`；`internal/ois/store/maintain.go:63` `Maintain`、`:115` `validMaintain`；`internal/protocol/api.go:215` `MaxVacuumPages` | `TestIncrementalVacuumShrinks`、`TestUsageAndMaintain`、`TestRestoreConvertsAutoVacuum`；TV-B3-11h | 一致；回收并不承诺每次删除立即缩小 |
| 任务维护端点与离线命令 | `/v1/admin/store/maintain` 经单写队列；离线 `ois maintain` 取得实例锁；PG 仅 optimize/ANALYZE | `internal/ois/server/api.go:267` `maintainStore`；`internal/ois/relay/maintenance.go:44` `MaintainStore`；`internal/ois/store/serial.go:132` `Maintain`；`internal/ois/server/cli.go:307` `cmdMaintain` | `TestMaintainAndPruneThroughHub`、`TestRecoveryAPI`、`TestRecoveryCommands`、`TestRecoveryAdmin`；TV-B3-11h | 一致；离线不追加在线管理员维护审计，见解释6 |
| 审计保留 / 任务导出 | 无自动删除；客户端按 seq 升序分页导出至边界，保存内容、计数与规范化摘要 | `internal/client/admin.go:35` `ExportAudit`；`internal/client/cli.go:361` `auditFile`、`:399` `writeNew`；`internal/protocol/canonical.go:133` `AuditDigest` | `TestRecoveryAdmin`、`TestAuditDigest`；TV-B3-11h | 一致；摘要数值精度修复见 D8 |
| 审计保留 / 任务修剪 | 同一事务重新计算 seq≤N 的摘要，相符才删除并追加 audit_pruned；不匹配不得删除 | `internal/ois/store/maintain.go:134` `PruneAudit`、`:177` `auditRows`；`internal/ois/relay/maintenance.go:64` `PruneAudit`；`internal/ois/server/api.go:232` `pruneAudit` | `TestPruneAudit`、`TestMaintainAndPruneThroughHub`、`TestRecoveryAPI`；TV-B3-11h | 一致；非配置写，不增 config_version，见 D9 |
| 磁盘满/只读 / B3§6 | 写失败锁存不可用及分类日志，后续 Open/配置写 fail-closed；已开通道继续 | `internal/ois/store/store.go:62` `FailureKind`；`internal/ois/store/sqlite.go:36` `sqliteFailureKind`；`internal/ois/store/postgres.go:40` `postgresFailureKind`；`internal/ois/relay/maintenance.go:108` `writeFailed` | `TestFailureKinds`、`TestWriteFailureNeedsWriteProbe`；TV-B3-11e | 一致；并发锁存与恢复时序修复见 D1/D2 |
| 写探测 / 任务自动恢复 | 读探测成功不能清除写失败；同事务真实修改页后恢复原值，写提交成功才可恢复 | `internal/ois/store/maintain.go:19` `ProbeWrite`；`internal/ois/store/serial.go:123` `ProbeWrite`；`internal/ois/relay/background.go:55` `probe` | `TestMaintenanceMutationAndProbeWrite`（WAL 先 truncate，探测后必须有帧且 maintenance 不变）、`TestWriteFailureNeedsWriteProbe`；TV-B3-11e | `2bf1555` 修复相同值 UPDATE 无实际写入；并发边界由 D1/D2 回归验证 |
| 读取规划/容量 | 增长数据集、冷热查询、规划与 1C1G 资源目标 | P1 已有索引与内存快照；本任务不新增读取路径；`docs/architecture/alpha-v0.7.md` §8.1.1 | 本任务只观察小库的大小、耗时与维护行为；G5 另需目标环境 | 范围外；不能从 G4 推出规模容量或吞吐承诺 |

## TV-B3-11、G4 预注册与质量

向量实现集中在 `tests/integration/vectors_b3_recovery_test.go`，下列行号均指该文件。`each` 选择两后端；明确只适用于 SQLite 的 WAL 向量除外。证据分析器要求 manifest 登记的每个必需子测试为 pass，不能把 skip 当 pass。

| 向量/规格 | 实现与断言 | 验证入口 | 结论 |
| --- | --- | --- | --- |
| TV-B3-11a 在线一致备份 | `:232` `TestTVB3_11a_OnlineBackup`：管理写持续进行且无错；SQLite 检完整性和 max(audit.config_version)=meta；PG dump 恢复到临时库后比较；记录耗时/大小 | `:87` `takeBackup`、`:53` `pgTool`、`:216` `checkSQLiteCopy` | 一致；PG 工具路径必须在 Linux 实际执行 |
| TV-B3-11b 恢复维护与补撤销 | `:288` `TestTVB3_11b_RestoreMaintenance`：恢复后审计/维护，发现可用；alice/trent/bob 的新通道全部 Maintenance；重施吊销与删策略再 resume 后验证 401/404/正常 | 真实 `ois restore` 与 `ois admin resume` | 一致；D2/D5–D7 的额外边界需回归 |
| TV-B3-11c 旧任期拒绝 | `:357` `TestTVB3_11c_OldTermRejected`：恢复前通道中止、旧票据 401/410、旧 channel 不可见、OIC 注册 epoch 大于旧值 | 另回归 TV-B3-04、TV-B3-10 | 一致；为零容忍断言 |
| TV-B3-11d 双向迁移 | `:440` `TestTVB3_11d_Migration`：sqlite-to-postgres / postgres-to-sqlite；篡改、截断、非空拒绝；原库摘要不变；权限矩阵、摘要、epoch 保持 | 存储单测另覆盖四个同/跨后端组合 | 一致；初始化事务回滚补充见 D4 |
| TV-B3-11e 满盘/只读 | `:549` `TestTVB3_11e_DiskFullReadOnly`：32 MiB tmpfs 写满、PG 只读；失败类别、持续不可用、旧流完整、解除故障后恢复；Linux 只读目录拒绝启动 | `OI_IT_SMALLFS`；PG `ALTER SYSTEM`；补充 ProbeWrite 单测 | 一致；Windows 不替代 Linux 满盘/目录权限结果 |
| TV-B3-11f 长读与 WAL | `:664` `TestTVB3_11f_LongReader`：外部读事务持有；300 次配置写及吊销；WAL 增长、busy；读结束后 truncate=0 | 只要求 sqlite 子测试 | 一致 |
| TV-B3-11g 确定性拥塞 | `:746` `TestTVB3_11g_WriteCongestion` 与 `:714` `holdWriteLock`：队列8，40个预热连接；外部写锁持有至少1.2秒；32个请求在1秒内503，释放后1个200、7个409；吊销≤5500ms；Open/旧流/心跳继续 | SQLite BEGIN IMMEDIATE；PG 直连 meta FOR UPDATE | 一致；这是验证配置，不修改产品默认 WRITE_QUEUE=64 |
| TV-B3-11h 审计保留与回收 | `:899` `TestTVB3_11h_AuditRetention`：300+审计；篡改与摘要不符拒绝且不删；匹配导出后删除并留 audit_pruned；SQLite 回收后文件变小、PG ANALYZE | CLI 导出/修剪真实链路 | 一致；缺行/中途失败由摘要/导入单测补充 |
| TV-B3-11i 强制中断 | `:1004` `TestTVB3_11i_Interrupted`：6万审计，30/120ms 强杀导入后可重试；SQLite 中断备份/恢复后完整性与启动 | `:995` `interrupt`；恢复精确切换边界单测见 D5–D7 | 解释：定时强杀覆盖运行窗口，不能保证恰中每个文件系统边界，故需独立确定性回归 |
| G4 四项判定 | `experiments/manifests/G4-P2.json` `case.criteria`：并发版本冲突；备份与重启恢复；旧任期拒绝；SQLite规模维护拥塞及磁盘不足。登记11a–i、TV-B3-02/04/10及恢复存储单测 | `experiments/product/analyze.py:378` 向量逐项判定；`:399` 聚合 criteria | 历史结果保留，2ec3670新G4全部四项/13向量/75必需检查通过；C48/ITER-007待发布闭环 |
| 真实环境与不可跳过 | `.github/workflows/evidence.yml`：ubuntu-24.04、PG18.6、32MiB tmpfs、服务容器内 pg_dump/pg_restore、完整历史检出；`experiments/product/run.py:62` 运行集成全集 | analyzer 查每个要求的 backend/subtest 的 pass；缺失、skip、timeout 均非通过 | 一致；本机缺少夹具不减少 manifest 要求 |
| 质量与 G1 回归 | `case.steps` 固定 unit/race/vectors/两项60s fuzz/vet/staticcheck/govulncheck；`run.py` 固定 cmd/internal 全受管单元；coverage≥95%；四平台构建与Windows由CI补充 | `.github/workflows/ci.yml:66` 覆盖率、`:72` race、`:79` fuzz、`:114` tmpfs；`experiments/product/analyze.py:347` 覆盖率判定 | 一致；G4 的 load=false 明确排除 O3负载 TV-B4-06，不重复声称 G1 的负载证据 |
| O2/O4 观察 | 11a备份耗时/大小，11b恢复与恢复受理及人工步骤，11c旧任期，11d导出/导入耗时，11e检测/恢复，11f WAL，11g拥塞/吊销，11h回收大小，11i中断 | 各测试 `e.obs`；analyzer 收集原始 OBS 行 | 一致；无容量、RTO/RPO、WAN 或 HA 外推 |

## 产品文档与架构图

| 文档 | 对照符号/验证 | 结论 |
| --- | --- | --- |
| `docs/product/README.md` 交付表 | `cmd/ois/main.go`、`server.Main`、`client.Main`；P1 + P2恢复功能，门槛另看 gates.json | 一致；实现存在不等于已获 G4 |
| `docs/product/operations.md` §7–§10 | 上述不可用、维护、恢复、迁移、审计与容量操作；`TestRecoveryCommands`、`TestRecoveryAdmin`、TV-B3-11a–i | 主流程一致；D4/D5–D7 修复后重新核对原子性描述；D9 修正 If-Match 泛称 |
| `docs/product/reference.md` §4.2/4.4/5/6 | `api.go:81` 路由、`:246` 维护、`:262` 用量、`:267` 维护操作、`:232` 修剪；`cli.go:206/223/250/279/307` 与 `client.cli.admin` | 端点/错误码/命令/上限一致；D9 将“写操作”收窄为“配置写操作” |
| 图04恢复 / 图05部署 | 受控单实例重启、旧代失效、显式双后端、WAL/有界写；本任务不增加进程、HA或热切换 | 关系无变化；阶段状态必须随 G4 最后记录更新 |
| 图06实施与共同架构§11.1、模块页 | G4先恢复G1资源前置，再在原始核验→综合分析/联动→Issue回读后记录Passed；G2/G3各有独立判定且同样依赖G1 | 当前不得提前画成已通过；阶段记录时同步状态 |

## 解释与取舍

1. **PostgreSQL 备份责任**：`ois backup` 拒绝 PG，部署方运行 `pg_dump`/`pg_basebackup`。向量运行服务容器同版本 `pg_dump`/`pg_restore`，不声称验证了 pg_basebackup、PITR 或主备。外部恢复期间也必须保持 OIS 停止；`--external` 标记只在其自身执行期间取得实例锁。
2. **epoch 与不可读旧库**：SQLite 能读旧 meta 时取其 epoch；原库已坏或 PG 已由外部工具覆盖时无法推知事故前最大值，管理员提供 `--min-epoch`，OIC `last_epoch_seen` 继续作为补充。`RestoreResult.epoch` 是下界，`StartRun` 后才严格增加；不能把未知的历史 epoch 写成自动获知。
3. **“新 Open”与既有协议**：维护模式阻止创建新 Channel。B1 已有的认证、参数、可见性、权限、revision、在线、就绪及额度检查仍保留各自错误；同主体既有 client_request_id 重放返回同一 Channel，维护前已受理的 Preparing 不因手动进入维护而新增撤销含义。恢复启动时这些运行状态原本为空。此解释不允许维护期间从旧快照新建 Channel，D2 正是要消除此窗口。
4. **导出对象与规范化**：逐表摘要涵盖 accesses、consumer_tokens、policies、exposures、audit；meta 单独摘要。schema_migrations 属目标二进制的后端 DDL/校验和，不把 SQLite DDL 原样导入 PG；来源版本必须是目标支持的版本。时间按共同持久精度 UTC 微秒，token 摘要为 base64；成功导入在原审计之后追加 `imported_from`，故目标最终审计行数比导出多一不是丢失/篡改。此例外必须先核对原行摘要再发生。
5. **两层有界写**：存储 `Serialized` 约束所有数据库写；Hub `adminSlots` 约束尚在等 adminMu 的配置请求，避免它们绕过数据库队列积压。维护/修剪直接进入已有存储有界队列；不引入独立无限重试。拥塞向量的8是专用测试值，默认64不变。
6. **维护不是多语句原子操作**：vacuum、optimize、checkpoint 各自执行，响应是实际前后观测，不承诺三者共同回滚。在线 `MaintainStore` 随后追加 `store_maintained`，会再次写 WAL；`after.wal_bytes=0` 描述维护结束那个时点，不保证响应到达时WAL一直为0。离线 `ois maintain` 直接调用同一存储操作，没有在线管理身份，不追加该管理审计。审计修剪本身则始终是摘要核对/删除/审计单事务。
7. **范围与证据**：本轮没有自动维护调度、审计保留期默认值、容量/SLO、1C1G结论、就绪探针、真实路径故障、HA或版本组合支持承诺。G4真实Linux样本与反例须保留；Windows单测和本表不能替代它。

## 偏差与缺口

严重度：**阻断**＝G4记录前必须解决；**需修复**＝提交正式评审前修正；**可接受**＝明确说明且不影响已采用判定。以下编号保留发现历史；修复后补充验证，不能删除反例或仅把严重度改名为通过。

| 编号 | 位置与触发轨迹 | 处理与验证 | 严重度 / 状态 |
| --- | --- | --- | --- |
| D1 | `internal/ois/relay/background.go:55` `probe` 原先读出 latched 后无条件写 storeOK；在 Health/ProbeWrite 与发布结果间发生新的写失败，会被旧成功探测覆盖 | 引入失败序号，探测只能发布没有较新失败的结果；`recovery_race_test.go:51` `TestProbePreservesConcurrentWriteFailures` 覆盖同类/异类较新失败，`:85` `TestReadProbePreservesConcurrentFailure` 覆盖读探测。旧实现反证失败；relay全包通过 | 阻断→已修复，6c7d5ad CI/race通过，D14前置另列 |
| D2 | 同一 `probe` 原先先 storeOK=true，再 `resyncIfStale`；维护写已提交但响应丢失时，LoadSnapshot 阻塞期间旧 maintenance=false 可放行新的 Open | adminMu内先同步未知写结果的快照并执行失效处理，完成前保持不可用；`recovery_race_test.go:103` `TestProbeReloadsMaintenanceBeforeRecovery` 覆盖重载成功/失败/取消。旧实现反证失败；relay全包通过 | 阻断→已修复，6c7d5ad CI/race通过，D14前置另列 |
| D3 | `internal/client/cli.go` `admin` 的 maintenance 分支用 args[1] 而不是 parse 后 pos[0]；`maintenance --if-match N on` 实际发 off，且显式版本被忽略 | 已用 pos[0]、`cli.version` 和实际 If-Match；`TestMaintenanceCLIHonorsFlagOrderAndVersion` 先复现旧错，再验证前后flag、陈旧版本不改状态；整个client包通过 | 需修复→已修复，6c7d5ad CI通过，D14前置另列 |
| D4 | `internal/ois/store/export.go` `Import` 先 Initialize 提交schema/meta，再开始数据事务；与任务“单事务建表后写入”及失败空目标表述不符 | modeImport只取锁开连接，`prepareImportSchema`在导入事务中执行DDL，不预种meta；真实回读meta摘要。`import_atomic_test.go:17` `TestImportSchemaAndDataAreAtomic`验证读完全部行后因尾随数据拒绝仍无schema/meta/半数据、可重试、有meta目标拒绝；含PG的store测试通过 | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D5 | SQLite目标主文件缺失但旧 `-wal` 留存时，restore 原先不处理 sidecar，新主库可重放旧 WAL；旧实现反例中返回maintenance=true/epoch=1后Open读为false/77 | `installSQLiteRestore`即使主库缺失也隔离并保存遗留WAL/移除SHM；`recovery_safety_test.go:15` `TestRestoreDetachesOrphanWAL`覆盖主库缺失/空文件；`:235` `TestRestoreDoesNotOverwritePreservedOrphanWAL`保全历史副本；含PG的store整包通过 | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D6 | `previousSQLite` 原先仅 Exec checkpoint，不读取busy/帧结果；外部长读阻挡时仍继续搬走WAL，替换前中断或失败使旧路径失去已提交页 | `checkpointSQLite`读取busy/帧结果，私有新库checkpoint并sync后才发布；失败会归还旧WAL。`recovery_safety_test.go:92` `TestRestoreWaitsForCompleteCheckpoint`、`:202` `TestRestoreInstallReturnsWALOnFailure`覆盖长读与SHM删除/主库rename失败；含PG的store整包通过 | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D7 | `--from` 指向含非空WAL的SQLite库时，inspect读取WAL合并视图，copyFile只复制主文件；可丢已提交状态且仍通过文件哈希比较 | `requireStandaloneBackup`解析符号链接并在复制前后拒绝非空源WAL，提示先backup；`recovery_safety_test.go:164` `TestRestoreRejectsSourceWithWAL`证明拒绝不动目标、先生成独立备份可恢复全部已提交数据；含PG的store整包及最终恢复针对性回归通过 | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D8 | `internal/protocol/canonical.go` 原先规范化数字经float64，可能把不同大整数/高精度小数变为相同摘要，且跨后端表示不一致 | 改为十进制精确规范化并拒绝重复键/尾随内容；`TestCanonicalJSON`覆盖等值/不等值；`import_atomic_test.go:59` `TestExportImportPreservesExactAuditNumbers`验证四后端组合与精确小数篡改拒绝；含PG的store/protocol测试通过 | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D9 | `docs/product/operations.md` §4、`reference.md` §4.2泛称“所有写操作需If-Match”，但store/maintain与audit/prune不是配置写，不读If-Match也不增config_version | 已改为配置写需If-Match，并明确维护/修剪不变更配置版本；API测试已有不带If-Match成功覆盖。导入运维说明亦明确无meta目标、不要先init | 需修复→已修复并回读 |
| D10 | G4阶段证据与门槛闭环 | 2ec3670新G4四项通过，历史6c通过与5668资源失败均保持，ITER-007承接修正；新G1同源五项通过 | 待四单Issue逐字回读、最终门槛与六项CI，产品证据已具备 |
| D11 | 原门槛/ITER检查只要求observed，没有拒绝verdict=fail，且未核对run所属gate与判定项；复制dirty: subject也可能被接受 | `check_iterations.py`产品primary必须observed/pass；`check_gates.py`核对完整提交SHA、gate、四项criteria与非空通过向量。用冻结G1真实失败run改为primary的反例以及完整指纹synthetic反例验证；旧6个run/4轮仍可复算 | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D12 | `go test -fuzz`未匹配任何目标也会发出package事件并exit0，原分析器会误报已完成模糊测试 | `product/analyze.py`要求注册目标pass、实际active fuzzing事件及完整fuzz_time；反例覆盖无目标/错目标/skip/仅seed/不足时长，现存G1真实raw在新分析器亦pass，归档分析器不变 | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D13 | runner原先忽略untracked源码，可把未提交测试或嵌入数据绑定为HEAD证据 | `product/run.py`拒绝源码目录与本次manifest内未跟踪文件（含ignored，只有Python字节码缓存除外）；真实临时Git仓库测试覆盖Go、ignored Go、数据与新manifest，生成输出目录不误判；dirty开发run不能用于gate | 阻断→已修复，6c7d5ad CI通过，D14前置另列 |
| D14 | `tests/integration/env_test.go` `oicStats`/`oisStats`未严格核对HTTP、JSON及进程身份；`settled`仅验上界、OIC原始统计缺失；部分故障后`baseline`重采可吸收残留，旧G1同样受影响 | 原G1验证对象移入historical_validation，C43撤回资源完整性结论；当前TASK-005先提交/回读[补证规格](README.md#d14先修复g1资源证据前置本任务评审内的必要修正)，再修夹具及必要实现缺陷，以新G1-P1-R2完整覆盖G1五项原判定，原始核验、ITER-006与Issue回读后恢复。下述为原修正范围；实现及本机验证见README检查点，Linux首轮补证为observed/fail，D16/D17修正后的5668新G1通过；同源新G4资源反例新增D18，2ec最终G1/G4已完整复证通过 | 严格夹具及修正版本已正式通过，G1/G4 Inconclusive待发布闭环 |
| D15 | [Windows CI 41787861636/job126434051696](https://github.com/wyw-ai/loom-oi/actions/runs/41787861636/job/126434051696)中，`experiments/check_gates.py`以cp1252打印中文blocker触发`UnicodeEncodeError`，把正确的P2阻断exit2变成异常exit1 | main入口对可重配置stdout/stderr设UTF-8，保留StringIO兼容；`test_chinese_blocker_with_legacy_redirected_codepage`用cp1252/ASCII/GBK真实子进程验证中文输出及exit2。本机复现旧错误，并验证修复后的真实P2仍exit2、未知中文阶段仍exit1；78项experiments与35项tools单测通过。判定、门槛状态与CI阶段均未放宽 | 已修复并远端验证：8cdd60b CI41789628138 Windows126439719568中文正常，无编码异常；wrapper因真门槛阻断退出1。D16/D17未闭合，P2仍应exit2 |
| D16 | Go test-json的1024字节Output分片被逐event OBS正则漏读；首轮原summary仅14 baseline/39 sample，raw完整为103/581 | 按(step, Package, Test)重组逻辑行；覆盖交错、多行、分片标记及残片反例。原raw内存诊断115次资源检查严格通过，但总fail/race断言仍在；原summary与13ed442分析器归档冻结 | 已修复，42项product回归通过；原摘要保持，待新run |
| D17 | b7af582的TestCompletedExchange在race失败：Up11/Down5完整但ChannelInterrupted；FIN写入对端可见到finSent发布之间，remote FIN/EOF先到造成错误终态 | 源码轨迹见ITER-006；显式发送状态、读EOF/writing时锁外中止失效leg并等待真实Write结果；首次终态和许可优先不弱化。`TestFINWriteOutcomeAfterLegEOF`九个门闩场景覆盖成功/失败/阻塞写/缺FIN/三处过期/Abort先/Completed先；b7 overlay旧码success反例确定失败，修复后tunnel包本机通过2.332s | 已修复并本机确定性验证，5668新G1与G4的Linux unit/race均通过全部309项测试事件；完整覆盖95.9%，G4另有D18反例，不能据FIN通过恢复整个G4 |

### D14的修正与补证计划

- 严格且有界采样：HTTP 200、完整JSON、必需字段/有效范围及预期进程身份；请求/解析失败、缺字段或进程退出都不得折成零。保存OIS与全部预期存活OIC的原始统计，供独立分析器复核。
- 故障前固定online基线：预热正常通道、统计与数据库连接池，清理空闲连接，确认双方同一在线拓扑及通道/票据回收后冻结。现有44处settled结束均回到同一online拓扑，故障后禁止重新采baseline；本轮不引入G3离线profile。重启同角色新PID沿用原初基线并记录进程变化；迁移在故障前预采目标后端参考profile，存活OIC继续使用原基线。`clientFor`替换旧client及`auditActions`临时管理client的idle连接回收须一并核对，夹具残留先修，尚不据此断言产品泄漏。
- 精确比较与绝对预算：OIS Preparing/Open/Closing/票据与预期存活OIC通道为0；goroutine为基线±10，Linux FD与基线精确相等。每个失败向量结束时的`vector_cleanup`必须在`closeIdle`等清理动作前记录，全部至少三次连续合格采样位于同一个绝对T_close+1s内；不能重起时钟或延长窗口。原向量独立的许可失效/撤销时限保留。Windows FD不可测明确记录，不替代Linux正式证据。
- 原始证据和反例：OBS含profile、起点/截止时刻、基线/全部样本及进程身份；补统计错误/损坏/缺字段/退出、goroutine低于下界、FD减少/增加、越时等确定性反例，证明错误不会成为通过。
- 运行归属与冻结：新G1-P1-R2保留旧五项criteria、所有必需向量、负载和质量步骤，补严格资源复核。新run只归属新ITER-006；ITER-005只拥有现存G4 run。冻结G1-P1.json、旧run、ITER-004、旧分析器、C36–C38原文和旧发布回执不改；没有新run前不登记补证完成。`check_gates --stage P2`当前exit2是正确阻断，不能修改CI阶段或绕过失败。

## 复核出口

D1–D17的阶段修正与原始结果保持各自范围；当前新增D18资源反例，最终出口仍须完整单测≥95%、Linux双后端CI、race/fuzz/静态检查、修复后G1/G4完整原始证据、ITER分析、产品/KB/图文与Issue回读。全部完成后才恢复Passed，并在最终CI六作业全绿后合入alpha。

### 5668b53续测后的当前检查点

D16/D17已获新G1-P1-R2正式通过证据（ITER-006/C47），五项判定及Linux race通过，完整覆盖95.9%；以上首轮“新run待完成/FIN Failed”叙述保留历史时点。相同源码的新G4为observed/fail（ITER-007/C46）：11h/sqlite末尾FD17对固定基线16未归位，审计功能断言和其余12向量ID通过。当前G1 Inconclusive、G4 Failed；不能以新G1通过覆盖该资源反例，也不能把旧FIN失败继续写成新run失败。

| 偏差 | 事实与影响 | 修正与验证 | 当前出口 |
| --- | --- | --- | --- |
| D18 | 5668 G4的11h/sqlite出现FD17对基线16；原raw无FD链接，不倒写具体身份 | 2ec固定一个独立读连接，保持WAL写；Linux正负对照默认相关FD6→6、旧max4池6→7且store.Close清零；等待取消/活读事务下写入通过，新G1/G4均通过 | 已修正并获正式支持；长读可能令Health排队超时，沿B3 fail-closed，吞吐/1C1G另由G5验证；待发布闭环 |

D18最终检查点：2ec3670的完整Linux unit/race与两个正式run通过；默认池和旧池负对照实际执行，新G4 11h两后端资源归位。原始字节、116源码指纹与原预注册保持，未改变清理预算或排除失败样本。

D18独立代码复审未发现阻断：读事务复用同一tx，无嵌套取第二reader；Linux回归检查实际文件链接与池状态，诊断只读子进程FD链接、不读取内容/环境或凭据，不改变清理预算。Linux机制实测仍待CI。

2ec3670当前出口：全部已发现阻断实现偏差均有修正和适用正式证据。剩余是新分析与图文回写、四单阶段追加/逐字回读、G1/G4 validation记录与最终CI；这些完成前不标Passed、不合入。此前各检查点保留其历史时点，当前状态以本段及页首为准。
