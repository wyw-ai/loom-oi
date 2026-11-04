# G1-P1-R4 run record

Gate G1, evidence level Implementation, subject `bff7aa278e997cf4777f62bc5e0f6a001d2c6090`.

Status: observed; verdict: pass.

Every number below is recomputed from raw.jsonl by experiments/product/analyze.py; `python experiments/verify.py <run>` repeats that check.

## Steps

| Step | Exit | Timed out | Duration (ms) |
| --- | --- | --- | --- |
| unit | 0 | False | 94344 |
| race | 0 | False | 117751 |
| vectors | 0 | False | 602069 |
| fuzz-frame | 0 | False | 103019 |
| fuzz-control | 0 | False | 60536 |
| vet | 0 | False | 13833 |
| staticcheck | 0 | False | 42979 |
| govulncheck | 0 | False | 13987 |

Unit coverage of the delivery unit: 96.1%. Engines: {"sqlite": ["3.54.1"], "postgres": ["18.6 (Debian 18.6-1.pgdg13+2)"]}.

## G1 criteria

| Criterion | Vectors | Result |
| --- | --- | --- |
| 真实注册授权双腿通道 | TV-B1-01, TV-B2-03 | pass |
| 双后端CRUD契约 | TV-B3-01, TV-B3-02, TV-B3-03 | pass |
| 目标替换拒绝 | TV-B2-04 | pass |
| 超时与资源清理 | TV-B1-14, TV-B4-03 | pass |
| 无业务实例化依赖 | TV-B4-04 | pass |

## Vectors

| Vector | Result | Checked |
| --- | --- | --- |
| TV-B1-01 | pass | vectors: TestTVB1_01_ReadOnlyHTTP<br>vectors: TestTVB1_01_ReadOnlyHTTP/sqlite<br>vectors: TestTVB1_01_ReadOnlyHTTP/postgres |
| TV-B1-02 | pass | vectors: TestTVB1_02_SubprotocolVersion<br>vectors: TestTVB1_02_SubprotocolVersion/sqlite<br>vectors: TestTVB1_02_SubprotocolVersion/postgres |
| TV-B1-03 | pass | vectors: TestTVB1_03_UnknownFields<br>vectors: TestTVB1_03_UnknownFields/sqlite<br>vectors: TestTVB1_03_UnknownFields/postgres |
| TV-B1-04 | pass | vectors: TestTVB1_04_PayloadTooLarge<br>vectors: TestTVB1_04_PayloadTooLarge/sqlite<br>vectors: TestTVB1_04_PayloadTooLarge/postgres |
| TV-B1-05 | pass | vectors: TestTVB1_05_DataBeforeReady<br>vectors: TestTVB1_05_DataBeforeReady/sqlite<br>vectors: TestTVB1_05_DataBeforeReady/postgres |
| TV-B1-06 | pass | vectors: TestTVB1_06_HalfClose<br>vectors: TestTVB1_06_HalfClose/sqlite<br>vectors: TestTVB1_06_HalfClose/postgres |
| TV-B1-07 | pass | vectors: TestTVB1_07_DataAfterFin<br>vectors: TestTVB1_07_DataAfterFin/sqlite<br>vectors: TestTVB1_07_DataAfterFin/postgres |
| TV-B1-08 | pass | vectors: TestTVB1_08_TargetReset<br>vectors: TestTVB1_08_TargetReset/sqlite<br>vectors: TestTVB1_08_TargetReset/postgres |
| TV-B1-09 | pass | vectors: TestTVB1_09_LargeSlowRead<br>vectors: TestTVB1_09_LargeSlowRead/sqlite<br>vectors: TestTVB1_09_LargeSlowRead/postgres |
| TV-B1-10 | pass | vectors: TestTVB1_10_Idempotency<br>vectors: TestTVB1_10_Idempotency/sqlite<br>vectors: TestTVB1_10_Idempotency/postgres |
| TV-B1-11 | pass | vectors: TestTVB1_11_RevokeDuringPrepare<br>vectors: TestTVB1_11_RevokeDuringPrepare/sqlite<br>vectors: TestTVB1_11_RevokeDuringPrepare/postgres |
| TV-B1-12 | pass | vectors: TestTVB1_12_RevokeOpen<br>vectors: TestTVB1_12_RevokeOpen/sqlite<br>vectors: TestTVB1_12_RevokeOpen/postgres |
| TV-B1-13 | pass | vectors: TestTVB1_13_GenerationReplaced<br>vectors: TestTVB1_13_GenerationReplaced/sqlite<br>vectors: TestTVB1_13_GenerationReplaced/postgres |
| TV-B1-14 | pass | vectors: TestTVB1_14_PrepareTimeout<br>vectors: TestTVB1_14_PrepareTimeout/sqlite<br>vectors: TestTVB1_14_PrepareTimeout/postgres |
| TV-B1-15 | pass | vectors: TestTVB1_15_TargetConnectFailed<br>vectors: TestTVB1_15_TargetConnectFailed/sqlite<br>vectors: TestTVB1_15_TargetConnectFailed/postgres |
| TV-B1-16 | pass | vectors: TestTVB1_16_Quota<br>vectors: TestTVB1_16_Quota/sqlite<br>vectors: TestTVB1_16_Quota/postgres |
| TV-B1-17 | pass | vectors: TestTVB1_17_PrepareOutOfOrder<br>vectors: TestTVB1_17_PrepareOutOfOrder/sqlite |
| TV-B1-18 | pass | vectors: TestTVB1_18_Admission<br>vectors: TestTVB1_18_Admission/sqlite<br>vectors: TestTVB1_18_Admission/postgres |
| TV-B2-01 | pass | vectors: TestTVB2_01_BadCredentials<br>vectors: TestTVB2_01_BadCredentials/sqlite<br>vectors: TestTVB2_01_BadCredentials/postgres |
| TV-B2-02 | pass | vectors: TestTVB2_02_DiscoverAndOpenGrants<br>vectors: TestTVB2_02_DiscoverAndOpenGrants/sqlite<br>vectors: TestTVB2_02_DiscoverAndOpenGrants/postgres |
| TV-B2-03 | pass | vectors: TestTVB2_03_NoCrossTalk<br>vectors: TestTVB2_03_NoCrossTalk/sqlite<br>vectors: TestTVB2_03_NoCrossTalk/postgres |
| TV-B2-04 | pass | vectors: TestTVB2_04_TargetReplaced<br>vectors: TestTVB2_04_TargetReplaced/sqlite<br>vectors: TestTVB2_04_TargetReplaced/postgres |
| TV-B2-05 | pass | vectors: TestTVB2_05_TicketOnce<br>vectors: TestTVB2_05_TicketOnce/sqlite<br>vectors: TestTVB2_05_TicketOnce/postgres |
| TV-B2-06 | pass | vectors: TestTVB2_06_TicketMisuse<br>vectors: TestTVB2_06_TicketMisuse/sqlite<br>vectors: TestTVB2_06_TicketMisuse/postgres |
| TV-B2-07 | pass | vectors: TestTVB2_07_Certificates<br>vectors: TestTVB2_07_Certificates/sqlite<br>vectors: TestTVB2_07_Certificates/postgres |
| TV-B2-08 | pass | vectors: TestTVB2_08_RegisterLimits<br>vectors: TestTVB2_08_RegisterLimits/sqlite<br>vectors: TestTVB2_08_RegisterLimits/postgres |
| TV-B2-09 | pass | vectors: TestTVB2_09_AddressNotAllowed<br>vectors: TestTVB2_09_AddressNotAllowed/sqlite<br>vectors: TestTVB2_09_AddressNotAllowed/postgres |
| TV-B2-10 | pass | vectors: TestTVB2_10_ControlCut<br>vectors: TestTVB2_10_ControlCut/sqlite<br>vectors: TestTVB2_10_ControlCut/postgres |
| TV-B2-11 | pass | vectors: TestTVB2_11_ProcessPause<br>vectors: TestTVB2_11_ProcessPause/sqlite<br>vectors: TestTVB2_11_ProcessPause/postgres |
| TV-B2-12 | pass | vectors: TestTVB2_12_TokenRevokeAndExpiry<br>vectors: TestTVB2_12_TokenRevokeAndExpiry/sqlite<br>vectors: TestTVB2_12_TokenRevokeAndExpiry/postgres |
| TV-B2-13 | pass | vectors: TestTVB2_13_DisableAccess<br>vectors: TestTVB2_13_DisableAccess/sqlite<br>vectors: TestTVB2_13_DisableAccess/postgres |
| TV-B2-14 | pass | vectors: TestTVB2_14_ScopeSeparation<br>vectors: TestTVB2_14_ScopeSeparation/sqlite<br>vectors: TestTVB2_14_ScopeSeparation/postgres |
| TV-B3-01 | pass | unit: TestMigrateAndEmptySnapshot<br>unit: TestMigrateAndEmptySnapshot/sqlite<br>unit: TestMigrateAndEmptySnapshot/postgres<br>unit: TestApplyConfigCRUDAndAudit<br>unit: TestApplyConfigCRUDAndAudit/sqlite<br>unit: TestApplyConfigCRUDAndAudit/postgres<br>unit: TestInvalidMutations<br>unit: TestInvalidMutations/sqlite<br>unit: TestInvalidMutations/postgres<br>unit: TestRegistryAndEpoch<br>unit: TestRegistryAndEpoch/sqlite<br>unit: TestRegistryAndEpoch/postgres |
| TV-B3-02 | pass | unit: TestConcurrentApplyConfigConflict<br>unit: TestConcurrentApplyConfigConflict/sqlite<br>unit: TestConcurrentApplyConfigConflict/postgres |
| TV-B3-03 | pass | unit: TestAuditFailureRollsBackConfig<br>unit: TestAuditFailureRollsBackConfig/sqlite<br>unit: TestAuditFailureRollsBackConfig/postgres |
| TV-B3-04 | pass | vectors: TestTVB3_04_Restart<br>vectors: TestTVB3_04_Restart/sqlite<br>vectors: TestTVB3_04_Restart/postgres |
| TV-B3-05 | pass | vectors: TestTVB3_05_PostgresDown<br>vectors: TestTVB3_05_PostgresDown/postgres |
| TV-B3-06 | pass | vectors: TestTVB3_06_SingleInstance<br>vectors: TestTVB3_06_SingleInstance/sqlite<br>vectors: TestTVB3_06_SingleInstance/postgres |
| TV-B3-07 | pass | vectors: TestTVB3_07_SchemaChecks<br>vectors: TestTVB3_07_SchemaChecks/sqlite<br>vectors: TestTVB3_07_SchemaChecks/postgres |
| TV-B3-08 | pass | vectors: TestTVB3_08_SQLiteSettings<br>vectors: TestTVB3_08_SQLiteSettings/sqlite |
| TV-B3-09 | pass | vectors: TestTVB3_09_RevokeVisibleToCommit<br>vectors: TestTVB3_09_RevokeVisibleToCommit/sqlite<br>vectors: TestTVB3_09_RevokeVisibleToCommit/postgres |
| TV-B3-10 | pass | vectors: TestTVB3_10_EpochRollback<br>vectors: TestTVB3_10_EpochRollback/sqlite<br>vectors: TestTVB3_10_EpochRollback/postgres |
| TV-B4-01 | pass | vectors: TestTVB4_01_ReproducibleBuild |
| TV-B4-03 | pass | vectors: TestTVB4_03_ResourceRegression<br>vectors: TestTVB4_03_ResourceRegression/sqlite<br>vectors: TestTVB4_03_ResourceRegression/postgres<br>raw: b4-resource-v2 |
| TV-B4-04 | pass | vectors: TestTVB4_04_NoBusinessDependency<br>vectors: TestTVB4_04_NoBusinessDependency/sqlite<br>vectors: TestTVB4_04_NoBusinessDependency/postgres |
| TV-B4-05 | pass | fuzz-frame: FuzzDecodeFrame<br>fuzz-control: FuzzDecodeControl |
| TV-B4-06 | pass | vectors: TestTVB4_06_Load<br>vectors: TestTVB4_06_Load/sqlite<br>vectors: TestTVB4_06_Load/postgres |
| TASK006-LIFECYCLE | pass | unit: TestForwardCancellation<br>unit: TestForwardCancellation/Completed_waiting_for_remote_close<br>unit: TestForwardCancellation/active_read<br>unit: TestForwardCancellation/already_cancelled_before_registration<br>unit: TestForwardCancellation/before_READY<br>unit: TestForwardCancellation/blocked_leg_write<br>unit: TestForwardCancellation/blocked_local_write<br>unit: TestForwardCancellation/cancel_before_FIN<br>unit: TestForwardCancellation/cancel_first<br>unit: TestForwardCancellation/remote_ABORT_first<br>unit: TestForwardNormalCompletionCancelsWatcher<br>unit: TestForwardCancellationJoinsForcedClose<br>unit: TestForwardCancellationRacesFIN<br>unit: TestForwardCancellationLocalWriteErrorWaitsDrain<br>unit: TestForwardCancellationLocalWriteErrorWaitsDrain/cancel_first<br>unit: TestForwardCancellationLocalWriteErrorWaitsDrain/local_write_error_first<br>unit: TestForwardProtocolFailureKeepsAbortGrace<br>unit: TestListenCancelActive<br>unit: TestStreamBackpressureBounded<br>unit: TestBridgeBackpressureBounded |
| TASK006-D20-BUFFERS | pass | unit: TestMessageReaderFinalBytesAndError<br>unit: TestMessageReaderFinalBytesAndError/exact_limit_eof<br>unit: TestMessageReaderFinalBytesAndError/over_limit_eof<br>unit: TestMessageReaderFinalBytesAndError/partial_error<br>unit: TestMessageReaderFinalBytesAndError/short_eof<br>race: TestMessageReaderFinalBytesAndError<br>race: TestMessageReaderFinalBytesAndError/exact_limit_eof<br>race: TestMessageReaderFinalBytesAndError/over_limit_eof<br>race: TestMessageReaderFinalBytesAndError/partial_error<br>race: TestMessageReaderFinalBytesAndError/short_eof<br>unit: TestMessageReaderInvalidBounds<br>race: TestMessageReaderInvalidBounds<br>unit: TestMessageReaderRealAllocationComparison<br>race: TestMessageReaderRealAllocationComparison<br>unit: TestMessageReaderRealFragmentedAndLimit<br>unit: TestMessageReaderRealFragmentedAndLimit/original/empty<br>unit: TestMessageReaderRealFragmentedAndLimit/original/maximum<br>unit: TestMessageReaderRealFragmentedAndLimit/original/over_limit<br>unit: TestMessageReaderRealFragmentedAndLimit/original/short<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/empty<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/maximum<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/over_limit<br>unit: TestMessageReaderRealFragmentedAndLimit/reusable/short<br>race: TestMessageReaderRealFragmentedAndLimit<br>race: TestMessageReaderRealFragmentedAndLimit/original/empty<br>race: TestMessageReaderRealFragmentedAndLimit/original/maximum<br>race: TestMessageReaderRealFragmentedAndLimit/original/over_limit<br>race: TestMessageReaderRealFragmentedAndLimit/original/short<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/empty<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/maximum<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/over_limit<br>race: TestMessageReaderRealFragmentedAndLimit/reusable/short<br>unit: TestMessageReaderReuseAndBounds<br>race: TestMessageReaderReuseAndBounds<br>unit: TestMessageReaderShortReadsAndErrors<br>unit: TestMessageReaderShortReadsAndErrors/at_limit<br>unit: TestMessageReaderShortReadsAndErrors/empty_message<br>unit: TestMessageReaderShortReadsAndErrors/over_limit<br>unit: TestMessageReaderShortReadsAndErrors/over_limit_read_error<br>unit: TestMessageReaderShortReadsAndErrors/partial_error<br>unit: TestMessageReaderShortReadsAndErrors/short_reads<br>race: TestMessageReaderShortReadsAndErrors<br>race: TestMessageReaderShortReadsAndErrors/at_limit<br>race: TestMessageReaderShortReadsAndErrors/empty_message<br>race: TestMessageReaderShortReadsAndErrors/over_limit<br>race: TestMessageReaderShortReadsAndErrors/over_limit_read_error<br>race: TestMessageReaderShortReadsAndErrors/partial_error<br>race: TestMessageReaderShortReadsAndErrors/short_reads<br>unit: TestNewDataMaxBounds<br>unit: TestNewDataMaxBounds/default<br>unit: TestNewDataMaxBounds/integer_maximum<br>unit: TestNewDataMaxBounds/maximum<br>unit: TestNewDataMaxBounds/negative_default<br>unit: TestNewDataMaxBounds/over_maximum<br>race: TestNewDataMaxBounds<br>race: TestNewDataMaxBounds/default<br>race: TestNewDataMaxBounds/integer_maximum<br>race: TestNewDataMaxBounds/maximum<br>race: TestNewDataMaxBounds/negative_default<br>race: TestNewDataMaxBounds/over_maximum<br>unit: TestStreamMessageBufferKeepsTerminalErrors<br>unit: TestStreamMessageBufferKeepsTerminalErrors/abort<br>unit: TestStreamMessageBufferKeepsTerminalErrors/data_over_peer_limit<br>unit: TestStreamMessageBufferKeepsTerminalErrors/empty<br>unit: TestStreamMessageBufferKeepsTerminalErrors/eof_without_fin<br>unit: TestStreamMessageBufferKeepsTerminalErrors/malformed_fin<br>unit: TestStreamMessageBufferKeepsTerminalErrors/message_over_read_limit<br>unit: TestStreamMessageBufferKeepsTerminalErrors/text<br>race: TestStreamMessageBufferKeepsTerminalErrors<br>race: TestStreamMessageBufferKeepsTerminalErrors/abort<br>race: TestStreamMessageBufferKeepsTerminalErrors/data_over_peer_limit<br>race: TestStreamMessageBufferKeepsTerminalErrors/empty<br>race: TestStreamMessageBufferKeepsTerminalErrors/eof_without_fin<br>race: TestStreamMessageBufferKeepsTerminalErrors/malformed_fin<br>race: TestStreamMessageBufferKeepsTerminalErrors/message_over_read_limit<br>race: TestStreamMessageBufferKeepsTerminalErrors/text<br>unit: TestStreamMessageBufferPreservesPendingAndPeerLimit<br>race: TestStreamMessageBufferPreservesPendingAndPeerLimit<br>unit: TestTunnelMessageBufferSurvivesAdapterValueCopies<br>unit: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.control.v1<br>unit: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.tunnel.v1<br>race: TestTunnelMessageBufferSurvivesAdapterValueCopies<br>race: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.control.v1<br>race: TestTunnelMessageBufferSurvivesAdapterValueCopies/oi.tunnel.v1<br>unit: TestUplinkBufferWaitsForWriteBeforeReuse<br>unit: TestUplinkBufferWaitsForWriteBeforeReuse/completed<br>unit: TestUplinkBufferWaitsForWriteBeforeReuse/write_failed<br>race: TestUplinkBufferWaitsForWriteBeforeReuse<br>race: TestUplinkBufferWaitsForWriteBeforeReuse/completed<br>race: TestUplinkBufferWaitsForWriteBeforeReuse/write_failed |

## Observations (recorded, no pass lines; B4 §6)

- g3_resources_baseline: 48 records
- g3_resources_begin: 70 records
- g3_resources_sample: 409 records
- g3_resources_complete: 70 records
- G3-fault: 146 records
- resources_baseline: 103 records
- fd_diagnostic: 282 records
- O1: 12 records
- resources_begin: 115 records
- resources_sample: 578 records
- resources: 115 records
- O4: 23 records
- O3: 16 records
- O2: 17 records
- O3-load-workers: 12 records
- O3-target-boundaries: 164 records
- G3-transfer: 952 records
- G3-effect: 16 records

## Problems

- none

## Limitations

- Single machine loopback topology of B4 §4 (GitHub-hosted ubuntu-24.04 runner); not the 1C1G target environment (G5) and not WAN latency
- Test configuration of B4 §3 (shortened T_close, heartbeat, license and idle values); values are not SLOs or capacity commitments
- Observations O1-O4 are recorded without pass lines except zero-tolerance events (B4 §6); readiness is always unknown in P1 (no probes, G2)
- This rerun preserves the P1 scope; it does not independently establish G4 backup/restore/migration, HA (G6), or real consumer/provider compatibility (G7).
- Synthetic deterministic HTTP fixtures T1-T3; two OICs are logically separate accesses on one host, not host isolation
