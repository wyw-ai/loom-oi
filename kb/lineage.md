# 问题与方案演进记录

本页记录问题、候选方案和判断依据怎样随条件与证据变化；名称为项目内的直白说明。旧标题“经验血缘”是AI对用户类比的不准确命名，不表示用户要求某种图谱技术，也不表示所有条目都是已验证经验。旧阶段原文保留，含义更正见下文W21；完整定义见[规范](../docs/issue-management.md#lineage)。

本页索引“从什么前提得到什么方案、为何改变、什么条件下值得重试”，不再复制实时设计。节点和关系只追加；误记通过后续更正节点说明。当前阅读入口可以更新，但不能消除旧节点。规则见[演进记录规范](../docs/issue-management.md#lineage)，原文保真见[快照目录](../docs/issue-history/README.md)。

当前能力缺口与候选见[防重复及图谱设计](../docs/designs/issue-evolution.md)，属于待评审提案；本页继续保存实际阶段，不把关系表等同已实现的自动检索。

## 当前阅读入口

| 问题 | 当前可核实阶段与规格 | 确认边界 |
| --- | --- | --- |
| #15 通道主问题 | [I15-P03追溯说明](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6607873200)沿用[I15-P02原回应](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6605914076)及[固定通道方案](https://github.com/wyw-ai/loom-oi/blob/885cb7a50d3b9a8a1f20a445654e3c9039a1eedc/docs/designs/ois-control-relay.md)；现行入口仍在#15 | R来自H8；H/M/部署细节未采用，没有新实验 |
| #14 协作规范 | [I14-P03能力与候选](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6608809009)；[固定设计](https://github.com/wyw-ai/loom-oi/blob/326ba0469fe3fc9044aaa17bdd57bf4df6b8aeed/docs/designs/issue-evolution.md) | W20只追加保持；W21/W22的含义与方案讨论已落实；B原型及其他新技术细节待确认 |

## 已记录节点

| 节点 | 来源/阶段 | 当时判断与证据级别 | 范围/局限 |
| --- | --- | --- | --- |
| SRC-CH-01 | [原讨论定位](https://github.com/wyw-ai/loom-oi/blob/3d21e2f375c6ee87d3abe69402984755a9786952/docs/changes/DISC-003.md#sources) | 非中继会改变中心负载、通道可能拆进程；讨论意图/假设 | 无产品测量，缺图不补猜 |
| I15-P01 | [实际留存正文/评论](../docs/issue-history/2026-11-03/I15-P01-observed.json)，[原交付评论](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6603765526)，[固定r1方案](https://github.com/wyw-ai/loom-oi/blob/3d21e2f375c6ee87d3abe69402984755a9786952/docs/designs/ois-control-relay.md) | 先问A/B进程、D/H后置；AI提案与文档检查 | 不是已采用，也不是进程方案被实验反驳；原始创建快照未知 |
| SRC-W19 | 用户请求`da911c62-2921-4c84-afc6-6b651bd89f8b`，见[固定DISC-004](https://github.com/wyw-ai/loom-oi/blob/885cb7a50d3b9a8a1f20a445654e3c9039a1eedc/docs/changes/DISC-004.md#sources) | 指出方向2属于方向1研究，要求回到loom-oi主体 | 方法修正授权，不等于采用H/M |
| I15-P02 | [实际正文/评论快照](../docs/issue-history/2026-11-03/I15-P02-observed.json)，[原回应](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6605914076) | 完整通道问题：R基线＋H研究，路径→B_R/失败→部署；AI整合提案 | 替代旧确认次序；R原采用及新技术待确认范围分开 |
| I14-P01 | [治理正文/评论快照](../docs/issue-history/2026-11-03/I14-P01-observed.json)，[原回应](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6605914809) | 已增加整体收束规则，但旧Issue维护仍要求原位重写 | 本轮捕获基线，不称为#14最早阶段；feature工作流独立 |
| SRC-W20 | 用户请求`667d7fc5-0554-4e86-8400-367ff940821c` | 明确已发布Issue只追加，偏好评论/新单/子单，保留每阶段与经验血缘 | 工作要求即刻执行；细化机制由DISC-005承接，无产品采用变化 |

## 阶段关系

| 关系ID | 起点 → 终点 | 类型、准确范围与理由 |
| --- | --- | --- |
| EDGE-001 | SRC-CH-01 → I15-P01 | 来源：原讨论触发承载研究；整理却提前限定进程 |
| EDGE-002 | I15-P01 → I15-P02 | 修正/替代：替代A/B先行确认，保留有效部署反例；依据SRC-W19，不能解释为A/B不可行或H已采用 |
| EDGE-003 | SRC-W19 → I15-P02 | 来源：按整体目标重组同一主问题，未产生新P2P探索分支 |
| EDGE-004 | I15-P02 → I14-P01 | 经验提炼：整体理念/双向推导进入协作规则；规范采用不等于产品验证 |
| EDGE-005 | I14-P01 → SRC-W20 | 被纠正的规则：旧“当前正文原位更新”不满足阶段追踪；因果来自明确用户反馈，不仅是时间先后 |

## 再次探索前需要知道什么

| 经验ID / 范围 | 停止或修正的原因 | 何时有理由再试 / 当前未知 |
| --- | --- | --- |
| EXP-CH-01：先冻结进程再研究路径 | 路径会改变实际中继字节、端侧成本及可用性前提；旧确认次序遗漏这些变量，是分析缺陷而非性能实测失败 | 已固定具体拓扑/路径/负载与硬故障要求时，可以独立比较A/B/C；保留原场景和判定预算。未知的是实际产品收益，不永久否决拆进程 |
| EXP-GOV-01：每轮重写Issue保留最新答案 | 旧决策依据和探索阶段在平台阅读路径上被覆盖；Git并不天然留存其全文 | 当前采用阶段追加＋仓库现行规格；仅当用户以后明确改变该维护规则时再评估覆写，不能以“更整洁”自作例外 |

本轮记录从真实可取得阶段开始；尚未形成完整历史图谱，也未补造所有中间版本。暂缓/被否决/证据不足与证伪严格分开。不存在新条件但想换名重提时先复用这些结论；有合理复验目的可以重新设计实验，不能被经验条目永久封禁。

## DISC-005新增阶段与关系（追加）

| 节点 | 实际来源 | 本阶段结果与边界 |
| --- | --- | --- |
| I14-P02 | [发布评论](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6607872608)、[回读原文/哈希](../docs/issue-history/2026-11-03/DISC-005-published.json)、[固定规范](https://github.com/wyw-ai/loom-oi/blob/ecb4c707105d8af33caafcb3493527a0bc77d6d4/docs/issue-management.md#append-only) | 执行W20：旧正文/评论保留，新阶段追加；拆单/经验细则供核对，没有新产品采用 |
| I15-P03 | [发布评论](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6607873200)、同一回执 | 追加两阶段追溯、准确停止/重试条件；技术方案仍沿用I15-P02，没有新实验或采用 |

| 关系ID | 起点 → 终点 | 类型、范围与理由 |
| --- | --- | --- |
| EDGE-006 | SRC-W20 → I14-P02 | 来源：用户明确的只追加要求已经执行，不重新索取该权限 |
| EDGE-007 | I14-P01 → I14-P02 | 修正/替代：只替代旧Issue原位重写规则，保留中文化、SDD及未合并feature原范围 |
| EDGE-008 | I14-P02 → I15-P03 | 实际应用：同一问题通过追加保留经历，而非再改正文或拆成重复P2P单 |
| EDGE-009 | I15-P02 → I15-P03 | 沿用：技术候选/采用/验证边界不变，只补追溯和重试依据 |

发布后核对：原节点快照未变，15个旧Issue标题/正文/状态/成员字段均与写前一致，#14/#15六条旧评论未改。固定阶段提交为`ecb4c707105d8af33caafcb3493527a0bc77d6d4`，新回执记录的是当次真实读取，不保证永久平台状态。

## W21：需求含义与AI命名的更正（追加）

| 条目 | 来源与准确范围 | 对此前记录的更正 |
| --- | --- | --- |
| SRC-W21 | 用户请求`c982c208-4c39-4a20-971b-5fae9138a559`：“经验血缘 需要准确的描述，我只是做了需求描述并不是专业用语容易造成偏差。” | W20/SRC-W20中的“经验血缘”是AI的概括，不是用户指定的专业术语；用户目标是阶段可追溯、保留判断依据并避免无依据重复探索 |
| CORR-TERM-01 | 本轮AI表述修正；[DISC-005 r2](../docs/changes/DISC-005.md#terminology-correction) | 当前名称改为“问题与方案演进记录”，定义记录内容和使用边界；无新产品方案、实测或人类技术采用 |

| 关系ID | 起点 → 终点 | 类型、范围与理由 |
| --- | --- | --- |
| EDGE-010 | SRC-W21 → CORR-TERM-01 | 来源：用户要求准确描述需求，AI据此纠正命名及解释 |
| EDGE-011 | SRC-W20 → CORR-TERM-01 | 解释修正：只纠正将需求类比固化成概念的表述；只追加、阶段追踪和有依据重试的目标继续有效，不表示旧规则已全部获批 |

此前I14-P02、I15-P03及历史记录中的旧称按本说明理解，原文不改。#15的技术候选、确认边界和实验状态均未改变。本轮回复沿用I14-P02追加澄清，不为措辞修正强行新建问题或阶段编号；实际发布链接回读后记录。

CORR-TERM-01发布记录（追加）：[I14-P02补充澄清](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6608312250)已回读，固定修订为`d6e5f99e3bd34e3c51869d3b6e3266b0f980e8e6`；[实际评论原文/哈希](../docs/issue-history/2026-11-03/DISC-005-r2-published.json)。#14原正文/相关状态字段及5条旧评论未改，旧阶段记录不因新名称重写。当前名称与定义以W21更正为准，其他旧确认范围保持。

## W22：从保留历史展开到防重复机制（追加）

| 条目 | 来源/本阶段分析 | 范围与局限 |
| --- | --- | --- |
| SRC-W22 | 用户请求`d70fe7f3-6aba-49a7-889b-9a731fede41f`询问防重蹈覆辙等优化是否存在，并要求展开是否采用图谱技术 | 授权分析和方案讨论，不是确认某种技术实现 |
| PROP-EVO-01 | [DISC-005 r3](../docs/changes/DISC-005.md#evolution-design)及[能力设计](../docs/designs/issue-evolution.md) | 已有手工追溯/重试规则；推荐B结构化关系记录作为验证候选，A现有方法为对照，C专用图服务保留演进；未实现、未实测、待确认 |

| 关系ID | 起点 → 终点 | 类型、准确范围与理由 |
| --- | --- | --- |
| EDGE-012 | SRC-W22 → PROP-EVO-01 | 来源：展开防重复能力、正常/失败链与三种技术候选 |
| EDGE-013 | CORR-TERM-01 → PROP-EVO-01 | 补充/修正：保留W21的准确命名与原话边界；纠正r2从未指定技术直接推出表格足够、排除图数据库的过早结论 |

本轮将EXP-CH-01用于设计推演：同条件改名须复用旧缺口，关键路径/负载/故障条件改变可重新研究部署；没有新产品采用或实验。#14承接该治理方案，#15仅为已有案例来源。

## I14-P03发布记录（追加）

| 节点 | 实际来源 | 结果与边界 |
| --- | --- | --- |
| I14-P03 | [评论](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6608809009)、[原文与哈希](../docs/issue-history/2026-11-03/DISC-005-r3-published.json)、固定提交`326ba0469fe3fc9044aaa17bdd57bf4df6b8aeed` | 展开防重复循环及A/B/C取舍，推荐B原型供评审；没有新实现、实测或采用 |

| 关系ID | 起点 → 终点 | 类型、范围与理由 |
| --- | --- | --- |
| EDGE-014 | PROP-EVO-01 → I14-P03 | 发布：完整机制、反例、代价与确认范围已提交#14，提案不等于采用 |
| EDGE-015 | I14-P02 → I14-P03 | 沿用/补充：保留只追加；补充r2遗漏的技术选择讨论，不追改旧命名或历史原文 |

#14原正文/元数据及6条旧评论回读保持，新增评论正文逐字一致。当前B推荐仍可被维护成本、漏判/误拦和查询预算推翻，不将本轮方案变成永久规则。

## W25：从工具实现收回规范与针对性确认（追加）

| 节点 | 实际来源 | 结果与边界 |
| --- | --- | --- |
| IMPL-EVO-01 | [PR #16](https://github.com/wyw-ai/loom-oi/pull/16)、[I14-P04](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6610161668)、feature提交`d329a58849fd057e032600670ea35afa442292af` | AI曾交付Git阶段JSON和脚本；未合入alpha，代码验证不证明需求匹配 |
| SRC-W25 | 用户请求`59abbba0-ee87-43e3-baa8-cfc39f82d9bc`两项原话见DISC-005 r5 | 用户澄清只需门控/结构/流程及重复风险确认，并授权收束已定Issue |
| CORR-EVO-02 | [当前规则](../docs/issue-management.md#revisit-gate)、[#17汇总](https://github.com/wyw-ai/loom-oi/issues/17) | 撤回工具PR，采用现有文档/阶段/KB及针对性Issue确认；不声称实测证明流程效果 |
| I8-P01 | [收束评论](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6610554083)、[#11承接](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6610553366) | Go决策按H12关闭，验证仍未完成；不重开语言选择 |

| 关系ID | 起点 → 终点 | 类型、准确范围与理由 |
| --- | --- | --- |
| EDGE-016 | PROP-EVO-01 → IMPL-EVO-01 | 历史执行：原B候选被AI细化成工具，保留当时方案和验证，不表示满足用户真实需求 |
| EDGE-017 | SRC-W25 → CORR-EVO-02 | 来源：明确需要规范与有条件额外确认，不需要工具方案 |
| EDGE-018 | IMPL-EVO-01 → CORR-EVO-02 | 替代：当前执行方式收回文档流程；不是实验反驳所有图技术，不改历史正文 |
| EDGE-019 | H12 → I8-P01 | 收束依据：只有首版核心语言选择完成，剩余验证交#11 |

本轮新阶段发布后追加实际链接；#14原有阶段包括I14-P04完整保留，当前阅读入口为#17。关闭治理阶段不表示旧提案全部采用。

## W25实际收束与汇总交付（追加）

| 节点 | 实际来源 | 结果与边界 |
| --- | --- | --- |
| I14-P05 | [阶段回应](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6610765080)、[固定规范修订](https://github.com/wyw-ai/loom-oi/commit/982eafa1a54247015af8182277202f390985f7e9) | #14已关闭，当前规则取代工具方案；未整体采纳历史附加提案 |
| I17-P01 | [本轮汇总](https://github.com/wyw-ai/loom-oi/issues/17)、[交付回应](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6610840222) | 汇总剩余C1/C2/C3，回复按范围回原单；不是新的产品决定来源 |

| 关系ID | 起点 → 终点 | 类型、准确范围与理由 |
| --- | --- | --- |
| EDGE-020 | CORR-EVO-02 → I14-P05 | 完成：纠偏规则已发布，关闭本轮治理阶段；保留旧阶段原文 |
| EDGE-021 | I14-P05 → I17-P01 | 承接：本轮整理反馈移至#17；#3/#4/#11/#13/#15仍各自维护真实未决范围，不复制采用状态 |

[发布回读](../docs/issue-history/2026-11-04/DISC-005-r5-published.json)保存新增原文与核对结果：7条原单回应和#17交付评论逐字一致，15个既有Issue标题/正文/成员字段与47条旧评论保持；#8/#14关闭，PR #16关闭未合并，main不变。早先表格中的#14活动状态仅代表当时阶段，现行入口以本节与[当前Issue导航](../docs/alpha-issues.md)为准。没有新增产品实验或采用。

## H15：三项范围确认与整体效果观察（追加）

| 节点 | 来源 | 变化与限制 |
| --- | --- | --- |
| H15 | [原确认](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6610997252)、[原文快照](../docs/issue-history/2026-11-04/DISC-006-source.json) | C1/C2/C3按原建议并持续观察，非整份架构批准 |
| ASSESS-READY-01 | [DISC-006](../docs/changes/DISC-006.md)、[准备度与观察](../docs/evidence/implementation-readiness-004.md) | 三项方向不再待定；四组具体契约/验收缺口阻挡G0，实施后分阶段取证 |

| 关系ID | 起点 → 终点 | 类型、准确范围与理由 |
| --- | --- | --- |
| EDGE-022 | I17-P01 → H15 | 明确采用：只覆盖汇总中C1/C2/C3建议及观察要求，不扩大到旧全文 |
| EDGE-023 | H15 → ASSESS-READY-01 | 分析：去除过期方向阻塞，保留真实共同契约与验证缺口；采用不证明实验通过 |

本轮原单阶段与汇总回应实际发布后追加链接；原评论保留。观察是已采用方案的常规验证，不属于无依据重选；若将来拟覆盖原决定，按具体新证据触发额外确认。

H15落实回执（追加）：[I17-P02](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6611351677)回应原确认并给开发入口结论；[I3-P02](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6611348800)、[I4-P02](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6611349417)、[I15-P05](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6611351159)落实准确采用，[I11-P02](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6611350517)承接观察/条件。固定修订`2cd7296ae0efc5913c8f683c67a6fea041fc34c1`；[原文及保真回执](../docs/issue-history/2026-11-04/DISC-006-published.json)记录16单文字/状态/成员及本轮读取6单的23条旧评论保持。此为H15→ASSESS-READY-01的实际交付，未添加产品数据或放行G0。

## DISC-007：B1–B4规格包（追加）

| 节点 | 来源 | 结果与边界 |
| --- | --- | --- |
| SRC-W27 | 本会话三段用户原话，见[DISC-007来源](../docs/changes/DISC-007.md#来源与范围) | 授权规格收束、F0与发布确认材料；不授权编码或G0通过 |
| PROP-P1-SPEC | [P1规格包](../docs/architecture/p1/README.md)；固定提交在发布后追加 | B1–B4的具体规则、差异D1–D4、测试向量与观察映射；AI提案，待确认 |

| 关系ID | 起点 → 终点 | 类型、准确范围与理由 |
| --- | --- | --- |
| EDGE-024 | ASSESS-READY-01 → PROP-P1-SPEC | 承接：把评估中的四组缺口落实为可测规格；不改变H15的采用范围 |
| EDGE-025 | SRC-W27 → PROP-P1-SPEC | 来源：用户要求一次整体确认材料，并把Windows修复列为前置项 |
| EDGE-026 | PROP-P1-SPEC → DISC-001-R6 | 收束：R6剩余详细契约迁至b1–b3，仍待确认，不重开R5或C1 |

重试条件：若开发者否决某项差异（如D3-e），只修订对应文件并重新提交该项；其余已确认项的指纹不受影响。

DISC-007发布记录（追加）：规格固定于`19bef43e788df0e7689ed2b568465e832845aeb5`；整体确认入口为新决策单[#18](https://github.com/wyw-ai/loom-oi/issues/18)（初始快照即本阶段节点I18-P00）；[I3-P03](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6614324363)、[I4-P03](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6614325932)、[I11-P03](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6614327477)、[I15-P06](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6614328969)、[I17-P03](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6614330414)只追加去向。[原文与保真回执](../docs/issue-history/2026-11-04/DISC-007-published.json)记录16单元数据与24条旧评论保持。此为PROP-P1-SPEC的实际交付，未添加产品数据，也未放行G0。

## DISC-008：从规格提案进入已采用实施入口

| 节点/关系 | 来源 | 准确含义 |
| --- | --- | --- |
| DEC-P1-01 | [ADR-004](../docs/decisions/ADR-004-p1-adoption.md)、W28原会话指令 | 采用B1–B4与R6承接范围，80%建议改回95%；不外推H/HA或真实产品通过 |
| EDGE-027：DEC-P1-01 → PROP-P1-SPEC | 采用/更正 | 保留PROP-P1-SPEC与#18原稿作为提案阶段；当前采用以ADR-004准确范围为准 |
| GOV-AUTO-01 | [开发流程](../docs/sdd/development-workflow.md)、[TASK-003](../spec/TASK-003-autopilot-entry/README.md) | 修正开发入口分散、整文件/无关指纹和来源限制；保留只追加和阶段证据，不恢复旧PR工具化方向 |

DISC-008交付节点：[I18-P01](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6615315184)为DEC-P1-01与GOV-AUTO-01的实际结果回复；G0设计入口于[03e1ce5](https://github.com/wyw-ai/loom-oi/commit/03e1ce5b3b64c7b9231640ea50bc16e58a50cdb0)核验后集成alpha。[回执](../docs/issue-history/2026-11-04/DISC-008-published.json)保存逐字回读和原文；不把AI转录当新人类决议，不把设计放行当产品完成。

## TASK-004：P1实现与G1证据（追加）

| 节点 | 来源 | 内容与关系 |
| --- | --- | --- |
| IMPL-P1-01 | [TASK-004](../spec/TASK-004-p1-r-vertical/README.md)、`f844b29` | 按ADR-004采用的B1–B4实现OIS/OIC/oi与双后端存储；一致性表记录37项解释与13项偏差的处理 |
| RUN-G1-01 | [首run](../experiments/runs/G1-P1-20261104T002624Z-35ec8b/report.md) | 反例：许可被迟到heartbeat复活、完成后截断；修正后被RUN-G1-02取代，原数据保留 |
| RUN-G1-02 | [主run](../experiments/runs/G1-P1-20261104T014622Z-327df6/report.md) | G1实现级主证据 |
| ITER-004 | [综合分析](../docs/evidence/p1-g1-evidence-005.md) | C36–C38；G1在Issue回读后记为通过 |
| EDGE-028 | DEC-P1-01 → IMPL-P1-01 | 实施：只实现已采用范围，规格未改 |
| EDGE-029 | RUN-G1-01 → IMPL-P1-01 | 反证/修正：c6c639f、11e9f21修正实现以符合B2§5与B1§6，判定与预注册不变 |
| EDGE-030 | RUN-G1-02 → ITER-004 → G1 | 验证：实现级证据满足G1五项；不外推G2–G7 |

## TASK-007：G2提案与准确采用（追加）

| 节点/关系 | 来源 | 准确含义 |
| --- | --- | --- |
| PROP-G2-01 | 固定提案`deff207de157b1bf40130494d47a85c4e8bfacce`、[提案发布回执](../docs/issue-history/2026-11-04/TASK-007-g2-proposal-published.json) | I18-P03/I3-P05/I11-P05提交三组具体选择；发布时Pending的原话保留 |
| DEC-G2-01 | H16、[ADR-005](../docs/decisions/ADR-005-g2-adoption.md)、[唯一确认凭据](../spec/TASK-007-p2-g2-readiness/approval.json) | 用户逐项采用Commit-Snapshot/Probe-HTTP/Export-Pull，绑定六个原片段；非产品证据 |
| EDGE-036：PROP-G2-01 → DEC-G2-01 | 采用 | 保留原提案及发布快照；只改变准确采用状态，不改规范字节、不重复C1决定 |

在上述采用记录形成时，G1资源补证仍是新实施前置，采用回应尚待发布回读；这些是当时阶段事实。后续回读与本次alpha集成见本页末尾检查点，当前仍没有TASK-007产品实现节点、run或ITER。

## TASK-005：P2恢复实现与G4证据（追加）

| 节点 | 来源 | 内容与关系 |
| --- | --- | --- |
| IMPL-P2-01 | [TASK-005](../spec/TASK-005-p2-g4-recovery/README.md)、`6c7d5ad25b164acbf862d739234ff0d3b730b7f5` | 按ADR-004采用的B3§8与共同架构§8.1.1实现双后端单实例恢复、维护及显式迁移；[一致性表](../spec/TASK-005-p2-g4-recovery/consistency.md)保留实现/工具偏差及修复，未新增HA或业务重放保证 |
| RUN-G4-01 | [主run](../experiments/runs/G4-P2-20261104T054837Z-00be90/report.md)、[Actions 41781459890](https://github.com/wyw-ai/loom-oi/actions/runs/41781459890) | 本轮唯一run，observed/pass；四项判定、75项必需父子检查与质量信号通过；E66为实现级证据，不存在另一个本轮失败run |
| ITER-005 | [综合分析](../docs/evidence/p2-g4-evidence-006.md) | C39–C43，区分恢复实测、确定性回归、实现解释、O2观察和统计夹具证据缺口；G1当前Inconclusive，G4仍Blocked |
| EDGE-031 | DEC-P1-01 → IMPL-P2-01 | 实施：承接原采用的P2恢复/迁移范围，不把G1实现证据扩大为G4或新产品采用 |
| EDGE-032 | IMPL-P2-01 → RUN-G4-01 | 验证输入：最终固定源码与运行前已提交的G4-P2预注册；真实二进制、双后端、tmpfs与PG工具均实际执行 |
| EDGE-033 | RUN-G4-01 → ITER-005 | 分析：原始核验与复算支持C39–C42；不因observed/pass提前将G4台账写为Passed |

本轮#3/#4/#11/#18纠正及结果阶段评论仍待完成；不预造阶段评论ID、发布链接或回读事实。历史RUN-G1-01/02、ITER-004及C36–C38原文保持，当前适用性受C43限制。后续源码、契约或环境改变影响对应恢复向量时，重新评估该范围并保留本run原始数据。

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| REVIEW-G1-01 | [TASK-005 D14](../spec/TASK-005-p2-g4-recovery/consistency.md)、[C43分析](../docs/evidence/p2-g4-evidence-006.md) | 发现既有统计与基线断言不足；不是新增run，也未证明产品泄漏 |
| EDGE-034 | RUN-G1-02 → REVIEW-G1-01 → G1 | 保留原始observed/pass及ITER-004，撤回当前资源完整性结论；G1降为Inconclusive，P2入口暂停 |
| EDGE-035 | REVIEW-G1-01 → ITER-005 → G4 | G4四项直接证据不删除；由于G1前置失效及Issue未闭环，G4保持Blocked |

当时未运行的G1-P1-R2随后取得首轮失败run，现按下表归属ITER-006；RUN-G1-01/02或RUN-G4-01不重复计作新增样本。

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G1-R2-01 | [首轮补证](../experiments/runs/G1-P1-R2-20261104T064821Z-0830c3/report.md)，b7af582 | E67，observed/fail；Output分片与FIN终态竞争分别记录，原始字节不改 |
| ITER-006 | [综合分析](../docs/evidence/p1-g1-resource-evidence-007.md) | C44/C45，pending-publication；一个失败run，内存诊断不是新增样本 |
| EDGE-037 | REVIEW-G1-01 → RUN-G1-R2-01 → ITER-006 | 验证/反例：严格资源补证在D16测量缺陷之外仍有D17产品断言失败，G1由Inconclusive降为Failed；EDGE-036由G2采用主线保留 |
| EDGE-038 | ITER-006 → TASK-005 D16/D17 → 新G1/G4证据 | 修正与重启条件：先守住原标准修测量和生产FIN语义，取得新完整run及闭环；旧G4仅保留原源码直接证据，不替代修复版本 |

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G4-02 | [续测失败run](../experiments/runs/G4-P2-20261104T072937Z-bebf95/report.md)，5668b53 | E68，observed/fail，11h/sqlite末尾FD未归位；不是原6c run改判 |
| RUN-G1-R2-02 | [续测通过run](../experiments/runs/G1-P1-R2-20261104T072920Z-9b6d51/report.md)，5668b53 | E69，observed/pass，五项判定与D16/D17正式支持；只属于ITER-006，不重复计入ITER-007 |
| ITER-007 | [综合分析](../docs/evidence/p2-g4-resource-evidence-008.md) | C46，新G4失败run独立分析；pending-publication，D18与最终复证未完成 |
| EDGE-039 | RUN-G4-02 → ITER-007 → TASK-005 D18 | 反例与修正：严格FD回归暴露同源存储资源问题，G4 Failed；不放宽基线和期限 |
| EDGE-040 | TASK-005 D16/D17 → RUN-G1-R2-02 → ITER-006/C47 | 验证：固定5668源码G1通过；同源资源反例与后续存储变更使最终适用性未闭合，当前G1 Inconclusive而非Passed |

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G4-03 | [修正主run](../experiments/runs/G4-P2-20261104T080816Z-097edc/report.md)，2ec3670 | E70，observed/pass，四项判定与Linux读池正负对照通过，原失败由同未发布ITER-007保留承接 |
| RUN-G1-R2-03 | [修正主run](../experiments/runs/G1-P1-R2-20261104T080816Z-0667d6/report.md)，2ec3670 | E71，observed/pass，五项原判定与全部严格资源/原负载通过，仅归ITER-006 |
| EDGE-041 | TASK-005 D18 → RUN-G4-03 → ITER-007/C48 | 验证：保持旧失败/原判线，新固定源码恢复直接四项支持；发布闭环前不晋级 |
| EDGE-042 | TASK-005 D16/D17/D18 → RUN-G1-R2-03 → ITER-006/C49 | 验证：最终共享源码G1通过；不将之前5668通过直接绑定到改后的存储实现 |

2026-11-04发布闭环：ITER-005/006/007的原输入和历史判断保持，I3-P07、I4-P05、I11-P07、I18-P05追加并逐字回读；26个固定链接解析、四单元数据及30条旧评论保持，实际原文见[TASK-005回执](../docs/issue-history/2026-11-04/TASK-005-g4-published.json)。C48/C49在回读后绑定2ec3670新主run恢复G4/G1；未新增样本或采用范围。

## alpha 570b573 集成检查点

2026-11-04，TASK-007工作区从已发布G2 `9aa01b7`合入最终alpha `570b573`，合并待提交。PROP-G2-01/DEC-G2-01/EDGE-036与TASK-005全部节点、反例和后续边同时保留；未重编号、删除旧run或增加产品样本。G2三条采用回应已发布回读，见[原回执](../docs/issue-history/2026-11-04/TASK-007-g2-adoption-published.json)；G1/G4已按2ec3670新主证据恢复Passed，P2入口通过。该集成仅恢复TASK-007实施前置，未形成G2产品实现节点、run或ITER，后续实施沿原六个采用片段与最终alpha既有边界推进。

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G1-R3-01 | [73bb667首轮G1](../experiments/runs/G1-P1-R3-20261104T103502Z-186529/report.md) | E72，observed/fail；新固定源码负载反例，不覆写2ec3670历史通过 |
| RUN-G4-R2-01 | [73bb667首轮G4](../experiments/runs/G4-P2-R2-20261104T103505Z-af5f96/report.md) | E73，observed/pass；独立原四项支持，不覆盖C50 |
| EDGE-043 | RUN-G1-R3-01 → ITER-008/C50 → TASK-006 D19 | 反例→诊断：FIN/首响应缺口未定因，保留2秒idle与原负载；不将Forward变更误作原因 |
| EDGE-044 | RUN-G4-R2-01 → ITER-009/C51 | 限定验证：共同G1未恢复，G4保持Inconclusive；两个新run各归一轮，旧ITER冻结 |

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G1-R4-01 | [bff7aa2新G1](../experiments/runs/G1-P1-R4-20261104T131129Z-686545/report.md) | E74 observed/pass；原失败与D19追踪不改 |
| RUN-G4-R3-01 | [bff7aa2新G4](../experiments/runs/G4-P2-R3-20261104T131132Z-cdbb34/report.md) | E75 observed/pass；不是新负载/容量样本 |
| EDGE-045 | TASK-006 D19→D20→RUN-G1-R4-01→ITER-010/C52 | 根据真实等待/分配反例保留原预算，修有界所有权并复证；不把候选原因提升为全部旧故障的唯一根因 |
| EDGE-046 | TASK-006 D20→RUN-G4-R3-01→ITER-011/C53 | 同源恢复复证并保留CI写fail-closed准备反例；发布闭环前不晋级 |

E74/E75的ITER-010/011四单发布回读已完成，38条旧评论/元数据保持；G1/G4恢复Passed。失败原件不改，仍不代表G2/G3/G5/G7通过。

2026-11-04的15deeab集成检查点（历史）：TASK-006的15deeab六项CI全绿并快进alpha；TASK-007已整合其源码和ITER-008至011/全部原始run、回执。新G2源码的G1/G4适用性仍Inconclusive，15deeab独立首次正式G3已失败，原件及后续修正另行闭环；上文旧检查点保持当时时态，不新增或重复计样本。

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G3-P2-01 | [15deeab首轮G3](../experiments/runs/G3-P2-20261104T141622Z-01edf5/report.md) | E76 observed/fail；完整Go与资源通过不覆盖原始行为失败 |
| EDGE-047 | RUN-G3-P2-01 → ITER-012/C54 → TASK-006 D21 | 反例→仪器分类修正：辅助预演不冒充正式行为，保持原schema/判线和原件；新源码须新正式run，I3-P10/I11-P10/I15-P07已发布回读，G3仍Failed |

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G3-P2-02 | [019139f第二G3](../experiments/runs/G3-P2-20261104T151546Z-1040b5/report.md) | E77 observed/pass；原四criteria与严格行为/资源完整复算通过，独立新样本 |
| ITER-013 | [综合分析](../docs/evidence/p2-g3-evidence-013.md) | C55，三单发布回读完成；不改ITER-012失败或重复计入其样本 |
| EDGE-048 | RUN-G3-P2-01 → TASK-006 D21 → RUN-G3-P2-02 → ITER-013/C55 | 反例→窄修→新验证：139源码仅辅助kind一处变化，原schema/判线保持；三单发布回读完成，G3按记录范围Passed |

当前TASK-007集成d8bb599检查点：两份G3新run及ITER-012/013、C54/C55、E76/E77、EDGE-047/048全部保留，019139f的Passed仍限定为其原记录范围。当前G2源码的G1/G3/G4适用性均Inconclusive；v2 preflight旧向量适配与PG关闭锁交接问题由TASK-007当前检查点承接，最终同源正式复证未完成。本次合并不新增、复制或改写实验样本。


## TASK-007：本地正式G1首轮失败（2026-11-05追加）

| 节点/边 | 来源或关系 | 含义与当前状态 |
| --- | --- | --- |
| RUN-G1-R5-01 | [d9b8849本地正式G1](../experiments/runs/G1-P1-R5-20261105T033158Z-5e02d0/report.md)、[固定预注册](../experiments/manifests/G1-P1-R5.json) | E78，唯一新增样本observed/fail；八步骤未超时，232源码与原15文件完整核验，执行监督complete不覆盖产品失败 |
| ITER-014 | [综合分析](../docs/evidence/p2-local-g1-first-evidence-014.md) | C56；保留metrics panic及残缺覆盖、race断言、B2控制切断输入、Load六条部分响应、G2输入及PG拒写原件；阶段Issue发布待完成 |
| EDGE-049 | TASK-007/d9b8849 → RUN-G1-R5-01 → ITER-014/C56 → 原判线下的诊断与复证 | 反例与重启条件：不改原run/判线，不以来源封口、资源数学采样或局部PASS恢复G1；先区分输入资格与未定产品根因，后续以新固定源码/预注册身份执行完整复证 |

以上检查点中的状态按其历史时点理解。当前G1 Failed、G2 Blocked、G3/G4 Inconclusive；019139f的G3历史Passed及bff7aa2的G1/G4历史Passed仍限原源码/环境。此次本地执行适配不新增产品契约，不把TCG运行视为原CI性能或G5/G6/G7证据；本阶段发布与回读由后续实际记录补齐，未提交Issue事实不预填。
