# Issue阶段原文快照

本目录保存**实际读到的平台文字**，用于补足Git不会自动保存Issue正文历史的缺口；它是证据，不是当前方案或复制文档的archive。维护规则见[只追加规范](../issue-management.md#append-only)，阶段关系与当前阅读入口见[问题与方案演进记录](../../kb/lineage.md)。

初始仅建立#14/#15基线；后续处理按实际读取扩展，不宣称补齐整个仓库历史。阶段编号从可核实记录起编，是当轮追溯标识，不代表此前没有讨论。

2026-11-04轻量规范与收束核查见[DISC-005-r5-review.json](2026-11-04/DISC-005-r5-review.json)：保存15个Issue的写前状态/正文与评论哈希，以及#8/#14本次实际读到的完整文本快照。其余历史正文不伪装为已全量归档；独立规则任务的合成草稿明确不作产品需求。发布后的新增原文与状态核对另存回执，不覆盖此记录。

| 快照 | 实际来源与保真范围 | 限制 |
| --- | --- | --- |
| [I15-P01](2026-11-03/I15-P01-observed.json) | 上轮DISC-004处理前留存的GitHub API正文及1条评论；旧标题为“确认OIS首版控制与中继的进程边界” | 原观察精确时间未留存，不是创建时正文；不以当前设计文档补造平台原文 |
| [I15-P02](2026-11-03/I15-P02-observed.json) | 本轮实际读到的整体通道正文及2条评论；对应上轮885cb7a交付后状态 | 保存至本次读取为止，不能反推其他未捕获编辑 |
| [I14-P01](2026-11-03/I14-P01-observed.json) | 本轮实际读到的治理正文及4条评论，含另一个未合并开发流程分支的原链接 | 不把跨分支同名DISC或历史测试计为本轮结果 |

每份JSON保存来源、平台URL/作者/时间、原始正文与评论，以及UTF-8文本SHA-256。平台时间保持原ISO时区；本轮观察日期按Asia/Hong_Kong记录，未精确记录的时间写null/说明，不伪造。快照内动态alpha链接保留原样；真正用于阶段比较的规格版本另在演进记录中固定到commit。

已提交快照不修改、不覆盖、更不重建“更准确”的旧原文。出现新阶段或来源纠错时新增文件并引用前序；可用哈希核对读取保真，但哈希不能证明作者授权、论证正确、未缺历史或远端永远不可变。当前没有后台抓取/防篡改服务。

## DISC-005发布回执（追加）

[实际评论与文本哈希](2026-11-03/DISC-005-published.json)保存[I14-P02](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6607872608)和[I15-P03](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6607873200)的发布后回读结果，固定阶段规格提交`ecb4c707105d8af33caafcb3493527a0bc77d6d4`。15个既有Issue的标题/正文/状态/成员字段保持；#14原4条、#15原2条评论逐条原文保持。本回执不重复复制实时方案，也不声称平台以后不能编辑。

DISC-005 r2的[实际澄清评论与回读结果](2026-11-03/DISC-005-r2-published.json)保留此前不准确命名的更正；既有快照不回改。

[DISC-005 r3发布原文与回读](2026-11-03/DISC-005-r3-published.json)保存防重复循环与图谱候选的完整讨论；旧阶段快照不改。

[DISC-005 r5发布原文与状态回读](2026-11-04/DISC-005-r5-published.json)记录7条原单阶段回应、汇总#17及其交付回应、PR #16撤回。15个旧Issue原文/成员字段与47条旧评论保持，仅授权关闭#8/#14；写前记录不覆盖。

[DISC-006来源](2026-11-04/DISC-006-source.json)保存#17初始三项建议、H15明确回复及当次相关Issue评论哈希；准确采用限ADR-003，原Issue文字不覆盖。

[DISC-006发布回执](2026-11-04/DISC-006-published.json)保存H15后5条实际阶段回应、39个文件回读及本轮检查结果；16单元数据与6单23条旧评论保持，无状态变更。来源快照不覆盖。

[DISC-007发布回执](2026-11-04/DISC-007-published.json)保存新决策单[#18](https://github.com/wyw-ai/loom-oi/issues/18)的正文与5条阶段回应（I3-P03、I4-P03、I11-P03、I15-P06、I17-P03）的实际原文与哈希，以及推送后39个文件的远端回读和检查输出；16个既有Issue元数据与5单24条旧评论保持，无状态变更。

2026-11-04：[DISC-008原始来源](2026-11-04/DISC-008-source.json)保存本次读取的#18原稿、实际评论列表及会话原指令；AI后续阶段回应不当作人类在GitHub的新回复。

[DISC-008发布回执](2026-11-04/DISC-008-published.json)保存[I18-P01](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6615315184)的实际原文与哈希、G0最终21份增量文件回读和alpha集成。#18原正文/状态/成员保持；会话采用的AI转录不冒充GitHub人类新批准。

2026-11-04：[TASK-004发布回执](2026-11-04/TASK-004-g1-published.json)保存[I18-P02](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6620751696)、[I3-P04](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6620756157)、[I4-P04](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6620758552)、[I11-P04](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6620762305)的实际原文、哈希与22个固定链接回读；4单标题/正文/状态/成员与20条旧评论保持，无状态变更。G1在此回读之后单独记录。

2026-11-04：[TASK-007 G2提案发布回执](2026-11-04/TASK-007-g2-proposal-published.json)保存[I18-P03](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6624265781)、[I3-P05](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6624268297)、[I11-P05](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6624270544)的实际原文、哈希和发布回读；固定规格提交`deff207de157b1bf40130494d47a85c4e8bfacce`的16个变更文件逐字节一致，7个唯一固定链接在发布前后核验成功。3单标题/正文/状态/成员与17条旧评论保持。三组新增契约仍Pending，尚待用户准确采用；这是提案交付回执，不是人类采用、产品run、ITER或G2通过证据。

2026-11-04：[TASK-007 G2采用回应回执](2026-11-04/TASK-007-g2-adoption-published.json)保存[I18-P04](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6624784353)、[I3-P06](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6624787385)、[I11-P06](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6624790959)的实际原文与哈希，publish_stage逐字节回读通过；3单标题/正文/状态/成员及20条旧评论保持。固定提交`4f12fea156bfdb7c97ba3d3d9f4a91a2a8970a2a`的10个变更文件远端字节一致，回执正文12个唯一GitHub链接核验通过。准确用户采用仍以TASK-007的原始问答凭据为准，AI阶段评论不替代用户来源；六个冻结规格片段和旧回执不改。#11承接G5/G7真实资源条件，资源答复尚未收到；G1仍Inconclusive，不由采用回读放行G2。

2026-11-04：[TASK-005发布回执](2026-11-04/TASK-005-g4-published.json)保存I3-P07、I4-P05、I11-P07、I18-P05的实际原文与哈希、26个固定链接核验及证据提交49文件逐字回读；四单正文/标题/状态/成员与30条旧评论保持。ITER-005/006/007据此闭环，G1/G4在回读后单独记录；尚待最终六项CI与alpha快进。

## alpha 570b573 集成检查点

2026-11-04，TASK-007工作区已合入最终alpha `570b573`的TASK-005发布闭环，合并待提交。本页两侧历史发布入口均保留，各回执JSON与正文原字节不变；上文“当时G1待恢复/资源待答复”不改成后来的状态。当前G1/G4已Passed，G2准确采用但尚未实施，用户已提供SSH/Bash候选资源；最新任务进度见[TASK-007](../../spec/TASK-007-p2-g2-readiness/README.md)，资源承接见[#11 I11-P07](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6626516421)。本次未发布新的Issue评论或回执。

2026-11-04：[TASK-006 D20复证回执](2026-11-04/TASK-006-d20-revalidation-published.json)保存I3-P09、I4-P07、I11-P09、I18-P07的真实原文与哈希；四单元数据和38条旧评论保持，11个固定链接已解析，证据提交46文件远端逐字回读。ITER-010/011据此闭环后恢复G1/G4；最终CI及alpha集成另行记录。

2026-11-04的15deeab集成检查点（历史）：TASK-006的15deeab六项CI全绿并快进alpha；TASK-007已整合其源码和ITER-008至011/全部原始run、回执。新G2源码的G1/G4适用性仍Inconclusive，15deeab独立首次正式G3已失败，原件及后续修正另行闭环；上文旧检查点保持当时时态，不新增或重复计样本。

2026-11-04：[TASK-006首轮G3失败回执](2026-11-04/TASK-006-g3-first-published.json)保存I3-P10、I11-P10、I15-P07原文与摘要；三单31条旧评论和元数据保持，9个固定链接可解析，证据提交f568bde的39文件远端逐字回读。ITER-012闭环完成，G3仍Failed，D21修正后须新正式run。

- 2026-11-04：[TASK-006 G3正式复验发布回执](2026-11-04/TASK-006-g3-published.json)：I3-P11/I11-P11/I15-P08逐字回读，34旧评论及元数据保持，9固定链接核验；C55/ITER-013闭环。

TASK-007当前集成d8bb599：上述G3两轮及三单回执、原文/摘要和旧评论保持，019139f的历史Passed不改。当前G2源码的G1/G3/G4仍为Inconclusive，旧黑洞向量v2预检适配、PG关闭锁交接及最终同源正式复证由[TASK-007检查点](../../spec/TASK-007-p2-g2-readiness/README.md)承接；本次集成本身不新增Issue发布或实验样本。


2026-11-05：[TASK-007 本地执行方向回执](2026-11-05/TASK-007-local-execution-published.json)保存 I11-P12 / I18-P08 原文及逐字回读，21条旧评论与两单元数据保持，6个链接逐项解析。用户明确转本地继续，旧 Actions 未启动及开发失败保持；无新增正式run/ITER、无门槛晋级或alpha集成。

2026-11-05：[TASK-007 本地实现检查点回执](2026-11-05/TASK-007-local-implementation-checkpoint-published.json)保存 [I11-P13](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6648622242) / [I18-P09](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6648624757) 原文与逐字回读；两单元数据和23条旧评论保持，6个固定链接发布前后核验、3个固定文件与 `ba3287c` Git blob 字节一致。准确记录活动快照、writer终态、开发质量及真实DEV smoke的verify次序失败；没有新正式run/ITER、门槛晋级或alpha合入。
