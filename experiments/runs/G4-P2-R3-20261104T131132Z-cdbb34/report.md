# G4-P2-R3 run record

Gate G4, evidence level Implementation, subject `bff7aa278e997cf4777f62bc5e0f6a001d2c6090`.

Status: observed; verdict: pass.

Every number below is recomputed from raw.jsonl by experiments/product/analyze.py; `python experiments/verify.py <run>` repeats that check.

## Steps

| Step | Exit | Timed out | Duration (ms) |
| --- | --- | --- | --- |
| unit | 0 | False | 86579 |
| race | 0 | False | 112700 |
| vectors | 0 | False | 531677 |
| fuzz-frame | 0 | False | 99909 |
| fuzz-control | 0 | False | 60470 |
| vet | 0 | False | 13048 |
| staticcheck | 0 | False | 41252 |
| govulncheck | 0 | False | 12935 |

Unit coverage of the delivery unit: 96.1%. Engines: {"sqlite": ["3.54.1"], "postgres": ["18.6 (Debian 18.6-1.pgdg13+2)"]}.

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
| TASK006-D20-BUFFERS | pass | unit: TestMessageReaderFinalBytesAndError<br>unit: TestMessageReaderFinalBytesAndError/exact_limit_eof<br>unit: TestMessageReaderFinalBytesAndError/over_limit_eof<br>unit: TestMessageReaderFinalBytesAndError/partial_error<br>unit: TestMessageReaderFinalBytesAndError/short_eof<br>race: TestMessageReaderFinalBytesAndError<br>race: TestMessageReaderFinalBytesAndError/exact_limit_eof<br>race: TestMessageReaderFinalBytesAndError/over_limit_eof<br>race: TestMessageReaderFinalBytesAndError/partial_error<br>race: TestMessageReaderFinalBytesAndError/short_eof<br>unit: TestMessageReaderInvalidBounds<br>race: TestMessageReaderInvalidBounds<br>unit: TestMessageReaderRealAllocationComparison<br>race: TestMessageReaderRealAllocationComparison<br>unit: TestMessageReaderRealFragmentedAndLimit<br>unit: TestMessageReaderRealFragmentedAndLimit/original/empty<br>unit: TestMessageReaderRealFragmentedAndLimit/original/maximum<br>unit: TestMessageReaderRealFragmentedAndLimit/original/over_limit<br>unit: TestMessageReaderRealFragmentedAndLimit/original/short<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/empty<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/maximum<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/over_limit<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/short<br>race: TestMessageReaderRealFragmentedAndLimit<br>race: TestMessageReaderRealFragmentedAndLimit/original/empty<br>race: TestMessageReaderRealFragmentedAndLimit/original/maximum<br>race: TestMessageReaderRealFragmentedAndLimit/original/over_limit<br>race: TestMessageReaderRealFragmentedAndLimit/original/short<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/empty<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/maximum<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/over_limit<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/short<br>unit: TestMessageReaderReuseAndBounds<br>race: TestMessageReaderReuseAndBounds<br>unit: TestMessageReaderShortReadsAndErrors<br>unit: TestMessageReaderShortReadsAndErrors/at_limit<br>unit: TestMessageReaderShortReadsAndErrors/empty_message<br>unit: TestMessageReaderShortReadsAndErrors/over_limit<br>unit: TestMessageReaderShortReadsAndErrors/over_limit_read_error<br>unit: TestMessageReaderShortReadsAndErrors/partial_error<br>unit: TestMessageReaderShortReadsAndErrors/short_reads<br>race: TestMessageReaderShortReadsAndErrors<br>race: TestMessageReaderShortReadsAndErrors/at_limit<br>race: TestMessageReaderShortReadsAndErrors/empty_message<br>race: TestMessageReaderShortReadsAndErrors/over_limit<br>race: TestMessageReaderShortReadsAndErrors/over_limit_read_error<br>race: TestMessageReaderShortReadsAndErrors/partial_error<br>race: TestMessageReaderShortReadsAndErrors/short_reads<br>unit: TestNewDataMaxBounds<br>unit: TestNewDataMaxBounds/default<br>unit: TestNewDataMaxBounds/integer_maximum<br>unit: TestNewDataMaxBounds/maximum<br>unit: TestNewDataMaxBounds/negative_default<br>unit: TestNewDataMaxBounds/over_maximum<br>race: TestNewDataMaxBounds<br>race: TestNewDataMaxBounds/default<br>race: TestNewDataMaxBounds/integer_maximum<br>race: TestNewDataMaxBounds/maximum<br>race: TestNewDataMaxBounds/negative_default<br>race: TestNewDataMaxBounds/over_maximum<br>unit: TestStreamMessageBufferKeepsTerminalErrors<br>unit: TestStreamMessageBufferKeepsTerminalErrors/abort<br>unit: TestStreamMessageBufferKeepsTerminalErrors/data_over_peer_limit<br>unit: TestStreamMessageBufferKeepsTerminalErrors/empty<br>unit: TestStreamMessageBufferKeepsTerminalErrors/eof_without_fin<br>unit: TestStreamMessageBufferKeepsTerminalErrors/malformed_fin<br>unit: TestStreamMessageBufferKeepsTerminalErrors/message_over_read_limit<br>unit: TestStreamMessageBufferKeepsTerminalErrors/text<br>race: TestStreamMessageBufferKeepsTerminalErrors<br>race: TestStreamMessageBufferKeepsTerminalErrors/abort<br>race: TestStreamMessageBufferKeepsTerminalErrors/data_over_peer_limit<br>race: TestStreamMessageBufferKeepsTerminalErrors/empty<br>race: TestStreamMessageBufferKeepsTerminalErrors/eof_without_fin<br>race: TestStreamMessageBufferKeepsTerminalErrors/malformed_fin<br>race: TestStreamMessageBufferKeepsTerminalErrors/message_over_read_limit<br>race: TestStreamMessageBufferKeepsTerminalErrors/text<br>unit: TestStreamMessageBufferPreservesPendingAndPeerLimit<br>race: TestStreamMessageBufferPreservesPendingAndPeerLimit<br>unit: TestTunnelMessageBufferSurvivesAdapterValueCopies<br>unit: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.control.v1<br>unit: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.tunnel.v1<br>race: TestTunnelMessageBufferSurvivesAdapterValueCopies<br>race: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.control.v1<br>race: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.tunnel.v1<br>unit: TestUplinkBufferWaitsForWriteBeforeReuse<br>unit: TestUplinkBufferWaitsForWriteBeforeReuse/completed<br>unit: TestUplinkBufferWaitsForWriteBeforeReuse/write_failed<br>race: TestUplinkBufferWaitsForWriteBeforeReuse<br>race: TestUplinkBufferWaitsForWriteBeforeReuse/completed<br>race: TestUplinkBufferWaitsForWriteBeforeReuse/write_failed |

## Observations (recorded, no pass lines; B4 §6)

- g3_resources_baseline: 48 records
- g3_resources_begin: 70 records
- g3_resources_sample: 399 records
- g3_resources_complete: 70 records
- G3-fault: 146 records
- resources_baseline: 101 records
- fd_diagnostic: 276 records
- O1: 12 records
- resources_begin: 103 records
- resources_sample: 522 records
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
