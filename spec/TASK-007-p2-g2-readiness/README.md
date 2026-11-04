# TASK-007：P2/G2 观测、准入与导出

**入口现状**：本工作区已合入 alpha `570b573f8151466fd0ad42b4cdf94ac3c81d323f` 的产品与证据，G1/G4 已恢复 Passed，P2 实施前置成立。G2 仍未实现、未预注册或运行；本次仅整理合并工作区，尚未提交或推送。

状态：用户已准确采用 Commit-Snapshot、Probe-HTTP、Export-Pull，DISC-009-R1/R2/R3 均 Accepted；[唯一原始确认](approval.json)绑定已发布固定规格，解释见[ADR-005](../../docs/decisions/ADR-005-g2-adoption.md)。G1/G4 前置已恢复；尚未实施、未预注册或运行 G2。

## 基线、授权与唯一规格

- 分支：`feature/TASK-007-p2-g2-readiness`；原规格基线为 `b50c08e228aa1c01686b35a59421ad2670154219`。本次从已发布 G2 提交 `9aa01b7c67d93eb0a76f66f7f788e9a7f689fc78` 合入实际 `origin/alpha` 的 `570b573f8151466fd0ad42b4cdf94ac3c81d323f`，已包含 TASK-005 最终实现；合并尚未提交，不改原规格来源版本。
- 原始任务来自本会话用户2026-11-04的交接文本，[原话与SHA-256](../../docs/changes/DISC-009.md)可追溯；要求G2先收束具体规格并提交新增取舍供确认。本任务先完成待确认规格包及来源/范围/索引。G3 按已采用规格先行，纯设计不等待 G4；产品实施前需核对实际采用、G1 及影响到的回归条件。
- **唯一详细契约**：[G2 观测规格](../../docs/architecture/p2/g2-observation.md)。本任务不复制字段、算法和预算；准确来源及差异在 [DISC-009](../../docs/changes/DISC-009.md)，机器范围见 [JSON](../../docs/changes/DISC-009.json)。
- 已采用的 C1、B1/B2/B4、95%有效单测和无业务重放保持；[ADR-003](../../docs/decisions/ADR-003-first-release-boundaries.md)、[ADR-004](../../docs/decisions/ADR-004-p1-adoption.md) 不改。候选配置/预算不是现有生产 SLO。

## 已采用的三组新增决定

| 需求 | 推荐及需要接受的代价 | 实质替代 | 准确规格 |
| --- | --- | --- | --- |
| DISC-009-R1 | **Commit-Snapshot**：以 OIC 最终检查固定本次观测决定，OIS 保留授权与 Channel 提交权。接受检查后至 READY 的新健康变化窗口；观测配置本地取消若先发生，保证 OIC 零载荷，但消费者可能先 READY 后 ABORT。新增控制 v2、错误传播及兼容范围 | **Commit-Strict**：若不接受窗口，继续设计更强语义；加查询/屏障不等于已有可行协议，此时只暂缓依赖该选择的实施 | [消息](../../docs/architecture/p2/g2-observation.md#wire)、[最终复核](../../docs/architecture/p2/g2-observation.md#commit) |
| DISC-009-R2 | **Probe-HTTP**：单一、本地允许的 HTTP(S) 健康状态检查；独立 observation_revision；probe 改动失效样本/check/Preparing，已 Open 长流保持。首版不覆盖所有非 HTTP/复杂鉴权健康接口 | **Probe-Report**：提供方主动报告，另定义认证写入口、来源/重放/冲突/失效；尚不能直接实现为替代 | [来源与代数](../../docs/architecture/p2/g2-observation.md#probe-http) |
| DISC-009-R3 | **Export-Pull**：独立有界的最新状态发布，默认关闭的回环指标拉取，有限标签；可合并中间事件、不保证完整历史。冻结候选负载/预算后预注册验证 | 主动导出需目的地/凭据/队列/重试契约；可靠历史需扩大记录责任；只做本地诊断则不能宣称满足包含 exporter 的 LAB-005 | [导出](../../docs/architecture/p2/g2-observation.md#export-pull)、[预算与验收](../../docs/architecture/p2/g2-observation.md#acceptance) |

用户已分别选择表中三个推荐，固定规格的字段/参数和代价一并采用；原问题和答案见approval.json，不再重问C1或同一三组选择。R1依赖R2，R3依赖R1/R2；采用记录仅解锁准确设计范围；当前G1/G4前置已恢复，实施仍须保持适用回归与产品证据要求。

## 后续实施与验证计划

| 阶段 | 输入/交付 | 当前状态 |
| --- | --- | --- |
| D0 规格准备 | 独立来源、三组选择、sdd-scope 指纹、既有正文仅加待确认导航 | 内容审核、提案发布回读和三组采用完成；采用回应I18-P04/I3-P06/I11-P06已发布并逐字节回读 |
| D1 准确采用 | 原始人类回复/明确指令，选项与范围；必要时修订候选后绑定指纹 | H16原始答复绑定六个原指纹，采用记录/兼容修复10文件远端回读完成；本工作区已集成最终alpha，G1/G4前置恢复 |
| M1 观测源与代数 | Probe-HTTP、独立观测版本、TTL/乱序/失效、受控配置 | Planned，依赖 R2 |
| M2 控制与最终复核 | v2、preflight/final、错误/幂等/回收、旧版矩阵、消费者展示 | Planned，依赖 R1/R2 |
| M3 独立发布与导出 | 有界调度/控制公平、目录挑战查询、Export-Pull、有限指标 | Planned，依赖 R3 及前置 |
| M4 产品证据 | G2-01–14 与 TV-B1-19；两后端、G1适用回归、>=95%有效单测、race/静态/安全；manifest先于run | 未预注册、未运行 |
| M5 闭环 | raw复算、独立评审、文档代码一致性、综合分析、图/产品文档/Issue/ITER/gate 联动 | 未开始；不得用记录校验替代产品通过 |

本地纯代数测试、夹具设计和既有范围内的资源审查可独立推进；本任务当前完成准确采用，产品实施尚未开始。没有新增 G2 run 或 ITER；仅同步 G2 当前阻挡说明，不修改 G1/G4 验证对象或旧证据。

## 本轮检查点

- [x] 初建工作树时核对原规格基线，当时未引入 TASK-005；本次实际集成见下方检查点。
- [x] 独立观测版本保持 B2 target revision 及已 Open 长流边界。
- [x] A1 观测固定点、异步健康更新、配置取消与晚到 READY 的竞态写入具体契约。
- [x] 完成内容审核与相关Issue阶段发布：固定规格提交`deff207de157b1bf40130494d47a85c4e8bfacce`的16文件逐字节回读；[I18-P03](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6624265781)、[I3-P05](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6624268297)、[I11-P05](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6624270544)发布后逐字节回读，旧正文/元数据/评论保持，详见[回执](../../docs/issue-history/2026-11-04/TASK-007-g2-proposal-published.json)。
- [x] 用户逐项准确采用三组新契约，完整问答、实际tool call/item来源及原规格指纹见[approval.json](approval.json)；不再重复确认。
- [x] 采用记录与D2兼容计划已由`bf51b38146e8dee48f47e742c4e5116e773ff0bf`提交、推送并回读22个变更文件，字节一致。
- [x] 固定提交`4f12fea156bfdb7c97ba3d3d9f4a91a2a8970a2a`的10个变更文件远端逐字节回读完成；[CI 41789106132](https://github.com/wyw-ai/loom-oi/actions/runs/41789106132)在本次回读时仍为in_progress，尚无成功结论。
- [x] 采用回应[I18-P04](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6624784353)、[I3-P06](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6624787385)、[I11-P06](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6624790959)已发布并逐字节回读；回执正文12个唯一链接核验通过，3单原正文/元数据及20条旧评论保持，详见[采用回应回执](../../docs/issue-history/2026-11-04/TASK-007-g2-adoption-published.json)。
- [x] TASK-005最终alpha已纳入本工作区；G1/G4通过仍绑定2ec3670及其正式run，P2入口恢复。G2实施与正式验证尚未开始。

G5/G7真实资源由[#11 I11-P07](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6626516421)继续承接。用户已提供SSH可达候选主机、允许随时使用，并明确经OIC暴露现有SSH/Bash能力；无需重复询问时段或首组任务。资源预算、原生身份与真实样本边界仍需独立预注册和验证：当前候选主机不等于整机1C1G，不能据此宣告G5/G7通过或扩大支持矩阵。仓库不记录主机地址、账号或凭据。

本轮自动记录检查与限制见 [DISC-009](../../docs/changes/DISC-009.md#validation)。

## D2：准确问答来源的记录兼容修复（文档先行）

发现时`tools/check_sdd.py`只接受GitHub原评论或带`chat-request` UUID的会话指令。本次真实采用来自`request_user_input_async`的用户逐项答复，只有实际tool call id和questionItemId，没有请求UUID。不得伪造UUID，也不得借AI发布评论替代用户来源；先提交、推送并回读本补充规格，再修改记录器。

修正保持schema 2、唯一`confirmation_ref`、需求指纹及旧确认路径：新增明确的`source.kind=user_response`，locator为`tool-call:<实际call id>`；approval的`items`保存原questionItemId、完整question、原answer及准确需求scope。检查器要求真实结构与非空问题/答案、非负整数item序号、item内call id与locator一致、item唯一、scope合法且当前需求由明确item覆盖；缺失items、仅有AI quote、重复item、错call或未覆盖需求都拒绝。工具只检查记录关联，不能自行鉴别人类身份、推断自由文本是否同意或取代原话核对。

确定性回归覆盖合法三项采用、错call/item结构、空问答、重复item、scope遗漏或重复及指纹变化；原`user_instruction`/`github_comment`行为与旧Accepted对象保持。验收包括tools完整自有源码的有效单测覆盖率≥95%（统计包含未执行源码）、全tools回归、所有适用文档/SDD/编码检查和CI。六个G2规格片段字节不变，Pending→Accepted只来自已收到的准确用户答复；记录器修复不是新产品选择，不创建run/ITER或放行G1/G2。文档先行提交`bf51b38`时仅完成本段计划及采用文档，tools代码尚未修改。

随后完成`user_response_errors`及确定性反例，并经独立只读复核。39项tools单测通过；以coverage.py 7.17.1执行`coverage run --source=tools --omit='tools/test_*.py' -m unittest discover -s tools -p 'test_*.py'`，完整两个非测试源码共384条语句、5条未覆盖，覆盖率98.70%（`check_sdd.py`98.18%、`check_structure.py`99.39%），没有排除未执行自有源码。实际DISC-009 review/implement（R3连同R1/R2依赖）及旧DISC-007-R4检查通过；这仅证明来源结构、范围与指纹关联。工具不能自行证明外部答复身份或解释答案是否同意，仍以完整原问答核对为依据。修复已随`4f12fea156bfdb7c97ba3d3d9f4a91a2a8970a2a`提交、推送并完成10文件远端回读；三条Issue采用回应也已回读。CI 41789106132在本次记录时仍运行，不能把本地检查或发布完成写成CI/产品通过。

## alpha 570b573 集成检查点

2026-11-04，按本轮明确授权在本feature执行fetch及不提交的merge：G2原HEAD为`9aa01b7c67d93eb0a76f66f7f788e9a7f689fc78`，实际`origin/alpha`为`570b573f8151466fd0ad42b4cdf94ac3c81d323f`。TASK-005的[CI 41802573081](https://github.com/wyw-ai/loom-oi/actions/runs/41802573081)六项成功及alpha快进已由主流程核验；main保持不变。本工作区解决10个文档冲突，保留两侧目录、谱系、发布回执及工具改动；目录登记为70项。当前合并未提交、未推送、未产生本次远端回读。

G1/G4完整validation、historical_validation和publication仍采用alpha原对象，主证据仍绑定`2ec36709471fe75c0e194c0ddf8f7699b7939fe2`；未把合并版本写入旧run。六个G2冻结片段、全部Accepted需求、approval和旧发布回执保持。冻结g2-commit中的原基线说明不修改；后续产品实施沿用TASK-005已有维护/存储拒绝新Open边界，接线与错误次序在实施一致性表核对，若需实质改变再确认准确差异。

P2入口检查已通过（退出0），P3仍仅因G2/G3未完成而阻挡（退出2）；T02/T03由资源前置Blocked改为Planned，V03产品验证保持Blocked。M1–M5仍按上表实际进度，不以本次文档/tools集成替代产品实现、预注册或运行。

实际集成检查：结构/链接/生成目录、DISC-001–009 review、DISC-009-R3及其依赖和DISC-007-R4 implement、39项tools、84项experiments回归、12个run/7个ITER完整性及物料检查均通过。两个最新G1/G4主run均以`verify.py --current-source`精确复算通过。111个产品文件、178个证据/图/归档文件与最终alpha字节一致；G1/G4两个完整gate对象、各116项source-manifest和9个validation绑定产物均一致。13个既有Accepted需求对象、六个G2片段、approval、两份G2回执和两份G2工具原文件保持；没有刷新Accepted。只重算实际变化的Pending指纹，所有受改文档为UTF-8无BOM、LF，`git diff --check`通过。这些检查不产生新产品样本，也不替代后续实现后的质量与门槛验证。
