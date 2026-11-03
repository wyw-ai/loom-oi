# DISC-007：P1实施规格包（B1–B4）与Windows前置修复

**后续状态（2026-11-04）**：B1–B4及R6承接范围已按[ADR-004](../decisions/ADR-004-p1-adoption.md)采用，B4恢复95%要求；G0以门槛台账为准。下列原轮次分析/发布记录保留当时状态，不能再将其中“等待#18回复”当作当前阻塞。

2026-11-04 · alpha · 修订1 · 基线`ca0dae609b1c029e673d28a001bb920b4d3bd4ef` · 确认入口：[#18](https://github.com/wyw-ai/loom-oi/issues/18)；#3/#4/#11/#15/#17追加去向说明。不创建探索分支，不合并main，不开始产品编码。

## 来源与范围

W27：本会话`7f8a3c59-64b2-4ffb-93bc-d8e4548e20c2`（2026-11-04，+08:00）的三段用户原话：

1. 03:12：“分步骤启动：先阅读 alpha 分支 AGENTS.md 及关联文档、KB、SDD和最新Issue；再汇报项目理解、已定/待定事项、开发门槛及首个任务建议，引用关键依据。汇报后等待我确认，当前不修改文件、不编码；后续获明确授权后，再遵循仓库规范与门槛进入 Autopilot 开发。”
2. 汇报后的选项：“授权启动：会话分支重置到 origin/alpha，完成后直接推送 alpha，沿用以往做法”。
3. 03:30：“收束并补齐现有 B1–B4 规格，逐项关联已确认决策、待确认差异、测试向量及已有观察项；形成一次清晰的整体确认材料。Windows 兼容问题明确列为前置小修复。确认并核验 G0 后，再启动 P1 实现。”

W27授权：规格与文档收束、F0工具修复、发布整体确认材料、直接推送alpha。**不授权**：开始产品编码、记录G0通过、改变已采用决定或代开发者确认。

其他来源：H15（[#17原回复](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6610997252)）及既有H8/H9/H10/H12/H13；[开发准备度评估](../evidence/implementation-readiness-004.md)定义的B1–B4缺口（E59）；本轮读取的alpha `ca0dae6`架构、模块、ADR、门槛、KB与#3/#4/#11/#13/#15/#17全部正文和评论；版本依据E61–E63（[证据台账](../../kb/evidence.md)）。最新开发者回复仍是H15，之后没有新的确认。

## 本轮求解与需求

唯一正文为[P1规格包](../architecture/p1/README.md)。四项各自成文、各自指纹，使部分确认不会锁住其他部分。

| 需求 | 范围 | 状态 | 规格 | 验证 |
| --- | --- | --- | --- | --- |
| DISC-007-R1 | B1：三平面v1线格式、READY提交点、帧规则、Channel状态机、错误码、幂等 | 待确认 | [b1-protocol.md](../architecture/p1/b1-protocol.md) | V01、V02；产品V03受阻 |
| DISC-007-R2 | B2：身份与证书映射、授权链、一次性票据、命名空间与revision、目标安全、许可与失联；依赖R1 | 待确认 | [b2-security.md](../architecture/p1/b2-security.md) | 同上 |
| DISC-007-R3 | B3：共同存储契约、最小模式、后端设置、单实例、epoch、存储不可用、迁移命令；备份恢复为P2契约 | 待确认 | [b3-storage.md](../architecture/p1/b3-storage.md) | 同上 |
| DISC-007-R4 | B4：版本锁定、初始测试配置、拓扑与范围、P1完成判定与G1映射、CI、观察记录；依赖R1–R3 | 待确认 | [b4-baseline.md](../architecture/p1/b4-baseline.md) | 同上 |
| DISC-007-R5 | F0：检查器在Windows的编码、路径与换行可移植性 | W27授权已完成；记录供核对，不请求产品决定 | [F0说明](../architecture/p1/README.md#f0) | V04 |

R1–R3承接[DISC-001](DISC-001.md#requirements)-R6的剩余详细契约：R6的规格指纹由共同架构与模块页改指b1–b3，R6仍为待确认，随R1–R3的确认一起收束。DISC-006-R2提出的“建议规格包”由本轮交付，其历史记录不改写。

## 项目收束

| 项目承诺与共同契约 | 本轮保持/改变 | 依赖及产物 | 验证/未证限制 |
| --- | --- | --- | --- |
| 接对获准目标、授权连续 | 保持；把绑定、票据、唯一提交点与撤销线性化写成可测规则（B1§6、B2§3） | #3；B1、B2 | 尚无实现；TV-B1-11/12、TV-B2-03/04/05待P1 |
| 失败可解释、资源有界 | 保持；错误码与ABORT语义、背压与初始上界（B1§5/§7、B4§3） | B1、B4 | 上界未实测，O3记录 |
| 业务状态外置、运行状态可重建 | 保持；持久/临时边界、epoch与重启行为（B3§2/§6） | #4；B3 | 备份、恢复、迁移留G4 |
| 可维护的首版部署 | 在C2范围内具体化；存储不可用时fail-closed为新取舍，待确认（D3-e） | B3、B4 | 单实例，不涉及HA |
| 路径与部署演进 | R为唯一实现路径；部署A只用于P1（D4-c） | #15；B4 | H研究不受影响，O3写“不适用/未测” |

共同架构正文只加指向P1规格包的说明，概念与跨模块边界不变。图1–11不重绘：拓扑与职责未变；图2的“两腿配齐→复核→ChannelReady”在B1中以READY帧实现，抽象层一致，精确转移以B1§6的表为唯一来源。

## F0前置修复

- 现象：中文Windows（cp936、未设`PYTHONUTF8`）上，工具测试1个错误、实验测试10个错误，5个检查器中4个失败；另外`autocrlf`把工作区文件转成CRLF，使SHA-256指纹与run摘要失配。
- 修复（提交`e68d0da`）：检查器显式UTF-8读写、生成目录固定LF；`check_structure`与`check_gates`的路径键改用`as_posix()`；新增`.gitattributes`；新增`tools/test_portability.py`；同步刷新DISC-001-R4、DISC-002-R1的待确认指纹（两者都没有确认记录，范围不变）。
- 验证F0时发现实验覆盖测试的夹具只复制ITER-001的分析文件，在任何系统都报错；以单独提交`3f98fd0`修复。
- 未改受哈希约束的实验源（`run.py`、`analyze.py`、`cases/`）。`run.py`在Windows新建run时仍写反斜杠路径键，留待下次修订实验工具时按`experiments/sources/vN`快照规则处理。

## 产物与指纹维护

同步：共同架构（头部、§5.1、§5.2、§8.1、§11的指向说明）、OIC/OIS/消费接入模块页、文档导航与目录、Issue导航、变更索引、KB（W27、C34、E61–E63、演进节点）、G0阻塞说明与`gates.json`（G0仍为受阻）、产物联动页。

仅刷新**待确认**需求的指纹：DISC-001-R1/R4/R6（R6改指b1–b3）、DISC-002-R1、DISC-004-R2、DISC-006-R2。三份ADR与`vocabulary.json`受已采用需求约束，本轮未修改。

## 阶段与验证记录

发布前本地检查（Windows原生，cp936，未设`PYTHONUTF8`）：工具测试17项、实验测试28项全部通过；`check_structure`通过；DISC-001～007的review检查全部通过；`check_sdd DISC-007 --phase implement`按预期拒绝（R1–R5均待确认）；门槛台账有效，`check_gates --stage P1`返回2（G0受阻）；`check_iterations`无待分析run；`check_materials`通过。最新两次LAB-001/002 run在`--current-source`下核验通过，较早两次的分析器漂移为既有情况。没有新实验、新run或ITER。

实际发布（2026-11-04）：规格与记录推送为alpha [19bef43](https://github.com/wyw-ai/loom-oi/commit/19bef43e788df0e7689ed2b568465e832845aeb5)（`ca0dae6..19bef43`，含F0的两次提交），远端树193个文件与本地逐一一致。新建决策单[#18](https://github.com/wyw-ai/loom-oi/issues/18)作为唯一整体确认入口，正文回读逐字一致；阶段回应[I3-P03](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6614324363)、[I4-P03](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6614325932)、[I11-P03](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6614327477)、[I15-P06](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6614328969)、[I17-P03](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6614330414)回读逐字一致。16个既有Issue的标题、正文、状态、标签、成员与里程碑，以及5单24条旧评论保持不变；main未变；没有关闭或分派任何Issue。[发布回执](../issue-history/2026-11-04/DISC-007-published.json)保存原文、哈希与检查输出。

下一步：等待开发者在#18答复。G0仍受阻，P1未开始；收到确认后按[G0核验步骤](../architecture/p1/README.md#g0)处理。

## 修订2：原始会话采用

W28明确要求完成#18采用与G0核验。R1–R4和DISC-001-R6现为已采用，唯一确认记录见[approval.json](../../spec/TASK-003-autopilot-entry/approval.json)；B4按用户既定要求恢复95%。原阶段回执和原GitHub正文不改；本轮执行记录见[DISC-008](DISC-008.md)。R5只是F0已完成记录，不作为P1实施确认前置。
