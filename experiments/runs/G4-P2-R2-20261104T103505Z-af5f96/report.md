# G4-P2-R2 run record

Gate G4, evidence level Implementation, subject `73bb667e9d1535a4ccee5eb15aa440ff636964b9`.

Status: observed; verdict: pass.

Every number below is recomputed from raw.jsonl by experiments/product/analyze.py; `python experiments/verify.py <run>` repeats that check.

## Steps

| Step | Exit | Timed out | Duration (ms) |
| --- | --- | --- | --- |
| unit | 0 | False | 91742 |
| race | 0 | False | 115669 |
| vectors | 0 | False | 557317 |
| fuzz-frame | 0 | False | 101467 |
| fuzz-control | 0 | False | 61289 |
| vet | 0 | False | 13530 |
| staticcheck | 0 | False | 42251 |
| govulncheck | 0 | False | 13646 |

Unit coverage of the delivery unit: 96.0%. Engines: {"sqlite": ["3.54.1"], "postgres": ["18.6 (Debian 18.6-1.pgdg13+2)"]}.

## G4 criteria

| Criterion | Vectors | Result |
| --- | --- | --- |
| 并发版本冲突 | TV-B3-02, TV-B3-11g | pass |
| 备份与重启恢复 | TV-B3-04, TV-B3-11a, TV-B3-11b, TV-B3-11d, TV-B3-11i, TV-B3-11-store | pass |
| 旧任期拒绝 | TV-B3-10, TV-B3-11c | pass |
| SQLite规模维护拥塞及磁盘不足 | TV-B3-11e, TV-B3-11f, TV-B3-11g, TV-B3-11h | pass |

## Vectors

| Vector | Result | Checked |
| --- | --- | --- |
| TV-B3-02 | pass | unit: TestConcurrentApplyConfigConflict<br>unit: TestConcurrentApplyConfigConflict/sqlite<br>unit: TestConcurrentApplyConfigConflict/postgres |
| TV-B3-04 | pass | vectors: TestTVB3_04_Restart<br>vectors: TestTVB3_04_Restart/sqlite<br>vectors: TestTVB3_04_Restart/postgres |
| TV-B3-10 | pass | vectors: TestTVB3_10_EpochRollback<br>vectors: TestTVB3_10_EpochRollback/sqlite<br>vectors: TestTVB3_10_EpochRollback/postgres |
| TV-B3-11a | pass | vectors: TestTVB3_11a_OnlineBackup<br>vectors: TestTVB3_11a_OnlineBackup/sqlite<br>vectors: TestTVB3_11a_OnlineBackup/postgres |
| TV-B3-11b | pass | vectors: TestTVB3_11b_RestoreMaintenance<br>vectors: TestTVB3_11b_RestoreMaintenance/sqlite<br>vectors: TestTVB3_11b_RestoreMaintenance/postgres |
| TV-B3-11c | pass | vectors: TestTVB3_11c_OldTermRejected<br>vectors: TestTVB3_11c_OldTermRejected/sqlite<br>vectors: TestTVB3_11c_OldTermRejected/postgres |
| TV-B3-11d | pass | vectors: TestTVB3_11d_Migration<br>vectors: TestTVB3_11d_Migration/sqlite-to-postgres<br>vectors: TestTVB3_11d_Migration/postgres-to-sqlite |
| TV-B3-11e | pass | vectors: TestTVB3_11e_DiskFullReadOnly<br>vectors: TestTVB3_11e_DiskFullReadOnly/sqlite<br>vectors: TestTVB3_11e_DiskFullReadOnly/postgres |
| TV-B3-11f | pass | vectors: TestTVB3_11f_LongReader<br>vectors: TestTVB3_11f_LongReader/sqlite |
| TV-B3-11g | pass | vectors: TestTVB3_11g_WriteCongestion<br>vectors: TestTVB3_11g_WriteCongestion/sqlite<br>vectors: TestTVB3_11g_WriteCongestion/postgres |
| TV-B3-11h | pass | vectors: TestTVB3_11h_AuditRetention<br>vectors: TestTVB3_11h_AuditRetention/sqlite<br>vectors: TestTVB3_11h_AuditRetention/postgres |
| TV-B3-11i | pass | vectors: TestTVB3_11i_Interrupted<br>vectors: TestTVB3_11i_Interrupted/sqlite<br>vectors: TestTVB3_11i_Interrupted/postgres |
| TV-B3-11-store | pass | unit: TestMaintenanceMutationAndProbeWrite<br>unit: TestMaintenanceMutationAndProbeWrite/sqlite<br>unit: TestMaintenanceMutationAndProbeWrite/postgres<br>unit: TestUsageAndMaintain<br>unit: TestUsageAndMaintain/sqlite<br>unit: TestUsageAndMaintain/postgres<br>unit: TestPruneAudit<br>unit: TestPruneAudit/sqlite<br>unit: TestPruneAudit/postgres<br>unit: TestRestoreExternal<br>unit: TestRestoreExternal/sqlite<br>unit: TestRestoreExternal/postgres<br>unit: TestRecoveryOpsOnClosedStore<br>unit: TestRecoveryOpsOnClosedStore/sqlite<br>unit: TestRecoveryOpsOnClosedStore/postgres<br>unit: TestImportSchemaAndDataAreAtomic<br>unit: TestImportSchemaAndDataAreAtomic/sqlite<br>unit: TestImportSchemaAndDataAreAtomic/postgres<br>unit: TestExportImportAcrossBackends<br>unit: TestExportImportAcrossBackends/sqlite-to-sqlite<br>unit: TestExportImportAcrossBackends/sqlite-to-postgres<br>unit: TestExportImportAcrossBackends/postgres-to-sqlite<br>unit: TestExportImportAcrossBackends/postgres-to-postgres<br>unit: TestExportImportPreservesExactAuditNumbers<br>unit: TestExportImportPreservesExactAuditNumbers/sqlite-to-sqlite<br>unit: TestExportImportPreservesExactAuditNumbers/sqlite-to-postgres<br>unit: TestExportImportPreservesExactAuditNumbers/postgres-to-sqlite<br>unit: TestExportImportPreservesExactAuditNumbers/postgres-to-postgres<br>unit: TestBackupWhileWritingAndRestore<br>unit: TestBackupOfStoppedDatabase<br>unit: TestRestoreSQLiteEdges<br>unit: TestRestoreConvertsAutoVacuum<br>unit: TestInspectRejectsBadBackups<br>unit: TestImportRejectsBadFiles<br>unit: TestIncrementalVacuumShrinks<br>unit: TestRestoreDetachesOrphanWAL<br>unit: TestRestoreWaitsForCompleteCheckpoint<br>unit: TestRestoreRejectsSourceWithWAL<br>unit: TestRestoreInstallReturnsWALOnFailure<br>unit: TestRestoreDoesNotOverwritePreservedOrphanWAL |
| TASK006-LIFECYCLE | pass | unit: TestForwardCancellation<br>unit: TestForwardCancellation/Completed_waiting_for_remote_close<br>unit: TestForwardCancellation/active_read<br>unit: TestForwardCancellation/already_cancelled_before_registration<br>unit: TestForwardCancellation/before_READY<br>unit: TestForwardCancellation/blocked_leg_write<br>unit: TestForwardCancellation/blocked_local_write<br>unit: TestForwardCancellation/cancel_before_FIN<br>unit: TestForwardCancellation/cancel_first<br>unit: TestForwardCancellation/remote_ABORT_first<br>unit: TestForwardNormalCompletionCancelsWatcher<br>unit: TestForwardCancellationJoinsForcedClose<br>unit: TestForwardCancellationRacesFIN<br>unit: TestForwardCancellationLocalWriteErrorWaitsDrain<br>unit: TestForwardCancellationLocalWriteErrorWaitsDrain/cancel_first<br>unit: TestForwardCancellationLocalWriteErrorWaitsDrain/local_write_error_first<br>unit: TestForwardProtocolFailureKeepsAbortGrace<br>unit: TestListenCancelActive<br>unit: TestStreamBackpressureBounded<br>unit: TestBridgeBackpressureBounded |

## Observations (recorded, no pass lines; B4 §6)

- g3_resources_baseline: 48 records
- g3_resources_begin: 70 records
- g3_resources_sample: 390 records
- g3_resources_complete: 70 records
- G3-fault: 146 records
- resources_baseline: 101 records
- fd_diagnostic: 276 records
- O1: 12 records
- resources_begin: 103 records
- resources_sample: 539 records
- resources: 103 records
- O4: 23 records
- O3: 4 records
- O2: 17 records
- G3-transfer: 952 records
- G3-effect: 16 records

## Problems

- none

## Limitations

- Single machine loopback topology on a GitHub-hosted ubuntu-24.04 runner; not the 1C1G target environment (G5) and not WAN latency
- Disk full is a 32 MiB tmpfs and a read-only PostgreSQL is default_transaction_read_only; real disks, quotas and storage faults may fail differently
- PostgreSQL backups use pg_dump/pg_restore of the server version inside the service container; pg_basebackup and point-in-time recovery stay deployment procedures outside this run
- Backup, restore, migration and maintenance durations and sizes are observations of small test databases without pass lines (B4 §6); they are not capacity or RTO/RPO commitments (G5, G6)
- No PostgreSQL HA (G6), probes (G2), real path faults (G3) or real compatibility (G7)
