# G1-P1-R2 run record

Gate G1, evidence level Implementation, subject `b7af58249eb57e3b32e0fd0622764a439a530d54`.

Status: observed; verdict: fail.

Every number below is recomputed from raw.jsonl by experiments/product/analyze.py; `python experiments/verify.py <run>` repeats that check.

## Steps

| Step | Exit | Timed out | Duration (ms) |
| --- | --- | --- | --- |
| unit | 0 | False | 90476 |
| race | 1 | False | 115148 |
| vectors | 0 | False | 475439 |
| fuzz-frame | 0 | False | 101708 |
| fuzz-control | 0 | False | 60520 |
| vet | 0 | False | 13209 |
| staticcheck | 0 | False | 41559 |
| govulncheck | 0 | False | 13492 |

Unit coverage of the delivery unit: 95.9%. Engines: {"sqlite": ["3.54.1"], "postgres": ["18.6 (Debian 18.6-1.pgdg13+2)"]}.

## G1 criteria

| Criterion | Vectors | Result |
| --- | --- | --- |
| 真实注册授权双腿通道 | TV-B1-01, TV-B2-03 | pass |
| 双后端CRUD契约 | TV-B3-01, TV-B3-02, TV-B3-03 | pass |
| 目标替换拒绝 | TV-B2-04 | pass |
| 超时与资源清理 | TV-B1-14, TV-B4-03 | fail |
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
| TV-B4-03 | fail | vectors: TestTVB4_03_ResourceRegression<br>vectors: TestTVB4_03_ResourceRegression/sqlite<br>vectors: TestTVB4_03_ResourceRegression/postgres<br>raw: b4-resource-v2 |
| TV-B4-04 | pass | vectors: TestTVB4_04_NoBusinessDependency<br>vectors: TestTVB4_04_NoBusinessDependency/sqlite<br>vectors: TestTVB4_04_NoBusinessDependency/postgres |
| TV-B4-05 | pass | fuzz-frame: FuzzDecodeFrame<br>fuzz-control: FuzzDecodeControl |
| TV-B4-06 | pass | vectors: TestTVB4_06_Load<br>vectors: TestTVB4_06_Load/sqlite<br>vectors: TestTVB4_06_Load/postgres |

## Observations (recorded, no pass lines; B4 §6)

- O1: 12 records
- resources_begin: 115 records
- resources: 115 records
- O4: 23 records
- O3: 16 records
- O2: 17 records
- resources_baseline: 14 records
- resources_sample: 39 records

## Problems

- race: exit code 1
- TestTVB1_01_ReadOnlyHTTP/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_01_ReadOnlyHTTP/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_01_ReadOnlyHTTP/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_01_ReadOnlyHTTP/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_02_SubprotocolVersion/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_02_SubprotocolVersion/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_02_SubprotocolVersion/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_02_SubprotocolVersion/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_03_UnknownFields/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_03_UnknownFields/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_03_UnknownFields/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_03_UnknownFields/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_04_PayloadTooLarge/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_04_PayloadTooLarge/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_04_PayloadTooLarge/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_04_PayloadTooLarge/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_05_DataBeforeReady/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_05_DataBeforeReady/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_05_DataBeforeReady/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_05_DataBeforeReady/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_06_HalfClose/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_06_HalfClose/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_06_HalfClose/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_06_HalfClose/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_07_DataAfterFin/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_07_DataAfterFin/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_07_DataAfterFin/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_07_DataAfterFin/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_08_TargetReset/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_08_TargetReset/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_08_TargetReset/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_08_TargetReset/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_09_LargeSlowRead/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_09_LargeSlowRead/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_09_LargeSlowRead/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_09_LargeSlowRead/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_10_Idempotency/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_10_Idempotency/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_10_Idempotency/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_10_Idempotency/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_11_RevokeDuringPrepare/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_11_RevokeDuringPrepare/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_11_RevokeDuringPrepare/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_11_RevokeDuringPrepare/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_12_RevokeOpen/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_12_RevokeOpen/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_12_RevokeOpen/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_12_RevokeOpen/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_13_GenerationReplaced/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_13_GenerationReplaced/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_13_GenerationReplaced/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_13_GenerationReplaced/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_14_PrepareTimeout/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_14_PrepareTimeout/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_14_PrepareTimeout/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_14_PrepareTimeout/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_15_TargetConnectFailed/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_15_TargetConnectFailed/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_15_TargetConnectFailed/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_15_TargetConnectFailed/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_16_Quota/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_16_Quota/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_16_Quota/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_16_Quota/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_18_Admission/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_18_Admission/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB1_18_Admission/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB1_18_Admission/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_01_BadCredentials/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_01_BadCredentials/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_01_BadCredentials/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_01_BadCredentials/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_02_DiscoverAndOpenGrants/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_02_DiscoverAndOpenGrants/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_02_DiscoverAndOpenGrants/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_02_DiscoverAndOpenGrants/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_03_NoCrossTalk/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_03_NoCrossTalk/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_03_NoCrossTalk/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_03_NoCrossTalk/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_04_TargetReplaced/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_04_TargetReplaced/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_04_TargetReplaced/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_04_TargetReplaced/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_05_TicketOnce/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_05_TicketOnce/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_05_TicketOnce/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_05_TicketOnce/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_06_TicketMisuse/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_06_TicketMisuse/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_06_TicketMisuse/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_06_TicketMisuse/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_07_Certificates/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_07_Certificates/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_07_Certificates/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_07_Certificates/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_08_RegisterLimits/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_08_RegisterLimits/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_08_RegisterLimits/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_08_RegisterLimits/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_09_AddressNotAllowed/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_09_AddressNotAllowed/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_09_AddressNotAllowed/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_09_AddressNotAllowed/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_10_ControlCut/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_10_ControlCut/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_10_ControlCut/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_10_ControlCut/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_11_ProcessPause/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_11_ProcessPause/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_11_ProcessPause/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_11_ProcessPause/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_12_TokenRevokeAndExpiry/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_12_TokenRevokeAndExpiry/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_12_TokenRevokeAndExpiry/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_12_TokenRevokeAndExpiry/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_13_DisableAccess/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_13_DisableAccess/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_13_DisableAccess/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_13_DisableAccess/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB2_14_ScopeSeparation/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_14_ScopeSeparation/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB2_14_ScopeSeparation/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB2_14_ScopeSeparation/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_11b_RestoreMaintenance/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11b_RestoreMaintenance/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_11b_RestoreMaintenance/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11b_RestoreMaintenance/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_11c_OldTermRejected/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11c_OldTermRejected/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_11c_OldTermRejected/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11c_OldTermRejected/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_11d_Migration/sqlite-to-postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11d_Migration/sqlite-to-postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_11d_Migration/postgres-to-sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11d_Migration/postgres-to-sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_11e_DiskFullReadOnly/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11e_DiskFullReadOnly/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_11e_DiskFullReadOnly/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11e_DiskFullReadOnly/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_11e_DiskFullReadOnly/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11e_DiskFullReadOnly/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_11g_WriteCongestion/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11g_WriteCongestion/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_11g_WriteCongestion/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_11g_WriteCongestion/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_04_Restart/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_04_Restart/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_04_Restart/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_04_Restart/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_05_PostgresDown/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_05_PostgresDown/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_09_RevokeVisibleToCommit/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_09_RevokeVisibleToCommit/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_09_RevokeVisibleToCommit/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_09_RevokeVisibleToCommit/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB3_10_EpochRollback/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_10_EpochRollback/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB3_10_EpochRollback/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB3_10_EpochRollback/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_03_ResourceRegression/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_04_NoBusinessDependency/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_04_NoBusinessDependency/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_04_NoBusinessDependency/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_04_NoBusinessDependency/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/sqlite: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources_begin: missing baseline or profile/backend mismatch
- TestTVB4_06_Load/postgres: resources: missing baseline or profile/backend mismatch
- TestTVB1_01_ReadOnlyHTTP/postgres: resource checks 0 below 1
- TestTVB1_01_ReadOnlyHTTP/sqlite: resource checks 0 below 1
- TestTVB1_02_SubprotocolVersion/postgres: resource checks 0 below 1
- TestTVB1_02_SubprotocolVersion/sqlite: resource checks 0 below 1
- TestTVB1_03_UnknownFields/postgres: resource checks 0 below 1
- TestTVB1_03_UnknownFields/sqlite: resource checks 0 below 1
- TestTVB1_04_PayloadTooLarge/postgres: resource checks 0 below 1
- TestTVB1_04_PayloadTooLarge/sqlite: resource checks 0 below 1
- TestTVB1_05_DataBeforeReady/postgres: resource checks 0 below 1
- TestTVB1_05_DataBeforeReady/sqlite: resource checks 0 below 1
- TestTVB1_06_HalfClose/postgres: resource checks 0 below 1
- TestTVB1_06_HalfClose/sqlite: resource checks 0 below 1
- TestTVB1_07_DataAfterFin/postgres: resource checks 0 below 1
- TestTVB1_07_DataAfterFin/sqlite: resource checks 0 below 1
- TestTVB1_08_TargetReset/postgres: resource checks 0 below 1
- TestTVB1_08_TargetReset/sqlite: resource checks 0 below 1
- TestTVB1_09_LargeSlowRead/postgres: resource checks 0 below 1
- TestTVB1_09_LargeSlowRead/sqlite: resource checks 0 below 1
- TestTVB1_10_Idempotency/postgres: resource checks 0 below 1
- TestTVB1_10_Idempotency/sqlite: resource checks 0 below 1
- TestTVB1_11_RevokeDuringPrepare/postgres: resource checks 0 below 1
- TestTVB1_11_RevokeDuringPrepare/sqlite: resource checks 0 below 1
- TestTVB1_12_RevokeOpen/postgres: resource checks 0 below 1
- TestTVB1_12_RevokeOpen/sqlite: resource checks 0 below 1
- TestTVB1_13_GenerationReplaced/postgres: resource checks 0 below 1
- TestTVB1_13_GenerationReplaced/sqlite: resource checks 0 below 1
- TestTVB1_14_PrepareTimeout/postgres: resource checks 0 below 1
- TestTVB1_14_PrepareTimeout/sqlite: resource checks 0 below 1
- TestTVB1_15_TargetConnectFailed/postgres: resource checks 0 below 1
- TestTVB1_15_TargetConnectFailed/sqlite: resource checks 0 below 1
- TestTVB1_16_Quota/postgres: resource checks 0 below 1
- TestTVB1_16_Quota/sqlite: resource checks 0 below 1
- TestTVB1_18_Admission/postgres: resource checks 0 below 1
- TestTVB1_18_Admission/sqlite: resource checks 0 below 1
- TestTVB2_01_BadCredentials/postgres: resource checks 0 below 1
- TestTVB2_01_BadCredentials/sqlite: resource checks 0 below 1
- TestTVB2_02_DiscoverAndOpenGrants/postgres: resource checks 0 below 1
- TestTVB2_02_DiscoverAndOpenGrants/sqlite: resource checks 0 below 1
- TestTVB2_03_NoCrossTalk/postgres: resource checks 0 below 1
- TestTVB2_03_NoCrossTalk/sqlite: resource checks 0 below 1
- TestTVB2_04_TargetReplaced/postgres: resource checks 0 below 1
- TestTVB2_04_TargetReplaced/sqlite: resource checks 0 below 1
- TestTVB2_05_TicketOnce/postgres: resource checks 0 below 1
- TestTVB2_05_TicketOnce/sqlite: resource checks 0 below 1
- TestTVB2_06_TicketMisuse/postgres: resource checks 0 below 1
- TestTVB2_06_TicketMisuse/sqlite: resource checks 0 below 1
- TestTVB2_07_Certificates/postgres: resource checks 0 below 1
- TestTVB2_07_Certificates/sqlite: resource checks 0 below 1
- TestTVB2_08_RegisterLimits/postgres: resource checks 0 below 1
- TestTVB2_08_RegisterLimits/sqlite: resource checks 0 below 1
- TestTVB2_09_AddressNotAllowed/postgres: resource checks 0 below 1
- TestTVB2_09_AddressNotAllowed/sqlite: resource checks 0 below 1
- TestTVB2_10_ControlCut/postgres: resource checks 0 below 1
- TestTVB2_10_ControlCut/sqlite: resource checks 0 below 1
- TestTVB2_11_ProcessPause/postgres: resource checks 0 below 1
- TestTVB2_11_ProcessPause/sqlite: resource checks 0 below 1
- TestTVB2_12_TokenRevokeAndExpiry/postgres: resource checks 0 below 1
- TestTVB2_12_TokenRevokeAndExpiry/sqlite: resource checks 0 below 1
- TestTVB2_13_DisableAccess/postgres: resource checks 0 below 1
- TestTVB2_13_DisableAccess/sqlite: resource checks 0 below 1
- TestTVB2_14_ScopeSeparation/postgres: resource checks 0 below 1
- TestTVB2_14_ScopeSeparation/sqlite: resource checks 0 below 1
- TestTVB3_04_Restart/postgres: resource checks 0 below 1
- TestTVB3_04_Restart/sqlite: resource checks 0 below 1
- TestTVB3_05_PostgresDown/postgres: resource checks 0 below 1
- TestTVB3_09_RevokeVisibleToCommit/postgres: resource checks 0 below 1
- TestTVB3_09_RevokeVisibleToCommit/sqlite: resource checks 0 below 1
- TestTVB3_10_EpochRollback/postgres: resource checks 0 below 1
- TestTVB3_10_EpochRollback/sqlite: resource checks 0 below 1
- TestTVB4_03_ResourceRegression/postgres: resource checks 0 below 4
- TestTVB4_03_ResourceRegression/sqlite: resource checks 0 below 4
- TestTVB4_04_NoBusinessDependency/postgres: resource checks 0 below 1
- TestTVB4_04_NoBusinessDependency/sqlite: resource checks 0 below 1
- TestTVB4_06_Load/postgres: resource checks 0 below 6
- TestTVB4_06_Load/sqlite: resource checks 0 below 6

## Limitations

- Single machine loopback topology of B4 §4 (GitHub-hosted ubuntu-24.04 runner); not the 1C1G target environment (G5) and not WAN latency
- Test configuration of B4 §3 (shortened T_close, heartbeat, license and idle values); values are not SLOs or capacity commitments
- Observations O1-O4 are recorded without pass lines except zero-tolerance events (B4 §6); readiness is always unknown in P1 (no probes, G2)
- This rerun preserves the P1 scope; it does not independently establish G4 backup/restore/migration, HA (G6), or real consumer/provider compatibility (G7).
- Synthetic deterministic HTTP fixtures T1-T3; two OICs are logically separate accesses on one host, not host isolation
